/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: LibraryDB_SQLite
 *
 * SQLite infrastructure for the BibTeX library persistence layer:
 * connection management, WAL/PRAGMA setup, write-session isolation,
 * safe-parse copy-on-write, FK integrity checks, dump/restore, and
 * write-session crash-detection markers.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 04.05.2026
 *
 */

package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sqliteDatabaseDriver = "sqlite"
)

// sqliteDSN appends the DSN parameters needed for correct WAL-mode behaviour.
// _txlock=immediate: every db.Begin() issues BEGIN IMMEDIATE, acquiring the write
// lock upfront. This prevents SQLITE_BUSY_SNAPSHOT (517) — the error that occurs
// when a deferred transaction tries to upgrade from read to write after another
// connection has already advanced the WAL.
func sqliteDSN(path string) string { return path + "?_txlock=immediate" }

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

// configureDatabasePragmas sets WAL journal mode and a busy timeout on the
// current db connection. WAL allows a writer to proceed concurrently with open
// read cursors (eliminating SQLITE_BUSY when setTableDirty is called from
// within a forEachBibEntryKey iteration). The busy timeout adds automatic
// retry for any residual lock contention.
// configureDatabasePragmas sets up WAL mode, busy timeout, and FK enforcement.
// Returns true when the WAL pragma succeeds (DB is healthy); false indicates
// the file is corrupt or missing — the caller should not proceed with ensure* calls.
func configureDatabasePragmas() bool {
	walOK := true
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		dbInteraction.Warning("Could not enable WAL journal mode: %s", err)
		walOK = false
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		dbInteraction.Warning("Could not set busy_timeout: %s", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		dbInteraction.Warning("Could not enable FK enforcement: %s", err)
	}
	return walOK
}

func connectToDatabase() {
	dbName := dbPath()

	var err error
	db, err = sql.Open(sqliteDatabaseDriver, sqliteDSN(dbName))
	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", dbName, err.Error())
	}
	if !configureDatabasePragmas() && dbIsolationActive() {
		dbInteraction.Progress("Working database stale or corrupt — will refresh from home database")
		return
	}

	ensureTableDatesTableExists()
	maybeConsolidateEntryFlags()
	ensureContributorsTableExists()
	ensureContributorNamesTableExists()
	ensureContributorRolesTableExists()
	ensureEntryContributorNamesTableExists()
	ensureNonDoubleContributorsTableExists()
	ensureContributorIDOldiesTableExists()
	ensureContributorORCIDsTableExists()
	ensureContributorORCIDSeenTableExists()
	maybeMigrateContributorORCIDs()
	ensureKeyHintsTableExists()
	ensureKeyOldiesTableExists()
	ensureKeyNonDoublesTableExists()
	ensureNonDoubleContributorNamesTableExists()
	ensureDblpParentTableExists()
	ensureDblpWaivedTableExists()
	ensureDblpCanonicalTableExists()
	maybeMigrateDblpCanonical()
	ensureCrossFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureFieldMappingsTableExists()
	db.Exec(`DELETE FROM generic_field_mappings`) //nolint:errcheck
	db.Exec(`DELETE FROM cross_field_mappings`)   //nolint:errcheck
	ensureURLsIgnoreTableExists()
	ensureIgnoreTitlesTableExists()
	ensureEntryMetadataTableExists()
	ensureSupersededFieldValuesTableExists()
	ensureEntryDoiAliasesTableExists()
	ensureEntryWarningsTableExists()
	ensureDeletedEntriesTableExists()
	ensureConfigTableExists()
	// Checking config row count is not a reliable "not yet populated from home"
	// signal: a half-finished prior attempt (e.g. interrupted at the key-prefix
	// prompt) can leave config rows (version, backup_folder, ...) behind from
	// maybeBootstrapConfigFromFile() without ever getting a real key_prefix or
	// real library content. Checking bib_entries' mere existence is not reliable
	// either: this same connectToDatabase() call creates that table further down
	// (ensureBibTablesExist), so once any run has reached that point once, the
	// table persists empty in a stale/leftover working copy and every later run
	// would see it "exist" and wrongly skip straight to loadBibTeXSettings against
	// that empty stub — never giving prepareWorkingDatabase a chance to refresh it
	// from home first. Row count is the reliable signal: a working copy actually
	// populated from home always has many rows; skip config bootstrap/load entirely
	// when it doesn't, and let prepareWorkingDatabase copy home's real config and
	// content over and call loadBibTeXSettings() once that's done.
	skipConfigSetup := false
	if dbIsolationActive() {
		var entryRowCount int
		db.QueryRow(`SELECT COUNT(*) FROM bib_entries`).Scan(&entryRowCount) //nolint:errcheck
		skipConfigSetup = entryRowCount == 0
	}
	if !skipConfigSetup {
		maybeBootstrapConfigFromFile()
		loadBibTeXSettings()
	}
	ensureShortenMappingsTableExists()
	ensureBibEntryKeysTableExists()
	ensureBibTablesExist()
	contributorRolesActive = tableModTime("contributor_roles") > 0
}

// --- safe parse (copy-on-write DB swap) ---

var safeParseTemp string // non-empty while a safe parse is in progress
var safeParseOriginalCount int

func countDistinctBibEntries() int {
	var n int
	db.QueryRow(`SELECT COUNT(DISTINCT entry_key) FROM bib_entries WHERE field = ?`, EntryTypeField).Scan(&n)
	return n
}

// copyRetryAttempts and copyRetryDelay control the retry behaviour of
// copyFile and copyFileAtomic when a size-mismatch (likely caused by a
// transient cloud-sync race on the source file) is detected.
const copyRetryAttempts = 3

var copyRetryDelay = 300 * time.Millisecond

// copyFile copies src to dst byte-for-byte. Verifies the number of bytes
// copied matches src's size as stat'd before the read started — if src is
// concurrently replaced or truncated mid-copy (e.g. by a cloud-sync client,
// since the home database lives inside a synced folder), io.Copy simply
// stops at the new EOF without an error, which would otherwise silently
// produce a truncated, corrupt-looking destination file. Retries a few times
// since this kind of interference is typically a brief, transient window.
func copyFile(src, dst string) error {
	var lastErr error
	for attempt := 1; attempt <= copyRetryAttempts; attempt++ {
		if lastErr = copyFileOnce(src, dst); lastErr == nil {
			return nil
		}
		if attempt < copyRetryAttempts {
			time.Sleep(time.Duration(attempt) * copyRetryDelay)
		}
	}
	return lastErr
}

func copyFileOnce(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	srcInfo, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	written, err := io.Copy(out, in)
	if err != nil {
		return err
	}
	if written != srcInfo.Size() {
		return fmt.Errorf("incomplete copy: wrote %d of %d bytes (source may have changed during copy)", written, srcInfo.Size())
	}
	// Re-stat src after the read completes: a cloud-sync client can leave src at a
	// stable-but-wrong size for the whole read (e.g. mid-hydration), in which case
	// written == srcInfo.Size() above passes even though both numbers are stale. A
	// size change across the read window is a clear sign src was not settled.
	if postInfo, err := os.Stat(src); err == nil && postInfo.Size() != srcInfo.Size() {
		return fmt.Errorf("source size changed during copy (%d -> %d bytes) — likely an active cloud sync", srcInfo.Size(), postInfo.Size())
	}
	return nil
}

// copyFileAtomic copies src to a temp file in dst's directory, syncs it to
// disk, then renames it over dst. The rename is atomic at the filesystem
// level, so a cloud-sync client watching dst (e.g. Nextcloud, since the home
// database lives inside a synced folder) never observes a partially written
// file — only the complete old file or the complete new file. Plain copyFile
// (truncate + incremental write) is unsafe for that path: a sync client can
// read and upload a half-written file, corrupting the synced copy. Retries a
// few times on a source size mismatch (see copyFile).
func copyFileAtomic(src, dst string) error {
	var lastErr error
	for attempt := 1; attempt <= copyRetryAttempts; attempt++ {
		if lastErr = copyFileAtomicOnce(src, dst); lastErr == nil {
			return nil
		}
		if attempt < copyRetryAttempts {
			time.Sleep(time.Duration(attempt) * copyRetryDelay)
		}
	}
	return lastErr
}

func copyFileAtomicOnce(src, dst string) error {
	tmp := fmt.Sprintf("%s.tmp-%d", dst, os.Getpid())
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	srcInfo, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	written, err := io.Copy(out, in)
	if err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if written != srcInfo.Size() {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("incomplete copy: wrote %d of %d bytes (source may have changed during copy)", written, srcInfo.Size())
	}
	// See copyFileOnce: a stable-but-wrong source size for the whole read defeats
	// the written-vs-srcInfo check above, so re-stat src after the read too.
	if postInfo, err := os.Stat(src); err == nil && postInfo.Size() != srcInfo.Size() {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("source size changed during copy (%d -> %d bytes) — likely an active cloud sync", srcInfo.Size(), postInfo.Size())
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// reopenDb closes the current db connection and opens a new one at path,
// re-ensuring all table schemas exist (idempotent).
func reopenDb(path string) {
	if db != nil {
		db.Close()
	}
	var err error
	db, err = sql.Open(sqliteDatabaseDriver, sqliteDSN(path))
	if err != nil {
		dbInteraction.Progress("Could not open sqlite database %s: %s", path, err)
	}
	configureDatabasePragmas()
	ensureTableDatesTableExists()
	ensureContributorsTableExists()
	ensureContributorNamesTableExists()
	ensureContributorRolesTableExists()
	ensureEntryContributorNamesTableExists()
	ensureNonDoubleContributorsTableExists()
	ensureContributorIDOldiesTableExists()
	ensureContributorORCIDsTableExists()
	ensureContributorORCIDSeenTableExists()
	maybeMigrateContributorORCIDs()
	ensureKeyHintsTableExists()
	ensureKeyOldiesTableExists()
	ensureKeyNonDoublesTableExists()
	ensureNonDoubleContributorNamesTableExists()
	ensureDblpParentTableExists()
	ensureDblpWaivedTableExists()
	ensureDblpCanonicalTableExists()
	ensureCrossFieldMappingsTableExists()
	ensureGenericFieldMappingsTableExists()
	ensureFieldMappingsTableExists()
	ensureURLsIgnoreTableExists()
	ensureIgnoreTitlesTableExists()
	ensureEntryMetadataTableExists()
	ensureSupersededFieldValuesTableExists()
	ensureEntryDoiAliasesTableExists()
	ensureConfigTableExists()
	ensureShortenMappingsTableExists()
	ensureBibEntryKeysTableExists()
	ensureBibTablesExist()
	contributorRolesActive = tableModTime("contributor_roles") > 0
}

// beginSafeParse creates a consistent backup of the live SQLite file in the
// system temp directory (outside Nextcloud), then switches db to work on that
// copy. Returns false if setup fails (caller proceeds on the live file instead).
// Uses VACUUM INTO rather than a raw file copy so that any uncheckpointed WAL
// pages are included in the backup. Shows a spinner while waiting.
func beginSafeParse() bool {
	ts := time.Now().Format("20060102_150405")
	safeParseTemp = fmt.Sprintf("%s/bibtex_check_%s_%s%s",
		os.TempDir(), bibTeXBaseName, ts, cacheFileExtension)

	safeParseOriginalCount = countDistinctBibEntries()

	progressTicker := dbInteraction.NewProgressTicker(ProgressBackingUpDatabase, 0)
	errCh := make(chan error, 1)
	go func() {
		_, err := db.Exec(`VACUUM INTO ?`, safeParseTemp)
		errCh <- err
	}()
	timeTicker := time.NewTicker(200 * time.Millisecond)
	defer timeTicker.Stop()
	for {
		select {
		case err := <-errCh:
			progressTicker.Done()
			if err != nil {
				dbInteraction.Warning("Safe parse: could not back up database: %s", err)
				os.Remove(safeParseTemp)
				safeParseTemp = ""
				return false
			}
			reopenDb(safeParseTemp)
			return true
		case <-timeTicker.C:
			progressTicker.Tick()
		}
	}
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
// prepareWorkingDatabase ensures the working database is a current copy of the home
// database before a write session begins. Detects a stale working copy left by a
// crash and offers the user a chance to restore. Returns false on setup failure.
func prepareWorkingDatabase() bool {
	if !dbIsolationActive() {
		dbWriteSessionActive = true
		return true
	}
	// Already in an active write session (e.g. a chained doHomework() step).
	// The working DB is live — do not re-copy from home or re-run crash detection.
	if dbWriteSessionActive {
		return true
	}
	home := dbHomePath()
	working := dbPath()

	// A transient stat failure here (e.g. a cloud-sync client unlinking and
	// recreating the file as part of its own reconciliation, rather than a safe
	// rename) must not be mistaken for "no home DB ever existed" — that falls
	// through below to os.Create(home), which would silently replace a real,
	// populated home DB with an empty stub. Retry first; a genuine absence is
	// stable across a second, while a sync race resolves within milliseconds.
	var hInfo os.FileInfo
	var hErr error
	for attempt := 1; attempt <= 5; attempt++ {
		hInfo, hErr = os.Stat(home)
		if hErr == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if hErr != nil {
		// No home DB yet. If a bib file exists for this base, create an empty home DB
		// so the normal open flow can proceed and auto-import it via ValidBibDb() == false.
		bibPath := bibTeXFolder + bibTeXBaseName + BibFileExtension
		bibInfo, bibErr := os.Stat(bibPath)
		if bibErr != nil {
			dbInteraction.Warning("Home database not found: %s", home)
			return false
		}
		// A substantial .bib file means this is an established library, not a
		// fresh one — a stat failure here despite that is the same kind of
		// anomaly this retry loop exists to survive, just outlasting it. Refuse
		// to silently overwrite a real home DB with an empty stub; surface the
		// problem instead of quietly destroying data.
		if bibInfo.Size() > 100*1024 {
			dbInteraction.Warning("Home database not found (%s) but %s is %d bytes — refusing to create an empty replacement for an established library; investigate before retrying", home, bibPath, bibInfo.Size())
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
	// Skip the legacy check when session markers confirm a clean close — the
	// working DB's mtime can legitimately be newer than home's when the
	// "nothing real written" early-return in finaliseWorkingDatabase wrote
	// session-close markers without re-syncing the mtime afterward.
	crashed := writeSessionIsCrashed()
	if !crashed && !writeSessionIsClean() && wErr == nil && wInfo.Size() > 0 &&
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
				if err := copyFileAtomic(working, home); err != nil {
					dbInteraction.Warning("Could not restore: %s", err)
					return false
				}
				hInfo, _ = os.Stat(home)
				dbInteraction.Progress("  Restored home database from working copy.")
			}
		}
	}

	// In sync: session markers confirm a clean previous close, sizes match, and home
	// was not modified after the working copy was last synced (guards against manual
	// sqlite3 edits to the home DB — SQLite DELETE does not shrink the file, so a
	// size-only check would silently reuse a stale working copy).
	if wErr == nil && writeSessionIsClean() && wInfo.Size() == hInfo.Size() && !hInfo.ModTime().After(wInfo.ModTime()) {
		dbInteraction.Progress(ProgressReusingWorkingDatabase)
		homeDbSizeAtOpen = hInfo.Size()
		markWriteSessionOpen()
		db.QueryRow(`SELECT total_changes()`).Scan(&changesAtSessionOpen)
		dbWriteSessionActive = true
		return true
	}

	// connectToDatabase opened db against this same working path and may have run
	// ensureXXXTableExists() against it (e.g. on a stale/missing working file)
	// before this function ever ran. That connection must not survive the raw
	// file-level copy below: SQLite has no way to know the file changed out from
	// under it, so its page cache/WAL still reflect whatever it last wrote — and
	// closing it later (in reopenDb) checkpoints that stale state back over the
	// freshly copied content, truncating the file to match. Close it now, before
	// the copy, so reopenDb's later Close is a no-op with nothing stale to flush.
	if db != nil {
		db.Close()
		db = nil
	}

	dbInteraction.Progress(ProgressCopyingToWorkingDatabase)
	// copyFile only verifies internal self-consistency (bytes written match the
	// source's size as stat'd immediately before the read) — if the source itself
	// is transiently mis-reporting a much smaller size (e.g. a cloud-sync client
	// mid-hydration on the home file, which lives inside a synced folder), that
	// check passes even though both numbers are wrong by the same amount. Guard
	// against that by comparing the result to hInfo, stat'd independently at the
	// top of this function, and retrying the whole copy if they disagree.
	copyOK := false
	for attempt := 1; attempt <= 3; attempt++ {
		if err := copyFile(home, working); err != nil {
			dbInteraction.Warning("Could not copy database to working location: %s", err)
			return false
		}
		if wNew, err := os.Stat(working); err == nil && wNew.Size() == hInfo.Size() {
			copyOK = true
			break
		} else {
			gotSize := int64(-1)
			if err == nil {
				gotSize = wNew.Size()
			}
			dbInteraction.Warning("Working copy size mismatch (got %d, expected %d) — likely a cloud-sync race on the home file; retrying", gotSize, hInfo.Size())
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	if !copyOK {
		dbInteraction.Warning("Home database copy is unreliable after retries — aborting this run without touching the working database")
		return false
	}
	os.Remove(working + "-wal") //nolint:errcheck
	os.Remove(working + "-shm") //nolint:errcheck
	if hNew, err := os.Stat(home); err == nil {
		os.Chtimes(working, hNew.ModTime(), hNew.ModTime())
	}
	reopenDb(working)

	// Hard backstop: two prior incidents each passed every check above (home
	// stable throughout, copy size verified against hInfo) and the working DB
	// still ended up empty by the time we get here — the exact mechanism is still
	// unconfirmed, but whatever it is happens between the verified copy and this
	// point. Don't rely on figuring that out before this can do more damage:
	// regardless of cause, never proceed into loadBibTeXSettings (which is what
	// produces the confusing key-prefix prompt) or any further work on a working
	// copy that's implausibly empty for a home DB this size.
	if hInfo.Size() > 10*1024*1024 {
		var rowCount int
		db.QueryRow(`SELECT COUNT(*) FROM bib_entries`).Scan(&rowCount) //nolint:errcheck
		if rowCount == 0 {
			dbInteraction.Warning("Working copy has 0 bib_entries rows right after reopening, but home is %d bytes — something cleared or replaced the working DB between the verified copy and here. Aborting without touching home; broken working copy left at %s for inspection.", hInfo.Size(), working)
			return false
		}
	}

	homeDbSizeAtOpen = hInfo.Size()
	if keyPrefix == "" {
		loadBibTeXSettings()
	}
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
	// Writes made via bibExec while a bib transaction is open go through activeTx
	// and are invisible to a WAL checkpoint until committed — including the very
	// write (e.g. a non_double_entries dismissal) that just triggered this flush.
	// Callers like doUpsertDblpEntries wrap long, interactive, step-paused loops in
	// one transaction spanning many flush calls, so commit it first and reopen an
	// equivalent one afterward; this makes the flush actually durable instead of a
	// silent no-op against uncommitted state.
	reopenTx := activeTx != nil
	if reopenTx {
		commitBibTransaction()
	}
	db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)                       //nolint:errcheck
	working := dbPath()
	wFlush, wFlushErr := os.Stat(working)
	if homeDbSizeAtOpen > 10*1024*1024 && wFlushErr == nil && wFlush.Size() < homeDbSizeAtOpen/10 {
		dbInteraction.Warning("Working database (%d bytes) is implausibly smaller than home was at session open (%d bytes) — skipping mid-session flush to prevent data loss.", wFlush.Size(), homeDbSizeAtOpen)
	} else if err := copyFileAtomic(working, dbHomePath()); err != nil {
		dbInteraction.Warning("Could not flush working database to home: %s", err)
	}
	if reopenTx {
		beginBibTransaction()
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

	// Run the pre-close hook (e.g. reportHomework) while the DB is still open.
	if preCloseHook != nil {
		preCloseHook()
		preCloseHook = nil
	}

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
	if homeDbSizeAtOpen > 10*1024*1024 && wErr == nil && wInfo.Size() < homeDbSizeAtOpen/10 {
		dbInteraction.Warning("Working database (%d bytes) is implausibly smaller than home was at session open (%d bytes) — refusing to overwrite home to prevent data loss; working copy left at %s for inspection.", wInfo.Size(), homeDbSizeAtOpen, working)
		return
	}
	if err := copyFileAtomic(working, home); err != nil {
		dbInteraction.Warning("Could not save working database to home: %s", err)
		return
	}
	// Sync home mtime to working's mtime so the next run's skip-copy check triggers.
	if wErr == nil {
		os.Chtimes(home, wInfo.ModTime(), wInfo.ModTime())
	}

	writeDatabaseDump()

	dumpPath := bibTeXFolder + bibTeXBaseName + ".dump"
	backupDumpPath := backupFolder + bibTeXBaseName + "_" + ts + ".dump"
	if _, err := os.Stat(dumpPath); err == nil {
		copyFile(dumpPath, backupDumpPath) //nolint:errcheck
	}

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
	if res, err := db.Exec(`DELETE FROM bib_entry_keys
		                   WHERE entry_key NOT IN (SELECT DISTINCT entry_key FROM bib_entries)`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			dbInteraction.Progress("  Removed %d stale anchor row(s) — cascading to dependent tables", n)
		}
	}
	repairOrphanRows()
	db.Exec(`PRAGMA foreign_keys = ON`)
}

// repairOrphanRows deletes rows from FK-dependent tables whose anchor (bib_entry_keys)
// row is missing. Shared by preCheckRepair (proactive, session start) and postCheckGate
// (reactive, session end) — an entry whose own bib_entries write failed mid-run can leave
// behind dependent rows (e.g. entry_metadata) for a key that never made it into bib_entries.
func repairOrphanRows() {
	repair := func(table, fkCol string) {
		res, err := db.Exec(fmt.Sprintf(
			`DELETE FROM %s WHERE %s NOT IN (SELECT entry_key FROM bib_entry_keys)`,
			table, fkCol))
		if err != nil {
			dbInteraction.Warning("repairOrphanRows %s: %s", table, err)
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			dbInteraction.Progress("  Repaired %d orphan row(s) in %s", n, table)
		}
	}
	repair("bib_groups", "entry_key")
	repair("superseded_field_values", "entry_key")
	repair("entry_warnings", "key")
	repair("entry_metadata", "entry_key")
	repair("contributor_roles", "entry_key")
	repair("entry_contributor_names", "entry_key")
}

// foreignKeyCheckOK runs PRAGMA foreign_key_check and warns about each violation found.
func foreignKeyCheckOK() bool {
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

// postCheckGate re-syncs bib_entry_keys to the current bib_entries state, then
// uses PRAGMA foreign_key_check to verify all FK constraints. Returns true when
// clean. Called from the write tail of main() just before finaliseWorkingDatabase;
// a false return suppresses the home-DB copy so a bad run cannot corrupt persisted state.
// On FK violations, attempts one round of orphan-row repair before giving up —
// a single entry whose bib_entries write failed (see upsertBibEntryField/closeEntry,
// which set dbWriteFailed) can otherwise discard an entire session's other edits.
func postCheckGate() bool {
	if dbWriteFailed {
		dbInteraction.Warning("Post-check: DB write failure(s) detected — home database not updated")
		return false
	}

	db.Exec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key)
	         SELECT DISTINCT entry_key FROM bib_entries`)
	db.Exec(`DELETE FROM bib_entry_keys
	         WHERE entry_key NOT IN (SELECT DISTINCT entry_key FROM bib_entries)`)
	// Also anchor any contributor_roles entry_keys that are present in bib_entries
	// but were written before bib_entry_keys was populated (e.g. from an older migration).
	db.Exec(`INSERT OR IGNORE INTO bib_entry_keys (entry_key)
	         SELECT DISTINCT entry_key FROM contributor_roles
	         WHERE entry_key IN (SELECT DISTINCT entry_key FROM bib_entries)`) //nolint:errcheck

	if foreignKeyCheckOK() {
		return true
	}

	dbInteraction.Warning("Post-check: attempting orphan-row repair before failing the session")
	repairOrphanRows()
	if foreignKeyCheckOK() {
		dbInteraction.Progress("Post-check: repaired — home database will be updated")
		return true
	}
	return false
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

// doRestoreFromDump restores the home database from a SQL dump file produced by
// writeDatabaseDump. The dump is loaded into a fresh SQLite file via the sqlite3
// CLI. The corrupt home DB is moved to the backups folder before replacement.
// This must be called before connectToDatabase (bibTeXFolder and bibTeXBaseName
// must already be set; backupFolder may still be empty, defaulting to $base.backups/).
func doRestoreFromDump() {
	dumpPath := bibTeXFolder + bibTeXBaseName + ".dump"
	info, err := os.Stat(dumpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "No dump file found at %s\n", dumpPath)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Restoring from %s (%.0f MB)...\n", dumpPath, float64(info.Size())/1e6)

	homePath := dbHomePath()
	newPath := homePath + ".restore"
	os.Remove(newPath)

	f, err := os.Open(dumpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot open dump file: %s\n", err)
		os.Exit(1)
	}
	defer f.Close()

	cmd := exec.Command("sqlite3", newPath)
	cmd.Stdin = f
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sqlite3 restore failed: %s\n%s\n", err, stderr.String())
		os.Remove(newPath)
		os.Exit(1)
	}

	bkDir := backupFolder
	if bkDir == "" {
		bkDir = bibTeXFolder + bibTeXBaseName + ".backups/"
	}
	if _, err := os.Stat(homePath); err == nil {
		ts := time.Now().Format("20060102_150405")
		corruptBackup := bkDir + bibTeXBaseName + "_corrupt_" + ts + cacheFileExtension
		if mkErr := os.MkdirAll(bkDir, 0755); mkErr == nil {
			if renErr := os.Rename(homePath, corruptBackup); renErr != nil {
				if cpErr := copyFile(homePath, corruptBackup); cpErr == nil {
					os.Remove(homePath)
				}
			}
			fmt.Fprintf(os.Stderr, "Moved corrupt DB to %s\n", corruptBackup)
		}
	}

	// Remove stale WAL/SHM files so SQLite does not apply them to the fresh DB.
	os.Remove(homePath + "-wal")
	os.Remove(homePath + "-shm")

	// Also wipe the working DB (cache folder) so the next run starts clean.
	if workPath := dbPath(); workPath != homePath {
		os.Remove(workPath)
		os.Remove(workPath + "-wal")
		os.Remove(workPath + "-shm")
		fmt.Fprintf(os.Stderr, "Cleared working database at %s\n", workPath)
	}

	if err := os.Rename(newPath, homePath); err != nil {
		if cpErr := copyFile(newPath, homePath); cpErr != nil {
			fmt.Fprintf(os.Stderr, "Could not install restored DB at %s: %s\n", homePath, cpErr)
			os.Exit(1)
		}
		os.Remove(newPath)
	}
	fmt.Fprintf(os.Stderr, "Restore complete: %s\n", homePath)
}

// doRestoreFromBackup copies a named backup file back to the home database path.
// backupName is the timestamp portion of the backup filename (e.g. "ErikProper_20260709_123456");
// the full path is resolved as backupFolder/backupName.sqlite3.
// Clears both home and working sidecar files before restoring.
func doRestoreFromBackup(backupName string) {
	bkDir := backupFolder
	if bkDir == "" {
		bkDir = bibTeXFolder + bibTeXBaseName + ".backups/"
	}
	backupPath := bkDir + backupName + cacheFileExtension
	info, err := os.Stat(backupPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Backup not found: %s\n", backupPath)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Restoring from %s (%.0f MB)...\n", backupPath, float64(info.Size())/1e6)

	homePath := dbHomePath()

	// Remove stale sidecar files at home before overwriting.
	os.Remove(homePath + "-wal")
	os.Remove(homePath + "-shm")

	// Also wipe the working DB so the next run starts with a fresh copy from home.
	if workPath := dbPath(); workPath != homePath {
		os.Remove(workPath)
		os.Remove(workPath + "-wal")
		os.Remove(workPath + "-shm")
		fmt.Fprintf(os.Stderr, "Cleared working database at %s\n", workPath)
	}

	if err := copyFileAtomic(backupPath, homePath); err != nil {
		fmt.Fprintf(os.Stderr, "Could not restore %s → %s: %s\n", backupPath, homePath, err)
		os.Exit(1)
	}
	restored, _ := os.Stat(homePath)
	fmt.Fprintf(os.Stderr, "Restore complete: %s (%d bytes)\n", homePath, restored.Size())
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
