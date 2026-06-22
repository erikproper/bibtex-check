/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: LibraryDB
 *
 * SQLite persistence layer for the BibTeX library. Each logical data source
 * (name mappings, key hints, …) follows the same three-function pattern:
 *   import*FromCSV()       — import a table from a CSV (only via -import)
 *   load<X>FromDb(l)        — populate in-memory library fields from DB
 *   save<X>ToDb(l)          — write in-memory library fields back to DB
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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sqliteDatabaseDriver = "sqlite"
)

var (
	dbInteraction               TInteraction
	db                          *sql.DB
	entryCache                  map[string]*TBibTeXEntry
	entrySnapshots              map[string]map[string]string
	bibEntriesModified          bool
	c2TrackingActive            bool
	c2EntryModified             bool
	entryModTrackingActive      bool
	entryModified               bool
	dbWriteSessionActive        bool
	dbWriteFailed               bool  // set when any end-of-run DB write fails; blocks finaliseWorkingDatabase
	changesAtSessionOpen        int64 // total_changes() right after markWriteSessionOpen; used to detect zero-write sessions
	fieldMappingsLoading        bool  // suppresses DB write-through in AddGenericFieldAlias/AddFieldMapping during initial load
	entryFieldMappingsLoading   bool  // suppresses DB write-through in AddEntryFieldAlias during initial load
	nameMappingsLoading         bool  // suppresses DB write-through in name-mapping mutations during initial load
)

// dbExecSave executes a DB statement during the end-of-run save phase. On error it
// logs msg and sets dbWriteFailed so postCheckGate blocks the home-DB copy.
func dbExecSave(msg, query string, args ...any) {
	if _, err := db.Exec(query, args...); err != nil {
		dbInteraction.Warning("%s: %s", msg, err)
		dbWriteFailed = true
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

	spinner := dbInteraction.NewSpinner(ProgressLoadingEntryCache)
	cache := map[string]*TBibTeXEntry{}
	for rows.Next() {
		var key, field, value string
		if err := rows.Scan(&key, &field, &value); err != nil {
			spinner.Stop()
			dbInteraction.Warning("Could not scan bib_entries for cache: %s", err)
			return
		}
		e, ok := cache[key]
		if !ok {
			e = &TBibTeXEntry{Key: key, Fields: map[string]string{}}
			cache[key] = e
			spinner.Update(len(cache), int(count))
		}
		e.Fields[field] = value
	}
	spinner.Stop()
	entryCache = cache
}

// --- database connection ---

// dbPath returns the path of the main SQLite cache file. When cache_folder is
// configured, the file lives there (outside Nextcloud/cloud-sync folders).
// Otherwise it lives next to the .bib file.
func dbPath() string {
	folder := bibTeXFolder
	if cacheFolder != "" {
		folder = cacheFolder
	}
	return folder + bibTeXBaseName + cacheFileExtension
}

// maybeMigrateDbFile renames any legacy database file to the current extension
// (.cache → .sqlite3). Migrates both the working path and, when isolation is
// active, the home path. Called at startup before connectToDatabase.
func maybeMigrateDbFile() {
	migrateDbFilePath(dbPath())
	if dbIsolationActive() {
		migrateDbFilePath(dbHomePath())
	}
}

func migrateDbFilePath(newPath string) {
	if info, err := os.Stat(newPath); err == nil {
		if info.Size() > 0 {
			return // valid file already at new extension
		}
		os.Remove(newPath) // remove stale zero-byte file before renaming
	}
	oldPath := strings.TrimSuffix(newPath, cacheFileExtension) + ".cache"
	if _, err := os.Stat(oldPath); err == nil {
		if err := os.Rename(oldPath, newPath); err == nil {
			dbInteraction.Progress("Migrated database: %s.cache → %s.sqlite3", bibTeXBaseName, bibTeXBaseName)
		}
	}
}

// maybeMigrateTablesFolder handles the two-step folder-name migration:
//   v13.1: .tables/ → .exchange/   (old direction, now reverted)
//   v23.0: .exchange/ → .tables/   (canonical name restored)
// It also renames internal files that changed name in v23.0.
func maybeMigrateTablesFolder() {
	newDir := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix // .tables
	oldDir := bibTeXFolder + bibTeXBaseName + ".exchange"

	// v23.0: rename .exchange → .tables
	if _, err := os.Stat(newDir); err != nil {
		if _, err2 := os.Stat(oldDir); err2 == nil {
			if err3 := os.Rename(oldDir, newDir); err3 != nil {
				dbInteraction.Warning("Could not migrate .exchange folder: %s", err3)
				return
			}
			dbInteraction.Progress("Migrated %s.exchange → %s.tables", bibTeXBaseName, bibTeXBaseName)
		}
	}

	// Rename filter_name_mappings.csv → name_mappings.csv (v23.0 file rename).
	maybeRenameInTablesDir("filter_name_mappings.csv", "name_mappings.csv")
	// Convert entry_metadata.json → entry_metadata.csv (v23.0 format change).
	maybeConvertEntryMetadataToCSV()
}

// maybeRenameInTablesDir renames oldFile to newFile inside the tables folder if
// oldFile exists and newFile does not.
func maybeRenameInTablesDir(oldFile, newFile string) {
	dir := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix
	oldPath := dir + "/" + oldFile
	newPath := dir + "/" + newFile
	if _, err := os.Stat(newPath); err == nil {
		return // already renamed
	}
	if _, err := os.Stat(oldPath); err != nil {
		return // old file absent
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		dbInteraction.Warning("Could not rename %s → %s: %s", oldFile, newFile, err)
	}
}

// maybeConvertEntryMetadataToCSV migrates the JSON entry_metadata export to
// CSV format (entry_key;property;value).
func maybeConvertEntryMetadataToCSV() {
	dir := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix
	jsonPath := dir + "/entry_metadata.json"
	csvPath := dir + "/entry_metadata.csv"
	if _, err := os.Stat(csvPath); err == nil {
		return // already converted
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return // no JSON file
	}
	var m map[string]map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	f, err := os.Create(csvPath)
	if err != nil {
		return
	}
	defer f.Close()
	for entryKey, props := range m {
		for prop, val := range props {
			f.WriteString(csvLine(entryKey, prop, val) + "\n")
		}
	}
	os.Remove(jsonPath)
}

// maybeMigrateScriptFile moves the legacy <base>.script file to
// <base>.scripts/entry_actions on first run after upgrading.
func maybeMigrateScriptFile() {
	newPath := bibTeXFolder + bibTeXBaseName + ScriptFilePath
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	oldPath := bibTeXFolder + bibTeXBaseName + ".script"
	if _, err := os.Stat(oldPath); err != nil {
		return
	}
	scriptsDir := bibTeXFolder + bibTeXBaseName + scriptsFolderSuffix
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		dbInteraction.Warning("Could not create scripts directory: %s", err)
		return
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		dbInteraction.Warning("Could not migrate .script file: %s", err)
		return
	}
	dbInteraction.Progress("Migrated %s.script → %s.scripts/entry_actions", bibTeXBaseName, bibTeXBaseName)
}

var tableConstraintsMigrated bool

// maybeMigrateTableConstraints detects mapping tables that were created without
// their required UNIQUE / PRIMARY KEY constraint (due to CREATE TABLE IF NOT EXISTS
// leaving an older schema in place) and recreates them with deduplication.
// Must be called after prepareWorkingDatabase() so the correct DB file is open.
// Guarded by tableConstraintsMigrated so it runs at most once per process.
func maybeMigrateTableConstraints() {
	if tableConstraintsMigrated {
		return
	}
	tableConstraintsMigrated = true
	type tableSpec struct {
		table   string
		pk      string // PRIMARY KEY column name
		cols    string // column definitions for the recreated table
		selCols string // explicit column list for INSERT ... SELECT
	}
	tables := []tableSpec{
		{"name_mappings", "alias", "alias TEXT PRIMARY KEY, name TEXT NOT NULL", "alias, name"},
		{"key_hints",     "hint",  "hint TEXT PRIMARY KEY, key TEXT NOT NULL",   "hint, key"},
		{"key_oldies",    "alias", "alias TEXT PRIMARY KEY, key TEXT NOT NULL",  "alias, key"},
	}

	for _, t := range tables {
		var createSQL string
		db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, t.table).Scan(&createSQL)
		if strings.Contains(strings.ToUpper(createSQL), "PRIMARY KEY") {
			continue // constraint already present
		}
		dbInteraction.Progress("Migrating %s: adding PRIMARY KEY on %s and deduplicating", t.table, t.pk)
		tmp := t.table + "_migration_tmp"
		stmts := []string{
			`DROP TABLE IF EXISTS ` + tmp,
			`CREATE TABLE ` + tmp + ` (` + t.cols + `)`,
			fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s`, tmp, t.selCols, t.selCols, t.table),
			`DROP TABLE ` + t.table,
			`ALTER TABLE ` + tmp + ` RENAME TO ` + t.table,
		}
		ok := true
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				dbInteraction.Warning("Migration of %s failed at %q: %s", t.table, stmt, err)
				ok = false
				break
			}
		}
		if ok {
			dbInteraction.Progress("Migrated %s: PRIMARY KEY added, duplicates removed", t.table)
		}
	}

}

var fkSchemaMigrated bool

// maybeMigrateToFKSchema recreates bib_groups, losing_field_values, entry_warnings,
// and entry_metadata with FOREIGN KEY ... ON DELETE CASCADE referencing bib_entry_keys.
// Cleans orphan rows from each source table before copying so no violations are
// introduced when FK is re-enabled. Guarded so it runs at most once per process.
func maybeMigrateToFKSchema() {
	if fkSchemaMigrated {
		return
	}
	fkSchemaMigrated = true

	type tableSpec struct {
		table   string
		cols    string
		selCols string
		fkCol   string
	}
	tables := []tableSpec{
		{
			"bib_groups",
			`group_name TEXT NOT NULL, entry_key TEXT NOT NULL,
			 PRIMARY KEY (group_name, entry_key),
			 FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE`,
			"group_name, entry_key",
			"entry_key",
		},
		{
			"losing_field_values",
			`entry_key TEXT NOT NULL, field TEXT NOT NULL, value TEXT NOT NULL,
			 PRIMARY KEY (entry_key, field, value),
			 FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE`,
			"entry_key, field, value",
			"entry_key",
		},
		{
			"entry_warnings",
			`key TEXT NOT NULL, warning TEXT NOT NULL DEFAULT '',
			 UNIQUE(key, warning),
			 FOREIGN KEY (key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE`,
			"key, warning",
			"key",
		},
		{
			"entry_metadata",
			`entry_key TEXT NOT NULL, property TEXT NOT NULL, value TEXT NOT NULL,
			 PRIMARY KEY (entry_key, property),
			 FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE`,
			"entry_key, property, value",
			"entry_key",
		},
	}

	needsMigration := false
	for _, t := range tables {
		var createSQL string
		db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, t.table).Scan(&createSQL)
		if !strings.Contains(strings.ToUpper(createSQL), "REFERENCES") {
			needsMigration = true
			break
		}
	}
	if !needsMigration {
		return
	}

	dbInteraction.Progress("Migrating schema: adding FK constraints to dependent tables")
	db.Exec(`PRAGMA foreign_keys = OFF`)
	for _, t := range tables {
		var createSQL string
		db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, t.table).Scan(&createSQL)
		if strings.Contains(strings.ToUpper(createSQL), "REFERENCES") {
			continue
		}
		db.Exec(fmt.Sprintf(
			`DELETE FROM %s WHERE %s NOT IN (SELECT entry_key FROM bib_entry_keys)`,
			t.table, t.fkCol))
		tmp := t.table + "_fk_migration_tmp"
		stmts := []string{
			`DROP TABLE IF EXISTS ` + tmp,
			`CREATE TABLE ` + tmp + ` (` + t.cols + `)`,
			fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s) SELECT %s FROM %s`, tmp, t.selCols, t.selCols, t.table),
			`DROP TABLE ` + t.table,
			`ALTER TABLE ` + tmp + ` RENAME TO ` + t.table,
		}
		ok := true
		for _, stmt := range stmts {
			if _, err := db.Exec(stmt); err != nil {
				dbInteraction.Warning("FK migration of %s failed: %s", t.table, err)
				ok = false
				break
			}
		}
		if ok {
			dbInteraction.Progress("Added FK ON DELETE CASCADE to %s", t.table)
		}
	}
	db.Exec(`PRAGMA foreign_keys = ON`)
}

// configureDatabasePragmas sets WAL journal mode and a busy timeout on the
// current db connection. WAL allows a writer to proceed concurrently with open
// read cursors (eliminating SQLITE_BUSY when setTableDirty is called from
// within a forEachBibEntryKey iteration). The busy timeout adds automatic
// retry for any residual lock contention.
func configureDatabasePragmas() {
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		dbInteraction.Warning("Could not enable WAL journal mode: %s", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		dbInteraction.Warning("Could not set busy_timeout: %s", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		dbInteraction.Warning("Could not enable FK enforcement: %s", err)
	}
}

func connectToDatabase() {
	dbName := dbPath()

	var err error
	db, err = sql.Open(sqliteDatabaseDriver, dbName)
	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", dbName, err.Error())
	}
	configureDatabasePragmas()

	ensureTableDatesTableExists()
	maybeMigrateFilterTableNames()
	maybeConsolidateEntryFlags()
	ensureNameMappingsTableExists()
	ensureKeyHintsTableExists()
	ensureKeyOldiesTableExists()
	ensureKeyNonDoublesTableExists()
	ensureDblpParentTableExists()
	ensureDblpWaivedTableExists()
	ensureDblpCanonicalTableExists()
	maybeMigrateDblpCanonical()
	ensureCrossFieldMappingsTableExists()
	ensureEntryFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureFieldMappingsTableExists()
	maybeMigrateToFieldMappings()
	ensureURLsIgnoreTableExists()
	ensureEntryMetadataTableExists()
	ensureLosingFieldValuesTableExists()
	ensureEntryWarningsTableExists()
	ensureDeletedEntriesTableExists()
	ensureConfigTableExists()
	maybeBootstrapConfigFromFile()
	loadBibTeXSettings()
	ensureShortenMappingsTableExists()
	ensureBibEntryKeysTableExists()
	ensureBibTablesExist()
}

// --- safe parse (copy-on-write DB swap) ---

var safeParseTemp string // non-empty while a safe parse is in progress
var safeParseOriginalCount int

func countDistinctBibEntries() int {
	var n int
	db.QueryRow(`SELECT COUNT(DISTINCT entry_key) FROM bib_entries WHERE field = ?`, EntryTypeField).Scan(&n)
	return n
}

// copyFile copies src to dst byte-for-byte.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// reopenDb closes the current db connection and opens a new one at path,
// re-ensuring all table schemas exist (idempotent).
func reopenDb(path string) {
	if db != nil {
		db.Close()
	}
	var err error
	db, err = sql.Open(sqliteDatabaseDriver, path)
	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", path, err)
	}
	configureDatabasePragmas()
	ensureTableDatesTableExists()
	maybeMigrateFilterTableNames()
	ensureNameMappingsTableExists()
	ensureKeyHintsTableExists()
	ensureKeyOldiesTableExists()
	ensureKeyNonDoublesTableExists()
	ensureDblpParentTableExists()
	ensureDblpWaivedTableExists()
	ensureDblpCanonicalTableExists()
	ensureCrossFieldMappingsTableExists()
	ensureEntryFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureFieldMappingsTableExists()
	ensureURLsIgnoreTableExists()
	ensureEntryMetadataTableExists()
	ensureLosingFieldValuesTableExists()
	ensureConfigTableExists()
	ensureShortenMappingsTableExists()
	ensureBibEntryKeysTableExists()
	ensureBibTablesExist()
}

// beginSafeParse copies the live SQLite file to a temp location outside Nextcloud,
// then switches db to that temp file. Returns false if setup fails (caller falls
// through to an unsafe parse on the live file).
func beginSafeParse() bool {
	dbInteraction.Progress(ProgressBackingUpDatabase)
	livePath := dbPath()
	ts := time.Now().Format("20060102_150405")
	safeParseTemp = fmt.Sprintf("%s/bibtex_check_%s_%s%s",
		os.TempDir(), bibTeXBaseName, ts, cacheFileExtension)

	safeParseOriginalCount = countDistinctBibEntries() // count entries before switching to temp

	if err := copyFile(livePath, safeParseTemp); err != nil {
		dbInteraction.Warning("Safe parse: could not copy database to temp: %s", err)
		safeParseTemp = ""
		return false
	}
	reopenDb(safeParseTemp)
	return true
}

// commitSafeParse completes a successful safe parse by installing the newly
// reparsed temp database as the live file. The bib+CSV directory backup
// (created by ensureLibraryBackup before the first write) is the canonical
// recovery point; no separate SQLite backup is needed.
func commitSafeParse() {
	if safeParseTemp == "" {
		return
	}
	livePath := dbPath()

	// Entry count sanity check: warn if the re-parsed DB has fewer entries than the original.
	if safeParseOriginalCount > 0 {
		newCount := countDistinctBibEntries()
		if newCount < safeParseOriginalCount {
			dbInteraction.Warning(
				"Safe parse: re-parsed DB has %d entries, original had %d (-%d). Check for missing entries before proceeding.",
				newCount, safeParseOriginalCount, safeParseOriginalCount-newCount)
		}
	}

	// Move temp → live. Fall back to copy+remove if crossing filesystems.
	if err := os.Rename(safeParseTemp, livePath); err != nil {
		if copyErr := copyFile(safeParseTemp, livePath); copyErr != nil {
			dbInteraction.Warning("Safe parse: could not install new database: %s", copyErr)
		}
		os.Remove(safeParseTemp)
	}

	reopenDb(livePath)
	safeParseTemp = ""
}

// rollbackSafeParse discards a failed safe parse:
// deletes the temp file and reopens the untouched live database.
func rollbackSafeParse() {
	if safeParseTemp == "" {
		return
	}
	livePath := dbPath()
	os.Remove(safeParseTemp)
	reopenDb(livePath)
	safeParseTemp = ""
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
		// Fresh install with no key_prefix anywhere: prompt and save to DB.
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

// --- write-session isolation ---

// dbHomePath returns the permanent home location for the database, next to the bib file.
func dbHomePath() string {
	return bibTeXFolder + bibTeXBaseName + cacheFileExtension
}

// dbIsolationActive reports whether the working path differs from the home path.
func dbIsolationActive() bool {
	return dbHomePath() != dbPath()
}

// maybeMigrateToHomePath establishes the home database copy on the first run after
// write-session isolation is introduced. If the home path is absent or empty but the
// working path holds a real database, the working copy becomes the initial home copy.
func maybeMigrateToHomePath() {
	if !dbIsolationActive() {
		return
	}
	home := dbHomePath()
	if info, err := os.Stat(home); err == nil && info.Size() > 0 {
		return
	}
	working := dbPath()
	wInfo, err := os.Stat(working)
	if err != nil || wInfo.Size() == 0 {
		return
	}
	if err := copyFile(working, home); err != nil {
		dbInteraction.Warning("Could not establish home database: %s", err)
		return
	}
	os.Chtimes(home, wInfo.ModTime(), wInfo.ModTime())
	dbInteraction.Progress("Established home database: %s", home)
}

// prepareWorkingDatabase ensures the working database is a current copy of the home
// database before a write session begins. Detects a stale working copy left by a
// crash and offers the user a chance to restore. Returns false on setup failure.
func prepareWorkingDatabase() bool {
	if !dbIsolationActive() {
		dbWriteSessionActive = true
		return true
	}
	home := dbHomePath()
	working := dbPath()

	hInfo, hErr := os.Stat(home)
	if hErr != nil {
		// No home DB yet. If a bib file exists for this base, create an empty home DB
		// so the normal open flow can proceed and auto-import it via ValidBibDb() == false.
		bibPath := bibTeXFolder + bibTeXBaseName + BibFileExtension
		if _, bibErr := os.Stat(bibPath); bibErr != nil {
			dbInteraction.Warning("Home database not found: %s", home)
			return false
		}
		f, createErr := os.Create(home)
		if createErr != nil {
			dbInteraction.Warning("Could not create home database %s: %s", home, createErr)
			return false
		}
		f.Close()
		hInfo, _ = os.Stat(home)
	}

	wInfo, wErr := os.Stat(working)

	// Crash recovery: primary check uses session markers; legacy fallback uses
	// size+mtime for DBs written before session markers were introduced.
	crashed := writeSessionIsCrashed()
	if !crashed && wErr == nil && wInfo.Size() > 0 &&
		wInfo.Size() != hInfo.Size() && wInfo.ModTime().After(hInfo.ModTime()) {
		crashed = true
	}
	if crashed {
		var entryCount int
		db.QueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = ?`, EntryTypeField).Scan(&entryCount)
		if entryCount > 0 {
			dbInteraction.Warning(WarningWorkingDbNewer)
			answer, _ := dbInteraction.AskForInput("Restore home from working copy? [y/N]")
			if strings.EqualFold(answer, "y") {
				db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`) //nolint:errcheck
				if err := copyFile(working, home); err != nil {
					dbInteraction.Warning("Could not restore: %s", err)
					return false
				}
				hInfo, _ = os.Stat(home)
				dbInteraction.Progress("Restored home database from working copy.")
			}
		}
	}

	// In sync: session markers confirm a clean previous close, sizes match, and home
	// was not modified after the working copy was last synced (guards against manual
	// sqlite3 edits to the home DB — SQLite DELETE does not shrink the file, so a
	// size-only check would silently reuse a stale working copy).
	if wErr == nil && writeSessionIsClean() && wInfo.Size() == hInfo.Size() && !hInfo.ModTime().After(wInfo.ModTime()) {
		markWriteSessionOpen()
		db.QueryRow(`SELECT total_changes()`).Scan(&changesAtSessionOpen)
		dbWriteSessionActive = true
		return true
	}

	dbInteraction.Progress(ProgressCopyingToWorkingDatabase)
	if err := copyFile(home, working); err != nil {
		dbInteraction.Warning("Could not copy database to working location: %s", err)
		return false
	}
	os.Remove(working + "-wal") //nolint:errcheck
	os.Remove(working + "-shm") //nolint:errcheck
	if hNew, err := os.Stat(home); err == nil {
		os.Chtimes(working, hNew.ModTime(), hNew.ModTime())
	}
	reopenDb(working)
	markWriteSessionOpen()
	db.QueryRow(`SELECT total_changes()`).Scan(&changesAtSessionOpen)
	dbWriteSessionActive = true
	return true
}

// flushWorkingDbToHome checkpoints the WAL and copies the working DB to the home
// path mid-session so that Ctrl-C during long-running network operations (URL
// checks, PDF downloads) does not lose changes already made.
// No-op when isolation is not active (single-DB mode).
func flushWorkingDbToHome() {
	if !dbIsolationActive() {
		return
	}
	db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	if err := copyFile(dbPath(), dbHomePath()); err != nil {
		dbInteraction.Warning("Could not flush working database to home: %s", err)
	}
}

// abandonWorkingDatabase closes the DB connection and deletes the working copy
// (including WAL/SHM) from the cache folder. Called when postCheckGate fails
// so the stale working copy does not trigger a spurious "restore?" prompt on
// the next run.
func abandonWorkingDatabase() {
	if !dbIsolationActive() {
		return
	}
	if db != nil {
		db.Close()
		db = nil
	}
	working := dbPath()
	os.Remove(working)
	os.Remove(working + "-wal")
	os.Remove(working + "-shm")
}

// finaliseWorkingDatabase copies the working database back to the home path,
// creates a timestamped backup of the previous home copy, and writes a SQL dump.
// Called at the end of main after all writes are flushed.
// Skips the copy entirely when no real data was written (only session markers).
func finaliseWorkingDatabase() {
	if !dbWriteSessionActive || !dbIsolationActive() {
		return
	}
	home := dbHomePath()
	working := dbPath()

	// Mark session closed and check whether anything real was written.
	// total_changes() is cumulative for this connection; changesAtSessionOpen was
	// recorded right after markWriteSessionOpen(). The close marker itself accounts
	// for one more change, so if total_changes() == changesAtSessionOpen+1, only
	// session markers were written — no need to copy the working DB back to home.
	if db != nil {
		markWriteSessionClosed()
		var totalChanges int64
		db.QueryRow(`SELECT total_changes()`).Scan(&totalChanges)
		if totalChanges <= changesAtSessionOpen+1 {
			db.Close()
			db = nil
			return // nothing real was written; home DB is already up to date
		}
	}

	if db != nil {
		db.Close()
		db = nil
	}

	ts := time.Now().Format("20060102_150405")
	backupPath := backupFolder + bibTeXBaseName + "_" + ts + cacheFileExtension
	if err := os.MkdirAll(backupFolder, 0o755); err == nil {
		if err := copyFile(home, backupPath); err != nil {
			dbInteraction.Warning("Could not back up home database: %s", err)
		}
	}

	dbInteraction.Progress(ProgressSavingDatabaseToHome)
	wInfo, wErr := os.Stat(working)
	if err := copyFile(working, home); err != nil {
		dbInteraction.Warning("Could not save working database to home: %s", err)
		return
	}
	// Sync home mtime to working's mtime so the next run's skip-copy check triggers.
	if wErr == nil {
		os.Chtimes(home, wInfo.ModTime(), wInfo.ModTime())
	}

	writeDatabaseDump()

	// writeDatabaseDump() opens the home DB via the sqlite3 CLI, which may trigger
	// a WAL checkpoint or other write that updates home's mtime. Read back home's
	// actual stored mtime and apply it to working so both agree exactly — prevents
	// a spurious "working newer than home" warning on the next run.
	if hFinal, err := os.Stat(home); err == nil {
		os.Chtimes(working, hFinal.ModTime(), hFinal.ModTime())
	}
}

// --- FK pre-check / post-check gate (step 15.1, Phase A) ---

// preCheckRepair syncs the bib_entry_keys anchor table to match bib_entries, then
// removes any orphan rows from dependent tables whose FK column is not in the anchor.
// Orphan rows can arrive via migrations run with FK OFF or from writes before FK was
// enforced; running this before maybeMigrateToFKSchema() ensures clean data is copied.
func preCheckRepair() {
	db.Exec(`PRAGMA foreign_keys = OFF`)
	db.Exec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key)
	         SELECT DISTINCT entry_key FROM bib_entries`)
	res, _ := db.Exec(`DELETE FROM bib_entry_keys
	                   WHERE entry_key NOT IN (SELECT DISTINCT entry_key FROM bib_entries)`)
	if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Warning("Removed %d stale anchor row(s) — cascading to dependent tables", n)
	}
	repair := func(table, fkCol string) {
		res, err := db.Exec(fmt.Sprintf(
			`DELETE FROM %s WHERE %s NOT IN (SELECT entry_key FROM bib_entry_keys)`,
			table, fkCol))
		if err != nil {
			dbInteraction.Warning("preCheckRepair %s: %s", table, err)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			dbInteraction.Warning("Repaired %d orphan row(s) in %s", n, table)
		}
	}
	repair("bib_groups", "entry_key")
	repair("losing_field_values", "entry_key")
	repair("entry_warnings", "key")
	repair("entry_metadata", "entry_key")
	db.Exec(`PRAGMA foreign_keys = ON`)
}

// postCheckGate re-syncs bib_entry_keys to the current bib_entries state, then
// uses PRAGMA foreign_key_check to verify all FK constraints. Returns true when
// clean. Called from the write tail of main() just before finaliseWorkingDatabase;
// a false return suppresses the home-DB copy so a bad run cannot corrupt persisted state.
func postCheckGate() bool {
	if dbWriteFailed {
		dbInteraction.Warning("Post-check: DB write failure(s) detected — home database not updated")
		return false
	}

	db.Exec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key)
	         SELECT DISTINCT entry_key FROM bib_entries`)
	db.Exec(`DELETE FROM bib_entry_keys
	         WHERE entry_key NOT IN (SELECT DISTINCT entry_key FROM bib_entries)`)

	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		dbInteraction.Warning("Post-check: foreign_key_check failed: %s", err)
		return false
	}
	defer rows.Close()

	ok := true
	for rows.Next() {
		var table, parent string
		var rowid, fkid int64
		rows.Scan(&table, &rowid, &parent, &fkid) //nolint:errcheck
		dbInteraction.Warning("Post-check: FK violation in %s (rowid %d → %s)", table, rowid, parent)
		ok = false
	}
	return ok
}

// writeDatabaseDump writes a SQL dump of the home database to $base.dump using
// the sqlite3 CLI tool. Skips with a warning if sqlite3 is not available.
func writeDatabaseDump() {
	dumpPath := bibTeXFolder + bibTeXBaseName + ".dump"
	out, err := os.Create(dumpPath)
	if err != nil {
		dbInteraction.Warning("Could not create dump file: %s", err)
		return
	}
	defer out.Close()
	cmd := exec.Command("sqlite3", dbHomePath(), ".dump")
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		dbInteraction.Warning("Could not write database dump (sqlite3 not in PATH?): %s", err)
		out.Close()
		os.Remove(dumpPath)
	}
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

// maybeMigrateFilterTableNames renames all legacy filter_* table names to their
// canonical non-prefixed form. This covers:
//   - The v23.3 intermediate rename filter_losing_field_values → losing_field_values
//   - All original filter_* tables that should never have had the prefix.
func maybeMigrateFilterTableNames() {
	renames := [][2]string{
		{"filter_losing_field_values", "losing_field_values"},
		{"filter_generic_field_mappings", "generic_field_mappings"},
		{"filter_cross_field_mappings", "cross_field_mappings"},
		{"filter_state_names", "state_names"},
		{"filter_state_countries", "state_countries"},
		{"filter_country_names", "country_names"},
		{"filter_booktitle_country_names", "booktitle_country_names"},
	}
	for _, pair := range renames {
		old, new := pair[0], pair[1]
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, old).Scan(&n)
		if n == 0 {
			continue
		}
		db.Exec(`ALTER TABLE ` + old + ` RENAME TO ` + new)
		db.Exec(`UPDATE table_modification_times SET table_name = ? WHERE table_name = ?`, new, old)
		dbInteraction.Progress("Migrated table %s → %s", old, new)
	}
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

// --- write-session markers ---
//
// Two virtual entries in table_modification_times track whether the working DB
// was left in a clean state:
//
//   write_session_open   — set to open_time when a write session starts;
//                          updated to close_time when it ends cleanly
//   write_session_closed — set to 0 when a write session starts;
//                          set to the same close_time when it ends cleanly
//
// After a clean close both entries hold the same non-zero time.
// After a crash or CTRL-C the closed entry is still 0 while open is non-zero.

func markWriteSessionOpen() {
	now := time.Now().UnixMicro()
	setTableDate("write_session_open", now)
	setTableDate("write_session_closed", 0)
}

func markWriteSessionClosed() {
	t := time.Now().UnixMicro()
	setTableDate("write_session_open", t)
	setTableDate("write_session_closed", t)
}

// writeSessionIsClean reports whether the working DB was cleanly closed in the
// previous write session (both marker times are equal and non-zero).
func writeSessionIsClean() bool {
	open := tableModTime("write_session_open")
	closed := tableModTime("write_session_closed")
	return open > 0 && open == closed
}

// writeSessionIsCrashed reports whether the working DB was left in an
// open-but-not-closed state (open time recorded, closed time is 0).
func writeSessionIsCrashed() bool {
	return tableModTime("write_session_open") > 0 &&
		tableModTime("write_session_closed") == 0
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

func setTableLastWritten(tableName string) {
	now := time.Now().UnixMicro()
	err := bibExec(`
		INSERT INTO table_modification_times (table_name, modification_time, last_written_time)
		  VALUES (?, 0, ?)
		  ON CONFLICT(table_name) DO UPDATE SET last_written_time = excluded.last_written_time;`,
		tableName, now)
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
//  3. generic_field_mappings: per-field value normalisation.
//     Depends on name_mappings (name values are normalised via name aliases).
//
//  4. filter_entry_field_mappings: per-(entry,field) overrides.
//     Depends on name_mappings and generic mappings.
//
//  5. cross_field_mappings: source-field → target-field value propagation.
//
//  6. key_hints, key_oldies, key_non_doubles: key alias tables.
//
//  7. dblp_parent, dblp_waived, entry_flags: DBLP-related tables.
//
//  8. urls_ignore: loaded on demand by specific commands.
//
// All field-mapping and metadata tables use write-through: mutations call db.Exec
// immediately rather than batching at end-of-run. Load functions use loading flags
// (fieldMappingsLoading, entryFieldMappingsLoading) to suppress the write-
// through during the initial DB→memory population pass.

// --- name_mappings table ---

func ensureNameMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS name_mappings (
		  alias TEXT PRIMARY KEY,
		  name  TEXT NOT NULL
		);`)
}

// upsertNameMapping writes a single alias → name pair to the DB. Suppressed
// during initial load (nameMappingsLoading) so bulk load does not generate O(n)
// individual writes.
func upsertNameMapping(alias, name string) {
	if nameMappingsLoading {
		return
	}
	if alias == name {
		return
	}
	if err := bibExec(`INSERT INTO name_mappings (alias, name) VALUES (?, ?)
	                    ON CONFLICT(alias) DO UPDATE SET name = excluded.name`, alias, name); err != nil {
		dbInteraction.Warning("name_mappings upsert failed: %s", err)
		dbWriteFailed = true
	}
}

// deleteNameMapping removes a single alias from the DB. Suppressed during load.
func deleteNameMapping(alias string) {
	if nameMappingsLoading {
		return
	}
	if err := bibExec(`DELETE FROM name_mappings WHERE alias = ?`, alias); err != nil {
		dbInteraction.Warning("name_mappings delete failed: %s", err)
		dbWriteFailed = true
	}
}

// loadNameMappingsFromDb populates l.NameAliasToName and l.NameToAliases from
// the DB, then derives additional aliases via FindAliases for each stored pair
// and for each unique canonical.
func loadNameMappingsFromDb(l *TBibTeXLibrary) {
	nameMappingsLoading = true
	defer func() { nameMappingsLoading = false }()

	rows, err := db.Query(`SELECT alias, name FROM name_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query name_mappings: %s", err)
		return
	}
	defer rows.Close()

	type pair struct{ alias, name string }
	var pairs []pair
	for rows.Next() {
		var alias, name string
		if err := rows.Scan(&alias, &name); err != nil {
			dbInteraction.Warning("Could not scan name_mappings row: %s", err)
			continue
		}
		pairs = append(pairs, pair{alias, name})
	}

	for _, p := range pairs {
		l.AddAlias(p.alias, p.name, &l.NameAliasToName, &l.NameToAliases, true)
	}

	for _, p := range pairs {
		l.FindAliases(p.name, p.alias)
	}

	canonicals := map[string]bool{}
	for _, p := range pairs {
		canonicals[p.name] = true
	}
	for canonical := range canonicals {
		l.FindAliases(canonical, canonical)
	}
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
	return newCachedTable(&TSQLiteTable[string, string]{
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
	}
}

// --- key_non_doubles table ---

func ensureKeyNonDoublesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_non_doubles (
		  key1 TEXT NOT NULL,
		  key2 TEXT NOT NULL,
		  PRIMARY KEY (key1, key2)
		);`)
}

// loadKeyNonDoublesFromDb populates l.NonDoubles from the DB.
// Stale pairs (unknown or aliased keys) are deleted from the DB immediately.
// Alias-resolved pairs are updated to their canonical form in the DB.
// Unimported DBLP: keys are kept as-is for future matching.
func loadKeyNonDoublesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT key1, key2 FROM key_non_doubles`)
	if err != nil {
		dbInteraction.Warning("Could not query key_non_doubles: %s", err)
		return
	}
	type rawPair struct{ k1, k2 string }
	var pairs []rawPair
	for rows.Next() {
		var key1, key2 string
		if err := rows.Scan(&key1, &key2); err != nil {
			dbInteraction.Warning("Could not scan key_non_doubles row: %s", err)
			continue
		}
		pairs = append(pairs, rawPair{key1, key2})
	}
	rows.Close()

	nonDoublesLoadingFromDb = true
	defer func() { nonDoublesLoadingFromDb = false }()

	deleteSQL := `DELETE FROM key_non_doubles WHERE key1 = ? AND key2 = ?`
	upsertSQL := `INSERT INTO key_non_doubles (key1, key2) VALUES (?, ?) ON CONFLICT DO NOTHING`

	for _, p := range pairs {
		r1 := l.resolveNonDoubleKey(p.k1)
		r2 := l.resolveNonDoubleKey(p.k2)
		if r1 == "" || r2 == "" || r1 == r2 {
			db.Exec(deleteSQL, p.k1, p.k2) //nolint:errcheck
			continue
		}
		if r1 != p.k1 || r2 != p.k2 {
			db.Exec(deleteSQL, p.k1, p.k2) //nolint:errcheck
			db.Exec(upsertSQL, r1, r2)     //nolint:errcheck
			db.Exec(upsertSQL, r2, r1)     //nolint:errcheck
		}
		l.AddNonDoubles(r1, r2)
	}
}

// saveKeyNonDoublesToDb writes the filtered non-doubles set directly to the DB without a file roundtrip.
// Pairs where one or both keys are unimported DBLP: keys are preserved alongside
// normal library-entry pairs.
func saveKeyNonDoublesToDb(l *TBibTeXLibrary) {
	dbExecSave("Could not clear key_non_doubles", `DELETE FROM key_non_doubles;`)
	insert := `INSERT INTO key_non_doubles (key1, key2) VALUES (?, ?) ON CONFLICT DO NOTHING;`
	isValidNonDoubleKey := func(k string) bool {
		return k == l.MapEntryKey(k) && (l.EntryExists(k) || strings.HasPrefix(k, "DBLP:"))
	}
	for key, set := range l.NonDoubles {
		if !isValidNonDoubleKey(key) {
			continue
		}
		for nonDouble := range set.Elements() {
			if nonDouble == key || !isValidNonDoubleKey(nonDouble) {
				continue
			}
			if l.EntryExists(key) && l.EntryExists(nonDouble) && l.EvidenceForBeingDifferentEntries(key, nonDouble) {
				continue
			}
			dbExecSave("key_non_doubles insert failed", insert, key, nonDouble)
		}
	}
	setTableDate("key_non_doubles", time.Now().UnixMicro())
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
	return newCachedTable(&TSQLiteTable[string, string]{
		upsertSQL: `INSERT INTO dblp_parent (child_key, parent_key) VALUES (?, ?)
		            ON CONFLICT(child_key) DO UPDATE SET parent_key = excluded.parent_key;`,
		deleteSQL: `DELETE FROM dblp_parent WHERE child_key = ?`,
		selectSQL: `SELECT child_key, parent_key FROM dblp_parent`,
		upsertArgs: func(k, v string) []any { return []any{k, v} },
		deleteArgs: func(k string) []any { return []any{k} },
		scanRow: func(rows *sql.Rows) (string, string, error) {
			var child, parent string
			return child, parent, rows.Scan(&child, &parent)
		},
	})
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
	return newCachedTable(&TSQLiteTable[string, bool]{
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
//     is removed; other oldie-related rows (key_hints, losing_field_values, …) stay.
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
		dbInteraction.Progress("repairDblpData: removed %d stale dblp field(s) from merged key_oldies", n)
	}

	// Category B: remove all rows for ghost entries (no entrytype, not in key_oldies).
	// CASCADE on bib_entry_keys propagates to bib_entries, losing_field_values, etc.
	db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck
	res, err = db.Exec(`
		DELETE FROM bib_entry_keys
		WHERE entry_key IN (
			SELECT DISTINCT entry_key FROM bib_entries WHERE field = ?
			EXCEPT SELECT entry_key FROM bib_entries WHERE field = ?
			EXCEPT SELECT alias FROM key_oldies
		)`, DBLPField, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("repairDblpData (B): %s", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Progress("repairDblpData: removed %d ghost entry rows", n)
	}

	// Remove stale dblp_canonical rows (target no longer exists as a live entry).
	res, err = db.Exec(`
		DELETE FROM dblp_canonical
		WHERE canonical_key NOT IN (SELECT entry_key FROM bib_entries WHERE field = ?)`,
		EntryTypeField)
	if err != nil {
		dbInteraction.Warning("repairDblpData (stale canonical): %s", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		dbInteraction.Progress("repairDblpData: removed %d stale dblp_canonical rows", n)
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
		dbInteraction.Progress("repairDblpData: back-filled %d missing dblp_canonical rows", n)
	}
}

// upsertDblpCanonical writes (dblpKey → canonicalKey) to dblp_canonical.
// When dblpKey is empty, the call is a no-op.
func upsertDblpCanonical(dblpKey, canonicalKey string) {
	if dblpKey == "" {
		return
	}
	bibExec( //nolint:errcheck
		`INSERT INTO dblp_canonical (dblp_key, canonical_key) VALUES (?, ?)
		 ON CONFLICT(dblp_key) DO UPDATE SET canonical_key = excluded.canonical_key`,
		dblpKey, canonicalKey)
}

// deleteDblpCanonicalByDblpKey removes the row for dblpKey from dblp_canonical.
func deleteDblpCanonicalByDblpKey(dblpKey string) {
	if dblpKey == "" {
		return
	}
	bibExec(`DELETE FROM dblp_canonical WHERE dblp_key = ?`, dblpKey) //nolint:errcheck
}

// deleteDblpCanonicalByCanonicalKey removes all rows for canonicalKey from dblp_canonical.
// Used when an entry's dblp field is cleared (value = "").
func deleteDblpCanonicalByCanonicalKey(canonicalKey string) {
	bibExec(`DELETE FROM dblp_canonical WHERE canonical_key = ?`, canonicalKey) //nolint:errcheck
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

// --- cross_field_mappings table ---

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

// loadCrossFieldMappingsFromDb populates l.FieldMappings from the DB.
// Normalises source and target values via MapFieldValue / NormaliseFieldValue.
// When source_field contains "field:entrytype" (e.g. "author:techreport"), only
// the field part (left of ":") is used for source-value normalisation; the full
// "field:entrytype" string is kept as the FieldMappings key so
// MaybeApplyFieldMappings can match it by entry type.
// Returns true when at least one stored value was changed by normalisation, so
// the caller can arrange a write-back to persist the canonical form.
func loadCrossFieldMappingsFromDb(l *TBibTeXLibrary) bool {
	rows, err := db.Query(
		`SELECT source_field, source_value, target_field, target_value FROM cross_field_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query cross_field_mappings: %s", err)
		return false
	}
	defer rows.Close()

	normalisationChanged := false
	for rows.Next() {
		var sourceField, sourceValue, targetField, targetValue string
		if err := rows.Scan(&sourceField, &sourceValue, &targetField, &targetValue); err != nil {
			dbInteraction.Warning("Could not scan cross_field_mappings row: %s", err)
			continue
		}
		normField, _, _ := strings.Cut(sourceField, ":")
		normSourceValue := l.MapFieldValue(normField, sourceValue)
		normTargetValue := l.NormaliseFieldValue(targetField, targetValue)
		if normSourceValue != sourceValue || normTargetValue != targetValue {
			normalisationChanged = true
		}
		l.AddFieldMapping(sourceField, normSourceValue, targetField, normTargetValue)
	}
	return normalisationChanged
}

// saveCrossFieldMappingsToDb writes the field mappings directly to the DB without a file roundtrip.
func saveCrossFieldMappingsToDb(l *TBibTeXLibrary) {
	dbExecSave("Could not clear cross_field_mappings", `DELETE FROM cross_field_mappings;`)
	upsert := `INSERT INTO cross_field_mappings
	             (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
	             ON CONFLICT(source_field, source_value, target_field)
	               DO UPDATE SET target_value = excluded.target_value;`
	for sourceField, sourceFieldMappings := range l.FieldMappings {
		for sourceValue, targetFieldMappings := range sourceFieldMappings {
			for targetField, targetValue := range targetFieldMappings {
				dbExecSave("cross_field_mappings insert failed", upsert, sourceField, sourceValue, targetField, targetValue)
			}
		}
	}
	setTableDate("cross_field_mappings", time.Now().UnixMicro())
}

// --- filter_entry_field_mappings table ---

func ensureEntryFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS filter_entry_field_mappings (
		  entry_key  TEXT NOT NULL,
		  field      TEXT NOT NULL,
		  winner     TEXT NOT NULL,
		  challenger TEXT NOT NULL,
		  PRIMARY KEY (entry_key, field, challenger)
		);`)
}

// loadEntryFieldMappingsFromDb populates l.EntryFieldSourceToTarget from the DB.
// The winner is read from bib_entries (current accepted value); only the loser is stored
// in losing_field_values. Rows with no matching bib_entries value are skipped.
// Returns true when at least one value was remapped by MapFieldValue, so the caller
// can arrange a write-back.
func loadEntryFieldMappingsFromDb(l *TBibTeXLibrary) bool {
	entryFieldMappingsLoading = true
	defer func() { entryFieldMappingsLoading = false }()

	rows, err := db.Query(`
		SELECT lfv.entry_key, lfv.field, lfv.value AS loser, be.value AS winner
		FROM losing_field_values lfv
		JOIN bib_entries be ON be.entry_key = lfv.entry_key AND be.field = lfv.field`)
	if err != nil {
		dbInteraction.Warning("Could not query losing_field_values: %s", err)
		return false
	}
	defer rows.Close()

	normalisationChanged := false
	for rows.Next() {
		var key, field, loser, winner string
		if err := rows.Scan(&key, &field, &loser, &winner); err != nil {
			dbInteraction.Warning("Could not scan losing_field_values row: %s", err)
			continue
		}
		normWinner := l.MapFieldValue(field, winner)
		normLoser := l.MapFieldValue(field, loser)
		if normWinner != winner || normLoser != loser {
			normalisationChanged = true
		}
		l.AddEntryFieldAlias(key, field, normLoser, normWinner, true)
	}
	return normalisationChanged
}

// saveEntryFieldMappingsToDb writes the losing field values to the DB without a file roundtrip.
func saveEntryFieldMappingsToDb(l *TBibTeXLibrary) {
	dbExecSave("Could not clear losing_field_values", `DELETE FROM losing_field_values`)
	upsert := `INSERT INTO losing_field_values (entry_key, field, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, field, value) DO NOTHING`
	for key, fieldChallenges := range l.EntryFieldSourceToTarget {
		if l.EntryExists(key) {
			for field, challenges := range fieldChallenges {
				if field != PreferredAliasField {
					for challenger, winner := range challenges {
						if l.MapFieldValue(field, challenger) != l.MapEntryFieldValue(key, field, winner) {
							dbExecSave("losing_field_values insert failed", upsert, key, field, challenger)
						}
					}
				}
			}
		}
	}
	setTableDate("losing_field_values", time.Now().UnixMicro())
}

// --- generic_field_mappings table ---

func ensureGenericFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS generic_field_mappings (
		  field      TEXT NOT NULL,
		  winner     TEXT NOT NULL,
		  challenger TEXT NOT NULL,
		  PRIMARY KEY (field, challenger)
		);`)
}

// loadGenericFieldMappingsFromDb populates l.GenericFieldSourceToTarget from the DB.
// Normalises challenger and winner via NormaliseFieldValue. Returns true when at least
// one stored value was changed by normalisation, so the caller can arrange a write-back.
func loadGenericFieldMappingsFromDb(l *TBibTeXLibrary) bool {
	fieldMappingsLoading = true
	defer func() { fieldMappingsLoading = false }()

	rows, err := db.Query(
		`SELECT field, winner, challenger FROM generic_field_mappings`)
	if err != nil {
		dbInteraction.Warning("Could not query generic_field_mappings: %s", err)
		return false
	}
	defer rows.Close()

	normalisationChanged := false
	for rows.Next() {
		var field, winner, challenger string
		if err := rows.Scan(&field, &winner, &challenger); err != nil {
			dbInteraction.Warning("Could not scan generic_field_mappings row: %s", err)
			continue
		}
		normChallenger := l.MapFieldValue(field, challenger)
		normWinner := l.MapFieldValue(field, winner)
		if normChallenger != challenger || normWinner != winner {
			normalisationChanged = true
		}
		l.AddGenericFieldAlias(field, normChallenger, normWinner, true)
	}
	return normalisationChanged
}

// saveGenericFieldMappingsToDb writes the filtered generic field aliases directly to the DB without a file roundtrip.
func saveGenericFieldMappingsToDb(l *TBibTeXLibrary) {
	dbExecSave("Could not clear generic_field_mappings", `DELETE FROM generic_field_mappings;`)
	upsert := `INSERT INTO generic_field_mappings
	             (field, winner, challenger) VALUES (?, ?, ?)
	             ON CONFLICT(field, challenger)
	               DO UPDATE SET winner = excluded.winner;`
	for field, challenges := range l.GenericFieldSourceToTarget {
		if field != PreferredAliasField {
			for challenger, winner := range challenges {
				if challenger != winner {
					dbExecSave("generic_field_mappings insert failed", upsert, field, l.MapFieldValue(field, winner), challenger)
				}
			}
		}
	}
	setTableDate("generic_field_mappings", time.Now().UnixMicro())
	setTableDate("losing_field_values", 0)
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

// maybeMigrateToFieldMappings populates field_mappings from the two legacy tables on first use.
// Generic mappings (field, challenger→winner) become (field, challenger, field, winner).
// Cross-field mappings copy directly. Safe to re-run: INSERT OR IGNORE skips duplicates.
func maybeMigrateToFieldMappings() {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM field_mappings`).Scan(&count)
	if count > 0 {
		return
	}
	db.Exec(`INSERT OR IGNORE INTO field_mappings (source_field, source_value, target_field, target_value)
	         SELECT field, challenger, field, winner FROM generic_field_mappings`)
	db.Exec(`INSERT OR IGNORE INTO field_mappings (source_field, source_value, target_field, target_value)
	         SELECT source_field, source_value, target_field, target_value FROM cross_field_mappings`)
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

// --- losing_field_values table ---

func ensureLosingFieldValuesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS losing_field_values (
		  entry_key     TEXT NOT NULL,
		  field         TEXT NOT NULL,
		  value         TEXT NOT NULL,
		  triage_status TEXT,
		  PRIMARY KEY (entry_key, field, value),
		  FOREIGN KEY (entry_key) REFERENCES bib_entry_keys(entry_key) ON DELETE CASCADE
		);`)
	// Add triage_status to databases created before this column existed.
	db.Exec(`ALTER TABLE losing_field_values ADD COLUMN triage_status TEXT`) //nolint:errcheck
}

// maybeMigrateToLosingFieldValues copies challengers from the legacy
// filter_entry_field_mappings table into losing_field_values on the first run
// after the 21.x upgrade.
func maybeMigrateToLosingFieldValues() {
	var newCount int
	db.QueryRow(`SELECT COUNT(*) FROM losing_field_values`).Scan(&newCount)
	if newCount > 0 {
		return
	}
	var oldCount int
	db.QueryRow(`SELECT COUNT(*) FROM filter_entry_field_mappings`).Scan(&oldCount)
	if oldCount == 0 {
		return
	}
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO losing_field_values (entry_key, field, value)
		SELECT entry_key, field, challenger FROM filter_entry_field_mappings
	`); err != nil {
		dbInteraction.Warning("Could not migrate entry field mappings: %s", err)
		return
	}
	// Stamp the timestamp beyond all upstream tables so a future -import of those
	// tables does not silently overwrite the migrated data.
	maxT := tableModTime("filter_entry_field_mappings")
	for _, dep := range []string{
		"state_names", "state_countries", "country_names",
		"name_mappings", "generic_field_mappings",
	} {
		if t := tableModTime(dep); t > maxT {
			maxT = t
		}
	}
	setTableDate("losing_field_values", maxT)
	dbInteraction.Progress("Migrated entry field mappings to losing_field_values")
}

// maybeMigrateStripLocalURL deletes all local-url rows from bib_entries on the first
// run after the PDFFiles migration. PDF presence is now derived from the filesystem
// (Library.PDFFiles) so these rows are stale and would never be read again.
func maybeMigrateStripLocalURL() {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = 'local-url'`).Scan(&count)
	if count == 0 {
		return
	}
	if _, err := db.Exec(`DELETE FROM bib_entries WHERE field = 'local-url'`); err != nil {
		dbInteraction.Warning("Could not strip local-url rows from bib_entries: %s", err)
		return
	}
	dbInteraction.Progress("Migrated: stripped %d local-url rows from bib_entries (PDF presence now tracked via filesystem)", count)
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
	db.Exec(`DELETE FROM entry_warnings`) //nolint:errcheck
}

// deleteEntryWarning removes a specific (key, warning) row, e.g. when a warning
// is subsequently waived and should not appear in repair.bib or warnings; selects.
func deleteEntryWarning(key, warning string) {
	db.Exec(`DELETE FROM entry_warnings WHERE key = ? AND warning = ?`, key, warning) //nolint:errcheck
}

// insertEntryWarning records key+warning, silently ignoring exact duplicates.
func insertEntryWarning(key, warning string) {
	db.Exec(`INSERT OR IGNORE INTO entry_warnings (key, warning) VALUES (?, ?)`, key, warning) //nolint:errcheck
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
func bibExec(query string, args ...any) error {
	if activeTx != nil {
		_, err := activeTx.Exec(query, args...)
		return err
	}
	_, err := db.Exec(query, args...)
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
			}
		}
	}

	for field := range snapshot {
		if _, exists := entry.Fields[field]; !exists {
			changed = true
			if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, entry.Key, field); err != nil {
				dbInteraction.Warning("bib_entries delete failed for %s.%s: %s", entry.Key, field, err)
			}
		}
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
}

// stopC2Tracking disarms C2 tracking and returns whether any entry was modified.
func stopC2Tracking() bool {
	c2TrackingActive = false
	return c2EntryModified
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
// Returns which tables were written so the caller can apply cascade re-read rules.
func repairDirtyMappingTables() (nameMappingsRepaired, entryFieldMappingsRepaired bool) {
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

	// name_mappings: "name;alias" per non-self pair
	if repair("name_mappings", base+NameMappingsFilePath) {
		rows, err := db.Query(`SELECT name, alias FROM name_mappings WHERE name != alias`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var name, alias string
				if rows.Scan(&name, &alias) == nil {
					lines = append(lines, name+csvDelimiter+alias)
				}
			}
			rows.Close()
			if writeRepairCSV(base+NameMappingsFilePath, lines) {
				finishRepair("name_mappings")
				nameMappingsRepaired = true
			}
		}
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

	// key_non_doubles: "key1;key2"
	if repair("key_non_doubles", base+KeyNonDoublesFilePath) {
		rows, err := db.Query(`SELECT key1, key2 FROM key_non_doubles`)
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
				finishRepair("key_non_doubles")
			}
		}
	}

	// cross_field_mappings: "source_field;source_value;target_field;target_value"
	if repair("cross_field_mappings", base+CrossFieldMappingsFilePath) {
		rows, err := db.Query(
			`SELECT source_field, source_value, target_field, target_value FROM cross_field_mappings`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var sf, sv, tf, tv string
				if rows.Scan(&sf, &sv, &tf, &tv) == nil {
					lines = append(lines, csvLine(sf, sv, tf, tv))
				}
			}
			rows.Close()
			if writeRepairCSV(base+CrossFieldMappingsFilePath, lines) {
				finishRepair("cross_field_mappings")
			}
		}
	}

	// losing_field_values: "entry_key;field;loser_value"
	if repair("losing_field_values", base+LosingFieldValuesFilePath) {
		rows, err := db.Query(
			`SELECT entry_key, field, value FROM losing_field_values`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var ek, field, loser string
				if rows.Scan(&ek, &field, &loser) == nil {
					lines = append(lines, csvLine(ek, field, loser))
				}
			}
			rows.Close()
			if writeRepairCSV(base+LosingFieldValuesFilePath, lines) {
				finishRepair("losing_field_values")
				entryFieldMappingsRepaired = true
			}
		}
	}

	// generic_field_mappings: "field;winner;challenger"
	if repair("generic_field_mappings", base+GenericFieldMappingsFilePath) {
		rows, err := db.Query(
			`SELECT field, winner, challenger FROM generic_field_mappings`)
		if err == nil {
			var lines []string
			for rows.Next() {
				var field, winner, challenger string
				if rows.Scan(&field, &winner, &challenger) == nil {
					lines = append(lines, csvLine(field, winner, challenger))
				}
			}
			rows.Close()
			if writeRepairCSV(base+GenericFieldMappingsFilePath, lines) {
				finishRepair("generic_field_mappings")
			}
		}
	}

	return
}

// upsertBibEntryField inserts or replaces a single (entry_key, field, value) row.
// An empty value deletes the row instead.
// Outside a transaction it compares the old cache value and only marks
// bibEntriesModified when the value actually changes, then updates the cache.
// Callers (setEntryField, deleteEntryField) must NOT pre-update the cache entry
// before calling this function, otherwise the comparison always finds equality.
func upsertBibEntryField(key, field, value string) {
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
		bibExec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key) VALUES (?)`, key) //nolint:errcheck
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

// deleteBibEntryField removes a single field row from bib_entries.
// Outside a transaction it checks for an actual change before marking modified.
func deleteBibEntryField(key, field string) {
	if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ? AND field = ?`, key, field); err != nil {
		dbInteraction.Warning("bib_entries delete failed for %s.%s: %s", key, field, err)
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
	db.Exec(`INSERT OR IGNORE INTO deleted_entries (key) VALUES (?)`, key) //nolint:errcheck
}

// isDeletedEntry reports whether key was explicitly deleted from the library.
func isDeletedEntry(key string) bool {
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM deleted_entries WHERE key = ?`, key).Scan(&n) //nolint:errcheck
	return n > 0
}

// deleteBibEntry removes all rows for a given entry key from bib_entries and the
// bib_entry_keys anchor. ON DELETE CASCADE propagates the anchor deletion to
// bib_groups, losing_field_values, entry_warnings, and entry_metadata.
func deleteBibEntry(key string) {
	if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ?`, key); err != nil {
		dbInteraction.Warning("bib_entries delete failed for %s: %s", key, err)
	}
	db.Exec(`DELETE FROM bib_entry_keys WHERE entry_key = ?`, key)
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
// preferredalias entries are persistent (written to key_oldies); DBLP entries are
// transient (regenerated each run from dblp_canonical, never written to the DB).
func buildKeyAliasesFromDb(l *TBibTeXLibrary) {
	dbInteraction.Progress(ProgressBuildingKeyAliases)
	if entryCache != nil {
		for key, e := range entryCache {
			if alias := e.Fields[PreferredAliasField]; alias != "" {
				l.AddKeyAlias(alias, key)
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
		l.AddKeyAlias(value, key)
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
