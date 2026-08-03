/*
 *
 * Module:    bibtex_check
 * Component:
 * - bibtex_library
 *   - bibtex_library_db
 *
 * Relational data layer for the BibTeX library. Each logical data source
 * (name mappings, key hints, …) follows the same three-function pattern:
 *   import*FromCSV()       — import a table from a CSV (only via -import)
 *   load<X>FromDb(l)        — populate in-memory library fields from DB
 *   save<X>ToDb(l)          — write in-memory library fields back to DB
 *
 * This is the lowest of three layers, and the only one concerned with what is
 * actually stored: raw SQL CRUD, schema, and table lifecycle. It does not manage
 * consistency between a map and its inverse — that's bibtex_library_maps.go's job,
 * which calls down into this file's functions (e.g. upsertNameMapping) to persist
 * whatever it has already kept internally consistent. bibtex_library.go sits above
 * both: the business rules that decide what a mapping *should* be (e.g. cycle
 * detection, absorbing one contributor's aliases into another) call into
 * bibtex_library_maps.go rather than touching a map or the DB directly.
 *
 * SQLite infrastructure (connection, WAL, isolation, safe-parse) is in
 * bibtex_library_db_sqlite.go.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 04.05.2026
 *
 */

package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

var (
	dbInteraction             TInteraction
	db                        *sql.DB
	entryCache                map[string]*TBibTeXEntry
	entrySnapshots            map[string]map[string]string
	bibEntriesModified        bool
	c2TrackingActive          bool
	c2EntryModified           bool
	c2ChildrenChecked         int
	entryModTrackingActive    bool
	entryModified             bool
	dbWriteSessionActive      bool
	dbWriteFailed             bool   // set when any end-of-run DB write fails; blocks finaliseWorkingDatabase
	changesAtSessionOpen      int64  // total_changes() right after markWriteSessionOpen; used to detect zero-write sessions
	homeDbSizeAtOpen          int64  // size of the home DB as stat'd in prepareWorkingDatabase; safeguards against overwriting home with a shrunken working copy
	fieldMappingsLoading      bool   // suppresses DB write-through in AddGenericFieldAlias/AddFieldMapping during initial load
	entryFieldMappingsLoading bool   // suppresses DB write-through in AddEntryFieldAlias during initial load
	contributorsLoading       bool   // suppresses DB write-through while loading contributors table
	contributorRolesActive    bool   // true once contributor_roles is populated; blocks author/editor writes to bib_entries
	defaultLangID             string // langid value treated as default; entries with this value have the field removed
	preCloseHook              func() // called inside finaliseWorkingDatabase while DB is still open; set by openLibraryToUpdate
)

// dbExecSave executes a DB statement during the end-of-run save phase. On error it
// logs msg and sets dbWriteFailed so postCheckGate blocks the home-DB copy. A
// "FOREIGN KEY constraint failed" error additionally runs PRAGMA foreign_key_check
// on the same, still-open connection — the only place the live (possibly still
// mid-transaction, uncommitted) violating row is visible, since a failure this deep
// in a run always blocks the write-back and a post-mortem read of the home DB only
// ever sees the pre-run state.
func dbExecSave(msg, query string, args ...any) {
	if err := bibExec(query, args...); err != nil {
		dbInteraction.Warning("%s: %s (args: %v)", msg, err, args)
		dbWriteFailed = true
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			logForeignKeyCheck(msg)
		}
	}
}

// logForeignKeyCheck runs PRAGMA foreign_key_check (no table argument — checks every
// table) and warns once per violating row: table, rowid, the parent table it fails
// to reference, and the failing FK's index within that table's FK list.
func logForeignKeyCheck(context string) {
	// Must run on the same handle bibExec used (activeTx during a transaction, db
	// otherwise) — database/sql gives a plain db.Query() no visibility into an open
	// transaction's uncommitted state, so querying the wrong handle here silently
	// checks the pre-transaction snapshot instead of the live, in-progress one and
	// reports "no violations" even when the failing statement's own writes (already
	// rolled back by SQLite before returning the error) are exactly the violation.
	var rows *sql.Rows
	var err error
	if activeTx != nil {
		rows, err = activeTx.Query(`PRAGMA foreign_key_check`)
	} else {
		rows, err = db.Query(`PRAGMA foreign_key_check`)
	}
	if err != nil {
		dbInteraction.Warning("%s: could not run foreign_key_check: %s", context, err)
		return
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var table, parent string
		var rowid, fkid any
		if rows.Scan(&table, &rowid, &parent, &fkid) != nil {
			continue
		}
		found = true
		dbInteraction.Warning("%s: foreign_key_check: table=%s rowid=%v references missing row in parent=%s (fkid=%v)",
			context, table, rowid, parent, fkid)
	}
	if !found {
		dbInteraction.Warning("%s: foreign_key_check found no violations (constraint failure was likely transient, mid-transaction)", context)
	}
}

// --- entry cache ---

// initEntryCache loads the entire bib_entries table into memory when there is
// sufficient available RAM (estimated headroom > 2× the projected cache size).
// If the machine is too constrained, entryCache stays nil and callers fall back
// to per-query DB reads.
func initEntryCache() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	count := int64(countBibEntries())
	const bytesPerEntry = 2048
	estimatedBytes := count * bytesPerEntry
	totalRAM := int64(systemTotalRAM())
	if totalRAM > 0 && estimatedBytes > (totalRAM-int64(ms.Sys))/2 {
		return
	}

	rows, err := db.Query(`SELECT entry_key, field, value FROM bib_entries`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for cache: %s", err)
		return
	}
	defer rows.Close()

	ticker := dbInteraction.NewProgressTicker(ProgressLoadingEntryCache, int(count))
	cache := map[string]*TBibTeXEntry{}
	for rows.Next() {
		var key, field, value string
		if err := rows.Scan(&key, &field, &value); err != nil {
			ticker.Done()
			dbInteraction.Warning("Could not scan bib_entries for cache: %s", err)
			return
		}
		e, ok := cache[key]
		if !ok {
			e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
			cache[key] = e
			ticker.Step()
		}
		e.Fields[field] = value
	}
	ticker.Done()

	// When contributor_roles is populated, reconstruct author/editor fields.
	// Rows arrive in (entry_key, role, position) order so appending gives the
	// correct " and "-joined string. contributor_roles REPLACES any raw value
	// left in bib_entries (garbled fields stay in bib_entries and are used as-is
	// only when contributor_roles has no rows for that entry+role).
	if contributorRolesActive {
		// First pass: collect which (entry_key, role) pairs have contributor_roles
		// data. We clear those fields from the bib_entries-derived cache so the
		// reconstruction starts from scratch, not concatenated onto the raw value.
		seen := map[string]map[string]bool{}
		roleRows, rErr := db.Query(
			`SELECT entry_key, role, contributor_id
			 FROM contributor_roles ORDER BY entry_key, role, position`)
		if rErr == nil {
			for roleRows.Next() {
				var key, role, id string
				if roleRows.Scan(&key, &role, &id) != nil {
					continue
				}
				contrib, ok := Library.ContributorByID[id]
				if !ok {
					continue
				}
				e, ok := cache[key]
				if !ok {
					e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
					cache[key] = e
				}
				// On the first contributor for this (key, role), clear any raw
				// bib_entries value so we don't accidentally concatenate onto it.
				if seen[key] == nil {
					seen[key] = map[string]bool{}
				}
				if !seen[key][role] {
					e.Fields[role] = ""
					seen[key][role] = true
				}
				if e.Fields[role] == "" {
					e.Fields[role] = contrib.Name
				} else {
					e.Fields[role] += " and " + contrib.Name
				}
			}
			roleRows.Close()
		}
	}

	entryCache = cache
}

// --- config table ---

func ensureConfigTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS config (
		  key   TEXT PRIMARY KEY,
		  value TEXT NOT NULL
		);`)
}

// GetConfig returns the value for key from the config table, or defaultValue if absent.
func GetConfig(key, defaultValue string) string {
	var val string
	if err := db.QueryRow(`SELECT value FROM config WHERE key = ?`, key).Scan(&val); err != nil {
		return defaultValue
	}
	return val
}

// SetConfig writes key → value into the config table.
func SetConfig(key, value string) {
	db.Exec(`INSERT INTO config (key, value) VALUES (?, ?)
	           ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
}

// maybeBootstrapConfigFromFile copies non-bootstrap settings from the in-memory config
// (loaded from the .folders file or migrated from the legacy .config file) into the DB
// config table on first run. Bootstrap keys (cache_folder, global_folder) remain in the
// .folders file only and are never written to the DB.
func maybeBootstrapConfigFromFile() {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM config`).Scan(&count)
	if count > 0 {
		return
	}
	if keyPrefix != "" {
		SetConfig("key_prefix", keyPrefix)
	}
	if csvDelimiter != "" {
		SetConfig("csv_delimiter", csvDelimiter)
	}
	if backupFolder != "" {
		SetConfig("backup_folder", backupFolder)
	}
	SetConfig("version", AppVersion)
}

// loadBibTeXSettings reads non-bootstrap settings from the DB config table and
// overrides the in-memory vars set by loadBibTeXFolders. Called after the DB is
// open and after maybeBootstrapConfigFromFile has run.
func loadBibTeXSettings() {
	if v := GetConfig("key_prefix", ""); v != "" {
		keyPrefix = v
	} else if keyPrefix != "" {
		// Migrated from legacy .folders: persist to DB now.
		SetConfig("key_prefix", keyPrefix)
	} else {
		// No key_prefix in DB config or legacy .folders. The caller (connectToDatabase /
		// prepareWorkingDatabase) is responsible for not calling loadBibTeXSettings at all
		// when the working DB has not yet been populated from home — by the time we get
		// here, the config table is assumed to genuinely lack a key_prefix.
		keyPrefix = promptKeyPrefix()
		SetConfig("key_prefix", keyPrefix)
	}
	if v := GetConfig("csv_delimiter", ""); v != "" {
		csvDelimiter = v
	}
	// .folders takes precedence; only read from DB when .folders did not set it.
	if backupFolder == "" {
		if v := GetConfig("backup_folder", ""); v != "" {
			backupFolder = expandHome(v)
			if !strings.HasSuffix(backupFolder, "/") {
				backupFolder += "/"
			}
		}
	}
	defaultLangID = GetConfig(DefaultLangIDConfigKey, DefaultLangIDFallback)
	SetConfig("version", AppVersion)
}

// configExchangePath returns the path of the config exchange CSV file.
func configExchangePath() string {
	return bibTeXFolder + bibTeXBaseName + tablesFolderSuffix + "/config.csv"
}

// ExportConfig writes the user-settable config table entries to the exchange CSV.
func ExportConfig() {
	path := configExchangePath()
	if err := os.MkdirAll(bibTeXFolder+bibTeXBaseName+tablesFolderSuffix, 0o755); err != nil {
		dbInteraction.Warning("Could not create exchange folder: %s", err)
		return
	}
	rows, err := db.Query(`SELECT key, value FROM config WHERE key != 'version' ORDER BY key`)
	if err != nil {
		dbInteraction.Warning("Could not read config table: %s", err)
		return
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var key, value string
		if rows.Scan(&key, &value) == nil {
			lines = append(lines, csvLine(key, value))
		}
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		dbInteraction.Warning("Could not write %s: %s", path, err)
		return
	}
	dbInteraction.Progress("Exported %d config entries to %s", len(lines), path)
}

// importConfigFromCSV reads key;value pairs from the exchange CSV. When replace is
// true, all existing non-version rows are deleted first (import); when false, existing
// rows are upserted (add).
func importConfigFromCSV(replace bool) {
	path := configExchangePath()
	count := 0
	processCSVFile(path, func(fields []string) {
		if len(fields) < 2 {
			return
		}
		key, value := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if key == "" || key == "version" {
			return
		}
		count++
		_ = value // validated below
	})
	if count == 0 {
		dbInteraction.Warning("No valid config entries found in %s", path)
		return
	}
	if replace {
		db.Exec(`DELETE FROM config WHERE key != 'version'`)
	}
	processCSVFile(path, func(fields []string) {
		if len(fields) < 2 {
			return
		}
		key, value := strings.TrimSpace(fields[0]), strings.TrimSpace(fields[1])
		if key == "" || key == "version" {
			return
		}
		SetConfig(key, value)
	})
	verb := "Imported"
	if !replace {
		verb = "Added"
	}
	dbInteraction.Progress("%s %d config entries from %s", verb, count, path)
}

// --- table_modification_times table ---

func ensureTableDatesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS table_modification_times (
		  table_name        TEXT    PRIMARY KEY,
		  modification_time INT     NOT NULL DEFAULT 0,
		  dirty             INT     NOT NULL DEFAULT 0,
		  last_written_time INT     NOT NULL DEFAULT 0
		);`)
	// Migrate existing databases that have only the original single column.
	db.Exec(`ALTER TABLE table_modification_times ADD COLUMN dirty INT NOT NULL DEFAULT 0`)
	db.Exec(`ALTER TABLE table_modification_times ADD COLUMN last_written_time INT NOT NULL DEFAULT 0`)
}

func setTableDate(tableName string, date int64) {
	err := bibExec(`
		INSERT INTO table_modification_times (table_name, modification_time)
		  VALUES (?, ?)
		  ON CONFLICT(table_name)
		    DO UPDATE SET modification_time = excluded.modification_time;`,
		tableName, date)
	if err != nil {
		dbInteraction.Error("Updating table modification date failed: %s", err)
	}
}

func tableModTime(tableName string) int64 {
	var date int64
	if err := bibQueryRow(
		`SELECT modification_time FROM table_modification_times WHERE table_name = ?`,
		tableName).Scan(&date); err != nil {
		date = 0
	}
	return date
}

func setTableDirty(tableName string) {
	err := bibExec(`
		INSERT INTO table_modification_times (table_name, modification_time, dirty)
		  VALUES (?, 0, 1)
		  ON CONFLICT(table_name) DO UPDATE SET dirty = 1;`,
		tableName)
	if err != nil {
		dbInteraction.Error("Setting dirty bit for %s failed: %s", tableName, err)
	}
}

func clearTableDirty(tableName string) {
	if err := bibExec(
		`UPDATE table_modification_times SET dirty = 0 WHERE table_name = ?`, tableName); err != nil {
		dbInteraction.Error("Clearing dirty bit for %s failed: %s", tableName, err)
	}
}

func isTableDirty(tableName string) bool {
	var dirty int
	if err := bibQueryRow(
		`SELECT dirty FROM table_modification_times WHERE table_name = ?`, tableName).Scan(&dirty); err != nil {
		return false
	}
	return dirty != 0
}

// setTableLastWritten records that tableName was just exported. It stores the
// current modification_time as last_written_time so the DB reflects what was in
// the CSV at the time of export (matching what writeExportMdate writes to .mdate).
func setTableLastWritten(tableName string) {
	modTime := tableModTime(tableName)
	err := bibExec(`
		INSERT INTO table_modification_times (table_name, modification_time, last_written_time)
		  VALUES (?, 0, ?)
		  ON CONFLICT(table_name) DO UPDATE SET last_written_time = excluded.last_written_time;`,
		tableName, modTime)
	if err != nil {
		dbInteraction.Error("Setting last_written_time for %s failed: %s", tableName, err)
	}
}

func tryCreateTableIfNeeded(command string) {
	if _, err := db.Exec(command); err != nil {
		dbInteraction.Error("Could not ensure existence of table: %s", err)
	}
}

// --- Mapping table load order ---
//
// All mapping tables are loaded from the DB (the primary source). CSV files in
// .tables/ are only written via -export and only read via -import.
//
// Load order in loadMappingFiles() / ReadAddressMappings():
//
//  1. Address tables (state_names, state_countries, country_names):
//     Bootstrap from built-in defaults when empty; feed address normalisation.
//
//  2. name_mappings: author/editor name canonicalisation; independent of others.
//
//  3. field_mappings: unified per-field value normalisation and cross-field propagation.
//     Depends on name_mappings (name values are normalised via name aliases).
//
//  6. key_hints, key_oldies, non_double_entries: key alias tables.
//
//  7. dblp_parent, dblp_waived, entry_flags: DBLP-related tables.
//
//  8. urls_ignore: loaded on demand by specific commands.
//
// All field-mapping and metadata tables use write-through: mutations call db.Exec
// immediately rather than batching at end-of-run. Load functions use loading flags
// (fieldMappingsLoading, entryFieldMappingsLoading) to suppress the write-
// through during the initial DB→memory population pass.

// --- contributors + contributor_names tables ---

func ensureContributorsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS contributors (
		  id    TEXT PRIMARY KEY,
		  name  TEXT NOT NULL,
		  orcid TEXT
		);`)
}

func ensureContributorNamesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS contributor_names (
		  id   TEXT NOT NULL,
		  name TEXT NOT NULL,
		  PRIMARY KEY (id, name),
		  FOREIGN KEY (id) REFERENCES contributors(id) ON DELETE CASCADE
		);`)
}

func ensureContributorRolesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS contributor_roles (
		  entry_key      TEXT    NOT NULL REFERENCES bib_entry_keys(entry_key),
		  role           TEXT    NOT NULL,
		  position       INTEGER NOT NULL,
		  contributor_id TEXT    NOT NULL REFERENCES contributors(id),
		  PRIMARY KEY (entry_key, role, position)
		);`)
}

func ensureEntryContributorNamesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS entry_contributor_names (
		  entry_key      TEXT    NOT NULL,
		  role           TEXT    NOT NULL,
		  position       INTEGER NOT NULL,
		  contributor_id TEXT    NOT NULL REFERENCES contributors(id),
		  name_used      TEXT    NOT NULL,
		  orcid_used     TEXT,
		  dblp_key_used  TEXT,
		  PRIMARY KEY (entry_key, role, position),
		  FOREIGN KEY (entry_key, role, position)
		      REFERENCES contributor_roles(entry_key, role, position) ON DELETE CASCADE
		);`)
	// dblp_key_used, used by applyDblpAuthorORCIDs, was for a long time only present on
	// databases that had it added out-of-band — the CREATE TABLE above never had it.
	// A genuinely fresh database (e.g. a new install, or a from-scratch test DB) would
	// hit "no such column: dblp_key_used" on every DBLP-with-ORCID entry. Add it to
	// databases created before this fix; no-op (duplicate column, ignored) otherwise.
	db.Exec(`ALTER TABLE entry_contributor_names ADD COLUMN dblp_key_used TEXT`) //nolint:errcheck
}

// maybeMigrateEntryContributorNamesAddOrcidUsed adds the orcid_used column to
// entry_contributor_names when it is absent (databases created before this migration).
func ensureNonDoubleContributorsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS non_double_contributors (
		  contributor_id_a TEXT NOT NULL REFERENCES contributors(id),
		  contributor_id_b TEXT NOT NULL REFERENCES contributors(id),
		  PRIMARY KEY (contributor_id_a, contributor_id_b),
		  CHECK (contributor_id_a < contributor_id_b)
		);`)
}

// addNonDoubleContributorPair records that idA and idB are confirmed different
// people. The IDs are stored in sorted order to satisfy the CHECK constraint.
// No-op when the pair is already recorded.
func addNonDoubleContributorPair(idA, idB string) {
	if idA > idB {
		idA, idB = idB, idA
	}
	dbExecSave("non_double_contributors insert",
		`INSERT OR IGNORE INTO non_double_contributors (contributor_id_a, contributor_id_b) VALUES (?, ?)`,
		idA, idB)
}

// --- contributor_id_oldies table ---

func ensureContributorIDOldiesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS contributor_id_oldies (
		  absorbed_id   TEXT PRIMARY KEY,
		  canonical_id  TEXT NOT NULL
		);`)
}

// upsertContributorIDOldie records that absorbedID has been merged into canonicalID,
// following any existing chain so the table always maps directly to the ultimate canonical.
func upsertContributorIDOldie(absorbedID, canonicalID string) {
	// Chase existing chain: if canonicalID was itself absorbed, find the ultimate target.
	ultimate := canonicalID
	for {
		var next string
		if err := db.QueryRow(`SELECT canonical_id FROM contributor_id_oldies WHERE absorbed_id = ?`, ultimate).Scan(&next); err != nil || next == "" {
			break
		}
		ultimate = next
	}
	dbExecSave("upsertContributorIDOldie: insert",
		`INSERT INTO contributor_id_oldies (absorbed_id, canonical_id) VALUES (?, ?) `+
			`ON CONFLICT(absorbed_id) DO UPDATE SET canonical_id = excluded.canonical_id`,
		absorbedID, ultimate)
	// If absorbedID's own oldies chain pointed here, redirect those too.
	dbExecSave("upsertContributorIDOldie: redirect chained oldies",
		`UPDATE contributor_id_oldies SET canonical_id = ? WHERE canonical_id = ?`,
		ultimate, absorbedID)
}

// loadContributorIDOldiesFromDB loads the contributor_id_oldies table into l.ContributorIDOldies.
func loadContributorIDOldiesFromDB(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT absorbed_id, canonical_id FROM contributor_id_oldies`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var absorbedID, canonicalID string
		if rows.Scan(&absorbedID, &canonicalID) == nil {
			l.ContributorIDOldies[absorbedID] = canonicalID
		}
	}
}

// ResolveContributorID returns the current canonical contributor ID for id,
// following any absorbed-ID chain recorded in ContributorIDOldies. Returns
// id itself if it is already canonical or unknown.
func (l *TBibTeXLibrary) ResolveContributorID(id string) string {
	seen := map[string]bool{id: true}
	cur := id
	for {
		next, ok := l.ContributorIDOldies[cur]
		if !ok {
			return cur
		}
		if seen[next] {
			return cur // cycle guard
		}
		seen[next] = true
		cur = next
	}
}

// --- contributor_orcid_seen table ---

// ensureContributorORCIDSeenTableExists creates the table that records the last
// state seen during -enrich_orcid_profiles for each (contributor, ORCID) pair.
// Columns cover the ORCID-provided name fields AND the canonical at the time,
// so any change to either the ORCID record or an external canonical update
// (e.g. a higher-priority DBLP fix) will trigger a fresh challenge.
func ensureContributorORCIDSeenTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS contributor_orcid_seen (
		  contributor_id TEXT NOT NULL,
		  orcid          TEXT NOT NULL,
		  canonical      TEXT NOT NULL DEFAULT '',
		  credit_name    TEXT NOT NULL DEFAULT '',
		  declared_name  TEXT NOT NULL DEFAULT '',
		  other_names    TEXT NOT NULL DEFAULT '',
		  PRIMARY KEY (contributor_id, orcid)
		);`)
}

// orcidSeenRecord holds the signature stored for one (contributor, ORCID) pair.
type orcidSeenRecord struct {
	canonical, creditName, declaredName, otherNames string
}

// loadORCIDSeen fetches the stored signature for (contributorID, orcid), or
// returns the zero value when no record exists.
func loadORCIDSeen(contributorID, orcid string) orcidSeenRecord {
	var r orcidSeenRecord
	db.QueryRow( //nolint:errcheck
		`SELECT canonical, credit_name, declared_name, other_names
		   FROM contributor_orcid_seen WHERE contributor_id = ? AND orcid = ?`,
		contributorID, orcid).Scan(&r.canonical, &r.creditName, &r.declaredName, &r.otherNames)
	return r
}

// upsertORCIDSeen stores (or updates) the seen signature for (contributorID, orcid).
func upsertORCIDSeen(contributorID, orcid string, r orcidSeenRecord) {
	dbExecSave("upsertORCIDSeen",
		`INSERT INTO contributor_orcid_seen
		   (contributor_id, orcid, canonical, credit_name, declared_name, other_names)
		   VALUES (?, ?, ?, ?, ?, ?)
		   ON CONFLICT(contributor_id, orcid) DO UPDATE SET
		     canonical = excluded.canonical,
		     credit_name = excluded.credit_name,
		     declared_name = excluded.declared_name,
		     other_names = excluded.other_names`,
		contributorID, orcid, r.canonical, r.creditName, r.declaredName, r.otherNames)
}

// clearContributorORCIDSeen deletes the seen record for (contributorID, orcid) so that
// the next -enrich_contributor_data run re-fetches and re-challenges that contributor.
func clearContributorORCIDSeen(contributorID, orcid string) {
	db.Exec(`DELETE FROM contributor_orcid_seen WHERE contributor_id = ? AND orcid = ?`,
		contributorID, orcid) //nolint:errcheck
}

// --- contributor_orcids table ---

func ensureContributorORCIDsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS contributor_orcids (
		  orcid          TEXT NOT NULL,
		  contributor_id TEXT NOT NULL REFERENCES contributors(id) ON DELETE CASCADE,
		  is_canonical   INTEGER NOT NULL DEFAULT 0,
		  PRIMARY KEY (orcid)
		);`)
}

// maybeMigrateContributorORCIDs seeds contributor_orcids from the canonical orcid
// column in contributors, for DBs opened before contributor_orcids existed.
func maybeMigrateContributorORCIDs() {
	dbExecSave("maybeMigrateContributorORCIDs",
		`INSERT OR IGNORE INTO contributor_orcids (orcid, contributor_id, is_canonical) `+
			`SELECT orcid, id, 1 FROM contributors WHERE orcid IS NOT NULL AND orcid != ''`)
}

// upsertContributorORCIDToDB records orcid for id in contributor_orcids.
// When canonical is true it also sets contributors.orcid if not already set.
func upsertContributorORCIDToDB(id, orcid string, canonical bool) {
	if id == "" || orcid == "" {
		return
	}
	isCanon := 0
	if canonical {
		isCanon = 1
	}
	if err := bibExec(`INSERT OR IGNORE INTO contributor_orcids (orcid, contributor_id, is_canonical) VALUES (?, ?, ?)`,
		orcid, id, isCanon); err != nil {
		dbInteraction.Warning("contributor_orcids upsert failed: %s", err)
		dbWriteFailed = true
		return
	}
	if canonical {
		if err := bibExec(`UPDATE contributors SET orcid = ? WHERE id = ? AND (orcid IS NULL OR orcid = '')`,
			orcid, id); err != nil {
			dbInteraction.Warning("contributors orcid update failed: %s", err)
			dbWriteFailed = true
			return
		}
		setTableDate("contributors", time.Now().UnixMicro())
	}
}

// loadContributorORCIDsFromDB populates l.ORCIDToContributorID from contributor_orcids.
func loadContributorORCIDsFromDB(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT orcid, contributor_id FROM contributor_orcids`)
	if err != nil {
		dbInteraction.Warning("Could not query contributor_orcids: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var orcid, id string
		if rows.Scan(&orcid, &id) == nil {
			l.ORCIDToContributorID[orcid] = id
		}
	}
}

// contributorORCIDs returns all ORCIDs for a contributor from contributor_orcids.
func contributorORCIDs(id string) []string {
	rows, err := db.Query(`SELECT orcid FROM contributor_orcids WHERE contributor_id = ?`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var orcid string
		if rows.Scan(&orcid) == nil {
			result = append(result, orcid)
		}
	}
	return result
}

// orcidToContributorID returns the contributor ID for an ORCID, or "" if unknown.
func orcidToContributorID(l *TBibTeXLibrary, orcid string) string {
	return l.ORCIDToContributorID[orcid]
}

// contributorAliasesFromDB returns all name forms for a contributor (canonical + aliases)
// from the contributor_names table.
func contributorAliasesFromDB(id string) []string {
	rows, err := db.Query(`SELECT name FROM contributor_names WHERE id = ?`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			names = append(names, name)
		}
	}
	return names
}

// contributorEntryKeys returns all library entry keys in which a contributor appears
// (as author, editor, or any other role) according to contributor_roles.
func contributorEntryKeys(id string) []string {
	rows, err := db.Query(`SELECT DISTINCT entry_key FROM contributor_roles WHERE contributor_id = ?`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			keys = append(keys, key)
		}
	}
	return keys
}

// entryExistsWithDOI reports whether any library entry already carries the given
// DOI value (case-insensitive, after stripping any "https://doi.org/" prefix).
// normalizeDOI returns the canonical lower-case DOI without any https?://doi.org/ prefix.
func normalizeDOI(doi string) string {
	doi = strings.TrimPrefix(doi, "https://doi.org/")
	doi = strings.TrimPrefix(doi, "http://doi.org/")
	return strings.ToLower(doi)
}

func entryExistsWithDOI(doi string) bool {
	doi = normalizeDOI(doi)
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = 'doi' AND lower(value) = ?`, doi).Scan(&count) //nolint:errcheck
	return count > 0
}

// doiHasSupersededRecord reports whether the given DOI appears in superseded_field_values
// for any entry's doi field. This catches the case where a DOI stub was merged
// into a library entry but the DOI was not transferred to the surviving entry
// (possible for merges performed before v27.14); the persisted superseded record is
// sufficient evidence that the DOI is already known to the system, so the watch
// need not create another stub.
func doiHasSupersededRecord(doi string) bool {
	doi = normalizeDOI(doi)
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM superseded_field_values WHERE field = 'doi' AND lower(value) = ?`, doi).Scan(&count) //nolint:errcheck
	return count > 0
}

// --- entry_doi_aliases table ---

func ensureEntryDoiAliasesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS entry_doi_aliases (
		  doi       TEXT NOT NULL PRIMARY KEY,
		  entry_key TEXT NOT NULL,
		  FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
}

// addEntryDoiAlias registers doi as an alternative/absorbed identifier for entryKey.
// The DOI is normalised before storage. Silent no-op when the DOI is already aliased
// to the same entry; warns if it maps to a different entry (data conflict).
func addEntryDoiAlias(entryKey, doi string) {
	doi = normalizeDOI(doi)
	if doi == "" || entryKey == "" {
		return
	}
	var existing string
	db.QueryRow(`SELECT entry_key FROM entry_doi_aliases WHERE doi = ?`, doi).Scan(&existing) //nolint:errcheck
	if existing == entryKey {
		return
	}
	if existing != "" {
		dbInteraction.Warning("addEntryDoiAlias: DOI %s already aliased to %s (requested %s) — skipped", doi, existing, entryKey)
		return
	}
	dbExecSave("addEntryDoiAlias", `INSERT INTO entry_doi_aliases (doi, entry_key) VALUES (?, ?)`, doi, entryKey)
}

// doiHasAlias reports whether doi has been explicitly registered as an alias for
// an existing entry via entry_doi_aliases.
func doiHasAlias(doi string) bool {
	doi = normalizeDOI(doi)
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM entry_doi_aliases WHERE doi = ?`, doi).Scan(&count) //nolint:errcheck
	return count > 0
}

func setContributorDblpKey(l *TBibTeXLibrary, id, dblpKey string) {
	if c := l.ContributorByID[id]; c != nil {
		c.DblpKey = dblpKey
	}
	if dblpKey != "" {
		l.DblpKeyToContributorID[dblpKey] = id
	}
	dbExecSave("contributors dblp_key update",
		`UPDATE contributors SET dblp_key = ? WHERE id = ?`, dblpKey, id)
}

// upsertContributorToDB records id as having the clean (non-garbled) canonical name
// `name`. All call sites already guard against garbled names before calling this, so
// the ON CONFLICT clause also clears garbled — otherwise a contributor once created
// via upsertGarbledContributorToDB (or otherwise flagged garbled=1) would stay flagged
// forever even after being given a clean canonical name, causing CheckGarbledContributors
// to keep reporting an already-fixed name as still needing fixing.
func upsertContributorToDB(id, name, orcid string) {
	if err := bibExec(`INSERT INTO contributors (id, name, orcid, garbled) VALUES (?, ?, ?, 0)
	                    ON CONFLICT(id) DO UPDATE SET name = excluded.name, garbled = 0,
	                      orcid = COALESCE(NULLIF(excluded.orcid, ''), orcid)`,
		id, name, orcid); err != nil {
		dbInteraction.Warning("contributors upsert failed: %s", err)
		dbWriteFailed = true
		return
	}
	setTableDate("contributors", time.Now().UnixMicro())
}

func upsertGarbledContributorToDB(id, name string) {
	if err := bibExec(`INSERT INTO contributors (id, name, orcid, garbled) VALUES (?, ?, '', 1)
	                    ON CONFLICT(id) DO UPDATE SET name = excluded.name, garbled = 1`,
		id, name); err != nil {
		dbInteraction.Warning("contributors (garbled) upsert failed: %s", err)
		dbWriteFailed = true
		return
	}
	setTableDate("contributors", time.Now().UnixMicro())
}

func upsertContributorNameToDB(id, name string) {
	if err := bibExec(`INSERT OR IGNORE INTO contributor_names (id, name) VALUES (?, ?)`, id, name); err != nil {
		dbInteraction.Warning("contributor_names upsert failed: %s", err)
		dbWriteFailed = true
	}
}

func deleteContributorNameFromDB(id, name string) {
	if err := bibExec(`DELETE FROM contributor_names WHERE id = ? AND name = ?`, id, name); err != nil {
		dbInteraction.Warning("contributor_names delete failed: %s", err)
		dbWriteFailed = true
	}
}

// --- name_mappings table ---

// forceNameMapping, when true, bypasses upsertNameMapping's automatic garbled-name
// heuristic. Set around explicit, user-directed alias commands (e.g. -add_name_mapping)
// where the user has already deliberately confirmed the name — organization names like
// "OMG, Object Management Group, Inc." trip the 2-comma heuristic (built to catch
// auto-derived "Lastname, Suffix, Firstname" garbage) despite being entirely valid.
// Automatic/derived callers leave this false so the heuristic still protects them.
var forceNameMapping bool

// upsertNameMapping records alias as a non-derived name form for the contributor
// whose canonical name is `name`. Suppressed during load. If no contributor
// exists yet for `name` a new one is created on the fly.
// If the alias is itself the canonical of another contributor, that contributor
// is absorbed into the target contributor in-place (DB + in-memory).
func upsertNameMapping(alias, name string) {
	if contributorsLoading {
		return
	}
	if alias == "" || name == "" || alias == name {
		return
	}
	if !forceNameMapping && (isGarbledContributorName(alias) || isGarbledContributorName(name)) {
		dbInteraction.Warning("Skipping name mapping %q -> %q: looks like a garbled/ambiguous name (heuristic check); use -add_name_mapping to force it", alias, name)
		return
	}
	id, ok := Library.NameToContributorID[name]
	if !ok {
		id = Library.NewKey()
		Library.ContributorByID[id] = &TContributor{Name: name}
		Library.NameToContributorID[name] = id
		upsertContributorToDB(id, name, "")
		upsertContributorNameToDB(id, name)
	}

	// If alias already maps to a contributor different from id, decide how to proceed.
	if aliasID, mapped := Library.NameToContributorID[alias]; mapped && aliasID != id {
		if aliasContrib, isContrib := Library.ContributorByID[aliasID]; isContrib && aliasContrib.Name == alias {
			// alias is the canonical name of aliasID → absorb aliasID into id using
			// the full merge routine so that contributor_roles FK constraints are
			// satisfied and no silent failures occur.
			if mergeContributorInDB(aliasID, id) {
				for n, nid := range Library.NameToContributorID {
					if nid == aliasID {
						Library.NameToContributorID[n] = id
					}
				}
				for orcid, nid := range Library.ORCIDToContributorID {
					if nid == aliasID {
						Library.ORCIDToContributorID[orcid] = id
					}
				}
				delete(Library.ContributorByID, aliasID)
			}
		} else {
			// alias is a non-canonical of aliasID → this name is now globally ambiguous.
			upsertContributorNameToDB(id, alias)
			makeNameAmbiguous(&Library, alias, aliasID, id)
			retroactivelyBackfillDisambiguation(&Library, alias)
			return
		}
	}

	// If alias is already in the ambiguous map, add id to the candidate set.
	if existingIDs, ambiguous := Library.AmbiguousNameToContributorIDs[alias]; ambiguous {
		upsertContributorNameToDB(id, alias)
		for _, existingID := range existingIDs {
			if existingID == id {
				return
			}
		}
		Library.AmbiguousNameToContributorIDs[alias] = append(existingIDs, id)
		retroactivelyBackfillDisambiguation(&Library, alias)
		return
	}

	upsertContributorNameToDB(id, alias)
	Library.NameToContributorID[alias] = id
	// A prior "different person" recording for this exact pair (e.g. from skipped
	// triage) is now contradicted by this alias — see doCorrectName's rename-rewire
	// logic for the same principle applied to renames.
	removeNonDoubleContributorNamePair(&Library, alias, name)
}

// removeNonDoubleContributorNamePair deletes any recorded "different person" pairing
// between name1 and name2, both in memory and in the DB. Called when a later, more
// authoritative decision (e.g. an explicit alias) establishes that they are in fact
// the same contributor — leaving the stale pair would keep it flagged as different.
func removeNonDoubleContributorNamePair(l *TBibTeXLibrary, name1, name2 string) {
	if name1 > name2 {
		name1, name2 = name2, name1
	}
	key := [2]string{name1, name2}
	if !l.NonDoubleContributorNames[key] {
		return
	}
	delete(l.NonDoubleContributorNames, key)
	dbExecSave("removeNonDoubleContributorNamePair",
		`DELETE FROM non_double_contributor_names WHERE name1 = ? AND name2 = ?`, name1, name2)
}

// deleteNameMapping removes a non-derived name form from the contributor
// identified by alias. Suppressed during load.
func deleteNameMapping(alias string) {
	if contributorsLoading {
		return
	}
	if id, ok := Library.NameToContributorID[alias]; ok {
		deleteContributorNameFromDB(id, alias)
		delete(Library.NameToContributorID, alias)
		return
	}
	if ids, ok := Library.AmbiguousNameToContributorIDs[alias]; ok {
		// Remove the first matching ID from the ambiguous set; caller does not
		// pass the specific ID, so we can only handle the single-entry case here.
		if len(ids) <= 1 {
			delete(Library.AmbiguousNameToContributorIDs, alias)
		}
		// With 2+ IDs we cannot remove the right one without an id parameter;
		// the ambiguity record stays until the contributor is merged or deleted.
	}
}

// maybeMergeSpuriousContributors detects and merges contributors whose canonical
// name appears as a non-canonical entry in another contributor's name list.
// This repairs data produced by the transitivity-blind migration: chains like
// A→B→C produced two contributors (B and C) instead of one; B's canonical
// appears in C's name list, identifying B as spurious.
func maybeMergeSpuriousContributors() {
	type mergeOp struct{ fromID, fromName, intoID, intoName string }
	seen := map[string]bool{}
	var ops []mergeOp

	// Pass 1: contributor whose canonical appears as a non-canonical of another.
	rows, err := db.Query(`
		SELECT c1.id, c1.name, c2.id, c2.name
		FROM contributors c1
		JOIN contributor_names cn ON cn.name = c1.name AND cn.id != c1.id
		JOIN contributors c2 ON c2.id = cn.id
		WHERE c2.name != c1.name
		  AND NOT EXISTS (
		        SELECT 1 FROM non_double_contributors ndc
		        WHERE ndc.contributor_id_a = CASE WHEN c1.id < c2.id THEN c1.id ELSE c2.id END
		          AND ndc.contributor_id_b = CASE WHEN c1.id < c2.id THEN c2.id ELSE c1.id END
		      )`)
	if err == nil {
		for rows.Next() {
			var fromID, fromName, intoID, intoName string
			if err := rows.Scan(&fromID, &fromName, &intoID, &intoName); err != nil {
				continue
			}
			if !seen[fromID] {
				seen[fromID] = true
				seen[intoID] = true
				ops = append(ops, mergeOp{fromID, fromName, intoID, intoName})
			}
		}
		rows.Close()
	}

	// Pass 2: two contributors that share the exact same canonical name (e.g. produced
	// by a rename that did not detect an existing contributor with the target name).
	rows2, err := db.Query(`
		SELECT c1.id, c1.name, c2.id, c2.name
		FROM contributors c1
		JOIN contributors c2 ON c1.name = c2.name AND c1.id < c2.id
		WHERE NOT EXISTS (
		        SELECT 1 FROM non_double_contributors ndc
		        WHERE ndc.contributor_id_a = c1.id AND ndc.contributor_id_b = c2.id
		      )`)
	if err == nil {
		for rows2.Next() {
			var fromID, fromName, intoID, intoName string
			if err := rows2.Scan(&fromID, &fromName, &intoID, &intoName); err != nil {
				continue
			}
			if !seen[fromID] && !seen[intoID] {
				seen[fromID] = true
				seen[intoID] = true
				ops = append(ops, mergeOp{fromID, fromName, intoID, intoName})
			}
		}
		rows2.Close()
	}

	if len(ops) == 0 {
		return
	}
	logPath := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix + "/spurious_contributor_merges.log"
	os.MkdirAll(bibTeXFolder+bibTeXBaseName+tablesFolderSuffix, 0o755) //nolint:errcheck
	logFile, logErr := os.Create(logPath)
	if logErr == nil {
		fmt.Fprintf(logFile, "Spurious contributor merges on %s (%d candidate(s))\n\n",
			time.Now().Format("2006-01-02 15:04:05"), len(ops))
	}
	merged := 0
	for _, op := range ops {
		if mergeContributorInDB(op.fromID, op.intoID) {
			merged++
			if logFile != nil {
				fmt.Fprintf(logFile, "  Merged %q into %q\n", op.fromName, op.intoName)
			}
		}
	}
	if logFile != nil {
		logFile.Close()
	}
	dbInteraction.Progress("  Merged %d/%d spurious contributor duplicate(s) (see spurious_contributor_merges.log)",
		merged, len(ops))
}

// mergeContributorInDB absorbs fromID into toID at the DB level.
// Moves contributor_names, contributor_roles, entry_contributor_names, and
// contributor_orcids to toID, cleans up non_double_contributors rows referencing
// fromID, then deletes fromID from contributors (which cascades to contributor_names
// and contributor_orcids). Returns true on success.
// Uses bibExec so the writes participate in any active bib transaction rather than
// opening a competing transaction that could cause SQLITE_BUSY.
// The caller is responsible for updating in-memory maps after the call.
func mergeContributorInDB(fromID, toID string) bool {
	var fromOrcid, toOrcid string
	db.QueryRow(`SELECT COALESCE(orcid, '') FROM contributors WHERE id = ?`, fromID).Scan(&fromOrcid) //nolint:errcheck
	db.QueryRow(`SELECT COALESCE(orcid, '') FROM contributors WHERE id = ?`, toID).Scan(&toOrcid)     //nolint:errcheck

	// Move contributor_names.
	dbExecSave("merge_contributors: move contributor_names",
		`INSERT OR IGNORE INTO contributor_names (id, name) `+
			`SELECT ?, name FROM contributor_names WHERE id = ?`, toID, fromID)

	// Re-assign contributor_roles rows from fromID to toID. The primary key is
	// (entry_key, role, position); one position belongs to at most one contributor,
	// so updating contributor_id in-place never causes a PK conflict.
	dbExecSave("merge_contributors: reassign contributor_roles",
		`UPDATE contributor_roles SET contributor_id = ? WHERE contributor_id = ?`, toID, fromID)

	// Re-point entry_contributor_names rows that still reference fromID. This covers
	// both the normal migration path and the case where contributor_roles was already
	// updated to a different contributor (e.g. by ORCID re-assignment via
	// applyDblpAuthorORCIDs) without a matching update to entry_contributor_names,
	// which would otherwise leave a dangling FK that blocks the DELETE below.
	dbExecSave("merge_contributors: reassign entry_contributor_names",
		`UPDATE entry_contributor_names SET contributor_id = ? WHERE contributor_id = ?`, toID, fromID)

	// Move contributor_orcids as non-canonical additional ORCIDs for toID.
	dbExecSave("merge_contributors: move contributor_orcids",
		`INSERT OR IGNORE INTO contributor_orcids (orcid, contributor_id, is_canonical) `+
			`SELECT orcid, ?, 0 FROM contributor_orcids WHERE contributor_id = ?`, toID, fromID)

	// If toID has no canonical ORCID but fromID does, promote it.
	if toOrcid == "" && fromOrcid != "" {
		dbExecSave("merge_contributors: promote canonical ORCID",
			`UPDATE contributors SET orcid = ? WHERE id = ?`, fromOrcid, toID)
		dbExecSave("merge_contributors: seed promoted ORCID",
			`INSERT OR IGNORE INTO contributor_orcids (orcid, contributor_id, is_canonical) VALUES (?, ?, 1)`,
			fromOrcid, toID)
	}

	// Carry over ORCID enrichment records. contributor_orcid_seen has no FK
	// constraint, so fromID rows are not removed by the CASCADE delete below.
	// INSERT OR IGNORE keeps toID's own record when the same ORCID was already
	// enriched there; then delete the now-orphaned fromID rows.
	dbExecSave("merge_contributors: move contributor_orcid_seen",
		`INSERT OR IGNORE INTO contributor_orcid_seen `+
			`(contributor_id, orcid, canonical, credit_name, declared_name, other_names) `+
			`SELECT ?, orcid, canonical, credit_name, declared_name, other_names `+
			`FROM contributor_orcid_seen WHERE contributor_id = ?`, toID, fromID)
	dbExecSave("merge_contributors: delete orphaned contributor_orcid_seen",
		`DELETE FROM contributor_orcid_seen WHERE contributor_id = ?`, fromID)

	// Remove non_double_contributors rows referencing fromID (required before delete).
	dbExecSave("merge_contributors: clear non_double_contributors",
		`DELETE FROM non_double_contributors WHERE contributor_id_a = ? OR contributor_id_b = ?`,
		fromID, fromID)

	// Delete fromID — cascades to contributor_names and contributor_orcids.
	if err := bibExec(`DELETE FROM contributors WHERE id = ?`, fromID); err != nil {
		dbInteraction.Warning("merge_contributors: could not delete contributor %s: %s", fromID, err)
		return false
	}
	// Re-seed toID's canonical ORCID: if fromID held the same ORCID, the earlier
	// INSERT OR IGNORE was blocked by a PRIMARY KEY conflict on orcid; now that the
	// CASCADE delete removed the fromID row from contributor_orcids, the seed works.
	if toOrcid != "" {
		dbExecSave("merge_contributors: reseed canonical ORCID",
			`INSERT OR IGNORE INTO contributor_orcids (orcid, contributor_id, is_canonical) VALUES (?, ?, 1)`,
			toOrcid, toID)
	}
	upsertContributorIDOldie(fromID, toID)
	return true
}

// maybeCleanupOrphanedContributors removes contributor records (and their
// contributor_names rows via CASCADE) for any contributor that has no
// contributor_roles entries — i.e. no longer appears as an author or editor in
// any library entry. Only runs when contributorRolesActive is true, to avoid
// deleting contributors that were seeded but not yet migrated to roles.
// The "others" sentinel is always exempt.
func maybeCleanupOrphanedContributors(l *TBibTeXLibrary) {
	if !contributorRolesActive {
		return
	}
	// Collect orphans with their canonical name and all registered alias forms.
	type orphanInfo struct {
		id      string
		name    string
		aliases []string
	}
	rows, err := db.Query(`
		SELECT c.id, c.name, COALESCE(cn.name, '')
		FROM contributors c
		LEFT JOIN contributor_names cn ON cn.id = c.id AND cn.name != c.name
		WHERE c.id NOT IN (SELECT DISTINCT contributor_id FROM contributor_roles)
		  AND c.name != 'others'
		ORDER BY c.name, c.id, cn.name`)
	if err != nil {
		return
	}
	orphanMap := map[string]*orphanInfo{}
	var orphanOrder []string
	for rows.Next() {
		var id, name, alias string
		if rows.Scan(&id, &name, &alias) != nil {
			continue
		}
		if _, seen := orphanMap[id]; !seen {
			orphanMap[id] = &orphanInfo{id: id, name: name}
			orphanOrder = append(orphanOrder, id)
		}
		if alias != "" {
			orphanMap[id].aliases = append(orphanMap[id].aliases, alias)
		}
	}
	rows.Close()
	if len(orphanMap) == 0 {
		return
	}

	orphanIDs := make([]string, 0, len(orphanMap))
	for _, id := range orphanOrder {
		orphanIDs = append(orphanIDs, id)
	}

	logPath := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix + "/orphaned_contributors.log"
	os.MkdirAll(bibTeXFolder+bibTeXBaseName+tablesFolderSuffix, 0o755) //nolint:errcheck
	if f, err := os.Create(logPath); err == nil {
		fmt.Fprintf(f, "Orphaned contributors removed on %s (%d total)\n\n",
			time.Now().Format("2006-01-02 15:04:05"), len(orphanIDs))
		for _, id := range orphanOrder {
			o := orphanMap[id]
			sort.Strings(o.aliases)
			if len(o.aliases) == 0 {
				fmt.Fprintf(f, "%s  [no aliases]\n", o.name)
			} else {
				fmt.Fprintf(f, "%s  [aliases: %s]\n", o.name, strings.Join(o.aliases, " | "))
			}
		}
		f.Close()
	} else {
		dbInteraction.Warning("Could not write orphaned_contributors.log: %s", err)
	}

	l.Progress("  Cleaning up %d orphaned contributor(s) with no roles (see orphaned_contributors.log) ...", len(orphanIDs))
	// Fix stale entry_contributor_names rows that reference orphaned contributors
	// before the DELETE: contributor_id has no ON DELETE CASCADE, so the bulk
	// delete fails with an FK constraint if any such rows remain.
	//
	// Case 1: contributor_roles still has a row for this position (under a different
	// contributor after a role reassignment) — update contributor_id to match.
	dbExecSave("maybeCleanupOrphanedContributors: reassign entry_contributor_names",
		`UPDATE entry_contributor_names
		 SET contributor_id = (
		     SELECT cr.contributor_id FROM contributor_roles cr
		     WHERE cr.entry_key  = entry_contributor_names.entry_key
		       AND cr.role       = entry_contributor_names.role
		       AND cr.position   = entry_contributor_names.position
		 )
		 WHERE contributor_id IN (
		     SELECT id FROM contributors
		     WHERE id NOT IN (SELECT DISTINCT contributor_id FROM contributor_roles)
		       AND name != 'others'
		 )
		 AND EXISTS (
		     SELECT 1 FROM contributor_roles cr
		     WHERE cr.entry_key = entry_contributor_names.entry_key
		       AND cr.role      = entry_contributor_names.role
		       AND cr.position  = entry_contributor_names.position
		 )`)
	// Case 2: no matching contributor_roles row exists (dangling, left behind when
	// FK enforcement was off during an earlier operation) — delete outright.
	dbExecSave("maybeCleanupOrphanedContributors: delete dangling entry_contributor_names",
		`DELETE FROM entry_contributor_names
		 WHERE contributor_id IN (
		     SELECT id FROM contributors
		     WHERE id NOT IN (SELECT DISTINCT contributor_id FROM contributor_roles)
		       AND name != 'others'
		 )`)
	// Case 3: non_double_contributors has no CASCADE on its two FKs to contributors.
	// Remove any non-double pair that references an orphaned contributor.
	dbExecSave("maybeCleanupOrphanedContributors: clear non_double_contributors",
		`DELETE FROM non_double_contributors
		 WHERE contributor_id_a IN (
		     SELECT id FROM contributors
		     WHERE id NOT IN (SELECT DISTINCT contributor_id FROM contributor_roles)
		       AND name != 'others'
		 )
		 OR contributor_id_b IN (
		     SELECT id FROM contributors
		     WHERE id NOT IN (SELECT DISTINCT contributor_id FROM contributor_roles)
		       AND name != 'others'
		 )`)
	// Single bulk DELETE instead of per-row deletes — avoids N separate auto-commits.
	if _, err := db.Exec(`
		DELETE FROM contributors
		WHERE id NOT IN (SELECT DISTINCT contributor_id FROM contributor_roles)
		  AND name != 'others'`); err != nil {
		dbInteraction.Warning("cleanup orphaned contributors: %s", err)
		return
	}
	deleted := make(map[string]bool, len(orphanIDs))
	for _, id := range orphanIDs {
		delete(l.ContributorByID, id)
		deleted[id] = true
	}
	for name, id := range l.NameToContributorID {
		if deleted[id] {
			delete(l.NameToContributorID, name)
		}
	}
	// ORCIDToContributorID/DblpKeyToContributorID are loaded once at library-open,
	// before this function runs (loadMappingFiles → loadContributorORCIDsFromDB,
	// called from openLibraryToUpdate before maybeCleanupOrphanedContributors) — so
	// an orphan with a recorded ORCID/DBLP key leaves a stale map entry for the rest
	// of the run if not cleared here. A later lookup (e.g. applyDblpAuthorORCIDs)
	// then resolves to a contributor ID that no longer exists in contributors,
	// and the next write through it hits a FOREIGN KEY violation.
	for orcid, id := range l.ORCIDToContributorID {
		if deleted[id] {
			delete(l.ORCIDToContributorID, orcid)
		}
	}
	for dblpKey, id := range l.DblpKeyToContributorID {
		if deleted[id] {
			delete(l.DblpKeyToContributorID, dblpKey)
		}
	}
	for name, ids := range l.AmbiguousNameToContributorIDs {
		kept := ids[:0]
		for _, id := range ids {
			if !deleted[id] {
				kept = append(kept, id)
			}
		}
		switch len(kept) {
		case 0:
			delete(l.AmbiguousNameToContributorIDs, name)
		case 1:
			delete(l.AmbiguousNameToContributorIDs, name)
			l.NameToContributorID[name] = kept[0]
		default:
			l.AmbiguousNameToContributorIDs[name] = kept
		}
	}
	l.Progress("  Cleaned up %d orphaned contributor(s) with no roles.", len(orphanIDs))
}

// derivableNameForms returns the complete set of name forms derivable from
// baseNames via the two name-alias rules (inversion + compressed initials),
// applied transitively. Forms that equal a member of baseNames are excluded so
// the result represents only the derived aliases, never the bases themselves.
func derivableNameForms(baseNames []string) map[string]bool {
	derived := map[string]bool{}
	var expand func(name string)
	expand = func(name string) {
		for _, form := range []string{invertedNameForm(name), compressedInitialsForm(name)} {
			if form != "" && !derived[form] {
				derived[form] = true
				expand(form)
			}
		}
	}
	for _, name := range baseNames {
		expand(name)
	}
	for _, name := range baseNames {
		delete(derived, name) // bases are not "derived from" themselves
	}
	return derived
}

// contributorNamePair is used during contributor load to accumulate non-canonical
// base forms for subsequent NameAliasToName / FindAliases processing.
type contributorNamePair struct{ alias, canonical string }

// setNameForContributor populates NameToContributorID for one (name, id) pair
// during load. If name is already mapped to a different contributor, the name is
// moved to AmbiguousNameToContributorIDs instead.
func setNameForContributor(l *TBibTeXLibrary, name, id, canonical string, pairs *[]contributorNamePair) {
	if existingID, exists := l.NameToContributorID[name]; exists && existingID != id {
		makeNameAmbiguous(l, name, existingID, id)
		return
	}
	if existingIDs, ambiguous := l.AmbiguousNameToContributorIDs[name]; ambiguous {
		for _, eid := range existingIDs {
			if eid == id {
				return
			}
		}
		l.AmbiguousNameToContributorIDs[name] = append(existingIDs, id)
		return
	}
	l.NameToContributorID[name] = id
	if name != canonical {
		*pairs = append(*pairs, struct{ alias, canonical string }{alias: name, canonical: canonical})
	}
}

// loadContributorsFromDb reads the
// contributors and contributor_names tables, builds NameAliasToName,
// NameToAliases, NameToContributorID, and ContributorByID, then prunes any
// derivable name forms from contributor_names (so the table converges to only
// non-derivable base forms over successive runs).
func loadContributorsFromDb(l *TBibTeXLibrary) {
	contributorsLoading = true
	defer func() { contributorsLoading = false }()

	// Load contributor metadata.
	rows, err := db.Query(`SELECT id, name, COALESCE(orcid, ''), COALESCE(dblp_key, ''), COALESCE(garbled, 0) FROM contributors`)
	if err != nil {
		dbInteraction.Warning("Could not query contributors: %s", err)
		return
	}
	for rows.Next() {
		var id, name, orcid, dblpKey string
		var garbled int
		if err := rows.Scan(&id, &name, &orcid, &dblpKey, &garbled); err != nil {
			dbInteraction.Warning("Could not scan contributors row: %s", err)
			continue
		}
		l.ContributorByID[id] = &TContributor{Name: name, ORCID: orcid, DblpKey: dblpKey, Garbled: garbled != 0}
		if orcid != "" {
			l.ORCIDToContributorID[orcid] = id
		}
		if dblpKey != "" {
			l.DblpKeyToContributorID[dblpKey] = id
		}
	}
	rows.Close()

	// Load contributor_names grouped by contributor ID.
	nameRows, err := db.Query(`SELECT id, name FROM contributor_names`)
	if err != nil {
		dbInteraction.Warning("Could not query contributor_names: %s", err)
		return
	}
	namesPerID := map[string][]string{}
	for nameRows.Next() {
		var id, name string
		if err := nameRows.Scan(&id, &name); err != nil {
			dbInteraction.Warning("Could not scan contributor_names row: %s", err)
			continue
		}
		namesPerID[id] = append(namesPerID[id], name)
	}
	nameRows.Close()

	// For each contributor: clean up derivable stored forms, build in-memory maps.
	var pairs []contributorNamePair
	for id, names := range namesPerID {
		c, ok := l.ContributorByID[id]
		if !ok {
			continue
		}
		canonical := c.Name

		// Find and remove derivable forms from contributor_names.
		derivable := derivableNameForms(names)
		for _, name := range names {
			if derivable[name] && name != canonical {
				deleteContributorNameFromDB(id, name)
				continue
			}
			// Non-derivable: populate in-memory maps; detect cross-contributor collisions.
			setNameForContributor(l, name, id, canonical, &pairs)
		}
		setNameForContributor(l, canonical, id, canonical, &pairs)
	}

	// Build NameAliasToName / NameToAliases from the base (non-derivable) pairs.
	for _, p := range pairs {
		l.setNameAlias(p.alias, p.canonical, true)
	}

	// Derive additional in-memory aliases via FindAliases.
	for _, p := range pairs {
		l.FindAliases(p.canonical, p.alias)
	}
	// FindAliases for the canonical itself must cover every contributor, not only
	// those that have non-canonical aliases — otherwise contributors whose only
	// stored name is their canonical (e.g. "Surname, Firstname" with no explicit
	// aliases) never get their derived non-inverted form added to NameAliasToName.
	for _, c := range l.ContributorByID {
		l.FindAliases(c.Name, c.Name)
	}

	ensureOthersContributor(l)
	loadContributorORCIDsFromDB(l)
}

// splitBibNameField splits a BibTeX author/editor value on " and " at brace
// depth 0, so that protected groups such as "{Butler and Bloor}" are kept
// intact as a single token. Empty tokens are omitted.
func splitBibNameField(s string) []string {
	const sep = " and "
	var result []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case ' ':
			if depth == 0 && len(s)-i >= len(sep) && s[i:i+len(sep)] == sep {
				if t := strings.TrimSpace(s[start:i]); t != "" {
					result = append(result, t)
				}
				i += len(sep) - 1
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		result = append(result, t)
	}
	return result
}

// seedContributorsFromEntries ensures that every author/editor name currently
// stored in bib_entries has a corresponding contributor record. Names already
// reachable via NameToContributorID (exact stored or canonical match) or via
// NameAliasToName (derivable form of a known contributor) are skipped. For each
// truly new name a standalone contributor is created and its derivable forms are
// added to the in-memory maps so duplicate contributors are not created for
// variant spellings encountered later in the same pass.
// Called after loadContributorsFromDb so the in-memory maps are complete.
func seedContributorsFromEntries(l *TBibTeXLibrary) {
	rows, err := db.Query(
		`SELECT DISTINCT value FROM bib_entries WHERE field IN ('author', 'editor')`)
	if err != nil {
		return
	}
	defer rows.Close()

	seeded := 0
	for rows.Next() {
		var value string
		if rows.Scan(&value) != nil {
			continue
		}
		for _, name := range splitBibNameField(normalizeEtAlTail(value)) {
			lc := strings.ToLower(name)
			if lc == "others" || lc == "et.al." || lc == "et al." {
				continue // "others" is handled by ensureOthersContributor, not seeded here
			}
			if isGarbledContributorName(name) {
				continue
			}
			if _, ok := l.NameToContributorID[name]; ok {
				continue // already a known contributor or registered derived form
			}
			if _, ok := l.NameAliasToName[name]; ok {
				continue // derivable form of a known contributor
			}
			// New contributor: create with this name as the canonical.
			id := l.NewKey()
			l.ContributorByID[id] = &TContributor{Name: name}
			l.NameToContributorID[name] = id
			upsertContributorToDB(id, name, "")
			upsertContributorNameToDB(id, name)
			// Register derived forms in-memory only to avoid same-pass duplicates.
			for derived := range derivableNameForms([]string{name}) {
				if _, exists := l.NameToContributorID[derived]; !exists {
					l.NameToContributorID[derived] = id
				}
			}
			seeded++
		}
	}
	if seeded > 0 {
		l.Progress("Seeded %d new contributor(s) from author/editor fields.", seeded)
	}
}

// --- contributor_roles migration ---

// resolveNameToContributorID returns the contributor ID for name, checking
// NameToContributorID first (direct or registered derived form), then following
// NameAliasToName if the direct lookup misses.
func resolveNameToContributorID(l *TBibTeXLibrary, name string) (string, bool) {
	if id, ok := l.NameToContributorID[name]; ok {
		return id, true
	}
	if canonical, ok := l.NameAliasToName[name]; ok {
		if id, ok := l.NameToContributorID[canonical]; ok {
			return id, true
		}
	}
	return "", false
}

// resolveNamesToIDSeq maps each name to its contributor ID. A name with no
// resolvable contributor ID falls back to itself (rather than being dropped)
// so idSeqEqual still sees it — silently dropping unmapped names let two
// author lists of different length/content compare as "equal" whenever an
// unmapped name happened to make both ID sequences the same length, which
// suppressed a real, visible difference in the raw text (e.g. a challenger
// with one extra not-yet-known co-author) instead of surfacing it.
func resolveNamesToIDSeq(l *TBibTeXLibrary, names []string) []string {
	seq := make([]string, len(names))
	for i, name := range names {
		if id, ok := resolveNameToContributorID(l, name); ok {
			seq[i] = id
		} else {
			seq[i] = name
		}
	}
	return seq
}

func idSeqEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// isGloballyAmbiguous reports whether name maps to more than one contributor.
func isGloballyAmbiguous(l *TBibTeXLibrary, name string) bool {
	_, ok := l.AmbiguousNameToContributorIDs[name]
	return ok
}

// contributorIDCandidates returns all candidate IDs for name: one element if
// unambiguous, two or more if globally ambiguous, nil if unknown.
func contributorIDCandidates(l *TBibTeXLibrary, name string) []string {
	if ids, ok := l.AmbiguousNameToContributorIDs[name]; ok {
		return ids
	}
	if id, ok := resolveNameToContributorID(l, name); ok {
		return []string{id}
	}
	return nil
}

// makeNameAmbiguous moves name from NameToContributorID to
// AmbiguousNameToContributorIDs, adding newID to the set if not already present.
// existingID is the ID already registered; pass "" to derive it from the map.
func makeNameAmbiguous(l *TBibTeXLibrary, name, existingID, newID string) {
	if existingID == "" {
		existingID = l.NameToContributorID[name]
	}
	delete(l.NameToContributorID, name)
	ids := l.AmbiguousNameToContributorIDs[name]
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if existingID != "" && !seen[existingID] {
		ids = append(ids, existingID)
		seen[existingID] = true
	}
	if newID != "" && !seen[newID] {
		ids = append(ids, newID)
	}
	l.AmbiguousNameToContributorIDs[name] = ids
}

// retroactivelyBackfillDisambiguation creates entry_contributor_names records for
// all contributor_roles rows whose contributor is one of the now-ambiguous
// candidates for name. This preserves correct resolutions that were made while the
// name was still unambiguous (see §7.2 of ARCHITECTURE.md).
func retroactivelyBackfillDisambiguation(l *TBibTeXLibrary, name string) {
	candidates := l.AmbiguousNameToContributorIDs[name]
	if len(candidates) == 0 {
		return
	}
	for _, cID := range candidates {
		rows, err := bibQuery(
			`SELECT entry_key, role, position FROM contributor_roles WHERE contributor_id = ?`, cID)
		if err != nil {
			continue
		}
		var roles []struct {
			key, role string
			pos       int
		}
		for rows.Next() {
			var r struct {
				key, role string
				pos       int
			}
			if rows.Scan(&r.key, &r.role, &r.pos) == nil {
				roles = append(roles, r)
			}
		}
		rows.Close()
		for _, r := range roles {
			dbExecSave("entry_contributor_names insert",
				`INSERT OR IGNORE INTO entry_contributor_names
				 (entry_key, role, position, contributor_id, name_used) VALUES (?, ?, ?, ?, ?)`,
				r.key, r.role, r.pos, cID, name)
		}
	}
}

// normalizeEtAlTail replaces trailing "et al." / "et.al." variants in an
// author/editor field value with "and others" so that the list splits cleanly
// and the "others" sentinel contributor is recognised.
func normalizeEtAlTail(value string) string {
	lower := strings.ToLower(value)
	for _, sfx := range []string{" and et al.", " and et.al."} {
		if strings.HasSuffix(lower, sfx) {
			return value[:len(value)-len(sfx)] + " and others"
		}
	}
	for _, sfx := range []string{" et al.", " et.al."} {
		if strings.HasSuffix(lower, sfx) {
			return value[:len(value)-len(sfx)] + " and others"
		}
	}
	return value
}

// ensureOthersContributor guarantees that the "others" sentinel contributor
// exists in both the in-memory maps and the database. "others" represents a
// truncated author/editor list ("... and others"). Safe to call multiple times.
func ensureOthersContributor(l *TBibTeXLibrary) {
	if _, ok := l.NameToContributorID["others"]; ok {
		return
	}
	id := l.NewKey()
	l.ContributorByID[id] = &TContributor{Name: "others"}
	l.NameToContributorID["others"] = id
	l.NameToContributorID["{others}"] = id // brace-wrapped form found in old bib_entries
	upsertContributorToDB(id, "others", "")
	upsertContributorNameToDB(id, "others")
}

// lookupEntryContributorID returns the recorded contributor ID for
// (key, role, position) from entry_contributor_names when the stored name_used
// matches name. Returns ("", false) if no matching record exists.
func lookupEntryContributorID(key, role string, position int, name string) (string, bool) {
	var storedID, storedName string
	err := db.QueryRow(
		`SELECT contributor_id, name_used FROM entry_contributor_names
		 WHERE entry_key = ? AND role = ? AND position = ?`,
		key, role, position).Scan(&storedID, &storedName)
	if err != nil || storedName != name {
		return "", false
	}
	return storedID, true
}

// coauthorInference returns the candidate ID that has the most entries in
// contributor_roles shared with the already-resolved contributors in the same
// (entry, role). Returns "" when no candidate has any shared entries.
func coauthorInference(candidates, resolvedSoFar []string) string {
	if len(resolvedSoFar) == 0 {
		return ""
	}
	bestCount := 0
	bestID := ""
	for _, candID := range candidates {
		count := 0
		for _, coID := range resolvedSoFar {
			var n int
			db.QueryRow( //nolint:errcheck
				`SELECT COUNT(*) FROM contributor_roles cr1
				 JOIN contributor_roles cr2
				   ON cr2.entry_key = cr1.entry_key AND cr2.role = cr1.role
				 WHERE cr1.contributor_id = ? AND cr2.contributor_id = ?`,
				candID, coID).Scan(&n)
			count += n
		}
		if count > bestCount {
			bestCount = count
			bestID = candID
		}
	}
	return bestID
}

// resolveContributorForPosition implements the 4-step resolution hierarchy for
// a contributor name at a specific (key, role, position):
//
//  1. Entry-specific override — look up entry_contributor_names.
//  2. Global unambiguous — NameToContributorID has exactly one match.
//  3. Co-author inference — pick the candidate with the most shared entries
//     with already-resolved co-contributors in resolvedSoFar.
//  4. Fallback — use the first ambiguous candidate and emit a warning.
//
// Returns ("", false) when name is entirely unknown (step 4 never triggers for
// unknown names; the caller should create a new contributor in that case).
func resolveContributorForPosition(l *TBibTeXLibrary, key, role string, position int, name string, resolvedSoFar []string) (string, bool) {
	if id, ok := lookupEntryContributorID(key, role, position, name); ok {
		return id, true
	}
	if id, ok := resolveNameToContributorID(l, name); ok {
		return id, true
	}
	candidates := l.AmbiguousNameToContributorIDs[name]
	if len(candidates) == 0 {
		return "", false
	}
	if id := coauthorInference(candidates, resolvedSoFar); id != "" {
		return id, true
	}
	l.ambiguousAssignmentCount[name]++
	if _, seen := l.ambiguousAssignmentPick[name]; !seen {
		l.ambiguousAssignmentPick[name] = candidates[0]
	}
	return candidates[0], true
}

// FlushAmbiguousAssignments reports any accumulated fallback contributor assignments as
// a single grouped WARNING and resets the accumulation maps for the next run.
func (l *TBibTeXLibrary) FlushAmbiguousAssignments() {
	if len(l.ambiguousAssignmentCount) == 0 {
		return
	}
	names := make([]string, 0, len(l.ambiguousAssignmentCount))
	for name := range l.ambiguousAssignmentCount {
		names = append(names, name)
	}
	sort.Strings(names)
	l.Warning("Ambiguous contributor assignments (%d unique name(s)):", len(names))
	for _, name := range names {
		l.Warning("  %q: %d occurrence(s), using %s", name, l.ambiguousAssignmentCount[name], l.ambiguousAssignmentPick[name])
	}
	l.ambiguousAssignmentCount = map[string]int{}
	l.ambiguousAssignmentPick = map[string]string{}
}

// --- key_hints table ---

func ensureKeyHintsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_hints (
		  hint TEXT PRIMARY KEY,
		  key  TEXT NOT NULL
		);`)
}

// newKeyHintsTable returns a write-through cache backed by the key_hints SQLite table.
// Stale hints (target entry removed) are deleted from the DB during Load().
// Transient DBLP-derived hints use SetTransient and are never written to the DB.
func newKeyHintsTable() *TCachedTable[string, string] {
	t := newCachedTable(&TSQLiteTable[string, string]{
		upsertSQL: `INSERT INTO key_hints (hint, key) VALUES (?, ?)
		            ON CONFLICT(hint) DO UPDATE SET key = excluded.key;`,
		deleteSQL:  `DELETE FROM key_hints WHERE hint = ?`,
		selectSQL:  `SELECT hint, key FROM key_hints`,
		upsertArgs: func(k, v string) []any { return []any{k, v} },
		deleteArgs: func(k string) []any { return []any{k} },
		scanRow: func(rows *sql.Rows) (string, string, error) {
			var hint, key string
			return hint, key, rows.Scan(&hint, &key)
		},
	})
	t.onModify = func() { setTableDate("key_hints", time.Now().UnixMicro()) }
	return t
}

// --- key_oldies table ---

func ensureKeyOldiesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_oldies (
		  alias TEXT PRIMARY KEY,
		  key   TEXT NOT NULL
		);`)
}

// newKeyOldiesTable returns a TKeyAliasTable backed by the key_oldies SQLite table.
// Load() flattens any stored chains and deletes stale entries immediately.
func newKeyOldiesTable() *TKeyAliasTable {
	return &TKeyAliasTable{
		upsertSQL: `INSERT INTO key_oldies (alias, key) VALUES (?, ?)
		            ON CONFLICT(alias) DO UPDATE SET key = excluded.key;`,
		deleteSQL: `DELETE FROM key_oldies WHERE alias = ?`,
		selectSQL: `SELECT alias, key FROM key_oldies`,
		onModify:  func() { setTableDate("key_oldies", time.Now().UnixMicro()) },
	}
}

// --- non_double_entries table ---

func ensureKeyNonDoublesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS non_double_entries (
		  key1 TEXT NOT NULL,
		  key2 TEXT NOT NULL,
		  PRIMARY KEY (key1, key2)
		);`)
}

// loadKeyNonDoublesFromDb populates l.NonDoubleEntries from the DB.
// Stale pairs (unknown or aliased keys) are deleted from the DB immediately.
// Alias-resolved pairs are updated to their canonical form in the DB.
// Unimported DBLP: keys are kept as-is for future matching.
func loadKeyNonDoublesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT key1, key2 FROM non_double_entries`)
	if err != nil {
		dbInteraction.Warning("Could not query non_double_entries: %s", err)
		return
	}
	type rawPair struct{ k1, k2 string }
	var pairs []rawPair
	for rows.Next() {
		var key1, key2 string
		if err := rows.Scan(&key1, &key2); err != nil {
			dbInteraction.Warning("Could not scan non_double_entries row: %s", err)
			continue
		}
		pairs = append(pairs, rawPair{key1, key2})
	}
	rows.Close()

	nonDoublesLoadingFromDb = true
	defer func() { nonDoublesLoadingFromDb = false }()

	deleteSQL := `DELETE FROM non_double_entries WHERE key1 = ? AND key2 = ?`
	upsertSQL := `INSERT INTO non_double_entries (key1, key2) VALUES (?, ?) ON CONFLICT DO NOTHING`

	for _, p := range pairs {
		r1 := l.resolveNonDoubleKey(p.k1)
		r2 := l.resolveNonDoubleKey(p.k2)
		if r1 == "" || r2 == "" || r1 == r2 {
			dbExecSave("loadKeyNonDoublesFromDb: clear", deleteSQL, p.k1, p.k2)
			continue
		}
		if r1 != p.k1 || r2 != p.k2 {
			dbExecSave("loadKeyNonDoublesFromDb: clear stale pair", deleteSQL, p.k1, p.k2)
			if r1 < r2 {
				dbExecSave("loadKeyNonDoublesFromDb: upsert resolved pair", upsertSQL, r1, r2)
			} else {
				dbExecSave("loadKeyNonDoublesFromDb: upsert resolved pair", upsertSQL, r2, r1)
			}
		}
		l.AddNonDoubleEntries(r1, r2)
	}
}

// saveKeyNonDoublesToDb garbage-collects non_double_entries rows that reference a
// key no longer valid (entry deleted, or superseded by a merge so MapEntryKey now
// resolves elsewhere). This is cleanup, not a persistence step — the table is
// already fully write-through (AddNonDoubleEntries and cleanupIgnoredTitleNonDoubles
// both insert/delete directly against the DB as changes happen, keeping
// l.NonDoubleEntries and the DB in sync throughout the session).
//
// Queries and deletes directly against the DB — never via a blanket
// delete-everything-then-reinsert-from-memory. A session's in-memory snapshot can
// lag behind pairs a concurrent session just wrote (e.g. a long -update_all_dblp_entries
// run alongside a harvest in another terminal); rebuilding the whole table from that
// stale snapshot would silently discard the concurrent session's additions. This
// exact bug lost ~37,000 rows on 2026-07-29 once Ctrl-C started reaching this call
// (via gracefulQuit) instead of just killing the process.
func saveKeyNonDoublesToDb(l *TBibTeXLibrary) {
	isValidNonDoubleKey := func(k string) bool {
		return k == l.MapEntryKey(k) && (l.EntryExists(k) || strings.HasPrefix(k, "DBLP:"))
	}
	rows, err := db.Query(`SELECT key1, key2 FROM non_double_entries`)
	if err != nil {
		dbInteraction.Warning("Could not query non_double_entries: %s", err)
		return
	}
	type pair struct{ k1, k2 string }
	var toDrop []pair
	for rows.Next() {
		var k1, k2 string
		if rows.Scan(&k1, &k2) != nil {
			continue
		}
		if !isValidNonDoubleKey(k1) || !isValidNonDoubleKey(k2) {
			toDrop = append(toDrop, pair{k1, k2})
		}
	}
	rows.Close()
	if len(toDrop) == 0 {
		return
	}
	for _, p := range toDrop {
		dbExecSave("non_double_entries delete failed", `DELETE FROM non_double_entries WHERE key1 = ? AND key2 = ?`, p.k1, p.k2)
		l.NonDoubleEntries.DeleteValueFromStringSetMap(p.k1, p.k2)
		l.NonDoubleEntries.DeleteValueFromStringSetMap(p.k2, p.k1)
	}
	setTableDate("non_double_entries", time.Now().UnixMicro())
}

// --- non_double_contributor_names table ---

func ensureNonDoubleContributorNamesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS non_double_contributor_names (
		  name1 TEXT NOT NULL,
		  name2 TEXT NOT NULL,
		  PRIMARY KEY (name1, name2),
		  CHECK (name1 < name2)
		);`)
}

// loadNonDoubleContributorNamesFromDb populates l.NonDoubleContributorNames.
func loadNonDoubleContributorNamesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT name1, name2 FROM non_double_contributor_names`)
	if err != nil {
		dbInteraction.Warning("Could not query non_double_contributor_names: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var n1, n2 string
		if err := rows.Scan(&n1, &n2); err != nil {
			continue
		}
		l.NonDoubleContributorNames[[2]string{n1, n2}] = true
	}
}

// addNonDoubleContributorNamePair records that name1 and name2 refer to different
// persons. The pair is stored with name1 < name2 (lexicographic) to match the
// CHECK constraint.
func addNonDoubleContributorNamePair(l *TBibTeXLibrary, name1, name2 string) {
	if name1 > name2 {
		name1, name2 = name2, name1
	}
	if name1 == name2 {
		return
	}
	key := [2]string{name1, name2}
	if l.NonDoubleContributorNames[key] {
		return
	}
	l.NonDoubleContributorNames[key] = true
	dbExecSave("addNonDoubleContributorNamePair",
		`INSERT OR IGNORE INTO non_double_contributor_names (name1, name2) VALUES (?, ?)`, name1, name2)
}

// isNonDoubleContributorNamePair returns true if name1 and name2 are recorded as
// referring to different persons.
func isNonDoubleContributorNamePair(l *TBibTeXLibrary, name1, name2 string) bool {
	if name1 > name2 {
		name1, name2 = name2, name1
	}
	return l.NonDoubleContributorNames[[2]string{name1, name2}]
}

// maybeMigrateSkippedToNonDoubleContributorNames seeds non_double_contributor_names
// from superseded_field_values rows that were skipped during triage. For each skipped
// author/editor pair, the single differing name position is extracted and recorded.
// Migrated rows are updated to triage_status='kept'.
func maybeMigrateSkippedToNonDoubleContributorNames(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT entry_key, field, value FROM superseded_field_values WHERE triage_status='skipped' AND field IN ('author','editor')`)
	if err != nil {
		return
	}
	type skippedPair struct{ key, field, superseded string }
	var pairs []skippedPair
	for rows.Next() {
		var p skippedPair
		if rows.Scan(&p.key, &p.field, &p.superseded) == nil {
			pairs = append(pairs, p)
		}
	}
	rows.Close()
	if len(pairs) == 0 {
		return
	}

	splitOnAnd := func(v string) []string {
		var out []string
		for _, p := range strings.Split(v, " and ") {
			p = strings.TrimSpace(p)
			lc := strings.ToLower(p)
			if lc != "others" && lc != "et.al." && lc != "et al." {
				out = append(out, p)
			}
		}
		return out
	}

	migrated := 0
	for _, p := range pairs {
		winner := l.EntryFieldValueity(p.key, p.field)
		if winner == "" {
			continue
		}
		wNames := splitOnAnd(winner)
		lNames := splitOnAnd(p.superseded)
		if len(wNames) != len(lNames) {
			continue
		}
		var diffPos []int
		for i := range wNames {
			if wNames[i] != lNames[i] {
				diffPos = append(diffPos, i)
			}
		}
		if len(diffPos) != 1 {
			continue
		}
		pos := diffPos[0]
		addNonDoubleContributorNamePair(l, wNames[pos], lNames[pos])
		dbExecSave("maybeMigrateSkippedToNonDoubleContributorNames: mark kept",
			`UPDATE superseded_field_values SET triage_status='kept' WHERE entry_key=? AND field=? AND value=?`, p.key, p.field, p.superseded)
		migrated++
	}
	if migrated > 0 {
		dbInteraction.Progress("Migrated %d skipped triage pairs to non_double_contributor_names.", migrated)
	}
}

// --- dblp_parent table ---

func ensureDblpParentTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS dblp_parent (
		  child_key  TEXT NOT NULL PRIMARY KEY,
		  parent_key TEXT NOT NULL
		);`)
}

// newDblpParentTable returns a write-through cache backed by the dblp_parent SQLite table.
func newDblpParentTable() *TCachedTable[string, string] {
	t := newCachedTable(&TSQLiteTable[string, string]{
		upsertSQL: `INSERT INTO dblp_parent (child_key, parent_key) VALUES (?, ?)
		            ON CONFLICT(child_key) DO UPDATE SET parent_key = excluded.parent_key;`,
		deleteSQL:  `DELETE FROM dblp_parent WHERE child_key = ?`,
		selectSQL:  `SELECT child_key, parent_key FROM dblp_parent`,
		upsertArgs: func(k, v string) []any { return []any{k, v} },
		deleteArgs: func(k string) []any { return []any{k} },
		scanRow: func(rows *sql.Rows) (string, string, error) {
			var child, parent string
			return child, parent, rows.Scan(&child, &parent)
		},
	})
	t.onModify = func() { setTableDate("dblp_parent", time.Now().UnixMicro()) }
	return t
}

// --- dblp_waived table ---

func ensureDblpWaivedTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS dblp_waived (
		  key TEXT NOT NULL PRIMARY KEY
		);`)
}

// newDblpWaivedTable returns a write-through cache backed by the dblp_waived SQLite table.
// The bool value is always true; only key presence matters (set semantics).
func newDblpWaivedTable() *TCachedTable[string, bool] {
	t := newCachedTable(&TSQLiteTable[string, bool]{
		upsertSQL:  `INSERT INTO dblp_waived (key) VALUES (?) ON CONFLICT(key) DO NOTHING;`,
		deleteSQL:  `DELETE FROM dblp_waived WHERE key = ?`,
		selectSQL:  `SELECT key FROM dblp_waived`,
		upsertArgs: func(k string, _ bool) []any { return []any{k} },
		deleteArgs: func(k string) []any { return []any{k} },
		scanRow: func(rows *sql.Rows) (string, bool, error) {
			var key string
			return key, true, rows.Scan(&key)
		},
	})
	t.onModify = func() { setTableDate("dblp_waived", time.Now().UnixMicro()) }
	return t
}

// --- dblp_canonical table ---

func ensureDblpCanonicalTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS dblp_canonical (
		  dblp_key      TEXT NOT NULL PRIMARY KEY,
		  canonical_key TEXT NOT NULL
		    REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
}

// maybeMigrateDblpCanonical populates dblp_canonical from bib_entries on first run.
// Each dblp field value becomes a dblp_key; duplicate dblp values (two entries sharing
// one DBLP key) are silently skipped — they remain detectable via forEachDuplicateDBLPKey
// until resolved.
func maybeMigrateDblpCanonical() {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM dblp_canonical`).Scan(&count) //nolint:errcheck
	if count > 0 {
		return
	}
	var srcCount int
	db.QueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = ?`, DBLPField).Scan(&srcCount) //nolint:errcheck
	if srcCount == 0 {
		return
	}
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO dblp_canonical (dblp_key, canonical_key)
		SELECT value, entry_key FROM bib_entries WHERE field = ?`, DBLPField); err != nil {
		dbInteraction.Warning("dblp_canonical migration failed: %s", err)
		return
	}
	dbInteraction.Progress("Migrated: populated dblp_canonical from %d dblp fields in bib_entries", srcCount)
}

// repairDblpData fixes stale data left by incomplete transactions or aborted runs:
//
//  1. Category-A ghosts — entries that are already in key_oldies (properly merged
//     away) but still carry a stale dblp field in bib_entries. Only the dblp row
//     is removed; other oldie-related rows (key_hints, superseded_field_values, …) stay.
//
//  2. Category-B ghosts — entries with a dblp field but no entrytype and not yet in
//     key_oldies (failed creates). All their rows are removed via CASCADE on
//     bib_entry_keys.
//
//  3. Stale dblp_canonical rows — canonical_key no longer has an entrytype — are
//     deleted.
//
//  4. Missing dblp_canonical rows for live entries are back-filled. When multiple
//     live entries share the same DBLP key, INSERT OR IGNORE keeps the first; the
//     rest are caught by CheckDblpDuplicates during startup checks.
//
// Must run before initEntryCache so the cache is built from clean data.
func repairDblpData() {
	// Category A: remove stale dblp field from properly-merged key_oldies.
	res, err := db.Exec(`
		DELETE FROM bib_entries
		WHERE field = ?
		  AND entry_key IN     (SELECT alias FROM key_oldies)
		  AND entry_key NOT IN (SELECT entry_key FROM bib_entries WHERE field = ?)`,
		DBLPField, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("repairDblpData (A): %s", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Progress("  Repair DBLP data: removed %d stale dblp field(s) from merged key_oldies", n)
	}

	// Category B: remove ghost entries (dblp field only, no entrytype, not in
	// key_oldies) that are redundant — i.e. another live entry already holds the
	// same DBLP key. Ghosts with no live sibling are NOT deleted: doing so would
	// erase the only record of that publication, leaving only orphaned
	// entry_lineage/source_field_signatures rows as posthumous evidence (the same
	// failure mode CheckDuplicateDBLPKeys was fixed for on 2026-07-21). Those are
	// reported instead so they can be repaired via -add_dblp_entry.
	//
	// bib_entries has no foreign-key link to bib_entry_keys (confirmed via schema),
	// so a plain "DELETE FROM bib_entry_keys" — the original Category B, and this
	// function's first fix — leaves the bib_entries ghost rows behind forever: the
	// ghost re-triggers this exact check on every subsequent run, never actually
	// clearing. deleteBibEntry (used by CheckDuplicateDBLPKeys for the same class of
	// row) removes bib_entries/bib_entry_keys/entry_lineage/source_field_signatures/
	// source_contributor_signatures together and is the only complete cleanup path.
	rows, err := db.Query(`
		SELECT g.entry_key, g.value
		FROM bib_entries g
		WHERE g.field = ?
		  AND g.entry_key NOT IN (SELECT entry_key FROM bib_entries WHERE field = ?)
		  AND g.entry_key NOT IN (SELECT alias FROM key_oldies)`,
		DBLPField, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("repairDblpData (B): %s", err)
	} else {
		var safe []string
		type unsafeGhost struct{ key, dblpKey string }
		var unsafe []unsafeGhost
		for rows.Next() {
			var key, dblpKey string
			if err := rows.Scan(&key, &dblpKey); err != nil {
				continue
			}
			var liveSiblings int
			db.QueryRow(`
				SELECT COUNT(*) FROM bib_entries live
				WHERE live.field = ? AND live.value = ? AND live.entry_key != ?
				  AND live.entry_key IN (SELECT entry_key FROM bib_entries WHERE field = ?)`,
				DBLPField, dblpKey, key, EntryTypeField).Scan(&liveSiblings) //nolint:errcheck
			if liveSiblings > 0 {
				safe = append(safe, key)
			} else {
				unsafe = append(unsafe, unsafeGhost{key, dblpKey})
			}
		}
		rows.Close()
		for _, key := range safe {
			deleteBibEntry(key)
		}
		if len(safe) > 0 {
			dbInteraction.Progress("  Repair DBLP data: removed %d redundant ghost entry(ies)", len(safe))
		}
		for _, g := range unsafe {
			dbInteraction.Warning("  Ghost dblp entry %s (dblp=%s) has no live sibling — not deleting; repair with -add_dblp_entry %s",
				g.key, g.dblpKey, g.dblpKey)
		}
	}

	// Remove stale dblp_canonical rows (target no longer exists as a live entry).
	res, err = db.Exec(`
		DELETE FROM dblp_canonical
		WHERE canonical_key NOT IN (SELECT entry_key FROM bib_entries WHERE field = ?)`,
		EntryTypeField)
	if err != nil {
		dbInteraction.Warning("repairDblpData (stale canonical): %s", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Progress("  Repair DBLP data: removed %d stale dblp_canonical rows", n)
	}

	// Back-fill missing dblp_canonical rows from live entries.
	res, err = db.Exec(`
		INSERT OR IGNORE INTO dblp_canonical (dblp_key, canonical_key)
		SELECT be.value, be.entry_key
		FROM bib_entries be
		WHERE be.field = ?
		  AND EXISTS (SELECT 1 FROM bib_entries WHERE entry_key = be.entry_key AND field = ?)`,
		DBLPField, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("repairDblpData (back-fill): %s", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Progress("  Repair DBLP data: back-filled %d missing dblp_canonical rows", n)
	}
}

// upsertDblpCanonical writes (dblpKey → canonicalKey) to dblp_canonical.
// When dblpKey is empty, the call is a no-op.
func upsertDblpCanonical(dblpKey, canonicalKey string) {
	if dblpKey == "" {
		return
	}
	dbExecSave("dblp_canonical upsert",
		`INSERT INTO dblp_canonical (dblp_key, canonical_key) VALUES (?, ?)
		 ON CONFLICT(dblp_key) DO UPDATE SET canonical_key = excluded.canonical_key`,
		dblpKey, canonicalKey)
}

// deleteDblpCanonicalByDblpKey removes the row for dblpKey from dblp_canonical.
func deleteDblpCanonicalByDblpKey(dblpKey string) {
	if dblpKey == "" {
		return
	}
	dbExecSave("dblp_canonical delete by dblp_key",
		`DELETE FROM dblp_canonical WHERE dblp_key = ?`, dblpKey)
}

// deleteDblpCanonicalByCanonicalKey removes all rows for canonicalKey from dblp_canonical.
// Used when an entry's dblp field is cleared (value = "").
func deleteDblpCanonicalByCanonicalKey(canonicalKey string) {
	dbExecSave("dblp_canonical delete by canonical_key",
		`DELETE FROM dblp_canonical WHERE canonical_key = ?`, canonicalKey)
}

// LookupDblpCanonical returns the canonical library key for a DBLP key by querying
// dblp_canonical directly (bypasses the transient KeyOldies alias).
func LookupDblpCanonical(dblpKey string) string {
	var canonical string
	db.QueryRow(`SELECT canonical_key FROM dblp_canonical WHERE dblp_key = ?`, dblpKey).Scan(&canonical) //nolint:errcheck
	return canonical
}

// --- entry_flags → entry_metadata (v23.4 merge) ---
//
// entry_flags is now stored inside entry_metadata with value = 'true'.
// knownEntryFlags lists every flag property name so load/save can target only
// those rows and leave unrelated metadata rows untouched.

func knownEntryFlags() []string {
	return []string{EntryFlagNoDBLPChildren, FlagLoneProceedingsWaived}
}

// maybeConsolidateEntryFlags migrates the legacy entry_flags table into
// entry_metadata (value = 'true'), then drops the old table.
func maybeConsolidateEntryFlags() {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='entry_flags'`).Scan(&n)
	if n == 0 {
		return
	}
	rows, err := db.Query(`SELECT entry_key, flag FROM entry_flags`)
	if err == nil {
		upsert := `INSERT OR IGNORE INTO entry_metadata (entry_key, property, value) VALUES (?, ?, 'true')`
		for rows.Next() {
			var key, flag string
			if rows.Scan(&key, &flag) == nil {
				db.Exec(upsert, key, flag)
			}
		}
		rows.Close()
	}
	db.Exec(`DROP TABLE entry_flags`)
	db.Exec(`DELETE FROM table_modification_times WHERE table_name = 'entry_flags'`)
	dbInteraction.Progress("Merged entry_flags into entry_metadata")
}

func loadEntryFlagsFromDb(l *TBibTeXLibrary) {
	flags := knownEntryFlags()
	placeholders := strings.Repeat("?,", len(flags))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(flags))
	for i, f := range flags {
		args[i] = f
	}
	rows, err := db.Query(
		`SELECT entry_key, property FROM entry_metadata WHERE value = 'true' AND property IN (`+placeholders+`)`,
		args...)
	if err != nil {
		dbInteraction.Warning("Could not query entry_flags from entry_metadata: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, flag string
		if err := rows.Scan(&key, &flag); err != nil {
			continue
		}
		if _, ok := l.EntryFlags[key]; !ok {
			l.EntryFlags[key] = TStringSetNew()
		}
		l.EntryFlags[key].Set().Add(flag)
	}
}

func saveEntryFlagsToDb(l *TBibTeXLibrary) {
	flags := knownEntryFlags()
	placeholders := strings.Repeat("?,", len(flags))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(flags))
	for i, f := range flags {
		args[i] = f
	}
	db.Exec(`DELETE FROM entry_metadata WHERE value = 'true' AND property IN (`+placeholders+`)`, args...)
	upsert := `INSERT OR IGNORE INTO entry_metadata (entry_key, property, value) VALUES (?, ?, 'true')`
	for key, flagSet := range l.EntryFlags {
		for flag := range flagSet.Elements() {
			if _, err := db.Exec(upsert, key, flag); err != nil {
				dbInteraction.Warning("entry_flags save to entry_metadata failed: %s", err)
			}
		}
	}
}

// --- cross_field_mappings table (legacy; kept for existing DBs, no longer primary load path) ---

func ensureCrossFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS cross_field_mappings (
		  source_field TEXT NOT NULL,
		  source_value TEXT NOT NULL,
		  target_field TEXT NOT NULL,
		  target_value TEXT NOT NULL,
		  PRIMARY KEY (source_field, source_value, target_field)
		);`)
}

// loadAuthorEditorFieldMappingsFromCache loads author/editor entry-field alias
// mappings from superseded_field_values using the in-memory entry cache to find the
// winner. Must be called after initEntryCache() and only when contributorRolesActive.
// After the contributor_roles migration, author/editor fields are no longer in
// bib_entries, so loadEntryFieldMappingsFromDb's JOIN misses them; this is the
// second-pass fix.
func loadAuthorEditorFieldMappingsFromCache(l *TBibTeXLibrary) {
	if entryCache == nil {
		return
	}
	entryFieldMappingsLoading = true
	defer func() { entryFieldMappingsLoading = false }()

	rows, err := db.Query(`
		SELECT entry_key, field, value FROM superseded_field_values
		WHERE field IN ('author', 'editor')`)
	if err != nil {
		dbInteraction.Warning("Could not query superseded_field_values for author/editor: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, field, superseded string
		if err := rows.Scan(&key, &field, &superseded); err != nil {
			continue
		}
		e, ok := entryCache[key]
		if !ok {
			continue
		}
		winner := e.Fields[field]
		if winner == "" {
			continue
		}
		normWinner := l.MapFieldValue(field, l.NormaliseFieldValue(field, winner))
		// Apply NormaliseFieldValue to the stored superseded value so that name-mapping
		// changes made after it was first recorded don't produce a stale key that no
		// longer matches the incoming DBLP challenge (which is also normalised).
		normSuperseded := l.MapFieldValue(field, l.NormaliseFieldValue(field, superseded))
		// If the superseded value is already registered with a stale winner (e.g. from a
		// loadEntryFieldMappingsFromDb pass that ran before migration deleted the
		// bib_entries author row), cascade the update from the old winner to the
		// new one so that all aliases pointing to the old winner get fixed too.
		if existingWinner, alreadyMapped := l.EntryFieldSourceToTarget[key][field][normSuperseded]; alreadyMapped && existingWinner != normWinner {
			l.UpdateEntryFieldAlias(key, field, existingWinner, normWinner)
		} else {
			l.AddEntryFieldAlias(key, field, normSuperseded, normWinner, true)
		}
	}
}

// loadEntryFieldMappingsFromDb populates l.EntryFieldSourceToTarget from the DB.
// The effective winner is COALESCE(lfv.winner, bib_entries.value): the stored winner
// column (set when the user makes a decision) takes precedence over bib_entries, which
// may not have been updated if closeEntry was skipped. Rows where superseded == winner
// are self-maps and are excluded by the query. Author/editor fields with contributor_roles
// are handled separately by loadAuthorEditorFieldMappingsFromCache.
// Returns true when at least one value was remapped by MapFieldValue, so the caller
// can arrange a write-back.
func loadEntryFieldMappingsFromDb(l *TBibTeXLibrary) bool {
	entryFieldMappingsLoading = true
	defer func() { entryFieldMappingsLoading = false }()

	// Prefer the stored winner column (written when the user makes a decision) over
	// the bib_entries value (which may not have been updated if closeEntry was skipped).
	// LEFT JOIN so rows with a stored winner but no bib_entries row (e.g. after a delete)
	// are still usable. Rows where superseded == effective winner are self-maps and are filtered.
	rows, err := db.Query(`
		SELECT lfv.entry_key, lfv.field, lfv.value AS superseded,
		       COALESCE(lfv.winner, be.value) AS winner
		FROM superseded_field_values lfv
		LEFT JOIN bib_entries be ON be.entry_key = lfv.entry_key AND be.field = lfv.field
		WHERE COALESCE(lfv.winner, be.value) IS NOT NULL
		  AND COALESCE(lfv.winner, be.value) != lfv.value`)
	if err != nil {
		dbInteraction.Warning("Could not query superseded_field_values: %s", err)
		return false
	}
	defer rows.Close()

	normalisationChanged := false
	for rows.Next() {
		var key, field, superseded, winner string
		if err := rows.Scan(&key, &field, &superseded, &winner); err != nil {
			dbInteraction.Warning("Could not scan superseded_field_values row: %s", err)
			continue
		}
		// Apply the same pipeline that ResolveFieldValue uses at runtime: NormaliseFieldValue
		// first (which for author/editor resolves current name aliases via NameAliasToName),
		// then MapFieldValue. Without the normalise step, a stored superseded value like
		// "Zhang, Wei and ..." would stop matching after "Zhang, Wei" acquires a name alias,
		// causing the question to recur on every subsequent run.
		normWinner := l.MapFieldValue(field, l.NormaliseFieldValue(field, winner))
		normSuperseded := l.MapFieldValue(field, l.NormaliseFieldValue(field, superseded))
		if normWinner != winner || normSuperseded != superseded {
			normalisationChanged = true
		}
		l.AddEntryFieldAlias(key, field, normSuperseded, normWinner, true)
	}
	return normalisationChanged
}

// saveEntryFieldMappingsToDb writes the losing field values to the DB without a file roundtrip.
func saveEntryFieldMappingsToDb(l *TBibTeXLibrary) {
	dbExecSave("Could not clear superseded_field_values", `DELETE FROM superseded_field_values`)
	upsert := `INSERT INTO superseded_field_values (entry_key, field, value, winner) VALUES (?, ?, ?, ?)`
	for key, fieldChallenges := range l.EntryFieldSourceToTarget {
		if l.EntryExists(key) {
			for field, challenges := range fieldChallenges {
				if field != PreferredAliasField {
					for challenger, winner := range challenges {
						if l.MapFieldValue(field, challenger) != l.MapEntryFieldValue(key, field, winner) {
							dbExecSave("superseded_field_values insert failed", upsert, key, field, challenger, winner)
						}
					}
				}
			}
		}
	}
	setTableDate("superseded_field_values", time.Now().UnixMicro())
}

// --- generic_field_mappings table (legacy; kept for existing DBs, no longer primary load path) ---

func ensureGenericFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS generic_field_mappings (
		  field      TEXT NOT NULL,
		  winner     TEXT NOT NULL,
		  challenger TEXT NOT NULL,
		  PRIMARY KEY (field, challenger)
		);`)
}

// --- field_mappings table (unified generic + cross-field) ---

func ensureFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS field_mappings (
		  source_field TEXT NOT NULL,
		  source_value TEXT NOT NULL,
		  target_field TEXT NOT NULL,
		  target_value TEXT NOT NULL,
		  PRIMARY KEY (source_field, source_value, target_field)
		);`)
}

// loadFieldMappingsFromDb populates both l.GenericFieldSourceToTarget and l.FieldMappings
// from the unified field_mappings table. Rows with source_field == target_field are generic;
// others are cross-field. Returns true when normalisation changed any stored value.
func loadFieldMappingsFromDb(l *TBibTeXLibrary) bool {
	fieldMappingsLoading = true
	defer func() { fieldMappingsLoading = false }()

	rows, err := db.Query(`SELECT source_field, source_value, target_field, target_value FROM field_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query field_mappings: %s", err)
		return false
	}
	defer rows.Close()

	normalisationChanged := false
	for rows.Next() {
		var sourceField, sourceValue, targetField, targetValue string
		if err := rows.Scan(&sourceField, &sourceValue, &targetField, &targetValue); err != nil {
			dbInteraction.Warning("Could not scan field_mappings row: %s", err)
			continue
		}
		if sourceField == targetField {
			normSource := l.MapFieldValue(sourceField, sourceValue)
			normTarget := l.MapFieldValue(targetField, targetValue)
			if normSource != sourceValue || normTarget != targetValue {
				normalisationChanged = true
			}
			l.AddGenericFieldAlias(sourceField, normSource, normTarget, true)
		} else {
			// For entry-type-qualified source fields (e.g. "author:techreport"), use
			// only the field part (left of ":") for source-value normalisation.
			normField, _, _ := strings.Cut(sourceField, ":")
			normSource := l.MapFieldValue(normField, sourceValue)
			normTarget := l.NormaliseFieldValue(targetField, targetValue)
			if normSource != sourceValue || normTarget != targetValue {
				normalisationChanged = true
			}
			l.AddFieldMapping(sourceField, normSource, targetField, normTarget)
		}
	}
	return normalisationChanged
}

// --- urls_ignore table ---
// Lives at FilesRoot level (no BaseName prefix); migrates from legacy urls.ignore on first use.

func ensureURLsIgnoreTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS urls_ignore (
		  url TEXT PRIMARY KEY
		);`)
}

func loadURLsIgnoreFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT url FROM urls_ignore`)
	if err != nil {
		dbInteraction.Warning("Could not query urls_ignore: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			dbInteraction.Warning("Could not scan urls_ignore row: %s", err)
			continue
		}
		l.URLsIgnore.Add(url)
	}
}

// --- ignore_titles table ---

func ensureIgnoreTitlesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS ignore_titles (
		  title TEXT PRIMARY KEY
		);`)
}

// maybeBootstrapIgnoreTitlesTable inserts the built-in seed titles when the
// table is empty. Runs once on first use; does nothing when rows already exist.
func maybeBootstrapIgnoreTitlesTable() {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM ignore_titles`).Scan(&count) //nolint:errcheck
	if count > 0 {
		return
	}
	for _, t := range []string{"Preface", "Introduction", "Conclusion"} {
		dbExecSave("ignore_titles bootstrap",
			`INSERT OR IGNORE INTO ignore_titles (title) VALUES (?)`, t)
	}
}

func loadIgnoreTitlesFromDb(l *TBibTeXLibrary) {
	l.IgnoredTitleIndexes = TStringSetNew()
	rows, err := db.Query(`SELECT title FROM ignore_titles`)
	if err != nil {
		dbInteraction.Warning("Could not query ignore_titles: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			dbInteraction.Warning("Could not scan ignore_titles row: %s", err)
			continue
		}
		l.IgnoredTitleIndexes.Add(TeXStringIndexer(title))
	}
}

// cleanupIgnoredTitleNonDoubles removes pairs from non_double_entries where
// both entries have titles in l.IgnoredTitleIndexes. Called after the entry
// cache is fully loaded so title lookups are available.
func cleanupIgnoredTitleNonDoubles(l *TBibTeXLibrary) {
	if l.IgnoredTitleIndexes.Size() == 0 {
		return
	}
	rows, err := db.Query(`SELECT key1, key2 FROM non_double_entries`)
	if err != nil {
		return
	}
	type pair struct{ k1, k2 string }
	var toDrop []pair
	for rows.Next() {
		var k1, k2 string
		if rows.Scan(&k1, &k2) != nil {
			continue
		}
		t1 := TeXStringIndexer(l.EntryFieldValueity(l.MapEntryKey(k1), TitleField))
		t2 := TeXStringIndexer(l.EntryFieldValueity(l.MapEntryKey(k2), TitleField))
		if t1 != "" && t2 != "" &&
			l.IgnoredTitleIndexes.Contains(t1) &&
			l.IgnoredTitleIndexes.Contains(t2) {
			toDrop = append(toDrop, pair{k1, k2})
		}
	}
	rows.Close()
	for _, p := range toDrop {
		dbExecSave("cleanupIgnoredTitleNonDoubles", `DELETE FROM non_double_entries WHERE key1 = ? AND key2 = ?`, p.k1, p.k2)
		dbExecSave("cleanupIgnoredTitleNonDoubles", `DELETE FROM non_double_entries WHERE key1 = ? AND key2 = ?`, p.k2, p.k1)
		l.NonDoubleEntries.DeleteValueFromStringSetMap(p.k1, p.k2)
		l.NonDoubleEntries.DeleteValueFromStringSetMap(p.k2, p.k1)
		l.Progress("Retired non-double pair (%s, %s): both have ignored titles", p.k1, p.k2)
	}
}

// --- entry_metadata table ---

func ensureEntryMetadataTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS entry_metadata (
		  entry_key TEXT NOT NULL,
		  property  TEXT NOT NULL,
		  value     TEXT NOT NULL,
		  PRIMARY KEY (entry_key, property),
		  FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
}
func loadEntryMetadataFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT entry_key, property, value FROM entry_metadata`)
	if err != nil {
		dbInteraction.Warning("Could not query entry_metadata: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, prop, val string
		if err := rows.Scan(&key, &prop, &val); err != nil {
			dbInteraction.Warning("Could not scan entry_metadata row: %s", err)
			continue
		}
		if l.Metadata[key] == nil {
			l.Metadata[key] = map[string]string{}
		}
		l.Metadata[key][prop] = val
	}
}

func saveEntryMetadataToDb(l *TBibTeXLibrary) {
	// All individual writes are already in the DB via write-through in SetMetadata
	// and DeleteMetadata.  This call only needs to update the table timestamp so
	// that the JSON re-import logic does not re-migrate old data on the next run.
	setTableDate("entry_metadata", time.Now().UnixMicro())
}

// --- entry_lineage and source signature tables ---

func ensureEntryLineageTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS entry_lineage (
		  entry_key  TEXT NOT NULL,
		  field      TEXT NOT NULL,
		  value      TEXT NOT NULL DEFAULT '',
		  source     TEXT NOT NULL DEFAULT '',
		  edited     INTEGER NOT NULL DEFAULT 0,
		  PRIMARY KEY (entry_key, field)
		);`)
	maybeMigrateEntryLineageValue()
}

// maybeMigrateEntryLineageValue adds the value column to entry_lineage for databases
// created before lineage became a property of the (field, value) pair rather than
// just the field — see PROJECT.md §18.2.2. Runs unconditionally; ALTER TABLE ADD
// COLUMN fails harmlessly with "duplicate column name" on every run after the first,
// which doubles as the signal that backfill has already happened.
//
// Backfill is best-effort: each row's value is set to bib_entries' current content
// for that (entry_key, field) — the closest available guess, since the old schema
// never recorded which value a lineage record was actually describing. Rows that
// guess wrong (or blank, e.g. author/editor fields under contributor_roles, which
// aren't in bib_entries at all) simply self-invalidate on first read via getLineage's
// value check and fall back to "unknown" rather than trusting stale data — exactly
// the failure mode this migration exists to close off going forward.
func maybeMigrateEntryLineageValue() {
	if _, err := db.Exec(`ALTER TABLE entry_lineage ADD COLUMN value TEXT NOT NULL DEFAULT ''`); err != nil {
		return
	}
	dbExecSave("maybeMigrateEntryLineageValue: backfill",
		`UPDATE entry_lineage SET value = COALESCE(
		   (SELECT b.value FROM bib_entries b WHERE b.entry_key = entry_lineage.entry_key AND b.field = entry_lineage.field),
		   '')`)
}

func ensureSourceFieldSignaturesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS source_field_signatures (
		  entry_key  TEXT NOT NULL,
		  field      TEXT NOT NULL,
		  source     TEXT NOT NULL,
		  signature  TEXT NOT NULL DEFAULT '',
		  PRIMARY KEY (entry_key, field, source)
		);`)
}

func ensureSourceContributorSignaturesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS source_contributor_signatures (
		  entry_key  TEXT NOT NULL,
		  role       TEXT NOT NULL,
		  position   INTEGER NOT NULL,
		  source     TEXT NOT NULL,
		  signature  TEXT NOT NULL DEFAULT '',
		  PRIMARY KEY (entry_key, role, position, source)
		);`)
}

func loadEntryLineageFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT entry_key, field, value, source, edited FROM entry_lineage`)
	if err != nil {
		dbInteraction.Warning("Could not query entry_lineage: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, field, value, source string
		var edited int
		if err := rows.Scan(&key, &field, &value, &source, &edited); err != nil {
			dbInteraction.Warning("Could not scan entry_lineage row: %s", err)
			continue
		}
		if _, ok := l.LineageMap[key]; !ok {
			l.LineageMap[key] = map[string]TLineageRecord{}
		}
		l.LineageMap[key][field] = TLineageRecord{Value: value, Source: source, Edited: edited != 0}
	}
}

func loadSourceFieldSignaturesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT entry_key, field, source, signature FROM source_field_signatures`)
	if err != nil {
		dbInteraction.Warning("Could not query source_field_signatures: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, field, source, sig string
		if err := rows.Scan(&key, &field, &source, &sig); err != nil {
			dbInteraction.Warning("Could not scan source_field_signatures row: %s", err)
			continue
		}
		if _, ok := l.SourceSignatures[key]; !ok {
			l.SourceSignatures[key] = map[string]map[string]string{}
		}
		if _, ok := l.SourceSignatures[key][field]; !ok {
			l.SourceSignatures[key][field] = map[string]string{}
		}
		l.SourceSignatures[key][field][source] = sig
	}
}

// --- superseded_field_values table (formerly losing_field_values) ---

func ensureSupersededFieldValuesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS superseded_field_values (
		  entry_key     TEXT NOT NULL,
		  field         TEXT NOT NULL,
		  value         TEXT NOT NULL,
		  triage_status TEXT,
		  PRIMARY KEY (entry_key, field, value),
		  FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
	// Add triage_status to databases created before this column existed.
	db.Exec(`ALTER TABLE superseded_field_values ADD COLUMN triage_status TEXT`) //nolint:errcheck
	// winner stores the accepted value at decision time so the mapping survives
	// even when bib_entries is not immediately updated (e.g. due to transaction
	// ordering or cache inconsistency). COALESCE(winner, bib_entries.value) in
	// loadEntryFieldMappingsFromDb prefers this stored winner.
	db.Exec(`ALTER TABLE superseded_field_values ADD COLUMN winner TEXT`) //nolint:errcheck
}

// cleanupRedundantSuperseded removes superseded_field_values rows whose value is identical
// to the current winner in bib_entries. These arise when a field is later set back
// to a value that was previously recorded as superseded (e.g. after DBLP re-import
// agrees with the library value).
func cleanupRedundantSuperseded() {
	// A row is redundant only when superseded value == bib_entries value AND no different
	// winner is stored. Rows where winner IS NOT NULL AND winner != value record a real
	// user decision (bib_entries was not updated yet) and must be kept.
	res, err := db.Exec(`
		DELETE FROM superseded_field_values
		WHERE EXISTS (
		    SELECT 1 FROM bib_entries
		    WHERE bib_entries.entry_key = superseded_field_values.entry_key
		      AND bib_entries.field     = superseded_field_values.field
		      AND bib_entries.value     = superseded_field_values.value
		)
		AND (winner IS NULL OR winner = value)`)
	if err != nil {
		dbInteraction.Warning("cleanupRedundantSuperseded: %s", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Progress("Removed %d redundant superseded value(s) (value matches current winner)", n)
	}
}

// --- entry_warnings table ---

func ensureEntryWarningsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS entry_warnings (
		  key     TEXT NOT NULL,
		  warning TEXT NOT NULL DEFAULT '',
		  UNIQUE(key, warning),
		  FOREIGN KEY (key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
}

// clearEntryWarnings deletes all rows — called once at the start of each normal check run.
func clearEntryWarnings() {
	dbExecSave("clearEntryWarnings", `DELETE FROM entry_warnings`)
}

// deleteEntryWarning removes a specific (key, warning) row, e.g. when a warning
// is subsequently waived and should not appear in repair.bib or warnings; selects.
func deleteEntryWarning(key, warning string) {
	dbExecSave("deleteEntryWarning", `DELETE FROM entry_warnings WHERE key = ? AND warning = ?`, key, warning)
}

// insertEntryWarning records key+warning, silently ignoring exact duplicates.
// entry_warnings.key has an FK on bib_entry_keys, and this can be called for a key
// whose bib_entry_keys row was written earlier in the *same* transaction (or, per
// the FK incidents this session, momentarily invalidated by a mid-transaction
// crossref/merge reentrancy this call has no visibility into) — guarantee the
// anchor row first rather than rely on it already being there.
func insertEntryWarning(key, warning string) {
	if err := bibExec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key) VALUES (?)`, key); err != nil {
		dbWriteFailed = true
		return
	}
	dbExecSave("insertEntryWarning", `INSERT OR IGNORE INTO entry_warnings (key, warning) VALUES (?, ?)`, key, warning)
}

// entryWarningTexts returns all non-empty warning strings for key, sorted alphabetically.
func entryWarningTexts(key string) []string {
	rows, err := db.Query(`SELECT warning FROM entry_warnings WHERE key = ? AND warning != '' ORDER BY warning`, key)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ws []string
	for rows.Next() {
		var w string
		rows.Scan(&w) //nolint:errcheck
		ws = append(ws, w)
	}
	return ws
}

// forEachDuplicateDBLPKey calls fn for every DBLP value shared by two or more library
// entries, passing the raw dblp field value and the slice of affected canonical keys.
// New duplicates are prevented by the PRIMARY KEY on dblp_canonical; this function
// finds legacy conflicts that pre-date the constraint and were skipped during migration.
func forEachDuplicateDBLPKey(fn func(dblpKey string, keys []string)) {
	rows, err := db.Query(`
		SELECT value, GROUP_CONCAT(entry_key, '|')
		FROM bib_entries
		WHERE field = 'dblp'
		GROUP BY value
		HAVING COUNT(*) > 1
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var dblpKey, concat string
		if rows.Scan(&dblpKey, &concat) == nil {
			fn(dblpKey, strings.Split(concat, "|"))
		}
	}
}

// allEntryWarningKeys returns all distinct keys present in entry_warnings (any warning text).
func allEntryWarningKeys() []string {
	rows, err := db.Query(`SELECT DISTINCT key FROM entry_warnings ORDER BY key`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		rows.Scan(&k) //nolint:errcheck
		keys = append(keys, k)
	}
	return keys
}

// --- shorten_mappings table ---

func ensureShortenMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS shorten_mappings (
		  field     TEXT NOT NULL,
		  original  TEXT NOT NULL,
		  shortened TEXT NOT NULL,
		  PRIMARY KEY (field, original)
		);`)
}

// loadShortenMappingsFromDb loads the shorten_mappings table into a TShortenMappings map.
func loadShortenMappingsFromDb() TShortenMappings {
	result := TShortenMappings{}
	rows, err := db.Query(`SELECT field, original, shortened FROM shorten_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query shorten_mappings: %s", err)
		return result
	}
	defer rows.Close()
	for rows.Next() {
		var field, original, shortened string
		if err := rows.Scan(&field, &original, &shortened); err != nil {
			dbInteraction.Warning("Could not scan shorten_mappings row: %s", err)
			continue
		}
		result[field] = append(result[field], [2]string{original, shortened})
	}
	return result
}

// --- bib_entries / bib_groups / bib_comments tables ---

func ensureBibEntryKeysTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_entry_keys (
		  entry_key TEXT PRIMARY KEY
		);`)
}

func ensureBibTablesExist() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_entries (
		  entry_key TEXT NOT NULL,
		  field     TEXT NOT NULL,
		  value     TEXT NOT NULL,
		  PRIMARY KEY (entry_key, field)
		);`)
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_groups (
		  group_name TEXT NOT NULL,
		  entry_key  TEXT NOT NULL,
		  PRIMARY KEY (group_name, entry_key),
		  FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS bib_comments (
		  position INTEGER PRIMARY KEY,
		  content  TEXT NOT NULL
		);`)
}

// --- bib entry write primitives ---

// activeTx, when non-nil, is used by bibExec so that parse and check passes can
// batch their writes in a single transaction.
var activeTx *sql.Tx

// bibExec executes a write statement using the active transaction if one is in
// progress, or directly on the DB otherwise.
// sqliteIndexCorrupt reports whether err is a SQLite SQLITE_CORRUPT (11) error,
// which typically indicates a damaged B-tree index that REINDEX can repair.
func sqliteIndexCorrupt(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is malformed")
}

// rebuildDone guards against repeated full-rebuild attempts within one session.
var rebuildDone bool

// repairCorruptDatabase rebuilds the working database by writing a clean copy
// via VACUUM INTO and reopening the connection. This resets both the on-disk
// file and the in-process page cache, which in-place VACUUM does not do.
func repairCorruptDatabase() error {
	tmpPath := dbPath() + ".repair.db"
	os.Remove(tmpPath)
	if _, err := db.Exec(`VACUUM INTO ?`, tmpPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	working := dbPath()
	db.Close()
	db = nil
	// The old WAL and SHM files belong to the corrupt database. Remove them
	// before installing the clean copy so SQLite does not try to apply the
	// old (corrupt) WAL pages to the new database, which would produce
	// another SQLITE_CORRUPT immediately after reopening.
	os.Remove(working + "-wal")
	os.Remove(working + "-shm")
	if err := os.Rename(tmpPath, working); err != nil {
		if copyErr := copyFile(tmpPath, working); copyErr != nil {
			os.Remove(tmpPath)
			return copyErr
		}
		os.Remove(tmpPath)
	}
	reopenDb(working)
	// Old multi-connection writes can leave duplicate (entry_key, field) rows in
	// bib_entries, violating its UNIQUE constraint. Remove extras before REINDEX,
	// keeping the lowest-rowid survivor for each pair.
	if _, dedupeErr := db.Exec(
		`DELETE FROM bib_entries WHERE rowid NOT IN (
		     SELECT MIN(rowid) FROM bib_entries GROUP BY entry_key, field
		 )`); dedupeErr != nil {
		dbInteraction.Warning("Could not deduplicate bib_entries: %s", dedupeErr)
	}
	// Rebuild all indexes from the now-clean table data.
	if _, reindexErr := db.Exec(`REINDEX`); reindexErr != nil {
		dbInteraction.Warning("REINDEX after rebuild failed: %s", reindexErr)
	}
	return nil
}

func bibExec(query string, args ...any) error {
	if activeTx != nil {
		_, err := activeTx.Exec(query, args...)
		return err
	}
	_, err := db.Exec(query, args...)
	if err != nil && !rebuildDone && sqliteIndexCorrupt(err) {
		rebuildDone = true
		dbInteraction.Warning("SQLite corruption detected — rebuilding database; this may take a while")
		if repairErr := repairCorruptDatabase(); repairErr != nil {
			dbInteraction.Warning("Database rebuild failed — corruption may persist: %s", repairErr)
		}
		_, err = db.Exec(query, args...)
	}
	return err
}

// bibQuery executes a read query using the active transaction if one is in
// progress, or directly on the DB otherwise.  Callers must close the returned
// Rows.
func bibQuery(query string, args ...any) (*sql.Rows, error) {
	if activeTx != nil {
		return activeTx.Query(query, args...)
	}
	return db.Query(query, args...)
}

// bibQueryRow executes a single-row read using the active transaction if one
// is in progress, or directly on the DB otherwise.
func bibQueryRow(query string, args ...any) *sql.Row {
	if activeTx != nil {
		return activeTx.QueryRow(query, args...)
	}
	return db.QueryRow(query, args...)
}

var txDepth int

func beginBibTransaction() {
	txDepth++
	if txDepth > 1 {
		return
	}
	var err error
	activeTx, err = db.Begin()
	if err != nil {
		dbInteraction.Error("Could not begin bib transaction: %s", err)
		activeTx = nil
		txDepth = 0
	}
}

func commitBibTransaction() {
	if txDepth == 0 {
		return
	}
	txDepth--
	if txDepth > 0 {
		return
	}
	if activeTx != nil {
		if err := activeTx.Commit(); err != nil {
			dbInteraction.Error("Could not commit bib transaction: %s", err)
		}
		activeTx = nil
	}
}

func rollbackBibTransaction() {
	txDepth = 0
	if activeTx != nil {
		activeTx.Rollback()
		activeTx = nil
	}
}

// forceCommitBibTransaction commits and clears any open bib transaction regardless of
// nesting depth. Called during graceful quit so that writes made earlier in the run are
// not rolled back when the database connection is closed.
func forceCommitBibTransaction() {
	if activeTx == nil {
		return
	}
	if err := activeTx.Commit(); err != nil {
		dbInteraction.Warning("Could not commit bib transaction on quit: %s", err)
	}
	activeTx = nil
	txDepth = 0
}

// openEntry snapshots entry.Fields so that subsequent setEntryField / deleteEntryField
// calls only update the in-memory struct (and the cache when active) without issuing any
// DB writes. closeEntry then diffs the snapshot against the current fields and writes only
// the changed fields to the DB.
// When the cache is active and entry is not yet in it, openEntry registers it so that
// future loadEntryFromDb calls for the same key return the same pointer.
func openEntry(entry *TBibTeXEntry) {
	if entrySnapshots == nil {
		entrySnapshots = map[string]map[string]string{}
	}
	if entryCache != nil {
		if _, ok := entryCache[entry.Key]; !ok {
			entryCache[entry.Key] = entry
		}
	}
	snapshot := map[string]string{}
	for k, v := range entry.Fields {
		snapshot[k] = v
	}
	entrySnapshots[entry.Key] = snapshot
}

// closeEntry diffs the snapshot taken by openEntry against the current entry.Fields
// and writes only the changed, added, or deleted fields directly to the DB (bypassing
// upsertBibEntryField so that the cache — already correct — is not re-evaluated).
// Returns true when at least one field differed; the caller is responsible for setting
// bibEntriesModified when appropriate.
func closeEntry(entry *TBibTeXEntry) bool {
	snapshot, open := entrySnapshots[entry.Key]
	if !open {
		return false
	}
	delete(entrySnapshots, entry.Key)

	changed := false

	for field, value := range entry.Fields {
		if snapshot[field] != value {
			changed = true
			if field == "crossref" && value == entry.Key {
				// See the matching guard in upsertBibEntryField — an entry must never
				// crossref itself. Also clear it from the in-memory struct (not just
				// skip the DB write), or entryCache — which may hold this same *entry
				// by reference — keeps the bad value even though the DB correctly
				// rejects it.
				dbInteraction.Warning("Refusing to set crossref=%s on itself for entry %s", value, entry.Key)
				delete(entry.Fields, field)
				continue
			}
			if contributorRolesActive && (field == "author" || field == "editor") {
				if value != "" {
					// Same reasoning as upsertBibEntryField's matching guard: this
					// bypasses that function entirely (see the doc comment above), so
					// it needs its own copy of the bib_entry_keys anchor insert.
					// Without it, a brand-new entry whose author/editor field happens
					// to be diffed before any other field — the common case for
					// MergeInMemoryDBLPEntry, used by -watch to add new entries — hits
					// a FOREIGN KEY violation on every contributor_roles insert.
					if err2 := bibExec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key) VALUES (?)`, entry.Key); err2 != nil {
						dbWriteFailed = true
						continue
					}
				}
				upsertContributorRolesForField(&Library, entry.Key, field, value)
			} else {
				var err error
				if value == "" {
					err = bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, entry.Key, field)
				} else {
					err = bibExec(
						`INSERT INTO bib_entries (entry_key, field, value) VALUES (?, ?, ?)
						   ON CONFLICT(entry_key, field) DO UPDATE SET value = excluded.value;`,
						entry.Key, field, value)
				}
				if err != nil {
					dbInteraction.Warning("bib_entries write failed for %s.%s: %s", entry.Key, field, err)
					dbWriteFailed = true
				}
			}
		}
	}

	for field := range snapshot {
		if _, exists := entry.Fields[field]; !exists {
			changed = true
			if contributorRolesActive && (field == "author" || field == "editor") {
				upsertContributorRolesForField(&Library, entry.Key, field, "")
			} else {
				if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, entry.Key, field); err != nil {
					dbInteraction.Warning("bib_entries delete failed for %s.%s: %s", entry.Key, field, err)
					dbWriteFailed = true
				}
			}
		}
	}

	// closeEntry's callers (MergeInMemoryDBLPEntry et al.) track bibEntriesModified
	// themselves from this return value, so data is saved correctly either way — but
	// this bypasses markBibEntryModified entirely, so the *other* trackers keyed off
	// it (c2EntryModified, driving -update_all_dblp_entries's "N updated" count;
	// entryModified) never fire for DBLP-merge writes, undercounting them as 0
	// regardless of how much actually changed. Fire it here once, at the one place
	// that already knows definitively whether this entry changed.
	if changed {
		markBibEntryModified()
	}

	return changed
}

// clearBibTables removes all rows from the three bib tables without dropping them.
func clearBibTables() {
	for _, stmt := range []string{
		`DELETE FROM bib_entries;`,
		`DELETE FROM bib_groups;`,
		`DELETE FROM bib_comments;`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			dbInteraction.Warning("Could not clear bib table: %s", err)
		}
	}
	entryCache = nil
	entrySnapshots = nil
	bibEntriesModified = false
}

// markBibEntryModified sets bibEntriesModified and signals active trackers.
// Also marks the bib_entries SQLite table dirty so a crash-recovery write can
// restore the bib file on the next startup before any command runs.
func markBibEntryModified() {
	bibEntriesModified = true
	if c2TrackingActive {
		c2EntryModified = true
	}
	if entryModTrackingActive {
		entryModified = true
	}
	setTableDirty("bib_entries")
}

// startC2Tracking arms the per-call C2 modification detector.
func startC2Tracking() {
	c2TrackingActive = true
	c2EntryModified = false
	c2ChildrenChecked = 0
}

// stopC2Tracking disarms C2 tracking and returns whether any entry was modified,
// plus how many DBLP children were inspected while checking it (see CheckDBLP's
// "Checking N children of X" loop) — that work is real but happens on keys the
// caller's own scan loop never visits directly, so it must be reported back
// explicitly rather than assumed to show up as separate top-level scans.
func stopC2Tracking() (modified bool, childrenChecked int) {
	c2TrackingActive = false
	return c2EntryModified, c2ChildrenChecked
}

// startEntryTracking arms the per-entry modification detector (across all check classes).
func startEntryTracking() {
	entryModTrackingActive = true
	entryModified = false
}

// stopEntryTracking disarms per-entry tracking and returns whether the entry was modified.
func stopEntryTracking() bool {
	entryModTrackingActive = false
	return entryModified
}

// writeRepairCSV writes sorted lines to filePath, backing up the old file first.
// Returns false if the file cannot be created.
func writeRepairCSV(filePath string, lines []string) bool {
	BackupFile(filePath)
	f, err := os.Create(filePath)
	if err != nil {
		dbInteraction.Warning("Repair write failed for %s: %s", filePath, err)
		return false
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	sort.Strings(lines)
	for _, line := range lines {
		w.WriteString(line + "\n")
	}
	w.Flush()
	return true
}

// repairDirtyMappingTables writes any mapping table whose dirty bit is set from
// the current SQLite state back to its CSV file. Called from initialiseLibrary
// before any file reads so that loadMappingFiles picks up repaired files.
// Returns whether entry_field_mappings was written (caller may apply cascade re-read).
func repairDirtyMappingTables() (entryFieldMappingsRepaired bool) {
	base := bibTeXFolder + bibTeXBaseName

	repair := func(tableName, filePath string) bool {
		if !isTableDirty(tableName) {
			return false
		}
		dbInteraction.Progress("Recovering %s from database after unclean shutdown", filePath)
		return true
	}

	finishRepair := func(tableName string) {
		setTableDate(tableName, time.Now().UnixMicro())
		clearTableDirty(tableName)
		setTableLastWritten(tableName)
	}

	// key_hints: "key;hint"
	if repair("key_hints", base+KeyHintsFilePath) {
		rows, err := db.Query(`SELECT key, hint FROM key_hints`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var key, hint string
				if rows.Scan(&key, &hint) == nil {
					lines = append(lines, key+csvDelimiter+hint)
				}
			}
			rows.Close()
			if writeRepairCSV(base+KeyHintsFilePath, lines) {
				finishRepair("key_hints")
			}
		}
	}

	// key_oldies: "key;alias"
	if repair("key_oldies", base+KeyOldiesFilePath) {
		rows, err := db.Query(`SELECT key, alias FROM key_oldies`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var key, alias string
				if rows.Scan(&key, &alias) == nil {
					lines = append(lines, key+csvDelimiter+alias)
				}
			}
			rows.Close()
			if writeRepairCSV(base+KeyOldiesFilePath, lines) {
				finishRepair("key_oldies")
			}
		}
	}

	// non_double_entries: "key1;key2"
	if repair("non_double_entries", base+KeyNonDoublesFilePath) {
		rows, err := db.Query(`SELECT key1, key2 FROM non_double_entries`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var k1, k2 string
				if rows.Scan(&k1, &k2) == nil {
					lines = append(lines, k1+csvDelimiter+k2)
				}
			}
			rows.Close()
			if writeRepairCSV(base+KeyNonDoublesFilePath, lines) {
				finishRepair("non_double_entries")
			}
		}
	}

	// field_mappings: "source_field;source_value;target_field;target_value"
	if repair("field_mappings", base+FieldMappingsFilePath) {
		rows, err := db.Query(
			`SELECT source_field, source_value, target_field, target_value FROM field_mappings`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var sf, sv, tf, tv string
				if rows.Scan(&sf, &sv, &tf, &tv) == nil {
					lines = append(lines, csvLine(sf, sv, tf, tv))
				}
			}
			rows.Close()
			if writeRepairCSV(base+FieldMappingsFilePath, lines) {
				finishRepair("field_mappings")
			}
		}
	}

	// superseded_field_values: "entry_key;field;superseded_value"
	if repair("superseded_field_values", base+SupersededFieldValuesFilePath) {
		rows, err := db.Query(
			`SELECT entry_key, field, value FROM superseded_field_values`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var ek, field, superseded string
				if rows.Scan(&ek, &field, &superseded) == nil {
					lines = append(lines, csvLine(ek, field, superseded))
				}
			}
			rows.Close()
			if writeRepairCSV(base+SupersededFieldValuesFilePath, lines) {
				finishRepair("superseded_field_values")
				entryFieldMappingsRepaired = true
			}
		}
	}

	return
}

// upsertBibEntryField inserts or replaces a single (entry_key, field, value) row.
// An empty value deletes the row instead.
// upsertContributorRolesForField replaces the contributor_roles and
// entry_contributor_names rows for (key, role) with the names parsed from
// value. An empty value deletes all rows for the role without inserting new
// ones. Uses bibExec so writes participate in any active bib transaction.
func upsertContributorRolesForField(l *TBibTeXLibrary, key, field, value string) {
	dbExecSave("upsertContributorRolesForField: clear roles", `DELETE FROM contributor_roles WHERE entry_key = ? AND role = ?`, key, field)
	dbExecSave("upsertContributorRolesForField: clear names", `DELETE FROM entry_contributor_names WHERE entry_key = ? AND role = ?`, key, field)
	if value == "" {
		setTableDate("contributor_roles", time.Now().UnixMicro())
		return
	}
	value = normalizeEtAlTail(value) // §4.6 step 2: normalise "et al." tails
	position := 0
	var resolvedIDs []string
	var newParts []string
	anyWrapped := false
	for _, name := range splitBibNameField(value) {
		lc := strings.ToLower(name)
		if lc == "et.al." || lc == "et al." {
			continue
		}

		storeName := name
		garbled := false
		if isGarbledContributorName(name) {
			if !strings.HasPrefix(name, "{") {
				storeName = "{" + name + "}"
				anyWrapped = true
			}
			garbled = true
		}
		newParts = append(newParts, storeName)

		position++
		var id string
		if garbled {
			var ok bool
			id, ok = resolveNameToContributorID(l, storeName)
			if !ok {
				id = l.NewKey()
				l.ContributorByID[id] = &TContributor{Name: storeName, Garbled: true}
				l.NameToContributorID[storeName] = id
				upsertGarbledContributorToDB(id, storeName)
				upsertContributorNameToDB(id, storeName)
			} else if c := l.ContributorByID[id]; c != nil {
				c.Garbled = true
			}
		} else {
			var resolved bool
			id, resolved = resolveContributorForPosition(l, key, field, position, name, resolvedIDs)
			if !resolved {
				id = l.NewKey()
				l.ContributorByID[id] = &TContributor{Name: name}
				l.NameToContributorID[name] = id
				upsertContributorToDB(id, name, "")
				upsertContributorNameToDB(id, name)
			}
		}
		resolvedIDs = append(resolvedIDs, id)
		dbExecSave("upsertContributorRolesForField: insert role",
			`INSERT OR IGNORE INTO contributor_roles (entry_key, role, position, contributor_id) VALUES (?, ?, ?, ?)`,
			key, field, position, id)
		if contrib := l.ContributorByID[id]; contrib != nil && storeName != contrib.Name {
			dbExecSave("upsertContributorRolesForField: insert name_used",
				`INSERT OR IGNORE INTO entry_contributor_names (entry_key, role, position, contributor_id, name_used) VALUES (?, ?, ?, ?, ?)`,
				key, field, position, id, storeName)
		}
	}
	// When any name was garble-wrapped, register the original field value as an alias
	// for the wrapped form and cascade any existing aliases (e.g. DBLP canonical) to
	// point to the new canonical wrapped form.  This prevents winner-mismatch and
	// ambiguous-alias warnings on subsequent checks.
	if anyWrapped {
		if newValue := strings.Join(newParts, " and "); newValue != value {
			l.UpdateEntryFieldAlias(key, field, value, newValue)
		}
	}
	setTableDate("contributor_roles", time.Now().UnixMicro())
}

// applyDblpAuthorORCIDs uses the per-author ORCID data from a TDblpJSONEntry to:
//  1. Re-assign contributor_roles when the ORCID-identified contributor differs from
//     the name-based assignment (ORCID takes priority as a more precise identifier).
//  2. Record orcid_used evidence in entry_contributor_names for every author/editor
//     whose DBLP entry carries an ORCID, regardless of whether a re-assignment occurred.
//
// Called immediately after MergeInMemoryDBLPEntry so the name-based assignment is
// already in place and can be corrected here.
func applyDblpAuthorORCIDs(l *TBibTeXLibrary, key string, je *TDblpJSONEntry) {
	process := func(role string, persons []TDblpJSONPerson) {
		for i, p := range persons {
			if p.ORCID == "" {
				continue
			}
			position := i + 1
			nameLatex := dblpPersonNameToLaTeX(p.Name)

			// What contributor is currently assigned to this position? Must read via
			// the active transaction (bibQueryRow), not db.QueryRow directly — this
			// runs inside the single large transaction wrapping "Fixing DBLP entries"
			// (beginBibTransaction in main.go), and a plain db.QueryRow only ever sees
			// the pre-transaction committed state, not writes already made earlier in
			// this same transaction (e.g. a contributor merge). Reading through it
			// returned a stale contributor_id for an already-merged-away contributor;
			// the write below (via bibExec → activeTx) then correctly saw the live,
			// already-mutated state and rejected it — a real, deterministic FK
			// violation caused by this read, not by the data.
			var currentID string
			bibQueryRow(`SELECT contributor_id FROM contributor_roles WHERE entry_key = ? AND role = ? AND position = ?`,
				key, role, position).Scan(&currentID) //nolint:errcheck
			if currentID == "" {
				continue // no role record yet — nothing to annotate
			}

			targetID := l.ORCIDToContributorID[p.ORCID]
			if targetID != "" && targetID != currentID {
				if _, exists := l.ContributorByID[targetID]; !exists {
					// ORCIDToContributorID points at a contributor that no longer exists
					// (a merge absorbed it without fully redirecting this ORCID) — using
					// it would fail contributor_roles'/entry_contributor_names' FK on
					// contributors(id). Fall back to the already-valid currentID instead
					// of failing the whole write; the stale map entry is the real bug and
					// belongs in the merge path, not here.
					dbInteraction.Warning("applyDblpAuthorORCIDs: ORCID %s maps to unknown contributor %s (stale merge?) — keeping %s for %s", p.ORCID, targetID, currentID, key)
				} else {
					// ORCID identifies a different contributor — re-assign.
					dbExecSave("applyDblpAuthorORCIDs: reassign role",
						`UPDATE contributor_roles SET contributor_id = ? WHERE entry_key = ? AND role = ? AND position = ?`,
						targetID, key, role, position)
					currentID = targetID
				}
			}

			// Upsert the evidence record with orcid_used. Alias defaults to the
			// DBLP name form; if a row already exists, update orcid_used and
			// contributor_id (in case of re-assignment above).
			if nameLatex == "" {
				if c := l.ContributorByID[currentID]; c != nil {
					nameLatex = c.Name
				}
			}
			personDblpKey := ""
			if c := l.ContributorByID[currentID]; c != nil {
				personDblpKey = c.DblpKey
			}
			dbExecSave("applyDblpAuthorORCIDs: upsert evidence",
				`INSERT INTO entry_contributor_names (entry_key, role, position, contributor_id, name_used, orcid_used, dblp_key_used) `+
					`VALUES (?, ?, ?, ?, ?, ?, ?) `+
					`ON CONFLICT(entry_key, role, position) DO UPDATE SET `+
					`contributor_id = excluded.contributor_id, `+
					`orcid_used = excluded.orcid_used, `+
					`dblp_key_used = COALESCE(excluded.dblp_key_used, dblp_key_used)`,
				key, role, position, currentID, nameLatex, p.ORCID, personDblpKey)
		}
	}
	process("author", je.Authors)
	process("editor", je.Editors)
}

// Outside a transaction it compares the old cache value and only marks
// bibEntriesModified when the value actually changes, then updates the cache.
// Callers (setEntryField, deleteEntryField) must NOT pre-update the cache entry
// before calling this function, otherwise the comparison always finds equality.
func upsertBibEntryField(key, field, value string) {
	// An entry must never crossref itself. Observed live (not present in the
	// settled home DB, so it's introduced mid-run) on bookish parents with real,
	// legitimate children — most likely via a reentrant DBLP-child-processing path
	// (SetEntryFieldValue(childKey, "crossref", key) where childKey has resolved,
	// via some merge/redirect earlier in the same transaction, to key itself) that
	// hasn't been fully traced. Reject at the one place all such writes funnel
	// through rather than write bad data and rely on CheckCrossref's
	// after-the-fact self-reference cleanup to catch it on some later run.
	if field == "crossref" && value == key {
		dbInteraction.Warning("Refusing to set crossref=%s on itself for entry %s", value, key)
		return
	}
	if contributorRolesActive && (field == "author" || field == "editor") {
		if value != "" {
			// Keep bib_entry_keys anchor in sync so contributor_roles (which has an
			// entry_key → bib_entry_keys FK) can be written for this entry even when
			// author/editor is the first field ever set on it — same reasoning as the
			// non-author/editor branch below, which already does this; this branch
			// returns before reaching it, so it needs its own copy. Without this, a
			// brand-new entry whose author/editor field is written before any other
			// field (e.g. entry creation during -watch) hits a FOREIGN KEY violation
			// on every contributor_roles insert for that entry.
			if err2 := bibExec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key) VALUES (?)`, key); err2 != nil {
				dbWriteFailed = true
				return
			}
		}
		upsertContributorRolesForField(&Library, key, field, value)
		// Keep entryCache in sync so in-memory readers (e.g. deriveAliasBase)
		// can see the author/editor without querying contributor_roles.
		if entryCache != nil && value != "" {
			e, ok := entryCache[key]
			if !ok {
				e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
				entryCache[key] = e
			}
			e.Fields[field] = value
		}
		return
	}
	var err error
	if value == "" {
		err = bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, key, field)
		if field == DBLPField {
			deleteDblpCanonicalByCanonicalKey(key)
		}
	} else {
		// Keep bib_entry_keys anchor in sync so FK-dependent tables can reference
		// this entry immediately (e.g. entry_warnings during the same run).
		// Use bibExec (not db.Exec) so the INSERT runs inside activeTx when a
		// bib transaction is active — a bare db.Exec competes for a second write
		// slot and gets SQLITE_BUSY_SNAPSHOT (517) in WAL mode.
		if err2 := bibExec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key) VALUES (?)`, key); err2 != nil {
			dbWriteFailed = true
			return
		}
		err = bibExec(
			`INSERT INTO bib_entries (entry_key, field, value) VALUES (?, ?, ?)
			   ON CONFLICT(entry_key, field) DO UPDATE SET value = excluded.value;`,
			key, field, value)
		if field == DBLPField {
			// Remove any stale row (old dblp_key → key) before inserting the new one.
			// dblp_canonical PK is dblp_key, so changing the dblp value requires
			// the old row to be removed explicitly.
			deleteDblpCanonicalByCanonicalKey(key)
			upsertDblpCanonical(value, key)
		}
	}
	if err != nil {
		dbInteraction.Warning("bib_entries write failed for %s.%s: %s", key, field, err)
		dbWriteFailed = true
	}
	if entryCache != nil {
		if value == "" {
			if e, ok := entryCache[key]; ok {
				if _, exists := e.Fields[field]; exists {
					if activeTx == nil {
						markBibEntryModified()
					}
					delete(e.Fields, field)
				}
			}
		} else {
			e, ok := entryCache[key]
			if !ok {
				if activeTx == nil {
					markBibEntryModified()
				}
				e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
				entryCache[key] = e
				e.Fields[field] = value
			} else if e.Fields[field] != value {
				if activeTx == nil {
					markBibEntryModified()
				}
				e.Fields[field] = value
			}
		}
	} else if activeTx == nil {
		markBibEntryModified()
	}
}

// deleteBibEntryField removes a single field from the DB.
// For author/editor with contributorRolesActive, the data lives in contributor_roles
// (not bib_entries), so we clear that table instead.
// Outside a transaction it checks for an actual change before marking modified.
func deleteBibEntryField(key, field string) {
	if contributorRolesActive && (field == "author" || field == "editor") {
		upsertContributorRolesForField(&Library, key, field, "")
	} else {
		if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, key, field); err != nil {
			dbInteraction.Warning("bib_entries delete failed for %s.%s: %s", key, field, err)
		}
	}
	if entryCache != nil {
		if e, ok := entryCache[key]; ok {
			if _, exists := e.Fields[field]; exists {
				if activeTx == nil {
					markBibEntryModified()
				}
				delete(e.Fields, field)
			}
		}
	} else if activeTx == nil {
		markBibEntryModified()
	}
}

// --- deleted_entries table ---

func ensureDeletedEntriesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS deleted_entries (
		  key TEXT PRIMARY KEY
		);`)
}

// recordDeletedKey adds key to deleted_entries so sync operations know not to
// offer re-adding it from stale bib files.
func recordDeletedKey(key string) {
	dbExecSave("recordDeletedKey", `INSERT OR IGNORE INTO deleted_entries (key) VALUES (?)`, key)
}

// isDeletedEntry reports whether key was explicitly deleted from the library.
func isDeletedEntry(key string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM deleted_entries WHERE key = ?`, key).Scan(&n) //nolint:errcheck
	return n > 0
}

// deleteBibEntry removes all rows for a given entry key from bib_entries and the
// bib_entry_keys anchor. ON DELETE CASCADE propagates the anchor deletion to
// bib_groups, superseded_field_values, entry_warnings, and entry_metadata.
// entry_lineage / source_field_signatures / source_contributor_signatures have no
// FK to bib_entry_keys (they must survive independent of whether the entry they
// describe currently exists, e.g. across a temporary ghost state) so they are
// wiped explicitly here — the one place a key stops being a library entry.
func deleteBibEntry(key string) {
	dbExecSave(fmt.Sprintf("deleteBibEntry: bib_entries delete failed for %s", key), `DELETE FROM bib_entries WHERE entry_key = ?`, key)
	dbExecSave("deleteBibEntry: bib_entry_keys", `DELETE FROM bib_entry_keys WHERE entry_key = ?`, key)
	dbExecSave("deleteBibEntry: entry_lineage", `DELETE FROM entry_lineage WHERE entry_key = ?`, key)
	dbExecSave("deleteBibEntry: source_field_signatures", `DELETE FROM source_field_signatures WHERE entry_key = ?`, key)
	dbExecSave("deleteBibEntry: source_contributor_signatures", `DELETE FROM source_contributor_signatures WHERE entry_key = ?`, key)
	delete(Library.LineageMap, key)
	delete(Library.SourceSignatures, key)
	if entryCache != nil {
		if _, ok := entryCache[key]; ok {
			if activeTx == nil {
				markBibEntryModified()
			}
			delete(entryCache, key)
		}
	} else if activeTx == nil {
		markBibEntryModified()
	}
}

// loadEntryFromDb returns a TBibTeXEntry snapshot of all fields for key.
// Returns an entry with an empty Fields map (Exists() == false) when key is absent.
// contributorRoleFieldValue reconstructs the " and "-joined author/editor string
// for (key, role) directly from contributor_roles, for callers (e.g. entryDisplayLines)
// that need the authoritative value regardless of whether entryCache's copy of the
// entry carries it. Returns "" when there are no contributor_roles rows for this pair.
func contributorRoleFieldValue(key, role string) string {
	rows, err := bibQuery(
		`SELECT c.name FROM contributor_roles cr
		 JOIN contributors c ON c.id = cr.contributor_id
		 WHERE cr.entry_key = ? AND cr.role = ? ORDER BY cr.position`, key, role)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			continue
		}
		names = append(names, name)
	}
	return strings.Join(names, " and ")
}

func loadEntryFromDb(key string) *TBibTeXEntry {
	if entryCache != nil {
		if e, ok := entryCache[key]; ok {
			return e
		}
		return &TBibTeXEntry{Key: key, Fields: map[string]string{}}
	}
	rows, err := bibQuery(`SELECT field, value FROM bib_entries WHERE entry_key = ?`, key)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for %s: %s", key, err)
		return &TBibTeXEntry{Key: key, Fields: map[string]string{}}
	}
	defer rows.Close()

	fields := map[string]string{}
	for rows.Next() {
		var field, value string
		if err := rows.Scan(&field, &value); err != nil {
			dbInteraction.Warning("Could not scan bib_entries row for %s: %s", key, err)
			continue
		}
		fields[field] = value
	}

	// When contributor_roles is active, author/editor are absent from bib_entries.
	// Reconstruct them directly from contributor_roles so that checks which read
	// entry fields (e.g. CheckWithdrawn) work correctly even while entryCache is nil
	// (i.e. during a parseSyncBibFile re-import that calls clearBibTables).
	if contributorRolesActive {
		roleRows, rErr := bibQuery(
			`SELECT cr.role, c.name FROM contributor_roles cr
			 JOIN contributors c ON c.id = cr.contributor_id
			 WHERE cr.entry_key = ? ORDER BY cr.role, cr.position`, key)
		if rErr == nil {
			defer roleRows.Close()
			for roleRows.Next() {
				var role, name string
				if roleRows.Scan(&role, &name) != nil {
					continue
				}
				if fields[role] == "" {
					fields[role] = name
				} else {
					fields[role] += " and " + name
				}
			}
		}
	}

	return &TBibTeXEntry{Key: key, Fields: fields}
}

// bibEntryExists reports whether bib_entries contains any row for key.
func bibEntryExists(key string) bool {
	if entryCache != nil {
		e, ok := entryCache[key]
		return ok && e.Fields[EntryTypeField] != ""
	}
	row := bibQueryRow(`SELECT 1 FROM bib_entries WHERE entry_key = ? AND field = ? LIMIT 1`, key, EntryTypeField)
	var dummy int
	return row.Scan(&dummy) == nil
}

// countBibEntries returns the number of distinct entry keys that have an entry-type row.
func countBibEntries() int {
	if entryCache != nil {
		n := 0
		for _, e := range entryCache {
			if e.Fields[EntryTypeField] != "" {
				n++
			}
		}
		return n
	}
	row := bibQueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = ?`, EntryTypeField)
	var n int
	row.Scan(&n)
	return n
}

// forEachBibEntryKey calls fn for every distinct entry key that has an entry-type row.
// Keys are collected into a slice before fn is called so the DB cursor is closed
// before any writes that fn might trigger.
// fn returns true to continue iteration, false to stop early (graceful quit).
func forEachBibEntryKey(fn func(key string) bool) {
	if entryCache != nil {
		for key, e := range entryCache {
			if e.Fields[EntryTypeField] != "" {
				if !fn(key) {
					return
				}
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key FROM bib_entries WHERE field = ?`, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for entry keys: %s", err)
		return
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			dbInteraction.Warning("Could not scan bib_entries entry key: %s", err)
			continue
		}
		keys = append(keys, key)
	}
	rows.Close()
	for _, key := range keys {
		if !fn(key) {
			return
		}
	}
}

// TBibFieldMatch holds one result row from findBibEntriesByField.
type TBibFieldMatch struct {
	Key   string
	Value string
}

// findBibEntriesByField returns all entries where the given field exists.
// When valueFilter is non-empty only entries whose field value contains it as a
// case-insensitive substring are returned. Results are sorted by entry key.
func findBibEntriesByField(field, valueFilter string) []TBibFieldMatch {
	var rows *sql.Rows
	var err error
	if valueFilter == "" {
		rows, err = db.Query(
			`SELECT entry_key, value FROM bib_entries WHERE field = ? ORDER BY entry_key`,
			field)
	} else {
		rows, err = db.Query(
			`SELECT entry_key, value FROM bib_entries WHERE field = ? AND LOWER(value) LIKE ? ORDER BY entry_key`,
			field, "%"+strings.ToLower(valueFilter)+"%")
	}
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries: %s", err)
		return nil
	}
	defer rows.Close()
	var matches []TBibFieldMatch
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			dbInteraction.Warning("Could not scan bib_entries: %s", err)
			continue
		}
		matches = append(matches, TBibFieldMatch{key, value})
	}
	return matches
}

// findBibEntriesByGroup returns bib_groups rows where the group_name contains
// groupFilter as a case-insensitive substring. If groupFilter is empty all rows
// are returned. Results are sorted by entry key.
func findBibEntriesByGroup(groupFilter string) []TBibFieldMatch {
	var rows *sql.Rows
	var err error
	if groupFilter == "" {
		rows, err = db.Query(
			`SELECT entry_key, group_name FROM bib_groups ORDER BY entry_key`)
	} else {
		rows, err = db.Query(
			`SELECT entry_key, group_name FROM bib_groups WHERE LOWER(group_name) LIKE ? ORDER BY entry_key`,
			"%"+strings.ToLower(groupFilter)+"%")
	}
	if err != nil {
		dbInteraction.Warning("Could not query bib_groups: %s", err)
		return nil
	}
	defer rows.Close()
	var matches []TBibFieldMatch
	for rows.Next() {
		var key, group string
		if err := rows.Scan(&key, &group); err != nil {
			dbInteraction.Warning("Could not scan bib_groups: %s", err)
			continue
		}
		matches = append(matches, TBibFieldMatch{key, group})
	}
	return matches
}

// addBibGroupEntry adds entryKey to groupName in bib_groups; no-op if already present.
func addBibGroupEntry(groupName, entryKey string) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO bib_groups (group_name, entry_key) VALUES (?, ?)`,
		groupName, entryKey)
	return err
}

// removeBibGroupEntry removes entryKey from groupName in bib_groups; no-op if not present.
func removeBibGroupEntry(groupName, entryKey string) error {
	_, err := db.Exec(
		`DELETE FROM bib_groups WHERE group_name = ? AND entry_key = ?`,
		groupName, entryKey)
	return err
}

// saveBibGroupsToDb writes l.GroupEntries to bib_groups using bibExec (transaction-aware).
func saveBibGroupsToDb(l *TBibTeXLibrary) {
	for group, entries := range l.GroupEntries {
		for entry := range entries.Elements() {
			if err := bibExec(`INSERT INTO bib_groups (group_name, entry_key) VALUES (?, ?)
			                     ON CONFLICT DO NOTHING;`, group, entry); err != nil {
				dbInteraction.Warning("bib_groups insert failed: %s", err)
			}
		}
	}
}

// saveBibCommentsToDb writes l.Comments to bib_comments using bibExec (transaction-aware).
func saveBibCommentsToDb(l *TBibTeXLibrary) {
	for i, comment := range l.Comments {
		if err := bibExec(`INSERT INTO bib_comments (position, content) VALUES (?, ?)
		                     ON CONFLICT DO NOTHING;`, i, comment); err != nil {
			dbInteraction.Warning("bib_comments insert failed: %s", err)
		}
	}
}

// loadGroupsFromDb populates l.GroupEntries from the bib_groups table.
func loadGroupsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT group_name, entry_key FROM bib_groups`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_groups: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var group, entry string
		if err := rows.Scan(&group, &entry); err != nil {
			dbInteraction.Warning("Could not scan bib_groups row: %s", err)
			continue
		}
		l.GroupEntries.AddValueToStringSetMap(group, entry)
	}
}

// resolveGroupEntriesKeys rewrites any alias or hint key in GroupEntries to the
// canonical key. Called after buildKeyAliasesFromDb so that MapEntryKey works.
// This handles preferred-alias cite keys stored in bib_groups during BibDesk import.
func resolveGroupEntriesKeys(l *TBibTeXLibrary) {
	for group, entries := range l.GroupEntries {
		for key := range entries.Elements() {
			canonical := l.MapEntryKey(key)
			if canonical != key {
				l.GroupEntries.DeleteValueFromStringSetMap(group, key)
				l.GroupEntries.AddValueToStringSetMap(group, canonical)
			}
		}
	}
}

// loadCommentsFromDb populates l.Comments from the bib_comments table.
func loadCommentsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT content FROM bib_comments ORDER BY position`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_comments: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			dbInteraction.Warning("Could not scan bib_comments row: %s", err)
			continue
		}
		l.Comments = append(l.Comments, content)
	}
}

// addDblpKeyHintTransient adds a DBLP-derived hint to HintToKey and KeyOldies as a
// transient (in-memory only) entry. DBLP hints are regenerated from bib_entries on
// every run, so they must not be persisted to the DB.
func addDblpKeyHintTransient(l *TBibTeXLibrary, dblpHint, key string) {
	if !l.HintToKey.Contains(dblpHint) {
		l.HintToKey.SetTransient(dblpHint, key)
	}
}

// buildKeyAliasesFromDb rebuilds the in-memory key alias and hint maps from fields
// stored in bib_entries (preferredalias) and dblp_canonical (dblp identity).
// Must be called on the fast path where no parse takes place.
// preferredalias entries go only to key_hints (not key_oldies); DBLP entries are
// transient (regenerated each run from dblp_canonical, never written to the DB).
func buildKeyAliasesFromDb(l *TBibTeXLibrary) {
	dbInteraction.Progress(ProgressBuildingKeyAliases)
	if entryCache != nil {
		for key, e := range entryCache {
			if alias := e.Fields[PreferredAliasField]; alias != "" {
				l.AddKeyHint(alias, key)
			}
			if dblp := e.Fields[DBLPField]; dblp != "" {
				l.KeyOldies.SetTransient(KeyForDBLP(dblp), key)
				addDblpKeyHintTransient(l, KeyForDBLP(dblp), key)
			}
		}
		return
	}

	rows, err := db.Query(
		`SELECT entry_key, value FROM bib_entries WHERE field = ?`,
		PreferredAliasField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for preferred aliases: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			dbInteraction.Warning("Could not scan preferred alias row: %s", err)
			continue
		}
		l.AddKeyHint(value, key)
	}

	dblpRows, err := db.Query(`SELECT dblp_key, canonical_key FROM dblp_canonical`)
	if err != nil {
		dbInteraction.Warning("Could not query dblp_canonical for key aliases: %s", err)
		return
	}
	defer dblpRows.Close()
	for dblpRows.Next() {
		var dblpKey, canonical string
		if err := dblpRows.Scan(&dblpKey, &canonical); err != nil {
			dbInteraction.Warning("Could not scan dblp_canonical row: %s", err)
			continue
		}
		l.KeyOldies.SetTransient(KeyForDBLP(dblpKey), canonical)
		addDblpKeyHintTransient(l, KeyForDBLP(dblpKey), canonical)
	}
}

// buildTitleIndexFromDb rebuilds l.TitleIndex from the title field in bib_entries.
func buildTitleIndexFromDb(l *TBibTeXLibrary) {
	dbInteraction.Progress(ProgressBuildingTitleIndex)
	l.TitleIndex = TStringSetMap{}
	if entryCache != nil {
		for key, e := range entryCache {
			if title := e.Fields[TitleField]; title != "" {
				l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key, value FROM bib_entries WHERE field = ?`, TitleField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for titles: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, title string
		if err := rows.Scan(&key, &title); err != nil {
			dbInteraction.Warning("Could not scan title row: %s", err)
			continue
		}
		l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
	}
}

// --- ValidBibDb / timestamp ---

// ValidBibDb returns true when bib_entries has ever been populated.
// In the DB-primary architecture the bib file and mapping tables are not checked
// here: bib file changes reach the DB only via -sync; mapping changes propagate
// via NormaliseEntryFields on every run (see ARCHITECTURE.md §4.5).
func (l *TBibTeXLibrary) ValidBibDb() bool {
	return tableModTime("bib_entries") > 0
}

// refreshBibDbTimestamp marks bib_entries as current. Must be called AFTER WriteBibTeXFile
// so the DB timestamp exceeds the bib file timestamp, keeping ValidBibDb true.
func refreshBibDbTimestamp() {
	setTableDate("bib_entries", time.Now().UnixMicro())
}

// --- state_names table ---

func ensureStateNamesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS state_names (
		  alias     TEXT PRIMARY KEY,
		  canonical TEXT NOT NULL
		);`)
}

func loadStateNamesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT alias, canonical FROM state_names`)
	if err != nil {
		dbInteraction.Warning("Could not query state_names: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var alias, canonical string
		if err := rows.Scan(&alias, &canonical); err != nil {
			dbInteraction.Warning("Could not scan state_names row: %s", err)
			continue
		}
		l.StateAliasToCanonical[alias] = canonical
	}
}

// --- state_countries table ---

func ensureStateCountriesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS state_countries (
		  state   TEXT PRIMARY KEY,
		  country TEXT NOT NULL
		);`)
}

func loadStateCountriesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT state, country FROM state_countries`)
	if err != nil {
		dbInteraction.Warning("Could not query state_countries: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var state, country string
		if err := rows.Scan(&state, &country); err != nil {
			dbInteraction.Warning("Could not scan state_countries row: %s", err)
			continue
		}
		l.StateToCountry[state] = country
	}
}

// --- country_names table ---

func ensureCountryNamesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS country_names (
		  alias     TEXT PRIMARY KEY,
		  canonical TEXT NOT NULL
		);`)
}

func loadCountryNamesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT alias, canonical FROM country_names`)
	if err != nil {
		dbInteraction.Warning("Could not query country_names: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var alias, canonical string
		if err := rows.Scan(&alias, &canonical); err != nil {
			dbInteraction.Warning("Could not scan country_names row: %s", err)
			continue
		}
		l.CountryAliasToCanonical[alias] = canonical
	}
}

// --- booktitle_country_names table ---

func ensureBooktitleCountryNamesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS booktitle_country_names (
		  alias     TEXT PRIMARY KEY,
		  canonical TEXT NOT NULL
		);`)
}

func loadBooktitleCountryNamesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT alias, canonical FROM booktitle_country_names`)
	if err != nil {
		dbInteraction.Warning("Could not query booktitle_country_names: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var alias, canonical string
		if err := rows.Scan(&alias, &canonical); err != nil {
			dbInteraction.Warning("Could not scan booktitle_country_names row: %s", err)
			continue
		}
		l.BooktitleCountryAliasToCanonical[alias] = canonical
	}
}

// --- bib entry iterators (used by write path) ---

// forEachLibraryChildOf calls fn for every library entry whose crossref field equals parentKey.
func forEachLibraryChildOf(parentKey string, fn func(childKey string)) {
	if entryCache != nil {
		for key, e := range entryCache {
			if e.Fields["crossref"] == parentKey {
				fn(key)
			}
		}
		return
	}
	rows, err := db.Query(
		`SELECT entry_key FROM bib_entries WHERE field = 'crossref' AND value = ?`, parentKey)
	if err != nil {
		dbInteraction.Warning("Could not query library children of %s: %s", parentKey, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			dbInteraction.Warning("Could not scan library child row: %s", err)
			continue
		}
		fn(key)
	}
}

// forEachBibEntryType calls fn(key, entryType) for every entry stored in bib_entries.
func forEachBibEntryType(fn func(key, entryType string)) {
	if entryCache != nil {
		for key, e := range entryCache {
			if t := e.Fields[EntryTypeField]; t != "" {
				fn(key, t)
			}
		}
		return
	}
	rows, err := db.Query(`SELECT entry_key, value FROM bib_entries WHERE field = ?`, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries entry types: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, entryType string
		if err := rows.Scan(&key, &entryType); err != nil {
			dbInteraction.Warning("Could not scan bib_entries row: %s", err)
			continue
		}
		fn(key, entryType)
	}
}
