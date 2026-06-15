/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: SyncDB
 *
 * Per-bib-file SQLite snapshot store for bidirectional sync (.sync file).
 * Records the exact state (field values, group memberships, PDF md5) of each
 * entry as it was written to the bib at the last successful sync. This common-
 * ancestor snapshot enables field-level three-way merge in subsequent syncs.
 *
 * Isolation strategy: home copy lives next to the bib file; a working copy is
 * kept in cache_folder during the sync session. On open, the home copy is
 * copied to cache (no recovery prompt on stale working copy — unlike the main
 * DB, an interrupted sync's partial working copy is never meaningful). On
 * close, the working copy is copied back to the home path.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 15.06.2026
 *
 */

package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TSyncEntry holds the last-synced state for one entry.
type TSyncEntry struct {
	CanonicalKey string
	OutputKey    string
	Fields       map[string]string // all fields as written to bib (excl. noise)
	Groups       TStringSet        // all group assignments for this entry in the bib (managed + local)
	PDFMd5       string            // MD5 of local PDF copy (pdf_files="local"; "" otherwise)
	DBHash       string            // subsetDBFingerprint at last sync (for db-changed detection)
	BibHash      string            // subsetBibFingerprint of the entry as written (for bib-changed detection)
	SyncTime     int64             // Unix timestamp of last sync
}

// TSyncState holds the full in-memory sync snapshot for one bib file.
type TSyncState struct {
	homePath    string
	workingPath string
	isolated    bool // whether home ≠ working (cache_folder active)
	db          *sql.DB
	entries     map[string]*TSyncEntry
	statuses    map[string]string // source_key → status ("waived", "ignored", …)
	modified    bool
}

// Sync status constants used across modes.
const (
	SyncStatusWaived  = "waived"  // harvest: never present this entry again
	SyncStatusIgnored = "ignored" // weave: write verbatim, do not merge
)

// syncWorkingPath returns the working (cache) path for a given home sync path.
func syncWorkingPath(homePath string) string {
	if cacheFolder == "" {
		return homePath
	}
	return filepath.Join(cacheFolder, filepath.Base(homePath))
}

// openSyncState opens (or creates) the .sync SQLite DB for the bib at
// keysBasePath. It copies the home file to cache_folder if isolation is
// active, creates the schema if needed, and bulk-loads all tables into memory.
// Returns nil when the open fails (caller should treat as empty state).
func openSyncState(keysBasePath string) *TSyncState {
	homePath := keysBasePath + SyncDbExtension
	workingPath := syncWorkingPath(homePath)
	isolated := workingPath != homePath

	if isolated {
		if err := os.MkdirAll(filepath.Dir(workingPath), 0o755); err != nil {
			dbInteraction.Warning("sync: cannot create cache dir %s: %s", filepath.Dir(workingPath), err)
			return nil
		}
		if FileExists(homePath) {
			// Always overwrite working copy from home — no recovery prompt.
			if err := copyFile(homePath, workingPath); err != nil {
				dbInteraction.Warning("sync: cannot copy %s to cache: %s", homePath, err)
				return nil
			}
		}
	}

	openPath := workingPath
	conn, err := sql.Open(sqliteDatabaseDriver, openPath)
	if err != nil {
		dbInteraction.Warning("sync: cannot open %s: %s", openPath, err)
		return nil
	}

	s := &TSyncState{
		homePath:    homePath,
		workingPath: workingPath,
		isolated:    isolated,
		db:          conn,
		entries:     make(map[string]*TSyncEntry),
	}
	if !s.ensureSchema() {
		conn.Close()
		return nil
	}
	s.loadAll()
	return s
}

// close flushes the in-memory state to the working SQLite DB, then copies it
// back to the home path if isolation is active.
func (s *TSyncState) close() {
	if s == nil || s.db == nil {
		return
	}
	if s.modified {
		if err := s.flush(); err != nil {
			dbInteraction.Warning("sync: flush failed: %s", err)
		}
	}
	s.db.Close()
	s.db = nil
	if s.isolated && s.modified {
		if err := copyFile(s.workingPath, s.homePath); err != nil {
			dbInteraction.Warning("sync: cannot copy working sync DB back to %s: %s", s.homePath, err)
		}
	}
}

// get returns the snapshot for canonicalKey, or nil if absent.
func (s *TSyncState) get(canonicalKey string) *TSyncEntry {
	if s == nil {
		return nil
	}
	return s.entries[canonicalKey]
}

// set records a snapshot entry (replaces any existing entry for the same key).
func (s *TSyncState) set(e TSyncEntry) {
	if s == nil {
		return
	}
	copy := e
	s.entries[e.CanonicalKey] = &copy
	s.modified = true
}

// delete removes the snapshot for canonicalKey.
func (s *TSyncState) delete(canonicalKey string) {
	if s == nil {
		return
	}
	if _, ok := s.entries[canonicalKey]; ok {
		delete(s.entries, canonicalKey)
		s.modified = true
	}
}

// keys returns all canonical keys in the snapshot, sorted.
func (s *TSyncState) keys() []string {
	if s == nil {
		return nil
	}
	keys := make([]string, 0, len(s.entries))
	for k := range s.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// contains reports whether canonicalKey has a snapshot entry.
func (s *TSyncState) contains(canonicalKey string) bool {
	if s == nil {
		return false
	}
	_, ok := s.entries[canonicalKey]
	return ok
}

// --- schema ---

const syncSchemaSQL = `
CREATE TABLE IF NOT EXISTS sync_manifest (
    canonical_key TEXT NOT NULL PRIMARY KEY,
    output_key    TEXT NOT NULL,
    db_hash       TEXT NOT NULL DEFAULT '',
    bib_hash      TEXT NOT NULL DEFAULT '',
    sync_time     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sync_entries (
    canonical_key TEXT NOT NULL,
    field         TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (canonical_key, field)
);
CREATE TABLE IF NOT EXISTS sync_groups (
    canonical_key TEXT NOT NULL,
    group_name    TEXT NOT NULL,
    PRIMARY KEY (canonical_key, group_name)
);
CREATE TABLE IF NOT EXISTS sync_pdfs (
    canonical_key TEXT NOT NULL PRIMARY KEY,
    pdf_md5       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sync_status (
    source_key TEXT NOT NULL PRIMARY KEY,
    status     TEXT NOT NULL,
    set_time   INTEGER NOT NULL DEFAULT 0
);
`

func (s *TSyncState) ensureSchema() bool {
	if _, err := s.db.Exec(syncSchemaSQL); err != nil {
		dbInteraction.Warning("sync: schema creation failed: %s", err)
		return false
	}
	return true
}

// --- bulk load ---

func (s *TSyncState) loadAll() {
	// Load manifest (canonical_key → output_key, sync_time).
	manifest := map[string]*TSyncEntry{}
	rows, err := s.db.Query(`SELECT canonical_key, output_key, db_hash, bib_hash, sync_time FROM sync_manifest`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e TSyncEntry
			rows.Scan(&e.CanonicalKey, &e.OutputKey, &e.DBHash, &e.BibHash, &e.SyncTime)
			e.Fields = make(map[string]string)
			e.Groups = TStringSetNew()
			manifest[e.CanonicalKey] = &e
		}
	}

	// Load field values.
	rows2, err := s.db.Query(`SELECT canonical_key, field, value FROM sync_entries`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var key, field, value string
			rows2.Scan(&key, &field, &value)
			if e, ok := manifest[key]; ok {
				e.Fields[field] = value
			}
		}
	}

	// Load group memberships.
	rows3, err := s.db.Query(`SELECT canonical_key, group_name FROM sync_groups`)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var key, group string
			rows3.Scan(&key, &group)
			if e, ok := manifest[key]; ok {
				e.Groups.Add(group)
			}
		}
	}

	// Load PDF md5s.
	rows4, err := s.db.Query(`SELECT canonical_key, pdf_md5 FROM sync_pdfs`)
	if err == nil {
		defer rows4.Close()
		for rows4.Next() {
			var key, md5 string
			rows4.Scan(&key, &md5)
			if e, ok := manifest[key]; ok {
				e.PDFMd5 = md5
			}
		}
	}

	s.entries = make(map[string]*TSyncEntry, len(manifest))
	for k, e := range manifest {
		s.entries[k] = e
	}

	// Load per-source-key status flags.
	s.statuses = make(map[string]string)
	rows5, err := s.db.Query(`SELECT source_key, status FROM sync_status`)
	if err == nil {
		defer rows5.Close()
		for rows5.Next() {
			var key, status string
			rows5.Scan(&key, &status)
			if key != "" && status != "" {
				s.statuses[key] = status
			}
		}
	}
}

// --- flush ---

// flush writes the full in-memory state to the SQLite DB, replacing all tables.
func (s *TSyncState) flush() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	// Clear all tables and rewrite from memory.
	for _, tbl := range []string{"sync_manifest", "sync_entries", "sync_groups", "sync_pdfs", "sync_status"} {
		if _, err := tx.Exec("DELETE FROM " + tbl); err != nil {
			tx.Rollback()
			return err
		}
	}

	now := time.Now().Unix()
	for _, e := range s.entries {
		syncTime := e.SyncTime
		if syncTime == 0 {
			syncTime = now
		}
		if _, err := tx.Exec(
			`INSERT INTO sync_manifest (canonical_key, output_key, db_hash, bib_hash, sync_time) VALUES (?, ?, ?, ?, ?)`,
			e.CanonicalKey, e.OutputKey, e.DBHash, e.BibHash, syncTime,
		); err != nil {
			tx.Rollback()
			return err
		}
		for field, value := range e.Fields {
			if value == "" {
				continue
			}
			if _, err := tx.Exec(
				`INSERT INTO sync_entries (canonical_key, field, value) VALUES (?, ?, ?)`,
				e.CanonicalKey, field, value,
			); err != nil {
				tx.Rollback()
				return err
			}
		}
		for group := range e.Groups.Elements() {
			if _, err := tx.Exec(
				`INSERT INTO sync_groups (canonical_key, group_name) VALUES (?, ?)`,
				e.CanonicalKey, group,
			); err != nil {
				tx.Rollback()
				return err
			}
		}
		if e.PDFMd5 != "" {
			if _, err := tx.Exec(
				`INSERT INTO sync_pdfs (canonical_key, pdf_md5) VALUES (?, ?)`,
				e.CanonicalKey, e.PDFMd5,
			); err != nil {
				tx.Rollback()
				return err
			}
		}
	}

	for sourceKey, status := range s.statuses {
		if _, err := tx.Exec(
			`INSERT INTO sync_status (source_key, status, set_time) VALUES (?, ?, ?)`,
			sourceKey, status, now,
		); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// GetStatus returns the sync status for sourceKey ("waived", "ignored", etc.) or "".
func (s *TSyncState) GetStatus(sourceKey string) string {
	if s == nil {
		return ""
	}
	return s.statuses[sourceKey]
}

// SetStatus sets or clears the sync status for sourceKey.
// An empty status string removes the entry.
func (s *TSyncState) SetStatus(sourceKey, status string) {
	if s == nil {
		return
	}
	if status == "" {
		delete(s.statuses, sourceKey)
	} else {
		s.statuses[sourceKey] = status
	}
	s.modified = true
}

// DoSetSyncStatus opens the .sync DB at stemPath and sets (or clears) the status
// for sourceKey. Called directly from the CLI — no main library needed.
func DoSetSyncStatus(status, sourceKey, stemPath string) {
	syncPath, _ := resolveSyncPath(stemPath)
	conn, err := openSyncDirect(syncPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "set_sync_status: cannot open %s: %s\n", syncPath, err)
		return
	}
	defer conn.Close()

	if status == "" {
		if _, err := conn.Exec(`DELETE FROM sync_status WHERE source_key = ?`, sourceKey); err != nil {
			fmt.Fprintf(os.Stderr, "set_sync_status: delete failed: %s\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "Cleared status for %s in %s\n", sourceKey, syncPath)
	} else {
		if _, err := conn.Exec(
			`INSERT INTO sync_status (source_key, status, set_time) VALUES (?, ?, ?)
			 ON CONFLICT(source_key) DO UPDATE SET status=excluded.status, set_time=excluded.set_time`,
			sourceKey, status, time.Now().Unix(),
		); err != nil {
			fmt.Fprintf(os.Stderr, "set_sync_status: upsert failed: %s\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "Set status %q for %s in %s\n", status, sourceKey, syncPath)
	}
}

// --- export / import ---

// syncTableNames maps the short names accepted on the CLI to the actual SQL table names.
var syncTableNames = map[string]string{
	"manifest": "sync_manifest",
	"entries":  "sync_entries",
	"groups":   "sync_groups",
	"pdfs":     "sync_pdfs",
}

// syncTableColumns defines the column list for each table (in export/import order).
var syncTableColumns = map[string][]string{
	"sync_manifest": {"canonical_key", "output_key", "db_hash", "bib_hash", "sync_time"},
	"sync_entries":  {"canonical_key", "field", "value"},
	"sync_groups":   {"canonical_key", "group_name"},
	"sync_pdfs":     {"canonical_key", "pdf_md5"},
}

// resolveSyncPath returns (syncFilePath, tablesDir) from a stem (no extension) or
// a full path including .sync.
func resolveSyncPath(stem string) (syncPath, tablesDir string) {
	stem = strings.TrimSuffix(stem, SyncDbExtension)
	return stem + SyncDbExtension, stem + ".tables"
}

// openSyncDirect opens the .sync SQLite at path for direct read/write (no cache
// isolation — for manual export/import operations only).
func openSyncDirect(syncPath string) (*sql.DB, error) {
	conn, err := sql.Open(sqliteDatabaseDriver, syncPath)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(syncSchemaSQL); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// DoExportSync exports the named sync tables (or "all") from stemPath.sync to
// stemPath.tables/<table>.csv. Called from the -export_sync CLI handler.
func DoExportSync(tableSpec, stemPath string) {
	syncPath, tablesDir := resolveSyncPath(stemPath)
	if !FileExists(syncPath) {
		fmt.Fprintf(os.Stderr, "export_sync: %s not found\n", syncPath)
		return
	}
	conn, err := openSyncDirect(syncPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export_sync: cannot open %s: %s\n", syncPath, err)
		return
	}
	defer conn.Close()

	if err := os.MkdirAll(tablesDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "export_sync: cannot create %s: %s\n", tablesDir, err)
		return
	}

	tables := resolveSyncTableSpec(tableSpec)
	for _, tbl := range tables {
		csvPath := filepath.Join(tablesDir, tbl+".csv")
		if err := exportSyncTableToCSV(conn, tbl, csvPath); err != nil {
			fmt.Fprintf(os.Stderr, "export_sync: %s: %s\n", tbl, err)
		} else {
			fmt.Fprintf(os.Stderr, "Exported %s → %s\n", tbl, csvPath)
		}
	}
}

// DoImportSync imports the named sync tables (or "all") from stemPath.tables/<table>.csv
// into stemPath.sync. Called from the -import_sync CLI handler.
func DoImportSync(tableSpec, stemPath string) {
	syncPath, tablesDir := resolveSyncPath(stemPath)
	conn, err := openSyncDirect(syncPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import_sync: cannot open %s: %s\n", syncPath, err)
		return
	}
	defer conn.Close()

	tables := resolveSyncTableSpec(tableSpec)
	for _, tbl := range tables {
		csvPath := filepath.Join(tablesDir, tbl+".csv")
		if !FileExists(csvPath) {
			fmt.Fprintf(os.Stderr, "import_sync: %s not found — skipped\n", csvPath)
			continue
		}
		if err := importSyncTableFromCSV(conn, tbl, csvPath); err != nil {
			fmt.Fprintf(os.Stderr, "import_sync: %s: %s\n", tbl, err)
		} else {
			fmt.Fprintf(os.Stderr, "Imported %s ← %s\n", tbl, csvPath)
		}
	}
}

func resolveSyncTableSpec(spec string) []string {
	if spec == "all" {
		return []string{"sync_manifest", "sync_entries", "sync_groups", "sync_pdfs"}
	}
	short := strings.ToLower(strings.TrimSpace(spec))
	if full, ok := syncTableNames[short]; ok {
		return []string{full}
	}
	// Accept full names (sync_manifest etc.) directly.
	if _, ok := syncTableColumns[short]; ok {
		return []string{short}
	}
	fmt.Fprintf(os.Stderr, "export/import_sync: unknown table %q (valid: manifest, entries, groups, pdfs, all)\n", spec)
	return nil
}

func exportSyncTableToCSV(conn *sql.DB, tbl, csvPath string) error {
	cols, ok := syncTableColumns[tbl]
	if !ok {
		return fmt.Errorf("unknown table %q", tbl)
	}
	rows, err := conn.Query(fmt.Sprintf("SELECT %s FROM %s ORDER BY 1, 2", strings.Join(cols, ", "), tbl))
	if err != nil {
		return err
	}
	defer rows.Close()

	f, err := os.Create(csvPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "%s\n", strings.Join(cols, ";"))
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		parts := make([]string, len(cols))
		for i, v := range vals {
			switch t := v.(type) {
			case string:
				parts[i] = t
			case int64:
				parts[i] = fmt.Sprintf("%d", t)
			case []byte:
				parts[i] = string(t)
			default:
				parts[i] = fmt.Sprintf("%v", t)
			}
		}
		fmt.Fprintf(f, "%s\n", strings.Join(parts, ";"))
	}
	return rows.Err()
}

func importSyncTableFromCSV(conn *sql.DB, tbl, csvPath string) error {
	cols, ok := syncTableColumns[tbl]
	if !ok {
		return fmt.Errorf("unknown table %q", tbl)
	}
	data, err := os.ReadFile(csvPath)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) < 1 {
		return nil
	}
	// Skip header line.
	lines = lines[1:]

	tx, err := conn.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM " + tbl); err != nil {
		tx.Rollback()
		return err
	}
	placeholders := strings.Repeat("?,", len(cols))
	placeholders = placeholders[:len(placeholders)-1]
	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", tbl, strings.Join(cols, ","), placeholders))
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ";", len(cols))
		for len(parts) < len(cols) {
			parts = append(parts, "")
		}
		args := make([]any, len(cols))
		for i, p := range parts {
			args[i] = p
		}
		if _, err := stmt.Exec(args...); err != nil {
			tx.Rollback()
			return fmt.Errorf("inserting row %q: %w", line, err)
		}
	}
	return tx.Commit()
}
