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
	"os"
	"path/filepath"
	"sort"
	"time"
)

// TSyncEntry holds the last-synced state for one entry.
type TSyncEntry struct {
	CanonicalKey string
	OutputKey    string
	Fields       map[string]string // all fields as written to bib (excl. noise)
	Groups       TStringSet        // cfg.SyncGroups groups as written to bib
	PDFMd5       string            // MD5 of local PDF copy (pdf_files="local"; "" otherwise)
	DBHash       string            // subsetDBFingerprint at last sync (for db-changed detection)
	SyncTime     int64             // Unix timestamp of last sync
}

// TSyncState holds the full in-memory sync snapshot for one bib file.
type TSyncState struct {
	homePath    string
	workingPath string
	isolated    bool // whether home ≠ working (cache_folder active)
	db          *sql.DB
	entries     map[string]*TSyncEntry
	modified    bool
}

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
`

func (s *TSyncState) ensureSchema() bool {
	_, err := s.db.Exec(syncSchemaSQL)
	if err != nil {
		dbInteraction.Warning("sync: schema creation failed: %s", err)
		return false
	}
	return true
}

// --- bulk load ---

func (s *TSyncState) loadAll() {
	// Load manifest (canonical_key → output_key, sync_time).
	manifest := map[string]*TSyncEntry{}
	rows, err := s.db.Query(`SELECT canonical_key, output_key, db_hash, sync_time FROM sync_manifest`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var e TSyncEntry
			rows.Scan(&e.CanonicalKey, &e.OutputKey, &e.DBHash, &e.SyncTime)
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
}

// --- flush ---

// flush writes the full in-memory state to the SQLite DB, replacing all tables.
func (s *TSyncState) flush() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	// Clear all tables and rewrite from memory.
	for _, tbl := range []string{"sync_manifest", "sync_entries", "sync_groups", "sync_pdfs"} {
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
			`INSERT INTO sync_manifest (canonical_key, output_key, db_hash, sync_time) VALUES (?, ?, ?, ?)`,
			e.CanonicalKey, e.OutputKey, e.DBHash, syncTime,
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

	return tx.Commit()
}
