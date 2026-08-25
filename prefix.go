package main

import (
	"fmt"
	"strings"
	"unicode"
)

func normalizeKeyPrefix(raw string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 8 {
			break
		}
	}
	s := b.String()
	if len(s) < 2 {
		return "", errInvalid
	}
	return s, nil
}

func deriveKeyPrefixFromName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if r > unicode.MaxASCII {
				continue
			}
			b.WriteRune(r)
		}
		if b.Len() >= 4 {
			break
		}
	}
	s := b.String()
	if len(s) < 2 {
		return "BOARD"
	}
	return s
}

func nextPrefixCandidate(base string, n int) (string, error) {
	if n <= 1 {
		if len(base) < 2 || len(base) > 16 {
			return "", errInvalid
		}
		return base, nil
	}
	c := fmt.Sprintf("%s%d", base, n)
	if len(c) > 16 {
		return "", errConflict
	}
	return c, nil
}

func ticketKey(prefix string, number int) string {
	return strings.ToUpper(prefix) + "-" + fmt.Sprintf("%d", number)
}

func isUUID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

func wantIncludeDeleted(rQuery string) bool {
	v := strings.TrimSpace(strings.ToLower(rQuery))
	return v == "1" || v == "true" || v == "yes"
}
