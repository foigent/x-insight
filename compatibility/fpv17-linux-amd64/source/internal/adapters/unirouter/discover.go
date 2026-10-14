package unirouter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var gateATables = []string{"request_trace_events", "call_logs"}

type DiscoveryReport struct {
	Source         string           `json:"source"`
	SourceInstance string           `json:"source_instance"`
	Mode           string           `json:"mode"`
	GeneratedAt    string           `json:"generated_at"`
	Tables         []TableReport    `json:"tables"`
	Snapshot       *SnapshotSummary `json:"snapshot,omitempty"`
	Safety         SafetySummary    `json:"safety"`
	Warnings       []string         `json:"warnings,omitempty"`
	NextActions    []string         `json:"next_actions"`
}

type TableReport struct {
	Name       string         `json:"name"`
	Columns    []ColumnReport `json:"columns"`
	Indexes    []IndexReport  `json:"indexes,omitempty"`
	RowCount   *int64         `json:"row_count,omitempty"`
	Present    bool           `json:"present"`
	FieldNames []string       `json:"field_names,omitempty"`
}

type ColumnReport struct {
	Name     string `json:"name"`
	DataType string `json:"data_type,omitempty"`
	Nullable bool   `json:"nullable,omitempty"`
	Default  string `json:"default,omitempty"`
}

type IndexReport struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

type SnapshotSummary struct {
	SchemaVersion  string         `json:"schema_version"`
	Source         string         `json:"source"`
	SourceInstance string         `json:"source_instance"`
	RequestIDs     []string       `json:"request_ids"`
	Counts         map[string]any `json:"counts"`
}

type SafetySummary struct {
	RawPromptsIncluded   bool     `json:"raw_prompts_included"`
	RawResponsesIncluded bool     `json:"raw_responses_included"`
	APIKeysIncluded      bool     `json:"api_keys_included"`
	UnsafeKeysObserved   []string `json:"unsafe_keys_observed,omitempty"`
}

func DiscoverPostgres(dsn string, sourceInstance string, includeCounts bool) (DiscoveryReport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return DiscoveryReport{}, err
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return DiscoveryReport{}, err
	}

	report := baseReport("postgres", sourceInstance)
	for _, table := range gateATables {
		tableReport, err := discoverTable(ctx, db, table, includeCounts)
		if err != nil {
			return DiscoveryReport{}, err
		}
		report.Tables = append(report.Tables, tableReport)
		if !tableReport.Present {
			report.Warnings = append(report.Warnings, fmt.Sprintf("required Gate A table %q was not found", table))
		}
	}
	report.NextActions = defaultNextActions()
	return report, nil
}

func DiscoverSnapshot(path string, sourceInstance string) (DiscoveryReport, error) {
	data, err := readSnapshotBytes(path)
	if err != nil {
		return DiscoveryReport{}, err
	}
	return discoverSnapshotBytes(data, sourceInstance)
}

func DiscoverSnapshotFile(snapshot SnapshotFile, sourceInstance string) (DiscoveryReport, error) {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return DiscoveryReport{}, err
	}
	return discoverSnapshotBytes(data, sourceInstance)
}

func discoverSnapshotBytes(data []byte, sourceInstance string) (DiscoveryReport, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return DiscoveryReport{}, err
	}

	if sourceInstance == "unknown" {
		if v, ok := raw["source_instance"].(string); ok && v != "" {
			sourceInstance = v
		}
	}
	report := baseReport("snapshot", sourceInstance)
	report.Snapshot = &SnapshotSummary{
		SchemaVersion:  stringValue(raw["schema_version"]),
		Source:         stringValue(raw["source"]),
		SourceInstance: stringValue(raw["source_instance"]),
		RequestIDs:     stringSlice(raw["request_ids"]),
		Counts:         objectValue(raw["counts"]),
	}
	report.Safety = safetyFromSnapshot(raw)

	for _, table := range gateATables {
		rows := objectRows(raw[table])
		report.Tables = append(report.Tables, TableReport{
			Name:       table,
			Present:    len(rows) > 0,
			Columns:    columnsFromRows(rows),
			FieldNames: fieldNamesFromRows(rows),
			RowCount:   int64Ptr(int64(len(rows))),
		})
		if len(rows) == 0 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("snapshot contains no %s rows", table))
		}
	}

	if len(report.Safety.UnsafeKeysObserved) > 0 {
		report.Warnings = append(report.Warnings, "snapshot includes unsafe-looking keys; do not import until reviewed")
	}
	report.NextActions = defaultNextActions()
	return report, nil
}

func baseReport(mode string, sourceInstance string) DiscoveryReport {
	return DiscoveryReport{
		Source:         "unirouter",
		SourceInstance: sourceInstance,
		Mode:           mode,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func discoverTable(ctx context.Context, db *sql.DB, table string, includeCounts bool) (TableReport, error) {
	columns, err := tableColumns(ctx, db, table)
	if err != nil {
		return TableReport{}, err
	}
	if len(columns) == 0 {
		return TableReport{Name: table, Present: false}, nil
	}
	indexes, err := tableIndexes(ctx, db, table)
	if err != nil {
		return TableReport{}, err
	}

	report := TableReport{Name: table, Present: true, Columns: columns, Indexes: indexes}
	if includeCounts {
		count, err := tableRowCount(ctx, db, table)
		if err != nil {
			return TableReport{}, err
		}
		report.RowCount = &count
	}
	return report, nil
}

func tableColumns(ctx context.Context, db *sql.DB, table string) ([]ColumnReport, error) {
	rows, err := db.QueryContext(ctx, `
SELECT column_name, data_type, is_nullable, COALESCE(column_default, '')
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = $1
ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []ColumnReport
	for rows.Next() {
		var c ColumnReport
		var nullable string
		if err := rows.Scan(&c.Name, &c.DataType, &nullable, &c.Default); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

func tableIndexes(ctx context.Context, db *sql.DB, table string) ([]IndexReport, error) {
	rows, err := db.QueryContext(ctx, `
SELECT indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public' AND tablename = $1
ORDER BY indexname`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []IndexReport
	for rows.Next() {
		var idx IndexReport
		if err := rows.Scan(&idx.Name, &idx.Definition); err != nil {
			return nil, err
		}
		indexes = append(indexes, idx)
	}
	return indexes, rows.Err()
}

func tableRowCount(ctx context.Context, db *sql.DB, table string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", table)
	var count int64
	err := db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

func safetyFromSnapshot(raw map[string]any) SafetySummary {
	safety := objectValue(raw["safety"])
	return SafetySummary{
		RawPromptsIncluded:   boolValue(safety["raw_prompts_included"]),
		RawResponsesIncluded: boolValue(safety["raw_responses_included"]),
		APIKeysIncluded:      boolValue(safety["api_keys_included"]),
		UnsafeKeysObserved:   unsafeKeys(raw),
	}
}

func unsafeKeys(value any) []string {
	seen := map[string]struct{}{}
	walkObject("", value, func(path string, key string) {
		if isUnsafeKey(key) {
			seen[path] = struct{}{}
		}
	})
	var keys []string
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isUnsafeKey(key string) bool {
	switch strings.ToLower(key) {
	case "api_key", "authorization", "access_token", "refresh_token", "token", "secret", "password":
		return true
	case "prompt", "messages", "completion", "request_body", "response_body", "raw_prompt", "raw_response":
		return true
	default:
		return false
	}
}

func walkObject(path string, value any, visit func(path string, key string)) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			visit(childPath, key)
			walkObject(childPath, child, visit)
		}
	case []any:
		for _, child := range v {
			walkObject(path, child, visit)
		}
	}
}

func objectRows(value any) []map[string]any {
	rows, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if ok {
			out = append(out, obj)
		}
	}
	return out
}

func columnsFromRows(rows []map[string]any) []ColumnReport {
	names := fieldNamesFromRows(rows)
	columns := make([]ColumnReport, 0, len(names))
	for _, name := range names {
		columns = append(columns, ColumnReport{Name: name, DataType: "snapshot"})
	}
	return columns
}

func fieldNamesFromRows(rows []map[string]any) []string {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for key := range row {
			seen[key] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func objectValue(value any) map[string]any {
	obj, _ := value.(map[string]any)
	if obj == nil {
		return map[string]any{}
	}
	return obj
}

func stringValue(value any) string {
	v, _ := value.(string)
	return v
}

func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func int64Ptr(value int64) *int64 {
	return &value
}

func defaultNextActions() []string {
	return []string{
		"review missing or unexpected fields before writing importer mappings",
		"confirm snapshot safety before importing rows into evidence_events",
		"derive capture gaps for missing call_logs or request_trace_events rows",
		"use request_id plus created_at/id ordering for Gate A importer design",
	}
}
