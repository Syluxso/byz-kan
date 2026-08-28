package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CW-39: read-through for text attachments.
//
// Agents can list attachments and attach a fileId, but never see content.
// byz-kan does not store bytes (CW-19) and byz-files streams content itself
// rather than handing out object URLs, so there is no way for an agent to read
// a board file without a human pasting it.
//
// This reads through, it does not cache. Nothing here writes bytes to Postgres,
// to activity, or to the log.

// textReadCap is the largest attachment this tool will return. Deliberately
// small: the result travels in an MCP tool response, and a truncated document
// returned as if it were whole is worse than a refusal.
const textReadCap = 256 * 1024

// textReadTypes are the content types this tool will return.
//
// Allowlist, never a blocklist. An unknown type must fail closed: returning
// image bytes as a JSON string produces mojibake that reads like a corrupted
// file rather than a wrong request.
var textReadTypes = map[string]bool{
	"text/markdown":    true,
	"text/plain":       true,
	"text/csv":         true,
	"text/yaml":        true,
	"text/x-yaml":      true,
	"application/json": true,
	"application/xml":  true,
	"text/xml":         true,
	"application/yaml": true,
}

// textReadExts back up the content type, which is caller-asserted on the kan
// row and is often octet-stream or empty for files uploaded by a script.
var textReadExts = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".csv": true,
	".xml": true, ".yml": true, ".yaml": true,
}

type mcpAttachmentTextIn struct {
	ID     string `json:"id,omitempty" jsonschema:"Attachment UUID, from list_attachments"`
	FileID string `json:"fileId,omitempty" jsonschema:"byz-file-service file UUID; must already be attached to something you can see"`
}

// AttachmentTextOut is the tool's result. No bytes field: this is text only,
// binaries are refused rather than base64'd.
type AttachmentTextOut struct {
	AttachmentID string `json:"attachmentId"`
	FileID       string `json:"fileId"`
	Filename     string `json:"filename"`
	ContentType  string `json:"contentType"`
	SizeBytes    int64  `json:"sizeBytes"`
	Text         string `json:"text"`
	// Truncated is always false. It exists so a caller can rely on the field
	// instead of assuming, and so that if a future variant ever does truncate,
	// old callers are not silently handed a partial document.
	Truncated bool `json:"truncated"`
}

func (a *app) mcpGetAttachmentText(ctx context.Context, req *mcp.CallToolRequest, in mcpAttachmentTextIn) (*mcp.CallToolResult, any, error) {
	sc, err := a.scopeFromMCP(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	att, err := a.resolveAttachmentForRead(ctx, sc, in.ID, in.FileID)
	if err != nil {
		return nil, nil, err
	}

	name := ""
	if att.Filename != nil {
		name = *att.Filename
	}
	ctype := ""
	if att.ContentType != nil {
		ctype = *att.ContentType
	}
	if err := checkReadableAsText(name, ctype); err != nil {
		return nil, nil, err
	}

	// Refuse on the recorded size before spending the request. The row's size
	// is denormalized from byz-files at attach time, so it is trustworthy when
	// present; when absent the body read below still enforces the cap.
	if att.SizeBytes != nil && *att.SizeBytes > textReadCap {
		return nil, nil, fmt.Errorf(
			"attachment is %d bytes, over the %d byte read limit; download it instead of reading it inline",
			*att.SizeBytes, textReadCap)
	}

	body, size, err := a.fetchFileContent(ctx, att.FileID, bearerFrom(ctx), req)
	if err != nil {
		return nil, nil, err
	}

	// Attachment id, size and outcome only. The content itself is never logged.
	log.Printf("get_attachment_text id=%s size=%d status=ok", att.ID, size)

	return mcpJSON(AttachmentTextOut{
		AttachmentID: att.ID,
		FileID:       att.FileID,
		Filename:     name,
		ContentType:  ctype,
		SizeBytes:    size,
		Text:         body,
		Truncated:    false,
	})
}

// resolveAttachmentForRead finds the row by attachment id, or by fileId if that
// is all the caller has.
//
// A bare fileId is only accepted when it is attached to something inside the
// caller's org and tenant. That is the whole access check: byz-files would
// happily serve any file the caller's own token can reach, so guessing a UUID
// must not turn byz-kan into a lookup service for other tenants' files.
func (a *app) resolveAttachmentForRead(ctx context.Context, sc scope, id, fileID string) (AttachmentView, error) {
	if isUUID(id) {
		att, err := a.store.GetAttachment(ctx, sc, id)
		if err == errNotFound {
			return AttachmentView{}, fmt.Errorf("no attachment %s here", id)
		}
		return att, err
	}
	if isUUID(fileID) {
		att, err := a.store.FindAttachmentByFileID(ctx, sc, fileID)
		if err == errNotFound {
			return AttachmentView{}, fmt.Errorf(
				"fileId %s is not attached to anything you can see", fileID)
		}
		return att, err
	}
	return AttachmentView{}, fmt.Errorf("id or fileId is required")
}

// checkReadableAsText allows a file through on either its content type or its
// extension. Either is enough: a .md uploaded as octet-stream is still text,
// and a text/markdown with no filename is still text.
func checkReadableAsText(filename, contentType string) error {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 { // strip "; charset=utf-8"
		ct = strings.TrimSpace(ct[:i])
	}
	if textReadTypes[ct] {
		return nil
	}
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename))); textReadExts[ext] {
		return nil
	}
	shown := ct
	if shown == "" {
		shown = "unknown"
	}
	return fmt.Errorf(
		"%s is not a text type this tool will read; it handles md, txt, json, csv, xml and yaml only",
		shown)
}

// fetchFileContent streams the file from byz-files using the CALLER's token.
//
// No service credential and no KAN_PAT_SECRET leave this process. byz-files
// authenticates the actual actor, so a caller who could not read the file
// directly cannot read it through byz-kan either.
func (a *app) fetchFileContent(ctx context.Context, fileID, bearer string, req *mcp.CallToolRequest) (string, int64, error) {
	if a.filesURL == "" {
		return "", 0, fmt.Errorf("byz-files is not configured on this server (BYZ_FILES_URL)")
	}
	if bearer == "" && req != nil && req.Extra != nil && req.Extra.Header != nil {
		bearer = strings.TrimSpace(strings.TrimPrefix(req.Extra.Header.Get("Authorization"), "Bearer "))
	}

	url := a.filesURL + "/api/v1/files/" + fileID + "/content"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	if bearer != "" {
		hreq.Header.Set("Authorization", "Bearer "+bearer)
	}

	client := a.httpc
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return "", 0, fmt.Errorf("could not reach the file service: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// The single most likely cause, and the one that wastes the most time
		// when reported as a generic auth failure. See CW-28.
		return "", 0, fmt.Errorf(
			"the file service rejected this session's token (%d). A byz-kan personal access token is HS256 and only byz-kan can verify it; reading files needs an IAM access token. See CW-28",
			resp.StatusCode)
	case http.StatusNotFound:
		// CW-29: kan will store a fileId byz-files never had.
		return "", 0, fmt.Errorf(
			"the file service does not have %s, so this attachment points at nothing. See CW-29", fileID)
	default:
		return "", 0, fmt.Errorf("the file service returned %d", resp.StatusCode)
	}

	// One byte past the cap, so a file that lied about its size in the kan row
	// is still caught here rather than returned short.
	buf, err := io.ReadAll(io.LimitReader(resp.Body, textReadCap+1))
	if err != nil {
		return "", 0, fmt.Errorf("could not read the file: %w", err)
	}
	if int64(len(buf)) > textReadCap {
		return "", 0, fmt.Errorf(
			"file is larger than the %d byte read limit; download it instead of reading it inline", textReadCap)
	}
	return string(buf), int64(len(buf)), nil
}
