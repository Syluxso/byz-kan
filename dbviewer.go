package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var tableNameRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

type tableDataResponse struct {
	Columns []string    `json:"columns"`
	Rows    [][]*string `json:"rows"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	Size    int         `json:"size"`
}

func (a *app) handleAdminDB(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/db")
	path = strings.Trim(path, "/")
	switch {
	case path == "tables":
		a.handleAdminDBTables(w, r)
	case strings.HasPrefix(path, "tables/"):
		table := strings.TrimPrefix(path, "tables/")
		table = strings.Trim(table, "/")
		a.handleAdminDBTableData(w, r, table)
	default:
		writeProblem(w, http.StatusNotFound, "Not Found", "unknown db path")
	}
}

func (a *app) handleAdminDBTables(w http.ResponseWriter, r *http.Request) {
	tables, err := a.store.listSchemaTables(r.Context())
	if err != nil {
		log.Printf("admin db tables: %v", err)
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to list tables")
		return
	}
	writeJSON(w, http.StatusOK, tables)
}

func (a *app) handleAdminDBTableData(w http.ResponseWriter, r *http.Request, table string) {
	if !tableNameRe.MatchString(table) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid table name")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if page < 0 {
		page = 0
	}
	if size <= 0 {
		size = 100
	}
	if size > 500 {
		size = 500
	}
	data, err := a.store.getSchemaTableData(r.Context(), table, page, size)
	if err != nil {
		if err == errNotFound {
			writeProblem(w, http.StatusNotFound, "Not Found", "unknown table")
			return
		}
		log.Printf("admin db table %s: %v", table, err)
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to load table data")
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *app) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	lines := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 {
		lines = n
	}
	level := r.URL.Query().Get("level")
	writeJSON(w, http.StatusOK, a.logBuf.Tail(lines, level))
}

func (s *Store) listSchemaTables(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT table_name::text
FROM information_schema.tables
WHERE table_schema = $1 AND table_type = 'BASE TABLE'
ORDER BY 1`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) getSchemaTableData(ctx context.Context, table string, page, size int) (tableDataResponse, error) {
	valid, err := s.listSchemaTables(ctx)
	if err != nil {
		return tableDataResponse{}, err
	}
	found := false
	for _, t := range valid {
		if t == table {
			found = true
			break
		}
	}
	if !found {
		return tableDataResponse{}, errNotFound
	}

	colRows, err := s.db.QueryContext(ctx, `
SELECT column_name::text
FROM information_schema.columns
WHERE table_schema = $1 AND table_name = $2
ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return tableDataResponse{}, err
	}
	defer colRows.Close()
	columns := make([]string, 0)
	for colRows.Next() {
		var c string
		if err := colRows.Scan(&c); err != nil {
			return tableDataResponse{}, err
		}
		columns = append(columns, c)
	}
	if err := colRows.Err(); err != nil {
		return tableDataResponse{}, err
	}
	if len(columns) == 0 {
		return tableDataResponse{}, fmt.Errorf("table has no columns")
	}

	orderCol := columns[0]
	for _, c := range columns {
		if c == "created_at" {
			orderCol = c
			break
		}
		if c == "id" {
			orderCol = c
		}
	}

	quoted := fmt.Sprintf(`"%s"."%s"`, schema, table)
	orderQuoted := `"` + orderCol + `"`
	offset := page * size

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoted).Scan(&total); err != nil {
		return tableDataResponse{}, err
	}

	q := fmt.Sprintf(
		`SELECT * FROM %s ORDER BY %s DESC NULLS LAST LIMIT $1 OFFSET $2`,
		quoted, orderQuoted,
	)
	rows, err := s.db.QueryContext(ctx, q, size, offset)
	if err != nil {
		return tableDataResponse{}, err
	}
	defer rows.Close()

	rawNames, err := rows.Columns()
	if err != nil {
		return tableDataResponse{}, err
	}

	outRows := make([][]*string, 0)
	for rows.Next() {
		vals := make([]any, len(rawNames))
		ptrs := make([]any, len(rawNames))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return tableDataResponse{}, err
		}
		byName := map[string]any{}
		for i, name := range rawNames {
			byName[strings.ToLower(name)] = vals[i]
		}
		cells := make([]*string, len(columns))
		for i, col := range columns {
			v := byName[strings.ToLower(col)]
			if v == nil {
				cells[i] = nil
				continue
			}
			sv := stringifyDBValue(v)
			cells[i] = &sv
		}
		outRows = append(outRows, cells)
	}
	if err := rows.Err(); err != nil {
		return tableDataResponse{}, err
	}

	return tableDataResponse{
		Columns: columns,
		Rows:    outRows,
		Total:   total,
		Page:    page,
		Size:    size,
	}, nil
}

func stringifyDBValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
