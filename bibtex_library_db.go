/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: LibraryDB
 *
 * SQLite persistence layer for the BibTeX library. Each logical data source
 * (name mappings, key hints, …) follows the same three-function pattern:
 *   maybeReload<X>Db()      — sync flat file → DB when file is newer
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
	"encoding/csv"
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
	dbInteraction          TInteraction
	db                     *sql.DB
	entryCache             map[string]*TBibTeXEntry
	entrySnapshots         map[string]map[string]string
	bibEntriesModified     bool
	c2TrackingActive       bool
	c2EntryModified        bool
	entryModTrackingActive bool
	entryModified          bool
	dbWriteSessionActive   bool
	changesAtSessionOpen   int64 // total_changes() right after markWriteSessionOpen; used to detect zero-write sessions
)

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
}

func connectToDatabase() {
	dbName := dbPath()

	var err error
	db, err = sql.Open(sqliteDatabaseDriver, dbName)
	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", dbName, err.Error())
	}
	db.SetMaxOpenConns(1) // SQLite supports only one write at a time; single connection avoids SQLITE_BUSY
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
	ensureCrossFieldMappingsTableExists()
	ensureEntryFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureURLsIgnoreTableExists()
	ensureEntryMetadataTableExists()
	ensureLosingFieldValuesTableExists()
	ensureEntryWarningsTableExists()
	ensureDeletedEntriesTableExists()
	ensureConfigTableExists()
	maybeBootstrapConfigFromFile()
	loadBibTeXSettings()
	ensureShortenMappingsTableExists()
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
	db.SetMaxOpenConns(1)
	configureDatabasePragmas()
	ensureTableDatesTableExists()
	maybeMigrateFilterTableNames()
	ensureNameMappingsTableExists()
	ensureKeyHintsTableExists()
	ensureKeyOldiesTableExists()
	ensureKeyNonDoublesTableExists()
	ensureDblpParentTableExists()
	ensureDblpWaivedTableExists()
	ensureCrossFieldMappingsTableExists()
	ensureEntryFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureURLsIgnoreTableExists()
	ensureEntryMetadataTableExists()
	ensureLosingFieldValuesTableExists()
	ensureConfigTableExists()
	ensureShortenMappingsTableExists()
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
				if err := copyFile(working, home); err != nil {
					dbInteraction.Warning("Could not restore: %s", err)
					return false
				}
				hInfo, _ = os.Stat(home)
				dbInteraction.Progress("Restored home database from working copy.")
			}
		}
	}

	// In sync: session markers confirm a clean previous close AND sizes match.
	if wErr == nil && writeSessionIsClean() && wInfo.Size() == hInfo.Size() {
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

// --- Filter-file reload dependency chain ---
//
// Each mapping table has a CSV flat file and a corresponding SQLite cache table.
// The table_modification_times table records the mtime of each CSV at the point it
// was last loaded. maybeReload* functions compare the current CSV mtime against that
// stored value and re-import only when the file is newer (avoids redundant work).
//
// Reload order in loadMappingFiles() / ReadAddressMappings():
//
//  1. Address tables (state_names, state_countries, country_names):
//     Loaded first because they feed into address normalisation used by the field
//     mapping tables below. If any address file changes, the three field-mapping
//     table timestamps are zeroed so they reload unconditionally on the next step.
//
//  2. name_mappings: author/editor name canonicalisation; independent of others.
//
//  3. generic_field_mappings: per-field value normalisation.
//     Depends on name_mappings (name values are normalised via name aliases).
//
//  4. filter_entry_field_mappings: per-(entry,field) overrides.
//     Depends on name_mappings and generic mappings (values are cross-normalised).
//
//  5. cross_field_mappings: source-field → target-field value propagation.
//     Depends on address tables, name_mappings, and both field mapping tables.
//
//  6. key_hints, key_oldies, key_non_doubles: key alias tables; independent of
//     field mappings but must be loaded before the BibTeX parse/validation pass.
//
//  7. dblp_parent, dblp_waived, dblp_key_missing, entry_lineage, entry_flags:
//     DBLP-related and lineage tables; independent of the mapping chain.
//
//  8. pdf_confirmed_ok, urls_ignore: loaded on demand by specific commands.
//
// Modified flags (e.g. genericFieldMappingsModified) are set by the load functions
// when the CSV content differs from what was previously in the DB. The write tail in
// main() checks these flags and calls the corresponding Write* functions.

// --- name_mappings table ---

var nameMappingsFileWritingAllowed = true

func ensureNameMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS name_mappings (
		  alias TEXT PRIMARY KEY,
		  name  TEXT NOT NULL
		);`)
}

// maybeReloadNameMappingsDb syncs the flat file into the DB when the file is newer.
// Each line must have two non-empty fields: canonical name and alias.
func maybeReloadNameMappingsDb() {

	nameMappingsFileName := bibTeXFolder + bibTeXBaseName + NameMappingsFilePath

	if fileModTime(nameMappingsFileName) <= tableModTime("name_mappings") {
		return
	}

	dbInteraction.Progress("Reloading name mappings from %s", nameMappingsFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS name_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop name_mappings table: %s", err)
	}
	ensureNameMappingsTableExists()

	upsert := `INSERT INTO name_mappings (alias, name) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET name = excluded.name;`

	processFile(nameMappingsFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) >= 2 && elements[0] != "" && elements[1] != "" {
			name := csvUnquoteField(elements[0])
			alias := csvUnquoteField(elements[1])
			if _, err := db.Exec(upsert, ApplyLaTeXMap(alias), ApplyLaTeXMap(name)); err != nil {
				dbInteraction.Warning("Name mapping insertion failed: %s", err)
			}
		} else {
			dbInteraction.Warning(WarningNameMappingsLineTooShort, line)
			nameMappingsFileWritingAllowed = false
		}
	})

	setTableDate("name_mappings", fileModTime(nameMappingsFileName))
	setTableDate("losing_field_values", 0)
}

// loadNameMappingsFromDb populates l.NameAliasToName and l.NameToAliases from
// the DB, then derives additional aliases via FindAliases for each stored pair
// and for each unique canonical.
func loadNameMappingsFromDb(l *TBibTeXLibrary) {
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

// saveNameMappingsToDb upserts all non-self alias pairs into the DB and
// refreshes the table modification time to match the (just-written) flat file.
func saveNameMappingsToDb(l *TBibTeXLibrary) {
	tx, err := db.Begin()
	if err != nil {
		dbInteraction.Warning("Could not begin name_mappings save transaction: %s", err)
		return
	}
	if _, err := tx.Exec(`DELETE FROM name_mappings`); err != nil {
		dbInteraction.Warning("Could not clear name_mappings table: %s", err)
		tx.Rollback()
		return
	}
	upsert := `INSERT INTO name_mappings (alias, name) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET name = excluded.name;`
	for alias, name := range l.NameAliasToName {
		if alias != name {
			if _, err := tx.Exec(upsert, alias, name); err != nil {
				dbInteraction.Warning("Name mapping upsert failed: %s", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		dbInteraction.Warning("Could not commit name_mappings save: %s", err)
		tx.Rollback()
		return
	}

	nameMappingsFileName := bibTeXFolder + bibTeXBaseName + NameMappingsFilePath
	setTableDate("name_mappings", fileModTime(nameMappingsFileName))
}

// --- key_hints table ---

var keyHintsFileWritingAllowed = true

func ensureKeyHintsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_hints (
		  hint TEXT PRIMARY KEY,
		  key  TEXT NOT NULL
		);`)
}

// maybeReloadKeyHintsDb syncs the flat file into the DB when the file is newer.
func maybeReloadKeyHintsDb() {
	keyHintsFileName := bibTeXFolder + bibTeXBaseName + KeyHintsFilePath

	if fileModTime(keyHintsFileName) <= tableModTime("key_hints") {
		return
	}

	dbInteraction.Progress("Reloading key hints from %s", keyHintsFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS key_hints;`); err != nil {
		dbInteraction.Warning("Could not drop key_hints table: %s", err)
	}
	ensureKeyHintsTableExists()

	upsert := `INSERT INTO key_hints (hint, key) VALUES (?, ?)
	             ON CONFLICT(hint) DO UPDATE SET key = excluded.key;`

	processFile(keyHintsFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 {
			dbInteraction.Warning(WarningKeyHintsLineTooShort, line)
			keyHintsFileWritingAllowed = false
			return
		}
		// File format: key TAB hint  (elements[0]=key, elements[1]=hint)
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("Key hint insertion failed: %s", err)
		}
	})

	setTableDate("key_hints", fileModTime(keyHintsFileName))
}

// loadKeyHintsFromDb populates l.HintToKey from the DB.
func loadKeyHintsFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT hint, key FROM key_hints`)
	if err != nil {
		dbInteraction.Warning("Could not query key_hints: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var hint, key string
		if err := rows.Scan(&hint, &key); err != nil {
			dbInteraction.Warning("Could not scan key_hints row: %s", err)
			continue
		}
		// Skip stale hints whose target was removed (e.g. by an interrupted merge).
		// Mark modified so saveKeyHintsToDb will write the clean state at end of run.
		if resolvedKey := l.MapEntryKey(key); !bibEntryExists(resolvedKey) {
			l.keyHintsModified = true
			continue
		}
		l.AddKeyHint(hint, key)
	}
}

// syncKeyHintsDbFromFile forces the DB to be reloaded from the flat file.
// Called after WriteKeyHintsFile so the DB mirrors the filtered flat file content.
func syncKeyHintsDbFromFile() {
	setTableDate("key_hints", 0)
	maybeReloadKeyHintsDb()
}

// saveKeyHintsToDb writes the filtered key hints directly to the DB without a file roundtrip.
func saveKeyHintsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM key_hints;`); err != nil {
		dbInteraction.Warning("Could not clear key_hints: %s", err)
	}
	upsert := `INSERT INTO key_hints (hint, key) VALUES (?, ?)
	             ON CONFLICT(hint) DO UPDATE SET key = excluded.key;`
	for source, target := range l.HintToKey {
		target = l.MapEntryKey(target)
		if bibEntryExists(target) &&
			source != target &&
			source != KeyForDBLP(l.EntryFieldValueity(target, DBLPField)) {
			if _, err := db.Exec(upsert, source, target); err != nil {
				dbInteraction.Warning("key_hints insert failed: %s", err)
			}
		}
	}
	setTableDate("key_hints", fileModTime(bibTeXFolder+bibTeXBaseName+KeyHintsFilePath))
}

// --- key_oldies table ---

var keyOldiesFileWritingAllowed = true

func ensureKeyOldiesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_oldies (
		  alias TEXT PRIMARY KEY,
		  key   TEXT NOT NULL
		);`)
}

// maybeReloadKeyOldiesDb syncs the flat file into the DB when the file is newer.
func maybeReloadKeyOldiesDb() {
	keyOldiesFileName := bibTeXFolder + bibTeXBaseName + KeyOldiesFilePath

	if fileModTime(keyOldiesFileName) <= tableModTime("key_oldies") {
		return
	}

	dbInteraction.Progress("Reloading key oldies from %s", keyOldiesFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS key_oldies;`); err != nil {
		dbInteraction.Warning("Could not drop key_oldies table: %s", err)
	}
	ensureKeyOldiesTableExists()

	upsert := `INSERT INTO key_oldies (alias, key) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET key = excluded.key;`

	processFile(keyOldiesFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 {
			dbInteraction.Warning(WarningKeyAliasesLineTooShort, line)
			keyOldiesFileWritingAllowed = false
			return
		}
		// File format: key TAB alias  (elements[0]=key, elements[1]=alias)
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("Key oldie insertion failed: %s", err)
		}
	})

	setTableDate("key_oldies", fileModTime(keyOldiesFileName))
	setTableDate("key_non_doubles", 0)
	setTableDate("key_hints", 0)
}

// loadKeyOldiesFromDb populates l.KeyToKey from the DB.
// Rows whose target no longer exists as a bib entry, or whose chain passes through
// a non-existent intermediate, are skipped and flagged dirty so saveKeyOldiesToDb
// will write a clean (flattened) state at end of run.
func loadKeyOldiesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT alias, key FROM key_oldies`)
	if err != nil {
		dbInteraction.Warning("Could not query key_oldies: %s", err)
		return
	}
	defer rows.Close()

	type pair struct{ alias, key string }
	var pairs []pair
	for rows.Next() {
		var alias, key string
		if err := rows.Scan(&alias, &key); err != nil {
			dbInteraction.Warning("Could not scan key_oldies row: %s", err)
			continue
		}
		pairs = append(pairs, pair{alias, key})
	}

	for _, p := range pairs {
		l.AddKeyAlias(p.alias, p.key)
	}

	// Detect chains that need flattening or targets that no longer exist.
	for _, p := range pairs {
		resolved := l.MapEntryKey(p.key)
		if !bibEntryExists(resolved) || resolved != p.key {
			l.keyOldiesModified = true
			break
		}
	}
}

// syncKeyOldiesDbFromFile forces the DB to be reloaded from the flat file.
// Called after WriteKeyOldiesFile so the DB mirrors the filtered flat file content.
func syncKeyOldiesDbFromFile() {
	setTableDate("key_oldies", 0)
	maybeReloadKeyOldiesDb()
}

// saveKeyOldiesToDb writes the filtered key aliases directly to the DB without a file roundtrip.
func saveKeyOldiesToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM key_oldies;`); err != nil {
		dbInteraction.Warning("Could not clear key_oldies: %s", err)
	}
	upsert := `INSERT INTO key_oldies (alias, key) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET key = excluded.key;`
	for source, target := range l.KeyToKey {
		target = l.MapEntryKey(target)
		if bibEntryExists(target) && source != target && IsValidKey(source) {
			if _, err := db.Exec(upsert, source, target); err != nil {
				dbInteraction.Warning("key_oldies insert failed: %s", err)
			}
		}
	}
	setTableDate("key_oldies", fileModTime(bibTeXFolder+bibTeXBaseName+KeyOldiesFilePath))
}

// --- key_non_doubles table ---

var keyNonDoublesFileWritingAllowed = true

func ensureKeyNonDoublesTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS key_non_doubles (
		  key1 TEXT NOT NULL,
		  key2 TEXT NOT NULL,
		  PRIMARY KEY (key1, key2)
		);`)
}

// maybeReloadKeyNonDoublesDb syncs the flat file into the DB when the file is newer.
func maybeReloadKeyNonDoublesDb() {
	keyNonDoublesFileName := bibTeXFolder + bibTeXBaseName + KeyNonDoublesFilePath

	if fileModTime(keyNonDoublesFileName) <= tableModTime("key_non_doubles") {
		return
	}

	dbInteraction.Progress("Reloading key non-doubles from %s", keyNonDoublesFileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS key_non_doubles;`); err != nil {
		dbInteraction.Warning("Could not drop key_non_doubles table: %s", err)
	}
	ensureKeyNonDoublesTableExists()

	insert := `INSERT INTO key_non_doubles (key1, key2) VALUES (?, ?)
	             ON CONFLICT(key1, key2) DO NOTHING;`

	processFile(keyNonDoublesFileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 {
			dbInteraction.Warning(WarningNonDoublesLineTooShort, line)
			keyNonDoublesFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(insert, elements[0], elements[1]); err != nil {
			dbInteraction.Warning("Key non-double insertion failed: %s", err)
		}
	})

	setTableDate("key_non_doubles", fileModTime(keyNonDoublesFileName))
}

// loadKeyNonDoublesFromDb populates l.NonDoubles from the DB.
// Calls resolveNonDoubleKey on both keys so that stale aliases are dropped and
// unimported DBLP: keys are kept as-is for future matching.
func loadKeyNonDoublesFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT key1, key2 FROM key_non_doubles`)
	if err != nil {
		dbInteraction.Warning("Could not query key_non_doubles: %s", err)
		return
	}
	defer rows.Close()

	nonDoublesLoadingFromDb = true
	defer func() { nonDoublesLoadingFromDb = false }()

	for rows.Next() {
		var key1, key2 string
		if err := rows.Scan(&key1, &key2); err != nil {
			dbInteraction.Warning("Could not scan key_non_doubles row: %s", err)
			continue
		}
		r1 := l.resolveNonDoubleKey(key1)
		r2 := l.resolveNonDoubleKey(key2)
		if r1 != "" && r2 != "" {
			l.AddNonDoubles(r1, r2)
		}
	}
}

// syncKeyNonDoublesDbFromFile forces the DB to be reloaded from the flat file.
// Called after WriteKeyNonDoublesFile so the DB mirrors the filtered flat file content.
func syncKeyNonDoublesDbFromFile() {
	setTableDate("key_non_doubles", 0)
	maybeReloadKeyNonDoublesDb()
}

// saveKeyNonDoublesToDb writes the filtered non-doubles set directly to the DB without a file roundtrip.
// Pairs where one or both keys are unimported DBLP: keys are preserved alongside
// normal library-entry pairs.
func saveKeyNonDoublesToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM key_non_doubles;`); err != nil {
		dbInteraction.Warning("Could not clear key_non_doubles: %s", err)
	}
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
			if _, err := db.Exec(insert, key, nonDouble); err != nil {
				dbInteraction.Warning("key_non_doubles insert failed: %s", err)
			}
		}
	}
	setTableDate("key_non_doubles", fileModTime(bibTeXFolder+bibTeXBaseName+KeyNonDoublesFilePath))
}

// --- dblp_parent table ---

var dblpParentFileWritingAllowed = true

func ensureDblpParentTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS dblp_parent (
		  child_key  TEXT NOT NULL PRIMARY KEY,
		  parent_key TEXT NOT NULL
		);`)
}

// maybeReloadDblpParentDb syncs the flat file into the DB when the file is newer.
func maybeReloadDblpParentDb() {
	fileName := bibTeXFolder + bibTeXBaseName + DblpParentFilePath

	if fileModTime(fileName) <= tableModTime("dblp_parent") {
		return
	}

	if _, err := db.Exec(`DROP TABLE IF EXISTS dblp_parent;`); err != nil {
		dbInteraction.Warning("Could not drop dblp_parent table: %s", err)
	}
	ensureDblpParentTableExists()

	insert := `INSERT INTO dblp_parent (child_key, parent_key) VALUES (?, ?)
	             ON CONFLICT(child_key) DO UPDATE SET parent_key = excluded.parent_key;`

	processCSVFile(fileName, func(fields []string) {
		if len(fields) < 2 {
			dbInteraction.Warning("dblp_parent line too short: %s", strings.Join(fields, csvDelimiter))
			dblpParentFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(insert, fields[0], fields[1]); err != nil {
			dbInteraction.Warning("dblp_parent insertion failed: %s", err)
		}
	})

	setTableDate("dblp_parent", fileModTime(fileName))
}

// loadDblpParentFromDb populates l.DblpParentOverrides from the DB.
func loadDblpParentFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT child_key, parent_key FROM dblp_parent`)
	if err != nil {
		dbInteraction.Warning("Could not query dblp_parent: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			dbInteraction.Warning("Could not scan dblp_parent row: %s", err)
			continue
		}
		l.DblpParentOverrides[child] = parent
	}
}

// saveDblpParentToDb writes l.DblpParentOverrides directly to the DB.
func saveDblpParentToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM dblp_parent;`); err != nil {
		dbInteraction.Warning("Could not clear dblp_parent: %s", err)
	}
	insert := `INSERT INTO dblp_parent (child_key, parent_key) VALUES (?, ?)
	             ON CONFLICT(child_key) DO UPDATE SET parent_key = excluded.parent_key;`
	for child, parent := range l.DblpParentOverrides {
		if _, err := db.Exec(insert, child, parent); err != nil {
			dbInteraction.Warning("dblp_parent insert failed: %s", err)
		}
	}
	setTableDate("dblp_parent", fileModTime(bibTeXFolder+bibTeXBaseName+DblpParentFilePath))
}

// --- dblp_waived table ---

var dblpWaivedFileWritingAllowed = true

func ensureDblpWaivedTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS dblp_waived (
		  key TEXT NOT NULL PRIMARY KEY
		);`)
}

// maybeReloadDblpWaivedDb syncs the flat file into the DB when the file is newer.
func maybeReloadDblpWaivedDb() {
	fileName := bibTeXFolder + bibTeXBaseName + DblpWaivedFilePath

	if fileModTime(fileName) <= tableModTime("dblp_waived") {
		return
	}

	if _, err := db.Exec(`DROP TABLE IF EXISTS dblp_waived;`); err != nil {
		dbInteraction.Warning("Could not drop dblp_waived table: %s", err)
	}
	ensureDblpWaivedTableExists()

	insert := `INSERT INTO dblp_waived (key) VALUES (?) ON CONFLICT(key) DO NOTHING;`

	processCSVFile(fileName, func(fields []string) {
		if len(fields) < 1 || fields[0] == "" {
			return
		}
		if _, err := db.Exec(insert, fields[0]); err != nil {
			dbInteraction.Warning("dblp_waived insertion failed: %s", err)
		}
	})

	setTableDate("dblp_waived", fileModTime(fileName))
}

// loadDblpWaivedFromDb populates l.DblpWaived from the DB.
func loadDblpWaivedFromDb(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT key FROM dblp_waived`)
	if err != nil {
		dbInteraction.Warning("Could not query dblp_waived: %s", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			dbInteraction.Warning("Could not scan dblp_waived row: %s", err)
			continue
		}
		l.DblpWaived.Add(key)
	}
}

// saveDblpWaivedToDb writes l.DblpWaived directly to the DB.
func saveDblpWaivedToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM dblp_waived;`); err != nil {
		dbInteraction.Warning("Could not clear dblp_waived: %s", err)
	}
	insert := `INSERT INTO dblp_waived (key) VALUES (?) ON CONFLICT(key) DO NOTHING;`
	for key := range l.DblpWaived.Elements() {
		if _, err := db.Exec(insert, key); err != nil {
			dbInteraction.Warning("dblp_waived insert failed: %s", err)
		}
	}
	setTableDate("dblp_waived", fileModTime(bibTeXFolder+bibTeXBaseName+DblpWaivedFilePath))
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

var crossFieldMappingsFileWritingAllowed = true

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

// maybeReloadCrossFieldMappingsDb syncs the flat file into the DB when the file is newer,
// or when entry_field_mappings (upstream dependency) has been updated.
func maybeReloadCrossFieldMappingsDb() {
	fileName := bibTeXFolder + bibTeXBaseName + CrossFieldMappingsFilePath

	crossTime := tableModTime("cross_field_mappings")
	if fileModTime(fileName) <= crossTime &&
		tableModTime("name_mappings") <= crossTime &&
		tableModTime("state_names") <= crossTime &&
		tableModTime("state_countries") <= crossTime &&
		tableModTime("country_names") <= crossTime &&
		tableModTime("generic_field_mappings") <= crossTime {
		return
	}

	dbInteraction.Progress("Reloading cross-field mappings from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS cross_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop cross_field_mappings table: %s", err)
	}
	ensureCrossFieldMappingsTableExists()

	upsert := `INSERT INTO cross_field_mappings
	             (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
	             ON CONFLICT(source_field, source_value, target_field)
	               DO UPDATE SET target_value = excluded.target_value;`

	processCSVFile(fileName, func(fields []string) {
		if len(fields) < 4 {
			dbInteraction.Warning(WarningFieldMappingsTooShort, strings.Join(fields, csvDelimiter))
			crossFieldMappingsFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(upsert, fields[0], fields[1], fields[2], fields[3]); err != nil {
			dbInteraction.Warning("Cross-field mapping insertion failed: %s", err)
		}
	})

	setTableDate("cross_field_mappings", time.Now().UnixMicro())
}

// loadCrossFieldMappingsFromDb populates l.FieldMappings from the DB.
// Normalises source and target values via MapFieldValue / NormaliseFieldValue.
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
		normSourceValue := l.MapFieldValue(sourceField, sourceValue)
		normTargetValue := l.NormaliseFieldValue(targetField, targetValue)
		if normSourceValue != sourceValue || normTargetValue != targetValue {
			normalisationChanged = true
		}
		l.AddFieldMapping(sourceField, normSourceValue, targetField, normTargetValue)
	}
	return normalisationChanged
}

// syncCrossFieldMappingsDbFromFile forces the DB to be reloaded from the flat file.
func syncCrossFieldMappingsDbFromFile() {
	setTableDate("cross_field_mappings", 0)
	maybeReloadCrossFieldMappingsDb()
}

// saveCrossFieldMappingsToDb writes the field mappings directly to the DB without a file roundtrip.
func saveCrossFieldMappingsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM cross_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not clear cross_field_mappings: %s", err)
	}
	upsert := `INSERT INTO cross_field_mappings
	             (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
	             ON CONFLICT(source_field, source_value, target_field)
	               DO UPDATE SET target_value = excluded.target_value;`
	for sourceField, sourceFieldMappings := range l.FieldMappings {
		for sourceValue, targetFieldMappings := range sourceFieldMappings {
			for targetField, targetValue := range targetFieldMappings {
				if _, err := db.Exec(upsert, sourceField, sourceValue, targetField, targetValue); err != nil {
					dbInteraction.Warning("cross_field_mappings insert failed: %s", err)
				}
			}
		}
	}
	setTableDate("cross_field_mappings", fileModTime(bibTeXFolder+bibTeXBaseName+CrossFieldMappingsFilePath))
}

// --- filter_entry_field_mappings table ---

var entryFieldMappingsFileWritingAllowed = true

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

// maybeReloadEntryFieldMappingsDb syncs the losing_field_values CSV into the DB when the
// file is newer, or when any upstream normaliser table has been updated.
func maybeReloadEntryFieldMappingsDb() {
	fileName := bibTeXFolder + bibTeXBaseName + LosingFieldValuesFilePath

	entryTime := tableModTime("losing_field_values")
	if fileModTime(fileName) <= entryTime &&
		tableModTime("name_mappings") <= entryTime &&
		tableModTime("generic_field_mappings") <= entryTime &&
		tableModTime("state_names") <= entryTime &&
		tableModTime("state_countries") <= entryTime &&
		tableModTime("country_names") <= entryTime {
		return
	}

	dbInteraction.Progress("Reloading entry field aliases from %s", fileName)

	if _, err := db.Exec(`DELETE FROM losing_field_values`); err != nil {
		dbInteraction.Warning("Could not clear losing_field_values: %s", err)
	}

	upsert := `INSERT INTO losing_field_values (entry_key, field, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, field, value) DO NOTHING`

	processCSVFile(fileName, func(fields []string) {
		if len(fields) < 3 {
			dbInteraction.Warning(WarningEntryFieldMappingsLineTooShort, strings.Join(fields, csvDelimiter))
			entryFieldMappingsFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(upsert, fields[0], fields[1], fields[2]); err != nil {
			dbInteraction.Warning("Entry field alias insertion failed: %s", err)
		}
	})

	setTableDate("losing_field_values", time.Now().UnixMicro())
	setTableDate("cross_field_mappings", 0)
}

// loadEntryFieldMappingsFromDb populates l.EntryFieldSourceToTarget from the DB.
// The winner is read from bib_entries (current accepted value); only the loser is stored
// in losing_field_values. Rows with no matching bib_entries value are skipped.
// Returns true when at least one value was remapped by MapFieldValue, so the caller
// can arrange a write-back.
func loadEntryFieldMappingsFromDb(l *TBibTeXLibrary) bool {
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

// syncEntryFieldMappingsDbFromFile forces the DB to be reloaded from the flat file.
func syncEntryFieldMappingsDbFromFile() {
	setTableDate("losing_field_values", 0)
	maybeReloadEntryFieldMappingsDb()
}

// saveEntryFieldMappingsToDb writes the losing field values to the DB without a file roundtrip.
func saveEntryFieldMappingsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM losing_field_values`); err != nil {
		dbInteraction.Warning("Could not clear losing_field_values: %s", err)
	}
	upsert := `INSERT INTO losing_field_values (entry_key, field, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, field, value) DO NOTHING`
	for key, fieldChallenges := range l.EntryFieldSourceToTarget {
		if l.EntryExists(key) {
			for field, challenges := range fieldChallenges {
				if field != PreferredAliasField {
					for challenger, winner := range challenges {
						if l.MapFieldValue(field, challenger) != l.MapEntryFieldValue(key, field, winner) {
							if _, err := db.Exec(upsert, key, field, challenger); err != nil {
								dbInteraction.Warning("losing_field_values insert failed: %s", err)
							}
						}
					}
				}
			}
		}
	}
	setTableDate("losing_field_values", fileModTime(bibTeXFolder+bibTeXBaseName+LosingFieldValuesFilePath))
}

// --- generic_field_mappings table ---

var genericFieldMappingsFileWritingAllowed = true

func ensureGenericFieldMappingsTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS generic_field_mappings (
		  field      TEXT NOT NULL,
		  winner     TEXT NOT NULL,
		  challenger TEXT NOT NULL,
		  PRIMARY KEY (field, challenger)
		);`)
}

// maybeReloadGenericFieldMappingsDb syncs the flat file into the DB when the file is newer
// or when any upstream normaliser table (name_mappings, address tables) has been updated.
func maybeReloadGenericFieldMappingsDb() {
	fileName := bibTeXFolder + bibTeXBaseName + GenericFieldMappingsFilePath

	genericTime := tableModTime("generic_field_mappings")
	if fileModTime(fileName) <= genericTime &&
		tableModTime("name_mappings") <= genericTime &&
		tableModTime("state_names") <= genericTime &&
		tableModTime("state_countries") <= genericTime &&
		tableModTime("country_names") <= genericTime {
		return
	}

	dbInteraction.Progress("Reloading generic field aliases from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS generic_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not drop generic_field_mappings table: %s", err)
	}
	ensureGenericFieldMappingsTableExists()

	upsert := `INSERT INTO generic_field_mappings
	             (field, winner, challenger) VALUES (?, ?, ?)
	             ON CONFLICT(field, challenger)
	               DO UPDATE SET winner = excluded.winner;`

	processCSVFile(fileName, func(fields []string) {
		if len(fields) < 3 {
			dbInteraction.Warning(WarningGenericFieldMappingsLineTooShort, strings.Join(fields, csvDelimiter))
			genericFieldMappingsFileWritingAllowed = false
			return
		}
		if _, err := db.Exec(upsert, fields[0], fields[1], fields[2]); err != nil {
			dbInteraction.Warning("Generic field alias insertion failed: %s", err)
		}
	})

	setTableDate("generic_field_mappings", time.Now().UnixMicro())
	setTableDate("losing_field_values", 0)
}

// loadGenericFieldMappingsFromDb populates l.GenericFieldSourceToTarget from the DB.
// Normalises challenger and winner via NormaliseFieldValue. Returns true when at least
// one stored value was changed by normalisation, so the caller can arrange a write-back.
func loadGenericFieldMappingsFromDb(l *TBibTeXLibrary) bool {
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

// syncGenericFieldMappingsDbFromFile forces the DB to be reloaded from the flat file.
func syncGenericFieldMappingsDbFromFile() {
	setTableDate("generic_field_mappings", 0)
	maybeReloadGenericFieldMappingsDb()
}

// saveGenericFieldMappingsToDb writes the filtered generic field aliases directly to the DB without a file roundtrip.
func saveGenericFieldMappingsToDb(l *TBibTeXLibrary) {
	if _, err := db.Exec(`DELETE FROM generic_field_mappings;`); err != nil {
		dbInteraction.Warning("Could not clear generic_field_mappings: %s", err)
	}
	upsert := `INSERT INTO generic_field_mappings
	             (field, winner, challenger) VALUES (?, ?, ?)
	             ON CONFLICT(field, challenger)
	               DO UPDATE SET winner = excluded.winner;`
	for field, challenges := range l.GenericFieldSourceToTarget {
		if field != PreferredAliasField {
			for challenger, winner := range challenges {
				if challenger != winner {
					if _, err := db.Exec(upsert, field, l.MapFieldValue(field, winner), challenger); err != nil {
						dbInteraction.Warning("generic_field_mappings insert failed: %s", err)
					}
				}
			}
		}
	}
	setTableDate("generic_field_mappings", fileModTime(bibTeXFolder+bibTeXBaseName+GenericFieldMappingsFilePath))
	setTableDate("losing_field_values", 0)
}

// --- urls_ignore table ---
// Lives at FilesRoot level (no BaseName prefix); migrates from legacy urls.ignore on first use.

func ensureURLsIgnoreTableExists() {
	tryCreateTableIfNeeded(`
		CREATE TABLE IF NOT EXISTS urls_ignore (
		  url TEXT PRIMARY KEY
		);`)
}

func maybeReloadURLsIgnoreDb() {
	csvFile := bibTeXFolder + bibTeXBaseName + URLsIgnoreFilePath

	if !FileExists(csvFile) || fileModTime(csvFile) <= tableModTime("urls_ignore") {
		return
	}

	if _, err := db.Exec(`DROP TABLE IF EXISTS urls_ignore;`); err != nil {
		dbInteraction.Warning("Could not drop urls_ignore table: %s", err)
	}
	ensureURLsIgnoreTableExists()

	insert := `INSERT INTO urls_ignore (url) VALUES (?) ON CONFLICT(url) DO NOTHING;`
	processFile(csvFile, func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		// Accept urls_failed.csv lines (url;reason;date) and plain URLs alike:
		// parse as CSV and take the first field only.
		r := csv.NewReader(strings.NewReader(line))
		r.Comma = rune(csvDelimiter[0])
		r.FieldsPerRecord = -1
		fields, err := r.Read()
		if err != nil || len(fields) == 0 {
			return
		}
		url := strings.TrimSpace(fields[0])
		if url == "" {
			return
		}
		if _, err := db.Exec(insert, url); err != nil {
			dbInteraction.Warning("urls_ignore insert failed: %s", err)
		}
	})

	writeNormalisedURLsIgnoreFile(csvFile)
	setTableDate("urls_ignore", time.Now().UnixMicro())
}

// writeNormalisedURLsIgnoreFile rewrites the urls_ignore CSV as a sorted,
// deduplicated list of plain URLs (one per line, no extra columns).
func writeNormalisedURLsIgnoreFile(path string) {
	rows, err := db.Query(`SELECT url FROM urls_ignore ORDER BY url`)
	if err != nil {
		return
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var url string
		if rows.Scan(&url) == nil && url != "" {
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return
	}
	var buf strings.Builder
	w := csv.NewWriter(&buf)
	w.Comma = rune(csvDelimiter[0])
	for _, url := range urls {
		_ = w.Write([]string{url})
	}
	w.Flush()
	_ = os.WriteFile(path, []byte(buf.String()), 0644)
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
		  PRIMARY KEY (entry_key, property)
		);`)
}

// maybeReloadEntryMetadataDb migrates entry_metadata.json into the DB when the JSON
// file is newer than the DB table (one-time migration on first 21.x run, or when the
// user has manually edited the JSON file).
func maybeReloadEntryMetadataDb() {
	fileName := bibTeXFolder + bibTeXBaseName + EntryMetadataFilePath
	if fileModTime(fileName) <= tableModTime("entry_metadata") {
		return
	}
	data, err := os.ReadFile(fileName)
	if err != nil {
		return
	}
	var meta TEntryMetadata
	if jsonErr := json.Unmarshal(data, &meta); jsonErr != nil {
		dbInteraction.Warning("Could not parse %s for DB migration: %s", fileName, jsonErr)
		return
	}
	db.Exec(`DELETE FROM entry_metadata`)
	upsert := `INSERT INTO entry_metadata (entry_key, property, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, property) DO UPDATE SET value = excluded.value`
	for key, props := range meta {
		for prop, val := range props {
			if _, err := db.Exec(upsert, key, prop, val); err != nil {
				dbInteraction.Warning("entry_metadata insert failed: %s", err)
			}
		}
	}
	setTableDate("entry_metadata", fileModTime(fileName))
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
		  entry_key TEXT NOT NULL,
		  field     TEXT NOT NULL,
		  value     TEXT NOT NULL,
		  PRIMARY KEY (entry_key, field, value)
		);`)
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
	// Set the timestamp to the max of all upstream dependency tables so that
	// maybeReloadEntryFieldMappingsDb does not immediately clear the migrated data
	// because an upstream table appears newer.
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
		  UNIQUE(key, warning)
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

// forEachDuplicateDBLPKey calls fn for every DBLP value shared by two or more library entries,
// passing the DBLP key and the slice of affected library entry keys.
func forEachDuplicateDBLPKey(fn func(dblpKey string, keys []string)) {
	rows, err := db.Query(`
		SELECT value, GROUP_CONCAT(key, '|')
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

// maybeReloadShortenMappingsDb syncs the global shorten_mappings.csv into the DB
// when the file is newer than the stored table timestamp.
func maybeReloadShortenMappingsDb() {
	fileName := globalFolder + ShortenMappingsFilePath
	if fileModTime(fileName) <= tableModTime("shorten_mappings") {
		return
	}
	if _, err := db.Exec(`DELETE FROM shorten_mappings`); err != nil {
		dbInteraction.Warning("Could not clear shorten_mappings: %s", err)
		return
	}
	upsert := `INSERT INTO shorten_mappings (field, original, shortened) VALUES (?, ?, ?)
	             ON CONFLICT(field, original) DO UPDATE SET shortened = excluded.shortened`
	processFile(fileName, func(line string) {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			return
		}
		parts := strings.SplitN(line, csvDelimiter, 3)
		if len(parts) != 3 {
			return
		}
		field := strings.TrimSpace(parts[0])
		from := strings.TrimSpace(parts[1])
		to := strings.TrimSpace(parts[2])
		if field == "" || from == "" {
			return
		}
		if _, err := db.Exec(upsert, field, from, to); err != nil {
			dbInteraction.Warning("shorten_mappings insert failed: %s", err)
		}
	})
	setTableDate("shorten_mappings", fileModTime(fileName))
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
		  PRIMARY KEY (group_name, entry_key)
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

	finishRepair := func(tableName, filePath string) {
		setTableDate(tableName, fileModTime(filePath))
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
				finishRepair("name_mappings", base+NameMappingsFilePath)
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
				finishRepair("key_hints", base+KeyHintsFilePath)
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
				finishRepair("key_oldies", base+KeyOldiesFilePath)
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
				finishRepair("key_non_doubles", base+KeyNonDoublesFilePath)
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
				finishRepair("cross_field_mappings", base+CrossFieldMappingsFilePath)
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
				finishRepair("losing_field_values", base+LosingFieldValuesFilePath)
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
				finishRepair("generic_field_mappings", base+GenericFieldMappingsFilePath)
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
	} else {
		err = bibExec(
			`INSERT INTO bib_entries (entry_key, field, value) VALUES (?, ?, ?)
			   ON CONFLICT(entry_key, field) DO UPDATE SET value = excluded.value;`,
			key, field, value)
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

// deleteBibEntry removes all rows for a given entry key from bib_entries.
// Outside a transaction it checks for an actual change before marking modified.
func deleteBibEntry(key string) {
	if err := bibExec(`DELETE FROM bib_entries WHERE entry_key = ?`, key); err != nil {
		dbInteraction.Warning("bib_entries delete failed for %s: %s", key, err)
	}
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

// deleteHintsByTarget removes all key_hints rows whose resolved target is canonicalKey.
func deleteHintsByTarget(canonicalKey string) {
	if _, err := db.Exec(`DELETE FROM key_hints WHERE key = ?`, canonicalKey); err != nil {
		dbInteraction.Warning("key_hints delete-by-target failed for %s: %s", canonicalKey, err)
	}
}

// deleteOldiesByTarget removes all key_oldies rows whose resolved target is canonicalKey.
func deleteOldiesByTarget(canonicalKey string) {
	if _, err := db.Exec(`DELETE FROM key_oldies WHERE key = ?`, canonicalKey); err != nil {
		dbInteraction.Warning("key_oldies delete-by-target failed for %s: %s", canonicalKey, err)
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

// buildKeyAliasesFromDb rebuilds the in-memory key alias and hint maps from fields
// stored in bib_entries that the parser would normally add via processPreferredAliasValue
// and processDblpValue.  Must be called on the fast path where no parse takes place.
// addDblpKeyHintTransient adds a DBLP-derived hint directly to HintToKey without
// setting keyHintsModified. DBLP hints are filtered out by saveKeyHintsToDb
// (source == KeyForDBLP(entry's dblp)), so persisting them would be wasted work.
// They are regenerated from bib_entries on every run, so this is safe.
func addDblpKeyHintTransient(l *TBibTeXLibrary, dblpHint, key string) {
	if _, exists := l.HintToKey[dblpHint]; !exists {
		l.HintToKey[dblpHint] = key
	}
}

func buildKeyAliasesFromDb(l *TBibTeXLibrary) {
	dbInteraction.Progress(ProgressBuildingKeyAliases)
	if entryCache != nil {
		for key, e := range entryCache {
			if alias := e.Fields[PreferredAliasField]; alias != "" {
				l.AddKeyAlias(alias, key)
				l.AddKeyHint(alias, key)
			}
			if dblp := e.Fields[DBLPField]; dblp != "" {
				l.AddKeyAlias(KeyForDBLP(dblp), key)
				addDblpKeyHintTransient(l, KeyForDBLP(dblp), key)
			}
		}
		l.keyOldiesModified = false
		// keyHintsModified is NOT reset here: ReadKeyHintsFile owns that reset.
		// Genuine new preferredalias hints found above must trigger a save.
		return
	}
	rows, err := db.Query(
		`SELECT entry_key, field, value FROM bib_entries WHERE field IN (?, ?)`,
		PreferredAliasField, DBLPField)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries for key aliases: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, field, value string
		if err := rows.Scan(&key, &field, &value); err != nil {
			dbInteraction.Warning("Could not scan key alias row: %s", err)
			continue
		}
		switch field {
		case PreferredAliasField:
			l.AddKeyAlias(value, key)
			l.AddKeyHint(value, key)
		case DBLPField:
			l.AddKeyAlias(KeyForDBLP(value), key)
			addDblpKeyHintTransient(l, KeyForDBLP(value), key)
		}
	}
	l.keyOldiesModified = false
	// keyHintsModified is NOT reset here (see cache path comment above).
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

func maybeReloadStateNamesDb() bool {
	fileName := bibTeXFolder + bibTeXBaseName + StateNamesFilePath

	if fileModTime(fileName) <= tableModTime("state_names") {
		return false
	}

	dbInteraction.Progress("Reloading state names from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS state_names;`); err != nil {
		dbInteraction.Warning("Could not drop state_names table: %s", err)
	}
	ensureStateNamesTableExists()

	upsert := `INSERT INTO state_names (alias, canonical) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET canonical = excluded.canonical;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 || elements[0] == "" || elements[1] == "" {
			dbInteraction.Warning(WarningStateNamesLineTooShort, line)
			return
		}
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("State name insertion failed: %s", err)
		}
	})

	setTableDate("state_names", fileModTime(fileName))
	return true
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

func maybeReloadStateCountriesDb() bool {
	fileName := bibTeXFolder + bibTeXBaseName + StateCountriesFilePath

	if fileModTime(fileName) <= tableModTime("state_countries") {
		return false
	}

	dbInteraction.Progress("Reloading state countries from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS state_countries;`); err != nil {
		dbInteraction.Warning("Could not drop state_countries table: %s", err)
	}
	ensureStateCountriesTableExists()

	upsert := `INSERT INTO state_countries (state, country) VALUES (?, ?)
	             ON CONFLICT(state) DO UPDATE SET country = excluded.country;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 || elements[0] == "" || elements[1] == "" {
			dbInteraction.Warning(WarningStateCountriesLineTooShort, line)
			return
		}
		if _, err := db.Exec(upsert, elements[0], elements[1]); err != nil {
			dbInteraction.Warning("State country insertion failed: %s", err)
		}
	})

	setTableDate("state_countries", fileModTime(fileName))
	return true
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

func maybeReloadCountryNamesDb() bool {
	fileName := bibTeXFolder + bibTeXBaseName + CountryNamesFilePath

	if fileModTime(fileName) <= tableModTime("country_names") {
		return false
	}

	dbInteraction.Progress("Reloading country names from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS country_names;`); err != nil {
		dbInteraction.Warning("Could not drop country_names table: %s", err)
	}
	ensureCountryNamesTableExists()

	upsert := `INSERT INTO country_names (alias, canonical) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET canonical = excluded.canonical;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 || elements[0] == "" || elements[1] == "" {
			dbInteraction.Warning(WarningCountryNamesLineTooShort, line)
			return
		}
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("Country name insertion failed: %s", err)
		}
	})

	setTableDate("country_names", fileModTime(fileName))
	return true
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

func maybeReloadBooktitleCountryNamesDb() bool {
	fileName := bibTeXFolder + bibTeXBaseName + BooktitleCountryNamesFilePath

	if fileModTime(fileName) <= tableModTime("booktitle_country_names") {
		return false
	}

	dbInteraction.Progress("Reloading booktitle country names from %s", fileName)

	if _, err := db.Exec(`DROP TABLE IF EXISTS booktitle_country_names;`); err != nil {
		dbInteraction.Warning("Could not drop booktitle_country_names table: %s", err)
	}
	ensureBooktitleCountryNamesTableExists()

	upsert := `INSERT INTO booktitle_country_names (alias, canonical) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET canonical = excluded.canonical;`

	processFile(fileName, func(line string) {
		elements := strings.Split(line, csvDelimiter)
		if len(elements) < 2 || elements[0] == "" || elements[1] == "" {
			dbInteraction.Warning(WarningBooktitleCountryNamesLineTooShort, line)
			return
		}
		if _, err := db.Exec(upsert, elements[1], elements[0]); err != nil {
			dbInteraction.Warning("Booktitle country name insertion failed: %s", err)
		}
	})

	setTableDate("booktitle_country_names", fileModTime(fileName))
	return true
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
