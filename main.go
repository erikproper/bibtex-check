package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// tableListFlag implements flag.Value for -export/-import.
// Used bare (-export) it sets the value to "all" via IsBoolFlag.
// Used with a value (-export name_mappings,bib_entries) it stores that value.
type tableListFlag string

func (f *tableListFlag) String() string   { return string(*f) }
func (f *tableListFlag) IsBoolFlag() bool { return true }
func (f *tableListFlag) Set(s string) error {
	if s == "true" {
		*f = "all"
	} else {
		*f = tableListFlag(s)
	}
	return nil
}

var (
	Library   TBibTeXLibrary
	Reporting TInteraction
)

const AppVersion = "27.94"

// Run-state flags consumed by the write tail in main.
var (
	skipBibDbRefresh      bool
	skipBibValidation     bool
	skipStartupChecks     bool // set by -import so the import runs before consistency checks
	forceWrite            bool
	cmdNoGarbageCleaning  bool
	cmdTrustHints              bool   // -trust_hints: auto-accept key-hint matches in harvest
	cmdCollectKeys             bool   // -collect_keys: add source keys to hints DB when unambiguous
	cmdHarvestGroup            string         // -group: add all resolved harvest entries to this group
	cmdHarvestTransferKeysPath string         // resolved .keys path for harvest_transfer target; "" = disabled
	cmdHarvestWeaveEntries     []TBibTeXEntry // ignored entries accumulated during this harvest run; flushed to follow .sync DB
	cmdFix                     bool // -fix: apply full per-entry checks when combined with -sync or -harvest
	cmdPull                    bool // -pull: with -sync, skip up-sync (phase 1); only write bib output from DB
	cmdMatchedOrcidDataOnly    bool // -matched_orcid_data_only: skip ORCID challenges in step 3
)

func reportCacheMode() {
	if entryCache != nil {
		Library.Progress(ProgressEntryCacheLoaded, len(entryCache))
	} else {
		Library.Progress(ProgressEntryPerQuery)
	}
}

func initialiseLibrary() {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, bibTeXFolder, bibTeXBaseName)
	Library.PreMergeCheck = func(source, target string) {
		// Only search the surviving key (target). source is about to be merged
		// into target — searching it separately is redundant and, when the
		// library has several duplicate entries for the same work, was causing
		// the user to be asked for the same DBLP match repeatedly (once per
		// duplicate key) before they converged into one entry.
		if Library.EntryFieldValueity(target, DBLPField) != "" {
			return
		}
		maybeFindDBLPCandidates(target)
	}

	// If bib_entries was dirty on the previous run (crash mid-write), advance its
	// timestamp now so that any repaired mapping files (whose timestamps are about
	// to be set to NOW) do not make ValidBibDb() return false and force a re-parse
	// from the stale bib file — which would lose in-DB changes.
	if isTableDirty("bib_entries") {
		skipBibValidation = true
		refreshBibDbTimestamp()
	}

	repairDirtyMappingTables()
}

func loadMappingFiles() {
	Library.ReadAddressMappings()
	Library.CheckAddressMappings()
	Library.ReadKeyHintsFile()
	Library.ReadKeyNonDoublesFile()
	Library.ReadIgnoreTitlesFile()
	Library.ReadNameMappingsFile()
	Library.ReadFieldMappingsFile()
	Library.ReadEntryFieldMappingsFile()
	Library.ReadDblpParentFile()
	Library.ReadDblpWaivedFile()
	Library.ReadMetadataFile()
	Library.ReadEntryFlagsFile()
	Library.CheckFieldMappings()
	Library.CheckNameMappingConsistency()
}

func loadBibFromDb() {
	loadGroupsFromDb(&Library)
	loadCommentsFromDb(&Library)
	buildKeyAliasesFromDb(&Library)
	resolveGroupEntriesKeys(&Library)
	initEntryCache()
	reportCacheMode()
	if contributorRolesActive {
		loadAuthorEditorFieldMappingsFromCache(&Library)
	}
}

// parseSyncBibFile clears the bib tables and re-parses a sync bib file (full mode).
// On success the working DB contains the re-imported entries and bibEntriesModified
// is set so the home DB is updated at close. On failure the working DB is restored
// from the home copy via the safe-parse rollback mechanism.
// doImportBib is the -import_bib entry point: clears all bib tables and re-imports
// from the given bib file (full replace — same semantics as -import_XXX for mapping
// tables). After this, normal runs load from DB only; the bib file is not touched
// again unless -import_bib or -sync is invoked explicitly.
//
// Companion: -upsert_bib upserts entries from a bib file without clearing existing
// ones (entries already in DB are updated; new ones are added; nothing is deleted).
// Unlike -import_bib, -upsert_bib also accepts stdin when no filename is given.
func doImportBib(path string) {
	if !prepareWorkingDatabase() {
		return
	}
	initialiseLibrary()
	Library.ReadKeyOldiesFile()
	loadMappingFiles()
	Library.Progress(ProgressImportingBibFile, path)
	if !parseSyncBibFile(path) {
		return
	}
	buildTitleIndexFromDb(&Library)
	Library.ReportLibrarySize()
}

func parseSyncBibFile(path string) bool {
	Library.NoDBUpdating = false // clear any flag left from a previous operation
	Library.Progress(ProgressReparsingBibFile)
	safeOk := beginSafeParse()
	if !safeOk {
		Library.Warning("Proceeding without safe-parse backup; database not protected during reparse.")
	}
	// Reset in-memory group and comment state before re-parsing so that the
	// additive BibDesk XML reader does not carry over stale memberships from
	// the previous in-memory state (clearBibTables only clears the DB tables).
	Library.GroupEntries = TStringSetMap{}
	Library.Comments = nil
	Library.Progress(ProgressClearingBibTables)
	clearBibTables()
	beginBibTransaction()
	parseCh := make(chan bool, 1)
	go func() { parseCh <- Library.ParseBibFile(path) }()
	parseTicker := Library.NewProgressTicker(ProgressParsingBibFile, 0)
	parseTimeTicker := time.NewTicker(200 * time.Millisecond)
	var parseOk bool
syncParseLoop:
	for {
		select {
		case ok := <-parseCh:
			parseTimeTicker.Stop()
			parseTicker.Done()
			parseOk = ok
			break syncParseLoop
		case <-parseTimeTicker.C:
			parseTicker.SetCount(int(atomic.LoadInt64(&bibParseCount)))
		}
	}
	if !parseOk {
		rollbackBibTransaction()
		if safeOk {
			rollbackSafeParse()
		}
		return false
	}
	saveBibGroupsToDb(&Library)
	saveBibCommentsToDb(&Library)
	commitBibTransaction()
	// Guard: if the parser set NoDBUpdating (unknown entry types, parse errors
	// that did not abort parsing, etc.) treat this as a failed import. Roll back the
	// safe-parse temp DB so the working DB reverts to the pre-parse state and the
	// home DB is not affected.
	if Library.NoDBUpdating {
		Library.Warning("Sync bib re-import aborted: parser reported errors — home database unchanged.")
		if safeOk {
			rollbackSafeParse()
		}
		return false
	}
	if safeOk {
		commitSafeParse()
	}
	bibEntriesModified = true
	setTableDirty("dblp_hierarchy")
	initEntryCache()
	reportCacheMode()
	refreshBibDbTimestamp()
	return true
}

func parseBibIntoDb() bool {
	Library.Progress(ProgressReparsingBibFile)
	safeOk := beginSafeParse()
	if !safeOk {
		Library.Warning("Proceeding without safe-parse backup; database not protected during reparse.")
	}
	Library.GroupEntries = TStringSetMap{}
	Library.Comments = nil
	Library.Progress(ProgressClearingBibTables)
	clearBibTables()
	beginBibTransaction()
	readCh := make(chan bool, 1)
	go func() { readCh <- Library.ReadBib(BibFile) }()
	readTicker := Library.NewProgressTicker(ProgressParsingBibFile, 0)
	readTimeTicker := time.NewTicker(200 * time.Millisecond)
	var readOk bool
bibParseLoop:
	for {
		select {
		case ok := <-readCh:
			readTimeTicker.Stop()
			readTicker.Done()
			readOk = ok
			break bibParseLoop
		case <-readTimeTicker.C:
			readTicker.SetCount(int(atomic.LoadInt64(&bibParseCount)))
		}
	}
	if !readOk {
		rollbackBibTransaction()
		if safeOk {
			rollbackSafeParse()
			os.Exit(1)
		}
		return false
	}
	saveBibGroupsToDb(&Library)
	saveBibCommentsToDb(&Library)
	commitBibTransaction()

	if safeOk {
		commitSafeParse()
	}

	bibEntriesModified = false // parse is a load, not a modification
	setTableDirty("dblp_hierarchy")
	initEntryCache()
	reportCacheMode()
	refreshBibDbTimestamp()
	return true
}

// sessionStart* capture library metrics at the beginning of a write session so
// that reportHomework() can print deltas at the end.
var (
	sessionStartEntryCount        int
	sessionStartDblpKeyCount      int
	sessionStartFieldMapCount     int
	sessionStartUnresolvedGroups  int
	sessionStartDblpCandidates    int
	sessionManualDblpAssignments  int // gross count of manually entered DBLP keys this session
)

func countDblpKeyedEntries() int {
	if entryCache != nil {
		n := 0
		for _, e := range entryCache {
			if e.Fields[DBLPField] != "" {
				n++
			}
		}
		return n
	}
	var n int
	bibQueryRow(`SELECT COUNT(*) FROM bib_entries WHERE field = ?`, DBLPField).Scan(&n)
	return n
}

// printSessionStats prints a library-state summary at the start of a write
// session and captures the current counts for the session-change report at end.
func printSessionStats() {
	total := countBibEntries()
	var contributors, nameStrings, fieldMaps, losingValues int
	bibQueryRow(`SELECT COUNT(*) FROM contributors`).Scan(&contributors)
	bibQueryRow(`SELECT COUNT(DISTINCT name_used) FROM entry_contributor_names`).Scan(&nameStrings)
	bibQueryRow(`SELECT COUNT(*) FROM field_mappings`).Scan(&fieldMaps)
	bibQueryRow(`SELECT COUNT(*) FROM losing_field_values`).Scan(&losingValues)
	dblpKeys := countDblpKeyedEntries()

	sessionStartEntryCount = total
	sessionStartDblpKeyCount = dblpKeys
	sessionStartFieldMapCount = fieldMaps
	sessionStartUnresolvedGroups = countUnresolvedGroups()
	sessionStartDblpCandidates = countDblpCandidates()

	entryFlags := 0
	for _, s := range Library.EntryFlags {
		entryFlags += len(s.Elements())
	}

	var ndEntries, ndContributors, ndNames int
	bibQueryRow(`SELECT COUNT(*) FROM non_double_entries`).Scan(&ndEntries)
	bibQueryRow(`SELECT COUNT(*) FROM non_double_contributors`).Scan(&ndContributors)
	bibQueryRow(`SELECT COUNT(*) FROM non_double_contributor_names`).Scan(&ndNames)

	pct := 0.0
	if total > 0 {
		pct = float64(dblpKeys) * 100.0 / float64(total)
	}
	printStatBlock("Library statistics:", []statRow{
		{StatEntries, fmt.Sprintf("%d", total), ""},
		{StatPDFFiles, fmt.Sprintf("%d", len(Library.PDFFiles)), ""},
		{StatContributors, fmt.Sprintf("%d", contributors), ""},
		{StatNameSpellings, fmt.Sprintf("%d", nameStrings), ""},
		{StatFieldMappings, fmt.Sprintf("%d", fieldMaps), ""},
		{StatLosingValues, fmt.Sprintf("%d", losingValues), ""},
		{StatDblpCoverage, fmt.Sprintf("%d/%d (%.0f%%)", dblpKeys, total, pct), ""},
		{StatDblpCrossrefOverrides, fmt.Sprintf("%d", Library.DblpParent.Len()), ""},
		{StatDblpWaivedChildren, fmt.Sprintf("%d", Library.DblpWaived.Len()), ""},
		{StatKeyOldies, fmt.Sprintf("%d", Library.KeyOldies.Len()), ""},
		{StatKeyHints, fmt.Sprintf("%d", Library.HintToKey.Len()), ""},
		{StatNonDoubleEntryPairs, fmt.Sprintf("%d", ndEntries), ""},
		{StatNonDoubleContributorPairs, fmt.Sprintf("%d", ndContributors), ""},
		{StatNonDoubleNamePairs, fmt.Sprintf("%d", ndNames), ""},
		{StatEntryFlags, fmt.Sprintf("%d", entryFlags), ""},
	})
}

func openLibraryToUpdate() bool {
	if !prepareWorkingDatabase() {
		return false
	}
	maybeMigrateTableConstraints()
	maybeMigrateStripLocalURL()
	preCheckRepair()
	maybeMigrateToFKSchema()
	maybeMigrateContributorRolesCascade()
	maybeMigrateDblpExportDirty()
	fmt.Fprintf(os.Stderr, "\nOpening the library:\n")
	initialiseLibrary()
	Library.ReadKeyOldiesFile()
	loadMappingFiles()
	seedContributorsFromEntries(&Library)
	maybeCleanupOrphanedContributors(&Library)

	if skipBibValidation || Library.ValidBibDb() {
		buildTitleIndexFromDb(&Library)
		repairDblpData()
		loadBibFromDb()
		if skipBibValidation {
			// Crash recovery: bib_entries was dirty on the previous run.
			// DB is already the primary — no bib file write needed; just clear dirty state.
			clearTableDirty("bib_entries")
			refreshBibDbTimestamp()
			skipBibValidation = false
		}
	} else {
		// No bib_entries yet. Bootstrap: if the canonical .bib file is present, import it now.
		bibPath := bibTeXFolder + bibTeXBaseName + BibFileExtension
		if FileExists(bibPath) {
			Library.Progress(ProgressImportingBibFile, bibPath)
			if !parseSyncBibFile(bibPath) {
				return false
			}
			buildTitleIndexFromDb(&Library)
		} else {
			fmt.Fprintln(os.Stderr, "Database not initialised — run '-import_bib <file.bib>' to import the bib file first.")
			return false
		}
	}

	cleanupIgnoredTitleNonDoubles(&Library)
	Library.LoadPDFFiles()
	if !skipStartupChecks {
		Library.CheckDblpDuplicates()
		Library.CheckKeyOldiesConsistency()
		Library.CheckKeyHintsConsistency()
		Library.CheckDblpWaivedConsistency()
		Library.CheckEntryFieldMappingWinners()
	}
	normalizeAuthorEditorEntryFields()
	retireResolvedAuthorEditorLosers()
	cleanupRedundantLosers()
	printSessionStats()
	if !Online {
		Library.Progress("Offline: actions requiring network connectivity will be skipped.")
	}
	preCloseHook = reportHomework
	return true
}

func openLibraryToReport() bool {
	initialiseLibrary()
	maybeMigrateDblpExportDirty()
	Library.progressSuppressed = true // read-only session: suppress routine progress noise
	Library.ReadKeyOldiesFile()
	loadMappingFiles()

	if skipBibValidation || Library.ValidBibDb() {
		loadBibFromDb()
		if skipBibValidation {
			clearTableDirty("bib_entries")
			refreshBibDbTimestamp()
			skipBibValidation = false
		}
	} else {
		fmt.Fprintln(os.Stderr, "Database not initialised — run '-import_bib <file.bib>' to import the bib file first.")
		return false
	}

	Library.LoadPDFFiles()
	Library.ReportLibrarySize()
	return true
}

func doC1Checks(key string) {
	Library.CheckNeedToMergeForEqualTitles(key)
	Library.CheckNeedToSplitBookishEntry(key)
}

// doC2Checks runs DBLP sync for key and returns true if it modified the entry.
func doC2Checks(key string) bool {
	startC2Tracking()
	Library.CheckDBLP(key)
	return stopC2Tracking()
}

func doC3Checks(key string) {
	Library.CheckEntry(Library.buildEntry(key))
}

// normalizeDblpKey strips a leading "DBLP:" prefix so that callers can pass
// keys either way and the internal KeyForDBLP wrapper adds it exactly once.
func normalizeDblpKey(key string) string {
	return strings.TrimPrefix(key, "DBLP:")
}

// findLibraryEqualWithDblp looks for an existing library entry that shares the
// same title index as key and already has a dblp field.  When such a peer is
// found, MaybeMergeEntries is called interactively; if the merge is accepted
// the function returns true (key now has a dblp field via the merge).
func findLibraryEqualWithDblp(key string) bool {
	title := Library.EntryFieldValueity(Library.MapEntryKey(key), TitleField)
	if title == "" {
		return false
	}
	titleIdx := TeXStringIndexer(title)
	if titleIdx == "" {
		return false
	}
	peers := Library.TitleIndex[titleIdx]
	if peers.Size() <= 1 {
		return false
	}
	for _, peer := range peers.ElementsSorted() {
		peer = Library.MapEntryKey(peer)
		mappedKey := Library.MapEntryKey(key)
		if peer == mappedKey {
			continue
		}
		if Library.EntryFieldValueity(peer, DBLPField) == "" {
			continue
		}
		if Library.NonDoubleEntries[mappedKey].Set().Contains(peer) {
			continue
		}
		if Library.EvidenceForBeingDifferentEntries(mappedKey, peer) {
			continue
		}
		Library.MaybeMergeEntries(key, peer)
		if Library.EntryFieldValueity(Library.MapEntryKey(key), DBLPField) != "" {
			return true
		}
	}
	return false
}

// maybeFindDBLPCandidates searches the DBLP title index for a library entry
// that has no dblp field and interactively offers the user numbered candidates.
// Pick 0 → all candidates are recorded as non-doubles with the library entry.
// Pick N → AssociateDblpKey is called and the function returns true so the
// dblpFilterYear returns the year to use for DBLP candidate year-range filtering
// and whether it is valid. Falls back through: year field → crossref parent year →
// year embedded in preferredalias.
func dblpFilterYear(key string) (int, bool) {
	yearStr := Library.EntryFieldValueity(key, "year")
	if yearStr == "" {
		if crossref := Library.EntryFieldValueity(key, "crossref"); crossref != "" {
			yearStr = Library.EntryFieldValueity(crossref, "year")
		}
	}
	if yearStr == "" {
		alias := Library.EntryFieldValueity(key, PreferredAliasField)
		if m := reYearInAlias.FindString(alias); m != "" {
			yearStr = m
		}
	}
	y, err := strconv.Atoi(yearStr)
	return y, err == nil
}

// dblpCandidateInYearRange reports whether the DBLP candidate c falls within
// the ±3-year window of the library entry's year (from dblpFilterYear).
// When the library entry has no resolvable year, all candidates are accepted.
func dblpCandidateInYearRange(c string, entryYear int, hasYear bool) bool {
	if !hasYear {
		return true
	}
	ce := dblpEntryFromFile(c)
	if ce == nil {
		return true
	}
	candYear, err := strconv.Atoi(ce.Fields["year"])
	if err != nil {
		return true
	}
	diff := candYear - entryYear
	return diff > -3 && diff < 3
}

// maybeFindDBLPCandidatesExcluding searches the DBLP title index for a library
// entry that has no dblp field and interactively offers the user numbered
// candidates. extraExclusions contains DBLP path strings (e.g.
// "conf/foo/Bar2022") already claimed by sibling variations in the same title
// bucket; those are filtered out so the same DBLP record is never offered to
// two different variations. Pick 0 → all candidates are recorded as non-doubles
// with the library entry. Pick N → AssociateDblpKey is called and the function
// returns true so the caller can re-run DBLP checks for the now-associated entry.
func maybeFindDBLPCandidatesExcluding(key string, extraExclusions TStringSet) bool {
	if Library.EntryFieldValueity(key, DBLPField) != "" {
		return false
	}
	title := Library.EntryFieldValueity(key, TitleField)
	hash := libraryTitleHash(title)
	if hash == "" {
		return false
	}
	allCandidates := readDblpTitleLinks(hash)
	existing := Library.NonDoubleEntries[key]
	entryYear, hasYear := dblpFilterYear(key)
	var candidates []string
	var yearFiltered []string
	for _, c := range allCandidates {
		// AddNonDoubleEntries stores both sides through MapEntryKey, so when this
		// candidate's DBLP key is already claimed by another library entry (e.g. a
		// near-duplicate that already has dblp set), what's actually recorded is
		// (key, thatOtherEntry) — not (key, "DBLP:..."). Checking membership with
		// the same mapping keeps this consistent with what was stored; otherwise a
		// dismissal silently never matches and the candidate is offered again on
		// every later run.
		if existing.Contains(Library.MapEntryKey(KeyForDBLP(c))) {
			continue
		}
		if extraExclusions.Contains(c) {
			Library.AddNonDoubleEntries(key, KeyForDBLP(c))
			continue
		}
		if !dblpCandidateInYearRange(c, entryYear, hasYear) {
			yearFiltered = append(yearFiltered, c)
			continue
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		// All remaining candidates were rejected by the year filter. Auto-dismiss
		// them into non-doubles so the homework count stops counting this entry.
		for _, c := range yearFiltered {
			Library.AddNonDoubleEntries(key, KeyForDBLP(c))
		}
		if len(yearFiltered) > 0 {
			flushWorkingDbToHome()
		}
		return false
	}

	// For bookish library entries, also offer the DBLP parent proceedings of each
	// candidate as a direct association target (prepended so they appear first).
	if BibTeXBookish.Contains(Library.EntryType(key)) {
		parentsSeen := map[string]bool{}
		var parentKeys []string
		for _, c := range candidates {
			if ce := dblpEntryFromFile(c); ce != nil {
				if crossref := ce.Fields["crossref"]; crossref != "" &&
					!existing.Contains(KeyForDBLP(crossref)) &&
					!extraExclusions.Contains(crossref) &&
					!parentsSeen[crossref] {
					parentsSeen[crossref] = true
					parentKeys = append(parentKeys, crossref)
				}
			}
		}
		candidates = append(parentKeys, candidates...)
	}

	if len(candidates) > 9 {
		candidates = candidates[:9]
	}
	chosen := Library.AskCandidateDblpKey(key, candidates)
	if chosen == "" {
		for _, c := range candidates {
			Library.AddNonDoubleEntries(key, KeyForDBLP(c))
			extraExclusions.Add(c)
		}
		flushWorkingDbToHome()
		return false
	}
	Library.AssociateDblpKey(key, chosen)
	return true
}

func maybeFindDBLPCandidates(key string) bool {
	return maybeFindDBLPCandidatesExcluding(key, TStringSetNew())
}

// doVariationSetDblpLinking links each variation in the list to DBLP, excluding
// DBLP keys already claimed by other variations in the same title bucket. After a
// successful link, the newly claimed key is added to the exclusion set so it cannot
// be offered to a later variation.
func doVariationSetDblpLinking(variations []string) {
	usedDblp := TStringSetNew()
	for _, v := range variations {
		if d := Library.EntryFieldValueity(Library.MapEntryKey(v), DBLPField); d != "" {
			usedDblp.Add(normalizeDblpKey(d))
		}
	}
	for _, v := range variations {
		v = Library.MapEntryKey(v)
		if !Library.EntryExists(v) {
			continue
		}
		if Library.EntryFieldValueity(v, DBLPField) != "" {
			continue
		}
		found := findLibraryEqualWithDblp(v)
		if !found {
			found = maybeFindDBLPCandidatesExcluding(v, usedDblp)
		}
		v = Library.MapEntryKey(v)
		if found {
			if d := Library.EntryFieldValueity(v, DBLPField); d != "" {
				usedDblp.Add(normalizeDblpKey(d))
			}
		}
	}
}

func doAllChecks(key string) {
	doC1Checks(key)
	maybeFindDBLPCandidates(key)
	doC2Checks(key)
	doC3Checks(key)
}

func cleanKey(rawKey string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(rawKey, "\\cite{", ""), "cite{", ""), "}", ""))
}

var BibFile string

var instanceLockFile *os.File // held open for the lifetime of the process to maintain the flock

// flockPath obtains an exclusive non-blocking flock on lockPath.
// Exits immediately if another instance already holds that lock.
// The OS releases the lock automatically when the process exits.
func flockPath(lockPath string) {
	var err error
	instanceLockFile, err = os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open lock file %s: %s\n", lockPath, err)
		os.Exit(1)
	}
	if err := syscall.Flock(int(instanceLockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintf(os.Stderr, "Another instance is already running (%s)\n", lockPath)
		os.Exit(1)
	}
}

func acquireInstanceLock() {
	flockPath(bibTeXFolder + bibTeXBaseName + LockFileExtension)
}

func acquireDblpLock() {
	flockPath(dblpFolder() + "dblp" + LockFileExtension)
}

// requireNoDblpImport exits with a clear message if a DBLP import is currently
// in progress in another process (i.e. the DBLP lock file is held).
func requireNoDblpImport() {
	lockPath := dblpFolder() + "dblp" + LockFileExtension
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return // can't tell; let the command proceed
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, "DBLP import in progress — try again later.")
		os.Exit(1)
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint — check only, not holding
}

// cleanKeys strips \cite{} wrappers from each arg and splits comma-joined cite
// lists, returning the individual key strings.
func cleanKeys(args []string) []string {
	var result []string
	for _, a := range args {
		for _, k := range strings.Split(cleanKey(a), ",") {
			if k != "" {
				result = append(result, k)
			}
		}
	}
	return result
}

// normalizeAuthorEditorEntryFields applies current name mappings to all stored author/editor
// field values in bib_entries. Any entry whose stored value contains at least one mapped alias
// is updated in-place; the old value is recorded as a loser in losing_field_values.
// Called at startup so that name mappings added between runs are immediately propagated.
func normalizeAuthorEditorEntryFields() {
	if len(Library.NameAliasToName) == 0 {
		return
	}

	rows, err := db.Query(`SELECT entry_key, field, value FROM bib_entries WHERE field IN ('author', 'editor')`)
	if err != nil {
		return
	}
	type fieldRow struct{ key, field, value string }
	var fields []fieldRow
	for rows.Next() {
		var r fieldRow
		if rows.Scan(&r.key, &r.field, &r.value) == nil {
			fields = append(fields, r)
		}
	}
	rows.Close()

	updated := 0
	for _, r := range fields {
		parts := strings.Split(r.value, " and ")
		changed := false
		for i, name := range parts {
			name = strings.TrimSpace(name)
			if canonical, ok := Library.NameAliasToName[name]; ok {
				parts[i] = canonical
				changed = true
			}
		}
		if !changed {
			continue
		}
		newValue := strings.Join(parts, " and ")
		db.Exec(`INSERT INTO losing_field_values (entry_key, field, value) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, r.key, r.field, r.value) //nolint:errcheck
		Library.SetEntryFieldValue(r.key, r.field, newValue)
		updated++
	}
	if updated > 0 {
		Library.Progress("Normalized %d author/editor field(s) via name mappings", updated)
	}
}

// retireResolvedAuthorEditorLosers deletes losing_field_values rows for author/editor
// fields where the loser value is equal to the current entry winner after applying
// name mappings. Called at startup so that name mappings added outside of triage
// automatically cascade to clean up resolved pairs.
func retireResolvedAuthorEditorLosers() {
	rows, err := db.Query(`SELECT entry_key, field, value FROM losing_field_values WHERE field IN ('author', 'editor')`)
	if err != nil {
		return
	}
	type loserRow struct{ key, field, loser string }
	var pairs []loserRow
	for rows.Next() {
		var r loserRow
		if rows.Scan(&r.key, &r.field, &r.loser) == nil {
			pairs = append(pairs, r)
		}
	}
	rows.Close()

	splitAndFilter := func(value string) []string {
		var out []string
		for _, n := range strings.Split(value, " and ") {
			n = strings.TrimSpace(n)
			lc := strings.ToLower(n)
			if n != "" && lc != "others" && lc != "et.al." && lc != "et al." {
				out = append(out, n)
			}
		}
		return out
	}

	normList := func(names []string) []string {
		result := make([]string, len(names))
		for i, n := range names {
			if canonical, ok := Library.NameAliasToName[n]; ok {
				result[i] = canonical
			} else {
				result[i] = n
			}
		}
		return result
	}

	sliceEqual := func(a, b []string) bool {
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

	retired := 0
	for _, p := range pairs {
		winner := Library.EntryFieldValueity(p.key, p.field)
		if winner == "" {
			continue
		}
		if sliceEqual(normList(splitAndFilter(winner)), normList(splitAndFilter(p.loser))) {
			db.Exec(`DELETE FROM losing_field_values WHERE entry_key=? AND field=? AND value=?`, p.key, p.field, p.loser) //nolint:errcheck
			retired++
		}
	}
	if retired > 0 {
		Library.Progress("Retired %d resolved author/editor loser(s) via name mappings", retired)
	}
}

// --- Command functions ---

// doTriageAuthorMappings interactively reviews all author/editor pairs in losing_field_values.
// Each pair is classified and resolved: brace-wrap (auto-fixed), name-equal-after-mapping
// (auto-retired), single-name variant (name-mapping offered), missing-authors (prompt),
// multi-name variant (kept), or unclassifiable (flagged).
func doTriageAuthorMappings() {
	if !openLibraryToUpdate() {
		return
	}

	maybeMigrateSkippedToNonDoubleContributorNames(&Library)
	loadNonDoubleContributorNamesFromDb(&Library)

	rows, err := bibQuery(`SELECT entry_key, field, value FROM losing_field_values WHERE field IN ('author', 'editor') AND triage_status IS NULL ORDER BY entry_key, field`)
	if err != nil {
		// Index corruption (e.g. from old multi-connection SQLite access) makes
		// ORDER BY fail with SQLITE_CORRUPT. Rebuild the index and retry once.
		bibExec(`REINDEX losing_field_values`) //nolint:errcheck
		rows, err = bibQuery(`SELECT entry_key, field, value FROM losing_field_values WHERE field IN ('author', 'editor') AND triage_status IS NULL ORDER BY entry_key, field`)
	}
	if err != nil {
		Library.Warning("Could not query losing_field_values: %s", err)
		return
	}
	type triagePair struct{ key, field, loser string }
	var pairs []triagePair
	for rows.Next() {
		var p triagePair
		if scanErr := rows.Scan(&p.key, &p.field, &p.loser); scanErr == nil {
			pairs = append(pairs, p)
		}
	}
	rows.Close()

	retireLoser := func(key, field, loser string) {
		bibExec(`DELETE FROM losing_field_values WHERE entry_key=? AND field=? AND value=?`, key, field, loser) //nolint:errcheck
	}

	markKept := func(key, field, loser string) {
		bibExec(`UPDATE losing_field_values SET triage_status='kept' WHERE entry_key=? AND field=? AND value=?`, key, field, loser) //nolint:errcheck
	}

	splitOnAnd := func(value string) []string {
		parts := strings.Split(value, " and ")
		var out []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			lc := strings.ToLower(p)
			if lc != "others" && lc != "et.al." && lc != "et al." {
				out = append(out, p)
			}
		}
		return out
	}

	normalizeName := func(name string) string {
		if canonical, ok := Library.NameAliasToName[name]; ok {
			return canonical
		}
		return name
	}

	isBraceWrapped := func(value string) bool {
		return len(value) >= 2 && value[0] == '{' && value[len(value)-1] == '}' &&
			strings.Contains(value[1:len(value)-1], " and ")
	}

	normList := func(names []string) []string {
		result := make([]string, len(names))
		for i, n := range names {
			result[i] = normalizeName(n)
		}
		return result
	}

	sliceEqual := func(a, b []string) bool {
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

outer:
	for _, p := range pairs {
		winner := Library.EntryFieldValueity(p.key, p.field)
		if winner == "" {
			retireLoser(p.key, p.field, p.loser)
			continue
		}

		Library.ResetQuestionFlag()

		// Re-check: a name mapping added during this run may have already equated them.
		wNames := splitOnAnd(winner)
		lNames := splitOnAnd(p.loser)
		if sliceEqual(normList(wNames), normList(lNames)) {
			retireLoser(p.key, p.field, p.loser)
			continue
		}

		// Brace-wrap: either side has outer braces containing " and "
		if isBraceWrapped(winner) {
			fixed := winner[1 : len(winner)-1]
			Library.SetEntryFieldValue(p.key, p.field, fixed)
			Library.Progress("Auto-fixed brace-wrap for %s %s", p.key, p.field)
			retireLoser(p.key, p.field, p.loser)
			continue
		}
		if isBraceWrapped(p.loser) {
			Library.Progress("Auto-retired brace-wrap loser for %s %s", p.key, p.field)
			retireLoser(p.key, p.field, p.loser)
			continue
		}

		normW := normList(wNames)
		normL := normList(lNames)

		if len(wNames) == len(lNames) {
			var diffPos []int
			for i := range normW {
				if normW[i] != normL[i] {
					diffPos = append(diffPos, i)
				}
			}
			if len(diffPos) == 0 {
				retireLoser(p.key, p.field, p.loser)
				continue
			}
			if len(diffPos) == 1 {
				pos := diffPos[0]
				if isNonDoubleContributorNamePair(&Library, wNames[pos], lNames[pos]) {
					markKept(p.key, p.field, p.loser)
					continue
				}
				Library.Progress("Entry: %s / field: %s\n  Winner: %s\n  Loser:  %s", p.key, p.field, winner, p.loser)
				resultName, quit, mapped := Library.resolveNamePair(p.key, p.field, pos+1, len(wNames), 1, 1, wNames[pos], lNames[pos])
				if quit {
					break outer
				}
				if mapped {
					retireLoser(p.key, p.field, p.loser)
				} else {
					addNonDoubleContributorNamePair(&Library, wNames[pos], lNames[pos])
					if contributorRolesActive {
						if wID, wOK := resolveNameToContributorID(&Library, wNames[pos]); wOK {
							if lID, lOK := resolveNameToContributorID(&Library, lNames[pos]); lOK {
								addNonDoubleContributorPair(wID, lID)
							}
						}
					}
					markKept(p.key, p.field, p.loser)
				}
				_ = resultName
			} else {
				Library.Progress("Multi-name variant (%d diffs) kept: %s %s", len(diffPos), p.key, p.field)
				markKept(p.key, p.field, p.loser)
			}
		} else {
			wSet := make(map[string]bool, len(normW))
			for _, n := range normW {
				wSet[n] = true
			}
			lSet := make(map[string]bool, len(normL))
			for _, n := range normL {
				lSet[n] = true
			}
			isSubset := func(smaller, larger map[string]bool) bool {
				for k := range smaller {
					if !larger[k] {
						return false
					}
				}
				return true
			}
			if isSubset(lSet, wSet) || isSubset(wSet, lSet) {
				markKept(p.key, p.field, p.loser)
			} else {
				markKept(p.key, p.field, p.loser)
			}
		}

		if Reporting.QuitWasRequested() {
			break outer
		}
	}
}

func doTriageContributorAliases() {
	if !openLibraryToUpdate() {
		return
	}

	type aliasPair struct {
		alias, id string
		count      int
	}
	rows, err := db.Query(`
		SELECT ecn.name_used, ecn.contributor_id, COUNT(*) AS cnt
		FROM entry_contributor_names ecn
		WHERE NOT EXISTS (
		    SELECT 1 FROM contributor_names cn
		    WHERE cn.id = ecn.contributor_id AND cn.name = ecn.name_used
		)
		GROUP BY ecn.name_used, ecn.contributor_id
		ORDER BY ecn.name_used`)
	if err != nil {
		Library.Warning("Could not query entry_contributor_names: %s", err)
		return
	}
	var pairs []aliasPair
	for rows.Next() {
		var p aliasPair
		if rows.Scan(&p.alias, &p.id, &p.count) == nil {
			pairs = append(pairs, p)
		}
	}
	rows.Close()
	if len(pairs) == 0 {
		Library.Progress("No entry-specific contributor aliases pending triage.")
		return
	}

	options := TStringSetNew()
	options.Add("g", "k", "q")
	generalised := 0
	suffix := func(n int) string {
		if n == 1 {
			return "entry"
		}
		return "entries"
	}
outer:
	for _, p := range pairs {
		contrib := Library.ContributorByID[p.id]
		if contrib == nil {
			continue
		}
		Library.ResetQuestionFlag()

		// {others} is BibTeX's "et al." convention — always auto-generalise to "others".
		if p.alias == "{others}" && contrib.Name == "others" {
			upsertContributorNameToDB(p.id, p.alias)
			Library.NameToContributorID[p.alias] = p.id
			Library.FindAliases(contrib.Name, p.alias)
			db.Exec(`DELETE FROM entry_contributor_names WHERE name_used = ? AND contributor_id = ?`, p.alias, p.id) //nolint:errcheck
			generalised++
			continue
		}

		answer := Library.WarningQuestion(
			"Generalise alias (g), keep local (k), quit (q)?",
			options,
			"Entry-specific alias %q → contributor %q (%d %s)",
			p.alias, contrib.Name, p.count, suffix(p.count))
		switch answer {
		case "g":
			upsertContributorNameToDB(p.id, p.alias)
			Library.NameToContributorID[p.alias] = p.id
			Library.FindAliases(contrib.Name, p.alias)
			db.Exec(`DELETE FROM entry_contributor_names WHERE name_used = ? AND contributor_id = ?`, p.alias, p.id) //nolint:errcheck
			generalised++
		case "k":
			// no action — alias remains entry-specific
		case "q":
			break outer
		}
		if Reporting.QuitWasRequested() {
			break outer
		}
	}
	if generalised > 0 {
		Library.Progress("Generalised %d contributor alias(es) to global contributor_names.", generalised)
	}
}

func doDisambiguateContributors() {
	if !openLibraryToUpdate() {
		return
	}

	type ambigRow struct {
		key, role string
		position  int
		id, alias string
	}
	rows, err := db.Query(`
		SELECT entry_key, role, position, contributor_id, name_used
		FROM entry_contributor_names
		ORDER BY name_used, entry_key, role, position`)
	if err != nil {
		Library.Warning("Could not query entry_contributor_names: %s", err)
		return
	}
	var ambig []ambigRow
	for rows.Next() {
		var r ambigRow
		if rows.Scan(&r.key, &r.role, &r.position, &r.id, &r.alias) == nil {
			if _, ok := Library.AmbiguousNameToContributorIDs[r.alias]; ok {
				ambig = append(ambig, r)
			}
		}
	}
	rows.Close()
	if len(ambig) == 0 {
		Library.Progress("No ambiguous contributor assignments found.")
		absorbDblpNamesCore()
		absorbDblpOrcidsCore()
		return
	}

outer:
	for _, r := range ambig {
		candidates := Library.AmbiguousNameToContributorIDs[r.alias]
		if len(candidates) == 0 {
			continue
		}
		current := Library.ContributorByID[r.id]
		if current == nil {
			continue
		}
		Library.Progress("Entry %s %s pos %d: name %q is an alias for %d contributors:",
			r.key, r.role, r.position, r.alias, len(candidates))
		for i, candID := range candidates {
			cand := Library.ContributorByID[candID]
			if cand == nil {
				continue
			}
			var entryCount int
			db.QueryRow(`SELECT COUNT(*) FROM contributor_roles WHERE contributor_id = ?`, candID).Scan(&entryCount) //nolint:errcheck
			marker := ""
			if candID == r.id {
				marker = "  ← current"
			}
			details := fmt.Sprintf("  %d: %s  [id: %s", i+1, cand.Name, candID)
			if cand.ORCID != "" {
				details += "  orcid: " + cand.ORCID
			}
			if cand.DblpKey != "" {
				details += "  dblp: " + cand.DblpKey
			}
			details += fmt.Sprintf("  entries: %d]%s", entryCount, marker)
			Library.Progress(details)
		}
		// Before offering the choice, check whether any candidate without a DblpKey
		// has entries with potential DBLP matches. Resolving those first may
		// automatically collapse the ambiguity via absorbDblpNamesCore.
		for _, candID := range candidates {
			cand := Library.ContributorByID[candID]
			if cand == nil || cand.DblpKey != "" {
				continue
			}
			noCandRows, qErr := db.Query(`
				SELECT DISTINCT cr.entry_key FROM contributor_roles cr
				WHERE cr.contributor_id = ?
				  AND NOT EXISTS (
				      SELECT 1 FROM bib_entries be
				      WHERE be.entry_key = cr.entry_key AND be.field = ?
				  )`, candID, DBLPField)
			if qErr != nil {
				continue
			}
			var entriesWithCandidates []string
			for noCandRows.Next() {
				var entryKey string
				if noCandRows.Scan(&entryKey) == nil {
					hash := libraryTitleHash(Library.EntryFieldValueity(entryKey, TitleField))
					if hash == "" {
						continue
					}
					existing := Library.NonDoubleEntries[entryKey]
					for _, c := range readDblpTitleLinks(hash) {
						if !existing.Set().Contains(KeyForDBLP(c)) {
							entriesWithCandidates = append(entriesWithCandidates, entryKey)
							break
						}
					}
				}
			}
			noCandRows.Close()
			if len(entriesWithCandidates) == 0 {
				continue
			}
			Library.Progress("  Contributor %s has %d entry/ies with potential DBLP match(es). "+
				"Resolving these first may eliminate this ambiguity automatically.",
				candID, len(entriesWithCandidates))
			Library.ResetQuestionFlag()
			yn, _ := Library.AskForInput("Fix DBLP for these entries now? (y=yes, n=skip)")
			if strings.TrimSpace(yn) != "y" {
				continue
			}
			exclusions := TStringSetNew()
			for _, key := range entriesWithCandidates {
				key = Library.MapEntryKey(key)
				if !Library.EntryExists(key) {
					continue
				}
				found := findLibraryEqualWithDblp(key)
				if !found {
					found = maybeFindDBLPCandidatesExcluding(key, exclusions)
				}
				key = Library.MapEntryKey(key)
				if found {
					doAllChecks(key)
					key = Library.MapEntryKey(key)
					if d := Library.EntryFieldValueity(key, DBLPField); d != "" {
						exclusions.Add(normalizeDblpKey(d))
					}
				}
			}
			// Re-run DBLP name absorption so the contributor gets a DblpKey if
			// the newly linked entries now match a DBLP person entry.
			absorbDblpNamesCore()
			absorbDblpOrcidsCore()
			// Re-check: if the ambiguity has collapsed (only 1 candidate remains or
			// all candidates now point to the same contributor), skip the dialog.
			newCandidates := Library.AmbiguousNameToContributorIDs[r.alias]
			if len(newCandidates) <= 1 {
				Library.Progress("Ambiguity for %q resolved automatically after DBLP linking.", r.alias)
				continue outer
			}
			// Refresh candidates for the choice below.
			candidates = newCandidates
		}

		// Pre-compute "best" candidate for the merge option so the user can see it.
		bestIdx := 1
		bestScore := -1
		for i, candID := range candidates {
			if cand := Library.ContributorByID[candID]; cand != nil {
				score := 0
				if cand.ORCID != "" {
					score += 2
				}
				if cand.DblpKey != "" {
					score += 2
				}
				var cnt int
				db.QueryRow(`SELECT COUNT(*) FROM contributor_roles WHERE contributor_id = ?`, candID).Scan(&cnt) //nolint:errcheck
				score += cnt
				if score > bestScore {
					bestScore = score
					bestIdx = i + 1
				}
			}
		}
		Library.ResetQuestionFlag()
		raw, inputErr := Library.AskForInput(fmt.Sprintf("Pick (1-%d), k=keep, m=merge all into #%d, q=quit", len(candidates), bestIdx))
		if inputErr != nil {
			break outer
		}
		switch strings.TrimSpace(raw) {
		case "q":
			break outer
		case "k":
			// keep current assignment
		case "m":
			// Merge all candidates into pre-computed best (#bestIdx).
			bestID := candidates[bestIdx-1]
			for _, candID := range candidates {
				if candID == bestID {
					continue
				}
				if fromCand := Library.ContributorByID[candID]; fromCand != nil {
					Library.Progress("Merging %q (%s) into %q (%s).",
						fromCand.Name, candID,
						Library.ContributorByID[bestID].Name, bestID)
					if mergeContributorInDB(candID, bestID) {
						for n, id := range Library.NameToContributorID {
							if id == candID {
								Library.NameToContributorID[n] = bestID
							}
						}
						delete(Library.ContributorByID, candID)
					}
				}
			}
		default:
			n, convErr := strconv.Atoi(strings.TrimSpace(raw))
			if convErr != nil || n < 1 || n > len(candidates) {
				Library.Warning("Invalid choice %q — skipping.", raw)
				continue
			}
			chosenID := candidates[n-1]
			if chosenID == r.id {
				continue
			}
			db.Exec(`UPDATE entry_contributor_names SET contributor_id = ? WHERE entry_key = ? AND role = ? AND position = ?`, //nolint:errcheck
				chosenID, r.key, r.role, r.position)
			db.Exec(`UPDATE contributor_roles SET contributor_id = ? WHERE entry_key = ? AND role = ? AND position = ?`, //nolint:errcheck
				chosenID, r.key, r.role, r.position)
			if cand := Library.ContributorByID[chosenID]; cand != nil {
				Library.Progress("Assigned %s %s pos %d → %q.", r.key, r.role, r.position, cand.Name)
			}
		}
		if Reporting.QuitWasRequested() {
			break outer
		}
	}
}

// reportHomework prints a summary of remaining work:
func countUnresolvedGroups() int {
	n := 0
	for _, keys := range Library.TitleIndex {
		var canonicals []string
		for _, k := range keys.ElementsSorted() {
			if k == Library.MapEntryKey(k) {
				canonicals = append(canonicals, k)
			}
		}
		groupUnresolved := false
		for i, a := range canonicals {
			for _, b := range canonicals[i+1:] {
				if Library.NonDoubleEntries[a].Set().Contains(b) {
					continue
				}
				if Library.EvidenceForBeingDifferentEntries(a, b) {
					continue
				}
				groupUnresolved = true
				break
			}
			if groupUnresolved {
				break
			}
		}
		if groupUnresolved {
			n++
		}
	}
	return n
}

func countDblpCandidates() int {
	n := 0
	forEachBibEntryKey(func(key string) bool {
		if Library.EntryFieldValueity(key, DBLPField) != "" {
			return true
		}
		hash := libraryTitleHash(Library.EntryFieldValueity(key, TitleField))
		if hash == "" {
			return true
		}
		existing := Library.NonDoubleEntries[key]
		for _, c := range readDblpTitleLinks(hash) {
			if !existing.Set().Contains(KeyForDBLP(c)) {
				n++
				return true
			}
		}
		return true
	})
	return n
}

//   - number of title-hash groups that contain at least one unresolved potential
//     duplicate pair (title-equal, not in non_doubles, no divergent DBLP/DOI evidence)
//   - number of entries without a dblp field that have at least one unresolved DBLP
//     title-index candidate (not already in non_doubles)
func reportHomework() {
	// Session-change summary: compare current state to what was captured at session open.
	currentEntries := countBibEntries()
	currentDblpKeys := countDblpKeyedEntries()
	var currentFieldMaps int
	bibQueryRow(`SELECT COUNT(*) FROM field_mappings`).Scan(&currentFieldMaps)
	newEntries := currentEntries - sessionStartEntryCount
	newDblpLinks := currentDblpKeys - sessionStartDblpKeyCount
	newFieldMaps := currentFieldMaps - sessionStartFieldMapCount
	unresolvedGroups := countUnresolvedGroups()
	dblpCandidates := countDblpCandidates()

	var changeRows []statRow
	if newEntries != 0 {
		changeRows = append(changeRows, statRow{StatEntries, fmt.Sprintf("%+d", newEntries), ""})
	}
	if newDblpLinks != 0 {
		changeRows = append(changeRows, statRow{StatDblpLinks, fmt.Sprintf("%+d", newDblpLinks), ""})
	}
	if newFieldMaps != 0 {
		changeRows = append(changeRows, statRow{StatFieldMappings, fmt.Sprintf("%+d", newFieldMaps), ""})
	}
	if delta := unresolvedGroups - sessionStartUnresolvedGroups; delta != 0 {
		changeRows = append(changeRows, statRow{StatTitleGroupsWithUnresolvedDuplicates, fmt.Sprintf("%+d", delta), ""})
	}
	if delta := dblpCandidates - sessionStartDblpCandidates; delta != 0 {
		changeRows = append(changeRows, statRow{StatEntriesWithUnresolvedDblpCandidates, fmt.Sprintf("%+d", delta), ""})
	}
	if sessionManualDblpAssignments > 0 {
		changeRows = append(changeRows, statRow{StatDblpKeysManuallyEntered, fmt.Sprintf("%d", sessionManualDblpAssignments), ""})
	}
	if len(changeRows) > 0 {
		printStatBlock("Session changes:", changeRows)
	}

	// Count contributors with an ORCID that have never been enriched (no seen record).
	// Only available when the contributor_orcid_seen table exists (dev tree).
	newOrcidContributors := 0
	var seenTableExists int
	bibQueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='contributor_orcid_seen'`).Scan(&seenTableExists)
	if seenTableExists > 0 {
		bibQueryRow(`SELECT COUNT(*) FROM contributors WHERE orcid != '' AND NOT EXISTS (SELECT 1 FROM contributor_orcid_seen s WHERE s.contributor_id = contributors.id AND s.canonical != '')`).Scan(&newOrcidContributors)
	}

	hwComment := func(count int, cmd string) string {
		if cmd != "" && count > 0 {
			return "[-" + cmd + "]"
		}
		return ""
	}
	hwRows := []statRow{
		{StatTitleGroupsWithUnresolvedDuplicates, fmt.Sprintf("%d", unresolvedGroups), hwComment(unresolvedGroups, "fix_duplicates")},
		{StatEntriesWithUnresolvedDblpCandidates, fmt.Sprintf("%d", dblpCandidates), hwComment(dblpCandidates, "fix_candidates")},
	}
	if seenTableExists > 0 {
		hwRows = append(hwRows, statRow{StatContributorsWithOrcidNotYetEnriched, fmt.Sprintf("%d", newOrcidContributors), hwComment(newOrcidContributors, "enrich_contributor_data")})
	}
	printStatBlock("Homework:", hwRows)
	fmt.Fprintf(os.Stderr, "\n")
}

// doFixCandidates interactively links library entries that have no DBLP key yet
// to DBLP records. Entries are processed per title-index bucket so that DBLP
// keys already claimed by one variation in the bucket are not offered to another
// (variation-aware exclusion). Within each bucket, entries that have no DBLP key
// are offered candidates; entries that already have one are skipped.
func doFixCandidates() {
	if openLibraryToUpdate() {
	outer:
		for _, keySet := range Library.TitleIndex {
			// Collect canonicals that still need a DBLP key; build exclusion set
			// from canonicals that already have one.
			exclusions := TStringSetNew()
			var needDblp []string
			for _, k := range keySet.ElementsSorted() {
				if k != Library.MapEntryKey(k) {
					continue
				}
				if d := Library.EntryFieldValueity(k, DBLPField); d != "" {
					exclusions.Add(normalizeDblpKey(d))
				} else {
					needDblp = append(needDblp, k)
				}
			}
			if len(needDblp) == 0 {
				continue
			}
			Library.ResetQuestionFlag()
			for _, key := range needDblp {
				key = Library.MapEntryKey(key)
				if !Library.EntryExists(key) {
					continue
				}
				found := findLibraryEqualWithDblp(key)
				if !found {
					found = maybeFindDBLPCandidatesExcluding(key, exclusions)
				}
				key = Library.MapEntryKey(key)
				if found {
					doAllChecks(key)
					// Re-resolve: doAllChecks may have merged key into another entry,
					// making key a loser whose DB row is gone. Use the surviving canonical.
					key = Library.MapEntryKey(key)
					if d := Library.EntryFieldValueity(key, DBLPField); d != "" {
						exclusions.Add(normalizeDblpKey(d))
					}
				}
			}
			if Library.QuitWasRequested() {
				break outer
			}
		}
	}
}

func doDefaultRun() {
	if openLibraryToUpdate() {
		clearEntryWarnings()
		Library.ReadURLsIgnoreFile()
		fmt.Fprintf(os.Stderr, "\nAnalysing and checking library:\n")
		Library.CheckEntries()
		if Library.QuitWasRequested() {
			return
		}
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		if Library.QuitWasRequested() {
			return
		}
		Library.CheckAlignTitles(false)
		if Library.QuitWasRequested() {
			return
		}
		Library.CheckDuplicateDBLPKeys()
		if Library.QuitWasRequested() {
			return
		}
		Library.CheckLoneProceedings()
		if Library.QuitWasRequested() {
			return
		}
		Library.ScanOrphanPDFs()
		Library.CheckAllURLs()
	}
}

func doCheckPDFs() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.ReadURLsIgnoreFile()
		Library.CheckPDFHealth()
	}
}

func doGetPdfs() {
	if openLibraryToUpdate() {
		Library.ReadURLsIgnoreFile()
		Library.GetPDFs()
	}
}

func doFindEntries(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		field := strings.ToLower(args[0])
		value := ""
		if len(args) > 1 {
			value = args[1]
		}
		var matches []TBibFieldMatch
		if field == "groups" {
			matches = findBibEntriesByGroup(value)
		} else {
			matches = findBibEntriesByField(field, value)
		}
		for _, m := range matches {
			fmt.Printf("%s\t%s\n", m.Key, m.Value)
		}
	}
}

func doAddToGroup(args []string) {
	if openLibraryToUpdate() {
		key := Library.MapEntryKey(cleanKey(args[0]))
		group := args[1]
		if err := addBibGroupEntry(group, key); err != nil {
			fmt.Fprintf(os.Stderr, "Could not add %s to group %s: %s\n", key, group, err)
			return
		}
		Library.GroupEntries.AddValueToStringSetMap(group, key)
		bibEntriesModified = true
	}
}

func doRenderAsBibTeX(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Print(Library.renderAsBibTeX(resolveInputKey(cleanKey(args[0]))))
	}
}

func doRenderGroup(args []string, useAliases bool) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		group := args[0]
		pubsFolder := args[1]
		if !strings.HasSuffix(pubsFolder, "/") {
			pubsFolder += "/"
		}
		citationsFolder := args[2]
		if !strings.HasSuffix(citationsFolder, "/") {
			citationsFolder += "/"
		}
		writeFile := func(path, content string) {
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Could not create directory for %s: %s\n", path, err)
				return
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Could not write %s: %s\n", path, err)
			}
		}
		for _, m := range findBibEntriesByGroup(group) {
			key := Library.MapEntryKey(m.Key)
			if key == "" {
				continue
			}

			fileKey := key
			if useAliases {
				if alias := Library.PreferredKey(key); alias != "" {
					fileKey = alias
				}
			}

			bib := Library.renderAsBibTeX(key, fileKey)
			if bib == "" {
				continue
			}

			entry := loadEntryFromDb(key)
			year := entry.FieldValue("year")
			rg := entry.FieldValue("researchgate")
			dblp := entry.FieldValue("dblp")
			if year == "" || dblp == "" || rg == "" {
				if parent, _ := Library.resolveParent(entry); parent != nil {
					if year == "" {
						year = parent.FieldValue("year")
					}
					if dblp == "" {
						dblp = parent.FieldValue("dblp")
					}
					if rg == "" {
						rg = parent.FieldValue("researchgate")
					}
				}
			}

			writeFile(pubsFolder+fileKey+".bib", bib)
			writeFile(citationsFolder+fileKey+".html", Library.renderAsHTML(key)+"\n")
			writeFile(citationsFolder+fileKey+".tex", Library.renderAsTeX(key)+"\n")

			if useAliases {
				fmt.Printf("%s|%s|%s|%s|%s\n", year, fileKey, rg, dblp, key)
			} else {
				fmt.Printf("%s|%s|%s|%s\n", year, key, rg, dblp)
			}
		}
	}
}

func doListGroupAliases(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		for _, m := range findBibEntriesByGroup(args[0]) {
			key := Library.MapEntryKey(m.Key)
			if key == "" {
				continue
			}
			alias := Library.PreferredKey(key)
			if alias == "" {
				alias = key
			}
			fmt.Printf("%s|%s\n", key, alias)
		}
	}
}

func doRenderAsTex(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.renderAsTeX(resolveInputKey(cleanKey(args[0]))))
	}
}

func doRenderAsHTML(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.renderAsHTML(resolveInputKey(cleanKey(args[0]))))
	}
}

func doRenderAsText(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.renderAsText(resolveInputKey(cleanKey(args[0]))))
	}
}

func doSetField(args []string) {
	if openLibraryToUpdate() {
		key := Library.MapEntryKey(cleanKey(args[0]))
		if !Library.EntryExists(key) {
			fmt.Fprintf(os.Stderr, "set_field: entry not found: %s\n", args[0])
			return
		}
		field := args[1]
		entry := loadEntryFromDb(key)
		if len(args) >= 3 && args[2] != "" {
			Library.setEntryField(entry, field, args[2])
		} else {
			Library.deleteEntryField(entry, field)
		}
		bibEntriesModified = true
	}
}

func doRemoveFromGroup(args []string) {
	if openLibraryToUpdate() {
		key := Library.MapEntryKey(cleanKey(args[0]))
		group := args[1]
		if err := removeBibGroupEntry(group, key); err != nil {
			fmt.Fprintf(os.Stderr, "Could not remove %s from group %s: %s\n", key, group, err)
			return
		}
		Library.GroupEntries.DeleteValueFromStringSetMap(group, key)
		bibEntriesModified = true
	}
}

// doSetGroups implements -set_groups <key> [+] <group>...
// Without '+': replaces the entry's current group membership with the given list.
// With '+' as the second argument: adds the given groups to the existing membership.
func doSetGroups(args []string) {
	if !openLibraryToUpdate() {
		return
	}
	key := Library.MapEntryKey(cleanKey(args[0]))
	if !Library.EntryExists(key) {
		fmt.Fprintf(os.Stderr, "Unknown entry key: %s\n", args[0])
		return
	}

	additive := len(args) > 1 && args[1] == "+"
	var newGroups []string
	if additive {
		newGroups = args[2:]
	} else {
		newGroups = args[1:]
	}

	if !additive {
		// Remove the entry from all its current groups.
		rows, err := db.Query(`SELECT group_name FROM bib_groups WHERE entry_key = ?`, key)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not query current groups for %s: %s\n", key, err)
			return
		}
		var current []string
		for rows.Next() {
			var g string
			if rows.Scan(&g) == nil {
				current = append(current, g)
			}
		}
		rows.Close()
		for _, g := range current {
			if err := removeBibGroupEntry(g, key); err != nil {
				fmt.Fprintf(os.Stderr, "Could not remove %s from group %s: %s\n", key, g, err)
				return
			}
			Library.GroupEntries.DeleteValueFromStringSetMap(g, key)
		}
	}

	for _, g := range newGroups {
		if g == "" {
			continue
		}
		if err := addBibGroupEntry(g, key); err != nil {
			fmt.Fprintf(os.Stderr, "Could not add %s to group %s: %s\n", key, g, err)
			return
		}
		Library.GroupEntries.AddValueToStringSetMap(g, key)
	}
	bibEntriesModified = true
}

// resolveInputKey maps a user-supplied key to its canonical entry key,
// falling back to HintToKey when MapEntryKey returns the input unchanged.
func resolveInputKey(raw string) string {
	resolved := Library.MapEntryKey(raw)
	if resolved == raw {
		if hint, ok := Library.HintToKey.Get(raw); ok {
			resolved = Library.MapEntryKey(hint)
		}
	}
	return resolved
}

func doEntryKey(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(resolveInputKey(cleanKey(args[0])))
	}
}

func doEntryKeyAlias(args []string, withMap bool) {
	Reporting.SetInteractionOff()
	if openLibraryToUpdate() {
		rawKey := cleanKey(args[0])
		key := Library.MapEntryKey(rawKey)
		alias := Library.PreferredKey(key)
		if alias == "" && key != rawKey && validPreferredKeyAlias.MatchString(rawKey) {
			// Input is a valid preferred-alias that resolved to a canonical key;
			// use it directly so -map works before preferredalias is set on the entry.
			alias = rawKey
		}
		if alias == "" {
			// No preferred alias yet — try to generate one from author/year/title.
			entry := Library.buildEntry(key)
			if derived := Library.derivePreferredAlias(entry); derived != "" {
				Library.setPreferredAlias(entry, derived)
				alias = derived
			}
		}
		if alias != "" {
			fmt.Println(alias)
			if withMap {
				appendToKeysFile(alias, key)
			}
		}
	}
}

// loadBackupKeyOldies reads a backup key_oldies CSV (format: canonical;alias)
// and returns a map from alias → canonical for use as a secondary key remap source.
func loadBackupKeyOldies(path string) map[string]string {
	m := map[string]string{}
	if !FileExists(path) {
		return m
	}
	processFile(path, func(line string) {
		parts := strings.SplitN(line, csvDelimiter, 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			m[parts[1]] = parts[0] // alias → canonical
		}
	})
	return m
}

// doRestoreKeyHints reads a backup key_hints CSV (format: key;hint per line),
// maps each old entry key to the current canonical key via MapEntryKey/EntryExists,
// and inserts surviving hints into key_hints. When the current library does not
// recognise an old key, it also tries the backup key_oldies (auto-derived from
// the same directory) to chain through an extra level of remapping.
// Processes in batches of 10 000 rows for performance.
func doRestoreKeyHints(csvPath string) {
	if csvPath == "" {
		fmt.Fprintln(os.Stderr, "-restore_key_hints requires -hints_csv <path>")
		return
	}
	if !openLibraryToUpdate() {
		return
	}

	// Load the backup key_oldies from the same directory as the hints CSV.
	oldiesPath := filepath.Join(filepath.Dir(csvPath), "key_oldies.csv")
	backupOldies := loadBackupKeyOldies(oldiesPath)
	if len(backupOldies) > 0 {
		Library.Progress("Loaded %d backup key_oldies entries from %s", len(backupOldies), oldiesPath)
	}

	f, err := os.Open(csvPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open %s: %s\n", csvPath, err)
		return
	}
	defer f.Close()

	// resolveKey tries to find a current canonical key for oldKey.
	// First via the live library's alias chain, then via the backup key_oldies.
	resolveKey := func(oldKey string) string {
		canon := Library.MapEntryKey(oldKey)
		if Library.EntryExists(canon) {
			return canon
		}
		if backupCanon, ok := backupOldies[oldKey]; ok {
			canon = Library.MapEntryKey(backupCanon)
			if Library.EntryExists(canon) {
				return canon
			}
		}
		return ""
	}

	// Load the current key_hints table so we can detect ambiguities before inserting.
	Library.Progress("Loading current key_hints for ambiguity check")
	currentHints := map[string]string{} // hint → current canonical key
	if rows, err2 := db.Query(`SELECT hint, key FROM key_hints`); err2 == nil {
		for rows.Next() {
			var h, k string
			if rows.Scan(&h, &k) == nil {
				currentHints[h] = Library.MapEntryKey(k)
			}
		}
		rows.Close()
	}

	insertOnly := `INSERT INTO key_hints (hint, key) VALUES (?, ?) ON CONFLICT(hint) DO NOTHING`

	const batchSize = 10000
	total, imported, skipped, remapped, ambiguous := 0, 0, 0, 0, 0

	commitBatch := func(tx *sql.Tx) {
		if err := tx.Commit(); err != nil {
			dbInteraction.Warning("key_hints restore commit failed: %s", err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not begin transaction: %s\n", err)
		return
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, csvDelimiter, 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		oldKey, hint := parts[0], parts[1]
		total++

		canon := resolveKey(oldKey)
		if canon == "" {
			skipped++
		} else if existing, conflict := currentHints[hint]; conflict && existing != canon {
			Library.Warning("Ambiguous key hint during restore: %s already maps to %s, refusing %s", hint, existing, canon)
			ambiguous++
		} else {
			if canon != oldKey {
				remapped++
			}
			if !conflict {
				currentHints[hint] = canon // track newly inserted hints for intra-batch conflict detection
				tx.Exec(insertOnly, hint, canon)
			}
			imported++
		}

		if total%batchSize == 0 {
			commitBatch(tx)
			Library.Progress("Restoring key hints: %d processed, %d imported, %d skipped, %d remapped, %d ambiguous", total, imported, skipped, remapped, ambiguous)
			tx, err = db.Begin()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Could not begin transaction: %s\n", err)
				return
			}
		}
	}
	commitBatch(tx)

	if err := scanner.Err(); err != nil {
		Library.Warning("Error reading %s: %s", csvPath, err)
	}

	Library.Progress("Key hints restore complete: %d total, %d imported, %d skipped (no entry), %d remapped via key_oldies, %d refused (ambiguous)", total, imported, skipped, remapped, ambiguous)
}

func doShowEntry(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.EntryString(resolveInputKey(cleanKey(args[0])), ""))
	}
}

func doFixEntries(args []string) {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		for _, key := range cleanKeys(args) {
			doAllChecks(key)
		}
	}
}

// hasUnresolvedPairs reports whether the canonical list contains at least one
// pair that is not yet recorded as a non-double and shows no contradicting
// evidence (different DBLP or DOI). Used to skip fully-resolved buckets quickly.
func hasUnresolvedPairs(canonicals []string) bool {
	for i, a := range canonicals {
		for _, b := range canonicals[i+1:] {
			if Library.NonDoubleEntries[a].Set().Contains(b) {
				continue
			}
			if Library.EvidenceForBeingDifferentEntries(a, b) {
				continue
			}
			return true
		}
	}
	return false
}

// doFixDuplicates processes each title-index bucket that contains at least one
// unresolved duplicate pair. Within each bucket it runs ResolveVariationSet
// (fixed-point content-equal auto-merge + interactive merge/non-double for
// differing pairs) and then links each surviving variation to DBLP while
// honouring the invariant that no two variations share a DBLP key.
func doFixDuplicates() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
	outer:
		for _, keySet := range Library.TitleIndex {
			var canonicals []string
			for _, k := range keySet.ElementsSorted() {
				if k == Library.MapEntryKey(k) {
					canonicals = append(canonicals, k)
				}
			}
			if len(canonicals) < 2 || !hasUnresolvedPairs(canonicals) {
				continue
			}
			Library.ResetQuestionFlag()
			variations := Library.ResolveVariationSet(canonicals)
			doVariationSetDblpLinking(variations)
			if Reporting.QuitWasRequested() {
				break outer
			}
		}
	}
}

func doUpsertDblpEntries() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		total := countBibEntries()

		// Load DBLP person maps once for contributor role cross-checking during
		// entry processing. If unavailable (no dblp import yet), the check is skipped.
		pm, pmOk := loadDblpPersonMaps()
		keyToNames := make(map[string][]string)
		if pmOk {
			for name, key := range pm.nameToKey {
				if key != "" {
					keyToNames[key] = append(keyToNames[key], name)
				}
			}
		}
		contribPersonEntries := make(map[string]map[string]map[string]bool)
		contribExistingKey := make(map[string]string)

		// Collect bookish and non-bookish keys separately so bookish entries are
		// processed first — proceedings/books establish venue field mappings that
		// inpapers/incollections then inherit, preventing double-counting artifacts
		// (e.g. ScitePress keynotes appearing in both proceedings and inproceedings).
		// seenKeys tracks every key visited so far; entries added during the bookish
		// pass (e.g. children added by CheckEntry → ForEachChildOfDBLPKey) are caught
		// by the drain loop and fixed before the non-bookish pass begins.
		var bookishKeys, otherKeys []string
		seenKeys := make(map[string]bool)
		forEachBibEntryType(func(key, entryType string) {
			seenKeys[key] = true
			if BibTeXBookish.Contains(entryType) {
				bookishKeys = append(bookishKeys, key)
			} else {
				otherKeys = append(otherKeys, key)
			}
		})

		inDblpUpdate = true
		scanned := 0
		fmt.Fprintf(os.Stderr, "\nDoing analysis based on DBLP data:\n")
		ticker := Library.NewProgressTicker(ProgressFixingDblpEntries, total)
		beginBibTransaction()
		processKey := func(key string) {
			scanned++
			ticker.SetCount(scanned)
			if dblpVal := Library.EntryFieldValueity(key, DBLPField); dblpVal != "" {
				Library.ResetQuestionFlag()
				doC1Checks(key)
				Library.MaybeFixDBLPEntry(key)
				doC3Checks(key)
				if pmOk {
					if normKey := normalizeDblpKey(dblpVal); normKey != "" {
						accumulateContributorMatchesFromEntry(key, normKey, pm,
							contribPersonEntries, contribExistingKey)
					}
				}
			}
		}
		for _, key := range bookishKeys {
			if Library.QuitWasRequested() {
				break
			}
			processKey(key)
		}
		// Drain: fix any entries added during the bookish pass (children created by
		// CheckEntry). Loop until stable — grandchildren are non-bookish so depth is
		// bounded, but looping is safer than assuming exactly one level.
		for !Library.QuitWasRequested() {
			var newKeys []string
			forEachBibEntryType(func(key, entryType string) {
				if !seenKeys[key] {
					seenKeys[key] = true
					newKeys = append(newKeys, key)
				}
			})
			if len(newKeys) == 0 {
				break
			}
			for _, key := range newKeys {
				if Library.QuitWasRequested() {
					break
				}
				processKey(key)
			}
		}
		for _, key := range otherKeys {
			if Library.QuitWasRequested() {
				break
			}
			processKey(key)
		}
		commitBibTransaction()
		inDblpUpdate = false
		ticker.Done()
		bibEntriesModified = true
		if Library.orcidAutoResolveSameCount > 0 || Library.orcidAutoResolveDiffCount > 0 {
			Library.Progress("  Auto-resolved by ORCID: %d same-person mapping(s), %d different-person disambiguation(s).",
				Library.orcidAutoResolveSameCount, Library.orcidAutoResolveDiffCount)
		}
		Library.orcidAutoResolveSameCount = 0
		Library.orcidAutoResolveDiffCount = 0
		if pmOk && len(contribPersonEntries) > 0 {
			logPath := bibTeXFolder + bibTeXBaseName + tablesFolderSuffix + "/dblp_contributor_splits.log"
			os.MkdirAll(bibTeXFolder+bibTeXBaseName+tablesFolderSuffix, 0o755) //nolint:errcheck
			var splitLog *os.File
			if f, err := os.Create(logPath); err == nil {
				splitLog = f
				fmt.Fprintf(splitLog, "DBLP contributor role cross-check splits on %s\n\n",
					time.Now().Format("2006-01-02 15:04:05"))
			}
			n := applyContributorMatchesFromEntries(&Library, pm, keyToNames,
				contribPersonEntries, contribExistingKey, splitLog)
			if splitLog != nil {
				splitLog.Close()
			}
			if n > 0 {
				Library.Progress("  DBLP contributor role cross-check: %d assignment(s)/split(s) (see dblp_contributor_splits.log).", n)
			}
		}
		absorbDblpNamesCore()
		Library.FlushAmbiguousAssignments()
	}
}

func doNewKey() {
	fmt.Println(KeyFromTime(time.Now()))
}

func doUpsertDblpFor(args []string) {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		for _, rawKey := range cleanKeys(args) {
			key := Library.MapEntryKey(rawKey)
			if key == "" {
				// Bare DBLP path (no "DBLP:" prefix) — try with the prefix.
				key = Library.MapEntryKey(KeyForDBLP(rawKey))
			}
			if key != "" {
				doAllChecks(key)
			}
		}
	}
}


func doUpsertDblpEntryFromDblpKeys(args []string) {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		for _, dblpKey := range args {
			dblpKey = normalizeDblpKey(dblpKey)
			if existing := Library.LookupDBLPKey(dblpKey); existing != "" {
				fmt.Println(existing)
			} else if added := Library.MaybeAddDBLPEntry(dblpKey); added != "" {
				Library.MarkDblpKeyMissing(added, dblpKey)
				fmt.Println(added)
				doAllChecks(added)
			}
		}
	}
}


// resolveNameToORCID looks up the DBLP persons index for canonicalName, scans
// for a homepages/ key, and returns the first ORCID found in that entry.
// Returns "" if no ORCID can be found.
func resolveNameToORCID(canonicalName string) string {
	for _, key := range readDblpPersonEntries(canonicalName) {
		if !strings.HasPrefix(key, "homepages/") {
			continue
		}
		je := readDblpJSONEntry(key)
		if je == nil {
			continue
		}
		for _, u := range je.Multi["url"] {
			if strings.HasPrefix(u, "https://orcid.org/") {
				return strings.TrimPrefix(u, "https://orcid.org/")
			}
		}
	}
	return ""
}

// resolveORCIDToName returns the canonical DBLP name for orcid.
// Strategy 1: find a homepages/ entry in the ORCID index and read its first author.
// Strategy 2: scan publication entries in the ORCID index for an author/editor
// whose ORCID field matches — more reliable because homepages entries are often
// not indexed under the ORCID.
func resolveORCIDToName(orcid string) string {
	keys := readDblpORCIDEntries(orcid)
	// Strategy 1: homepage entry.
	for _, key := range keys {
		if !strings.HasPrefix(key, "homepages/") {
			continue
		}
		je := readDblpJSONEntry(key)
		if je != nil && len(je.Authors) > 0 && je.Authors[0].Name != "" {
			return je.Authors[0].Name
		}
	}
	// Strategy 2: match ORCID field on authors/editors in publication entries.
	for _, key := range keys {
		je := readDblpJSONEntry(key)
		if je == nil {
			continue
		}
		for _, p := range je.Authors {
			if p.ORCID == orcid && p.Name != "" {
				return p.Name
			}
		}
		for _, p := range je.Editors {
			if p.ORCID == orcid && p.Name != "" {
				return p.Name
			}
		}
	}
	return ""
}

// upgradeWatchEntries resolves name→orcid upgrades and fills missing name comments
// on orcid entries. Returns the (possibly modified) slice and whether any change occurred.
func upgradeWatchEntries(entries []TWatchEntry) ([]TWatchEntry, bool) {
	changed := false
	for i, e := range entries {
		switch e.EntryType {
		case "name":
			orcid := resolveNameToORCID(e.Value)
			if orcid == "" {
				continue
			}
			// Upgrade name → orcid, preserving the name as a comment.
			comment := e.Value
			if e.Comment != "" {
				comment = e.Comment
			}
			entries[i] = TWatchEntry{EntryType: "orcid", Value: orcid, Comment: comment}
			Library.Progress("Watch: upgraded %q → orcid %s", e.Value, orcid)
			changed = true
		case "orcid":
			if e.Comment != "" {
				continue
			}
			name := resolveORCIDToName(e.Value)
			if name == "" {
				continue
			}
			entries[i].Comment = name
			changed = true
		case "contributor":
			if e.Comment != "" {
				continue
			}
			if contrib, ok := Library.ContributorByID[e.Value]; ok && contrib.Name != "" {
				entries[i].Comment = contrib.Name
				changed = true
			}
		}
	}
	return entries, changed
}

// watchEntryDblpKeys returns the complete set of DBLP entry keys for a watch entry,
// unioning the ORCID index and the person-name index.
//
// The ORCID index only contains entries where DBLP explicitly recorded the ORCID —
// typically only recent publications. The person-name index (keyed by the DBLP
// first-last name stored in w.Comment) covers all entries regardless of ORCID
// annotation. Both are needed for complete coverage.
func watchEntryDblpKeys(w TWatchEntry) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	switch w.EntryType {
	case "name":
		for _, k := range readDblpPersonEntries(w.Value) {
			add(k)
		}
		if orcid := resolveNameToORCID(w.Value); orcid != "" {
			for _, k := range readDblpORCIDEntries(orcid) {
				add(k)
			}
		}
	case "orcid":
		for _, k := range readDblpORCIDEntries(w.Value) {
			add(k)
		}
		// w.Comment holds the DBLP first-last name (e.g. "Sjaak Brinkkemper").
		// The person-name index is keyed by this format and is more complete than
		// the ORCID index, which only covers papers where DBLP recorded the ORCID.
		if w.Comment != "" {
			for _, k := range readDblpPersonEntries(w.Comment) {
				add(k)
			}
		}
	case "contributor":
		// Union all ORCID-indexed DBLP entries with person-name-indexed entries for
		// every name alias the contributor is known by.
		for _, orcid := range contributorORCIDs(w.Value) {
			for _, k := range readDblpORCIDEntries(orcid) {
				add(k)
			}
		}
		for _, alias := range contributorAliasesFromDB(w.Value) {
			for _, k := range readDblpPersonEntries(alias) {
				add(k)
			}
		}
	}
	return keys
}

// watchEntryORCIDs returns all ORCID values relevant to a watch entry for use in
// the online ORCID works fetch. For contributor entries this is the full set from
// contributor_orcids; for orcid entries the single ORCID; for name entries the
// ORCID resolved via DBLP (if any).
func watchEntryORCIDs(w TWatchEntry) []string {
	switch w.EntryType {
	case "contributor":
		return contributorORCIDs(w.Value)
	case "orcid":
		return []string{w.Value}
	case "name":
		if orcid := resolveNameToORCID(w.Value); orcid != "" {
			return []string{orcid}
		}
	}
	return nil
}

// parseBibTeXStringToEntry parses a raw BibTeX string and returns the first entry
// it contains, or nil on failure. Uses the capturedDBLPEntry mechanism so that
// the entry is captured in memory without being written to the library DB.
func parseBibTeXStringToEntry(bibtex string) *TBibTeXEntry {
	// CrossRef occasionally returns BibTeX with unbalanced braces. Append any
	// missing closing braces so the parser can close the entry cleanly.
	if extra := strings.Count(bibtex, "{") - strings.Count(bibtex, "}"); extra > 0 {
		bibtex += strings.Repeat("}", extra)
	}
	Library.capturedDBLPEntry = &TBibTeXEntry{Key: "", Fields: map[string]string{}}
	Library.ignoreIllegalFields = true
	Library.ParseBibString(bibtex + "\n")
	Library.ignoreIllegalFields = false
	entry := Library.capturedDBLPEntry
	Library.capturedDBLPEntry = nil
	if entry == nil || !entry.Exists() {
		return nil
	}
	return entry
}

// addDoiEntry creates a new library entry from a DOI-fetched TBibTeXEntry, assigns
// a new key, normalises all fields, and runs the standard entry checks.
// Returns the new library key, or "" if the entry could not be added.
func addDoiEntry(entry *TBibTeXEntry, label string) string {
	entryType := entry.EntryType()
	if entryType == "" || !BibTeXAllowedEntries.Contains(entryType) {
		return ""
	}
	key := Library.NewKey()
	Library.SetEntryType(key, entryType)
	for field, value := range entry.Fields {
		if field == EntryTypeField || !BibTeXAllowedFields.Contains(field) {
			continue
		}
		Library.SetEntryFieldValue(key, field, Library.NormaliseFieldValue(field, value))
	}
	Library.Progress("Watch: added from DOI (%s): %s", label, key)
	doAllChecks(key)
	return key
}

// runWatchORCID performs the online pass of the watch loop. For each watch entry
// it collects all known ORCIDs, fetches the ORCID works list, and for any DOI
// not already in the library it fetches BibTeX from doi.org and adds the entry.
func runWatchORCID(w TWatchEntry, label string) {
	orcids := watchEntryORCIDs(w)
	if len(orcids) == 0 {
		return
	}
	for _, orcid := range orcids {
		if Library.QuitWasRequested() {
			return
		}
		works := getORCIDWorks(orcid)
		seenDOI := map[string]bool{}
		for _, work := range works {
			for _, doi := range work.DOIs {
				if Library.QuitWasRequested() {
					return
				}
				if seenDOI[doi] {
					continue
				}
				seenDOI[doi] = true
				if entryExistsWithDOI(doi) || doiHasLoserRecord(doi) || doiHasAlias(doi) {
					continue
				}
				bibtex := fetchDoiBibTeX(doi)
				if bibtex == "" {
					continue
				}
				entry := parseBibTeXStringToEntry(bibtex)
				if entry == nil {
					continue
				}
				// The canonical DOI in the fetched BibTeX may differ from the ORCID-reported
				// DOI (doi aliasing / CrossRef redirects). Check both to avoid re-adding.
				if resolvedDOI := entry.Fields["doi"]; resolvedDOI != "" && resolvedDOI != doi && (entryExistsWithDOI(resolvedDOI) || doiHasLoserRecord(resolvedDOI) || doiHasAlias(resolvedDOI)) {
					continue
				}
				Library.ResetQuestionFlag()
				fmt.Fprintf(os.Stderr, "\nWatching %s (ORCID %s) — adding missing publication:\n", label, orcid)
				fmt.Fprintf(os.Stderr, "  DOI: %s\n", doi)
				for _, f := range []string{"title", "booktitle", "journal", "year", "author", "editor"} {
					if v := entry.Fields[f]; v != "" {
						fmt.Fprintf(os.Stderr, "  %-9s: %s\n", f, v)
					}
				}
				addDoiEntry(entry, label)
				if Reporting.QuitWasRequested() {
					return
				}
			}
		}
	}
}

// runWatch processes the watch file, adding any missing publications.
// Assumes the library is already open. Returns false (silently) if the watch
// file is absent or contains no valid entries.
func runWatch() bool {
	Library.ReadKeyNonDoublesFile()

	filePath := bibTeXFolder + bibTeXBaseName + scriptsFolderSuffix + "/watch"
	entries := ReadWatchFile(filePath)
	if len(entries) == 0 {
		return false
	}

	// Upgrade name→orcid and fill missing name comments; rewrite file if changed.
	if upgraded, changed := upgradeWatchEntries(entries); changed {
		entries = upgraded
		WriteWatchFile(filePath, entries)
	}

	for _, w := range entries {
		if Library.QuitWasRequested() {
			break
		}
		keys := watchEntryDblpKeys(w)

		label := w.Tag
		if label == "" {
			label = w.Value
		}

		if len(keys) == 0 {
			Library.Warning("No DBLP entries found for %s %q — person may not be in file store", w.EntryType, w.Value)
		}

		newCount := 0
		for _, key := range keys {
			// Skip homepage/www entries — they are not publications.
			if strings.HasPrefix(key, "homepages/") || strings.HasPrefix(key, "homepage/") {
				continue
			}
			// Skip entries already in the library.
			if Library.LookupDBLPKey(key) != "" {
				continue
			}
			newCount++

			Library.ResetQuestionFlag()
			fmt.Fprintf(os.Stderr, "\nWatching %s — adding missing publication:\n", label)
			fmt.Fprintf(os.Stderr, "  DBLP key: %s\n", key)
			if entry := dblpEntryFromFile(key); entry != nil {
				if t := entry.EntryType(); t != "" {
					fmt.Fprintf(os.Stderr, "  type    : %s\n", t)
				}
				for _, f := range []string{"title", "booktitle", "journal", "year", "author", "editor"} {
					if v := entry.Fields[f]; v != "" {
						fmt.Fprintf(os.Stderr, "  %-9s: %s\n", f, v)
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "  (not in local file store)\n")
			}

			if added := Library.MaybeAddDBLPEntry(key); added != "" {
				Library.MarkDblpKeyMissing(added, key)
				doAllChecks(added)
			}

			if Reporting.QuitWasRequested() {
				break
			}
		}

		if newCount == 0 {
			Library.Progress("Watching %s: all publications present (%d total)", label, len(keys))
		} else {
			Library.Progress("Watching %s: %d missing publication(s) checked", label, newCount)
		}

		// Online pass: fetch works from ORCID API and add entries not in DBLP yet.
		if Online && !Library.QuitWasRequested() {
			runWatchORCID(w, label)
		}
	}
	return true
}

// doWatch is the -watch entry point.
func doWatch() {
	if !openLibraryToUpdate() {
		return
	}
	if !runWatch() {
		filePath := bibTeXFolder + bibTeXBaseName + scriptsFolderSuffix + "/watch"
		Library.Progress("No watch entries found in %s", filePath)
	}
}

// runScript runs the entry_actions script if present; assumes the library is
// already open. Silent if the script file does not exist.
func runScript() bool {
	scriptPath := bibTeXFolder + bibTeXBaseName + ScriptFilePath
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return false
	}
	Library.ApplyScript(scriptPath)
	return true
}


func doAddKeyMapping(args []string) {
	if openLibraryToUpdate() {
		target := Library.MapEntryKey(cleanKey(args[len(args)-1]))
		for _, alias := range args[:len(args)-1] {
			fmt.Println("Mapping", cleanKey(alias), "to", target)
			Library.AddKeyAlias(cleanKey(alias), target)
		}
		doAllChecks(target)
	}
}

// resolveOrImportKey resolves rawKey to a library entry key. When the key is
// not in the library but looks like a DBLP path (has a "DBLP:" prefix or
// contains "/"), it is imported via MaybeAddDBLPEntry and *importedCount is
// incremented. Returns "" when the key cannot be resolved at all.
func resolveOrImportKey(rawKey string, importedCount *int) string {
	canon := resolveInputKey(rawKey)
	if Library.EntryExists(canon) {
		return canon
	}
	dblpKey := normalizeDblpKey(rawKey)
	if existing := Library.LookupDBLPKey(dblpKey); existing != "" {
		return existing
	}
	if strings.HasPrefix(rawKey, "DBLP:") || strings.Contains(rawKey, "/") {
		if added := Library.MaybeAddDBLPEntry(dblpKey); added != "" {
			*importedCount++
			return added
		}
	}
	return ""
}

func doMergeEntries(args []string) {
	keys := cleanKeys(args)
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		importedCount := 0
		resolvedKeys := make([]string, 0, len(keys))
		rawKeys := make([]string, 0, len(keys))
		for _, rawKey := range keys {
			resolved := resolveOrImportKey(rawKey, &importedCount)
			if resolved == "" {
				fmt.Fprintf(os.Stderr, "Unknown key: %s\n", rawKey)
				return
			}
			resolvedKeys = append(resolvedKeys, resolved)
			rawKeys = append(rawKeys, rawKey)
		}
		if importedCount > 1 {
			fmt.Fprintln(os.Stderr, "Error: -merge_entries accepts at most one not-yet-imported DBLP entry")
			return
		}
		target := resolvedKeys[len(resolvedKeys)-1]
		for i, alias := range resolvedKeys[:len(resolvedKeys)-1] {
			if alias == target {
				// Both keys resolve to the same canonical. Check for a ghost bib_entries
				// row under the raw key (entry still exists in bib_entries even though
				// key_oldies already records it as an alias). Merge its fields into the
				// canonical first — don't just delete, in case the ghost has un-merged data.
				rawKey := rawKeys[i]
				if rawKey != target && bibEntryExists(rawKey) {
					Library.Progress("Ghost entry %s found (alias for %s) — merging fields before cleanup", rawKey, target)
					Library.MergeEntries(rawKey, target)
				} else {
					fmt.Fprintf(os.Stderr, "%s is already an alias for %s — nothing to merge.\n", rawKey, target)
				}
				continue
			}
			Library.MergeEntries(alias, target)
		}
		doAllChecks(target)
	}
}

func doSetPreferredAlias(args []string) {
	if openLibraryToUpdate() {
		key := Library.MapEntryKey(cleanKey(args[0]))
		alias := args[1]
		entry := Library.buildEntry(key)
		if !entry.Exists() {
			fmt.Fprintf(os.Stderr, "Entry %s does not exist\n", key)
			return
		}
		if !validPreferredKeyAlias.MatchString(alias) {
			Library.Error(ErrorSetPreferredAliasInvalidFormat, alias)
			return
		}
		if target := Library.HintToKey.GetValue(alias); target != "" && target != key {
			Library.Error(ErrorSetPreferredAliasAlreadyInUse, alias, target)
			return
		}
		Library.setPreferredAlias(entry, alias)
		doAllChecks(key)
	}
}

func doApplyScript() {
	scriptPath := bibTeXFolder + bibTeXBaseName + ScriptFilePath
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "No script file found at %s\n", scriptPath)
		return
	}
	if openLibraryToUpdate() {
		runScript()
	}
}

// resolveContributorArg resolves an EP_XXXX contributor ID or any known name
// form (canonical or alias) to a contributor ID and canonical name.
// Prints an error and returns ok=false when the argument cannot be resolved.
func resolveContributorArg(arg string) (id, name string, ok bool) {
	if strings.HasPrefix(arg, "EP-") {
		c, exists := Library.ContributorByID[arg]
		if !exists {
			fmt.Fprintf(os.Stderr, "No contributor found with ID %q\n", arg)
			return "", "", false
		}
		return arg, c.Name, true
	}
	cid, found := Library.NameToContributorID[arg]
	if !found {
		fmt.Fprintf(os.Stderr, "No contributor found for %q\n", arg)
		return "", "", false
	}
	return cid, Library.ContributorByID[cid].Name, true
}

func doAddNameMapping(args []string) {
	if !openLibraryToUpdate() {
		return
	}
	canonical, alias := args[0], args[1]
	if strings.HasPrefix(args[0], "EP-") {
		if _, name, ok := resolveContributorArg(args[0]); !ok {
			return
		} else {
			canonical = name
		}
	}
	if strings.HasPrefix(args[1], "EP-") {
		if _, name, ok := resolveContributorArg(args[1]); !ok {
			return
		} else {
			alias = name
		}
	}
	Library.AddNameMapping(canonical, alias)
	Library.RenormaliseNameFields()
}

func doCorrectName(args []string) {
	if !openLibraryToUpdate() {
		return
	}
	oldName, newName := args[0], args[1]
	if oldName == newName {
		fmt.Fprintln(os.Stderr, "Old and new names are identical — nothing to do.")
		return
	}

	// Find all contributor IDs that have oldName as a stored name form.
	oldRows, err := bibQuery(`SELECT DISTINCT id FROM contributor_names WHERE name = ?`, oldName)
	if err != nil {
		Library.Error("contributor_names query failed: %s", err)
		return
	}
	var oldIDs []string
	for oldRows.Next() {
		var id string
		if e := oldRows.Scan(&id); e == nil {
			oldIDs = append(oldIDs, id)
		}
	}
	oldRows.Close()

	if len(oldIDs) == 0 {
		fmt.Fprintf(os.Stderr, "Name not found in contributor_names: %q\n", oldName)
		return
	}
	if len(oldIDs) > 1 {
		fmt.Fprintf(os.Stderr, "Name %q is stored for %d contributors — resolve ambiguity manually.\n", oldName, len(oldIDs))
		return
	}
	oldID := oldIDs[0]

	// Find the contributor that already has newName as canonical (if any).
	var newID string
	bibQueryRow(`SELECT id FROM contributors WHERE name = ?`, newName).Scan(&newID) //nolint:errcheck

	Library.Progress("Correcting name:")
	Library.Progress("  bad:     %s", oldName)
	Library.Progress("  correct: %s", newName)

	needMerge := newID != "" && newID != oldID
	if needMerge {
		Library.Progress("New name is already the canonical of a different contributor — merging.")
	}

	fmt.Fprintln(os.Stderr, "")
	if !Reporting.ConfirmAction("Proceed?") {
		return
	}

	// When new name belongs to a different contributor, merge old into new first.
	// mergeContributorInDB moves all names, roles, and entry_contributor_names from
	// oldID to newID, then deletes oldID. After the merge, oldName is a stored form
	// under newID — the cleanup below removes it.
	if needMerge {
		if !mergeContributorInDB(oldID, newID) {
			return
		}
		for n, id := range Library.NameToContributorID {
			if id == oldID {
				Library.NameToContributorID[n] = newID
			}
		}
		for orcid, id := range Library.ORCIDToContributorID {
			if id == oldID {
				Library.ORCIDToContributorID[orcid] = newID
			}
		}
		delete(Library.ContributorByID, oldID)
	}

	// Remove the bad name form from contributor_names.
	if err := bibExec(`DELETE FROM contributor_names WHERE name = ?`, oldName); err != nil {
		Library.Error("contributor_names delete failed: %s", err)
		return
	}

	// Pure rename: no pre-existing target contributor — update canonical and add new form.
	if newID == "" {
		if err := bibExec(`UPDATE contributors SET name = ? WHERE id = ?`, newName, oldID); err != nil {
			Library.Error("contributors rename failed: %s", err)
			return
		}
		if err := bibExec(`INSERT OR IGNORE INTO contributor_names (id, name) VALUES (?, ?)`, oldID, newName); err != nil {
			Library.Error("contributor_names insert failed: %s", err)
			return
		}
	}

	// Fix entry_contributor_names.
	if err := bibExec(`UPDATE entry_contributor_names SET name_used = ? WHERE name_used = ?`, newName, oldName); err != nil {
		Library.Error("entry_contributor_names update failed: %s", err)
		return
	}

	// Fix bib_entries author/editor field values.
	if err := bibExec(`UPDATE bib_entries SET value = REPLACE(value, ?, ?) WHERE field IN ('author','editor') AND value LIKE '%' || ? || '%'`, oldName, newName, oldName); err != nil {
		Library.Error("bib_entries update failed: %s", err)
		return
	}

	// Fix non_double_contributor_names: rewire pairs involving oldName.
	ndRows, err := bibQuery(`SELECT name1, name2 FROM non_double_contributor_names WHERE name1 = ? OR name2 = ?`, oldName, oldName)
	if err == nil {
		var pairs [][2]string
		for ndRows.Next() {
			var n1, n2 string
			if e := ndRows.Scan(&n1, &n2); e == nil {
				pairs = append(pairs, [2]string{n1, n2})
			}
		}
		ndRows.Close()
		for _, p := range pairs {
			bibExec(`DELETE FROM non_double_contributor_names WHERE name1 = ? AND name2 = ?`, p[0], p[1]) //nolint:errcheck
			n1, n2 := p[0], p[1]
			if n1 == oldName {
				n1 = newName
			}
			if n2 == oldName {
				n2 = newName
			}
			if n1 != n2 {
				if n1 > n2 {
					n1, n2 = n2, n1
				}
				bibExec(`INSERT OR IGNORE INTO non_double_contributor_names (name1, name2) VALUES (?, ?)`, n1, n2) //nolint:errcheck
			}
		}
	}

	loadContributorsFromDb(&Library)
	Library.CheckNameMappingConsistency()
	Library.Progress("Name correction complete.")
}

func doMergeContributors(args []string) {
	if !openLibraryToUpdate() {
		return
	}
	toID, toName, ok1 := resolveContributorArg(args[0])
	fromID, fromName, ok2 := resolveContributorArg(args[1])
	if !ok1 || !ok2 {
		return
	}
	if toID == fromID {
		fmt.Fprintln(os.Stderr, "Both arguments resolve to the same contributor — nothing to merge.")
		return
	}
	Library.Progress("Merging %q (%s) into %q (%s)", fromName, fromID, toName, toID)
	if !mergeContributorInDB(fromID, toID) {
		return
	}
	// Update in-memory maps before RenormaliseNameFields reloads contributors.
	for n, id := range Library.NameToContributorID {
		if id == fromID {
			Library.NameToContributorID[n] = toID
		}
	}
	for orcid, id := range Library.ORCIDToContributorID {
		if id == fromID {
			Library.ORCIDToContributorID[orcid] = toID
		}
	}
	if toContrib, ok := Library.ContributorByID[toID]; ok {
		if toContrib.ORCID == "" {
			if fromContrib, exists := Library.ContributorByID[fromID]; exists {
				toContrib.ORCID = fromContrib.ORCID
			}
		}
	}
	delete(Library.ContributorByID, fromID)
	Library.RenormaliseNameFields()
	Library.Progress("Merged %q into %q (%s).", fromName, toName, toID)
}

// isValidORCID checks that s matches the ORCID format NNNN-NNNN-NNNN-NNN[0-9X].
func isValidORCID(s string) bool {
	if len(s) != 19 {
		return false
	}
	for i, ch := range s {
		switch i {
		case 4, 9, 14:
			if ch != '-' {
				return false
			}
		case 18:
			if (ch < '0' || ch > '9') && ch != 'X' {
				return false
			}
		default:
			if ch < '0' || ch > '9' {
				return false
			}
		}
	}
	return true
}

// doEnrichOrcidProfilesCore is the body of -enrich_orcid_profiles, callable
// from both the standalone command and the combined -enrich_contributor_data pass.
// Fetches ORCID person records, adds aliases, and challenges canonical mismatches.
func doEnrichOrcidProfilesCore() {
	loadNonDoubleContributorNamesFromDb(&Library)

	if !Online {
		Library.Progress("Not online — ORCID profile enrichment skipped.")
		return
	}

	rows, err := db.Query(`SELECT contributor_id, orcid FROM contributor_orcids`)
	if err != nil {
		Library.Warning("Could not query contributor_orcids: %s", err)
		return
	}
	type idOrcid struct{ id, orcid string }
	var pairs []idOrcid
	for rows.Next() {
		var id, orcid string
		if rows.Scan(&id, &orcid) == nil {
			pairs = append(pairs, idOrcid{id, orcid})
		}
	}
	rows.Close()

	if len(pairs) == 0 {
		Library.Progress("No contributors with ORCIDs found.")
		return
	}

	// Count contributors that need processing, distinguishing disk-cache hits
	// (fast) from genuine network fetches (slow, cache miss).
	needsFetch := 0
	fromCache := 0
	fromNetwork := 0
	reCheckCount := 0
	for _, p := range pairs {
		if c, ok := Library.ContributorByID[p.id]; ok {
			s := loadORCIDSeen(p.id, p.orcid)
			if s.canonical == c.Name && s.canonical != "" {
				continue // already up to date
			}
			needsFetch++
			if loadCachedORCIDPerson(p.orcid) != nil {
				fromCache++
			} else {
				fromNetwork++
			}
			if s.canonical != "" {
				reCheckCount++
			}
		}
	}
	if needsFetch == 0 {
		Library.Progress("All %d ORCID contributor record(s) already up to date.", len(pairs))
		return
	}
	Library.Progress("%d ORCID contributor record(s) to process: %d from cache, %d need network fetch, %d re-check (%d already up to date).",
		needsFetch, fromCache, fromNetwork, reCheckCount, len(pairs)-needsFetch)
	newAliases := 0
	matchedOnly := cmdMatchedOrcidDataOnly
	skipped := 0
	ticker := Library.NewProgressTicker("Fetching ORCID person records", needsFetch)
	fetched := 0
	if matchedOnly {
		Library.Progress("Matched-only pass: challenges will be skipped (they remain as homework).")
	}

	for _, p := range pairs {
		if Library.QuitWasRequested() {
			break
		}
		contrib, ok := Library.ContributorByID[p.id]
		if !ok {
			continue
		}
		Library.ResetQuestionFlag()

		// Fast path: if the seen record's canonical matches the current canonical, the
		// ORCID data is unchanged and no challenge is needed — skip the network fetch.
		storedSeen := loadORCIDSeen(p.id, p.orcid)
		if storedSeen.canonical == contrib.Name && storedSeen.canonical != "" {
			continue
		}

		// Slow path: canonical changed or never seen — use cache then network.
		fetched++
		ticker.SetCount(fetched)
		person := getORCIDPerson(p.orcid)
		if person == nil {
			// Profile unavailable (private or not found): record as seen so the
			// homework counter and fast-path don't keep flagging this contributor.
			upsertORCIDSeen(p.id, p.orcid, orcidSeenRecord{canonical: contrib.Name})
			continue
		}

		// Build the full set of name forms from the ORCID record:
		// credit-name, declared (Last,First), natural order (First Last), other-names.
		passportNatural := "" // given-names + " " + family-name (First Last order)
		if person.DeclaredName != "" {
			passportNatural = swapBibTeXNameFormat(person.DeclaredName) // Last,First → First Last
		}
		swappedCredit := swapBibTeXNameFormat(person.CreditName)
		inferredName := inferBibTeXFromCreditName(person.CreditName, person.DeclaredName)
		allForms := uniqueForms(
			append([]string{person.CreditName, person.DeclaredName, passportNatural, swappedCredit, inferredName},
				person.OtherNames...)...)

		// Full seen-record check: compare all ORCID-provided name values (canonical
		// already confirmed to match above for the fast path; this catches cases where
		// the canonical changed externally, which bypasses the fast path).
		sortedOtherNames := append([]string(nil), person.OtherNames...)
		sort.Strings(sortedOtherNames)
		currentSeen := orcidSeenRecord{
			canonical:    contrib.Name,
			creditName:   person.CreditName,
			declaredName: person.DeclaredName,
			otherNames:   strings.Join(sortedOtherNames, "|"),
		}
		alreadySeen := storedSeen == currentSeen

		// Empty profile: ORCID has no names at all — nothing to decide.
		// Mark as seen silently so it doesn't recur as homework.
		if person.CreditName == "" && person.DeclaredName == "" && len(person.OtherNames) == 0 {
			upsertORCIDSeen(p.id, p.orcid, currentSeen)
			continue
		}

		// Determine credit-name and declared-name status BEFORE registering new aliases
		// so the "was it already explicitly mapped?" signal is not contaminated by this run.
		//
		// The manual canonical wins (suppresses challenge) when any of:
		//   (a) credit-name matches the canonical in some ordering,
		//   (b) credit-name (or its swap) was already mapped to this canonical in a prior run,
		//   (c) declared name (ORCID family+given) matches the canonical exactly or via swap —
		//       this is the most authoritative signal: ORCID's own passport-name matches.
		// Exception: a different surname in the declared name overrides all of the above,
		// since it suggests the canonical's surname boundary may be wrong.
		priorCreditMapped := Library.NameAliasToName[person.CreditName] == contrib.Name ||
			(swappedCredit != "" && Library.NameAliasToName[swappedCredit] == contrib.Name)
		creditMatches := creditNameMatches(contrib.Name, person.CreditName)
		declaredMatches := creditNameMatches(contrib.Name, person.DeclaredName)
		surnameBoundaryDiffers := declaredNameHasDifferentSurname(contrib.Name, person.DeclaredName)
		manualNameWins := ((creditMatches || priorCreditMapped || declaredMatches) && !surnameBoundaryDiffers) ||
			(alreadySeen && !surnameBoundaryDiffers)

		// Register all unambiguous, unmapped forms as aliases for this contributor.
		for _, form := range allForms {
			if form == contrib.Name {
				continue
			}
			_, mapped := Library.NameToContributorID[form]
			_, ambiguous := Library.AmbiguousNameToContributorIDs[form]
			if mapped || ambiguous {
				continue
			}
			upsertContributorNameToDB(p.id, form)
			Library.NameToContributorID[form] = p.id
			Library.AddNameMapping(contrib.Name, form)
			newAliases++
		}
		// Also register swapped forms of other-names.
		for _, alias := range person.OtherNames {
			if swapped := swapBibTeXNameFormat(alias); swapped != "" && swapped != contrib.Name {
				_, mapped := Library.NameToContributorID[swapped]
				_, ambiguous := Library.AmbiguousNameToContributorIDs[swapped]
				if !mapped && !ambiguous {
					upsertContributorNameToDB(p.id, swapped)
					Library.NameToContributorID[swapped] = p.id
					Library.AddNameMapping(contrib.Name, swapped)
					newAliases++
				}
			}
		}

		// Manual name wins: credit-name or declared-name matches, or already mapped.
		if manualNameWins {
			if !alreadySeen {
				upsertORCIDSeen(p.id, p.orcid, currentSeen)
			}
			continue
		}

		// For contributors with a DBLP key, DBLP has established their identity —
		// add ORCID aliases without a challenge. One exception: if the ORCID declared
		// surname differs significantly from the DBLP canonical, the ORCID↔DBLP link
		// may be wrong on either side. Flag it and skip rather than auto-apply.
		if contrib.DblpKey != "" {
			// DBLP has established this contributor's identity. If the ORCID declared
			// surname still differs (after case-fold and containment normalisation), the
			// DBLP↔ORCID link may be imprecise (cultural naming conventions, wrong link,
			// etc.) but there is nothing actionable here at run time. Mark as seen;
			// enrichment will be limited to the aliases already registered above.
			upsertORCIDSeen(p.id, p.orcid, currentSeen)
			continue
		}

		// Contributor has no DBLP key — fall through to challenge logic.
		if matchedOnly {
			skipped++
			continue
		}

		if person.CreditName == "" {
			// No credit-name to challenge with — mark as seen so this ORCID does not
			// recur as homework.  We still registered any declared-name aliases above.
			upsertORCIDSeen(p.id, p.orcid, currentSeen)
			continue
		}
		_, ambiguous := Library.AmbiguousNameToContributorIDs[person.CreditName]
		existingID, mapped := Library.NameToContributorID[person.CreditName]
		if ambiguous || (mapped && existingID != p.id) {
			Library.Warning("ORCID %s: credit-name %q is ambiguous or claimed by another contributor — not challenging",
				p.orcid, person.CreditName)
			upsertORCIDSeen(p.id, p.orcid, currentSeen)
			continue
		}

		// Commit the spinner line (make it permanently visible) before the challenge
		// dialog so the user can see the fetch count above the dialog.
		SpinnerCommit()

		// Present the options for the new canonical.
		options := TStringSetNew()
		options.Add("k", "c")
		offerDeclared := person.DeclaredName != ""
		if offerDeclared {
			options.Add("d")
		}
		if passportNatural != "" && passportNatural != person.CreditName && passportNatural != person.DeclaredName {
			options.Add("n") // natural order of declared name
		}
		offerInferred := inferredName != "" && inferredName != contrib.Name &&
			inferredName != person.DeclaredName && inferredName != person.CreditName
		if offerInferred {
			options.Add("i")
		}
		options.Add("e")
		fmt.Fprintf(os.Stderr,
			"ORCID %s  %s\n  Current canonical : %q\n  Credit-name       : %q\n",
			p.orcid, p.id, contrib.Name, person.CreditName)
		if person.DeclaredName != "" {
			fmt.Fprintf(os.Stderr, "  Declared name     : %q\n", person.DeclaredName)
		}
		if surnameBoundaryDiffers {
			fmt.Fprintf(os.Stderr, "  ^ surname boundary differs from current canonical\n")
		}
		if offerInferred {
			fmt.Fprintf(os.Stderr, "  Inferred name     : %q  (credit-name parsed using declared surname)\n", inferredName)
		}
		fmt.Fprintf(os.Stderr, "  k=keep current  c=use credit-name")
		if offerDeclared {
			if person.DeclaredName == contrib.Name {
				fmt.Fprintf(os.Stderr, "  d=use declared (%q = current)", person.DeclaredName)
			} else {
				fmt.Fprintf(os.Stderr, "  d=use declared (%q)", person.DeclaredName)
			}
		}
		if options.Contains("n") {
			fmt.Fprintf(os.Stderr, "  n=natural order of declared (%q)", passportNatural)
		}
		if offerInferred {
			fmt.Fprintf(os.Stderr, "  i=use inferred (%q)", inferredName)
		}
		fmt.Fprintf(os.Stderr, "  e=edit\n")

		answer := Library.WarningQuestion("", options,
			"ORCID %s: credit-name %q differs from canonical %q for %s",
			p.orcid, person.CreditName, contrib.Name, p.id)

		var newCanon string
		switch answer {
		case "c":
			newCanon = person.CreditName
		case "d":
			newCanon = person.DeclaredName
		case "i":
			newCanon = inferredName
		case "n":
			newCanon = passportNatural
		case "e":
			raw, _ := Library.AskForInput("Enter new canonical name")
			newCanon = strings.TrimSpace(raw)
		default: // "k" or empty → keep
		}

		if newCanon != "" && newCanon != contrib.Name {
			oldCanon := contrib.Name
			contrib.Name = newCanon
			upsertContributorToDB(p.id, newCanon, contrib.ORCID)
			upsertContributorNameToDB(p.id, oldCanon)
			Library.NameToContributorID[oldCanon] = p.id
			Library.NameToContributorID[newCanon] = p.id
			Library.AddNameMapping(newCanon, oldCanon)
			newAliases++
		}

		// Record the seen signature with the final canonical (after any promotion).
		// On future runs: if canonical, credit-name, declared-name, and other-names
		// all match this record, the challenge is suppressed. Any change to any of
		// these values — including an external canonical update — triggers a re-challenge.
		currentSeen.canonical = contrib.Name
		upsertORCIDSeen(p.id, p.orcid, currentSeen)

		Library.Progress("ORCID %s processed (%d/%d fetched, %d new alias(es) so far).",
			p.orcid, fetched, needsFetch, newAliases)

		if Library.QuitWasRequested() {
			break
		}
	}
	ticker.Done()
	if matchedOnly && skipped > 0 {
		Library.Progress("ORCID profile enrichment: %d new alias(es) added; %d challenge(s) skipped (run without -matched_orcid_data_only to process them).", newAliases, skipped)
	} else {
		Library.Progress("ORCID profile enrichment: %d new alias(es) added.", newAliases)
	}
}

// doEnrichOrcidProfiles is the -enrich_orcid_profiles entry point.
func doEnrichOrcidProfiles() {
	if !openLibraryToUpdate() {
		return
	}
	doEnrichOrcidProfilesCore()
}

// doUpgradeWatchActions rewrites the watch file, replacing name/orcid entries with
// contributor "EP_XXXX" where the contributor is known. Ambiguous names are resolved
// interactively. Duplicate entries for the same contributor are collapsed to one.
func doUpgradeWatchActions() {
	if !openLibraryToUpdate() {
		return
	}
	pm, _ := loadDblpPersonMaps()
	filePath := bibTeXFolder + bibTeXBaseName + scriptsFolderSuffix + "/watch"
	entries := ReadWatchFile(filePath)
	if len(entries) == 0 {
		Library.Progress("Watch file empty or absent: %s", filePath)
		return
	}

	var upgraded []TWatchEntry
	seenIDs := map[string]bool{}
	changed := false

	for _, e := range entries {
		switch e.EntryType {
		case "contributor":
			id := e.Value
			// Resolve stale IDs — follow any absorbed-ID chain to the current canonical.
			if canonical := Library.ResolveContributorID(id); canonical != id {
				if _, stillExists := Library.ContributorByID[canonical]; stillExists {
					Library.Progress("Watch: resolved stale contributor %q → %s", id, canonical)
					e = TWatchEntry{EntryType: "contributor", Value: canonical, Comment: e.Comment, Tag: e.Tag}
					id = canonical
					changed = true
				} else {
					Library.Warning("Watch: contributor %q (chain → %s) no longer exists — keeping entry", id, canonical)
				}
			} else if _, exists := Library.ContributorByID[id]; !exists {
				Library.Warning("Watch: contributor %q not found and has no oldie record — keeping entry", id)
			}
			if seenIDs[id] {
				Library.Progress("Watch: removed duplicate contributor %q", id)
				changed = true
				continue
			}
			seenIDs[id] = true
			// Fill comment if missing or stale.
			if contrib, ok := Library.ContributorByID[id]; ok && contrib.Name != "" && e.Comment != contrib.Name {
				e.Comment = contrib.Name
				changed = true
			}
			upgraded = append(upgraded, e)

		case "name":
			// Normalize HTML entities before lookup; the name may have been written
			// with &atilde; etc. from a raw DBLP/ORCID source.
			if clean := applyHtmlCharMap(e.Value); clean != e.Value {
				Library.Progress("Watch: normalized HTML in name %q → %q", e.Value, clean)
				e = TWatchEntry{EntryType: "name", Value: clean, Comment: e.Comment, Tag: e.Tag}
				changed = true
			}
			id, known := Library.NameToContributorID[e.Value]
			if !known {
				// Check for ambiguity.
				if ids, ambig := Library.AmbiguousNameToContributorIDs[e.Value]; ambig {
					fmt.Printf("\nAmbiguous name %q — which contributor?\n", e.Value)
					for i, cid := range ids {
						name := cid
						if contrib, ok := Library.ContributorByID[cid]; ok {
							name = contrib.Name
						}
						fmt.Printf("  %d) %s (%s)\n", i+1, name, cid)
					}
					fmt.Printf("  0) keep as name entry\n> ")
					var choice string
					fmt.Scanln(&choice)
					n := 0
					fmt.Sscan(choice, &n)
					if n >= 1 && n <= len(ids) {
						id = ids[n-1]
						known = true
					}
				}
			}
			if !known {
				upgraded = append(upgraded, e)
				continue
			}
			if seenIDs[id] {
				Library.Progress("Watch: removed duplicate name %q (same contributor as earlier entry)", e.Value)
				changed = true
				continue
			}
			seenIDs[id] = true
			comment := e.Value
			if contrib, ok := Library.ContributorByID[id]; ok && contrib.Name != "" {
				comment = contrib.Name
			}
			upgraded = append(upgraded, TWatchEntry{EntryType: "contributor", Value: id, Comment: comment})
			Library.Progress("Watch: upgraded name %q → contributor %s", e.Value, id)
			changed = true

		case "orcid":
			id := Library.ORCIDToContributorID[e.Value]
			if id == "" {
				if dblpKey := pm.orcidToKey[e.Value]; dblpKey != "" {
					id = Library.DblpKeyToContributorID[dblpKey]
					if id == "" {
						// DblpKey not yet set in DB — try resolving via canonical name.
						if canonical := pm.keyToCanonical[dblpKey]; canonical != "" {
							id = Library.NameToContributorID[canonical]
						}
					}
				}
			}
			if id == "" {
				upgraded = append(upgraded, e)
				continue
			}
			if seenIDs[id] {
				Library.Progress("Watch: removed duplicate orcid %q (same contributor as earlier entry)", e.Value)
				changed = true
				continue
			}
			seenIDs[id] = true
			comment := e.Comment
			if comment == "" {
				if contrib, ok := Library.ContributorByID[id]; ok && contrib.Name != "" {
					comment = contrib.Name
				}
			}
			upgraded = append(upgraded, TWatchEntry{EntryType: "contributor", Value: id, Comment: comment})
			Library.Progress("Watch: upgraded orcid %q → contributor %s", e.Value, id)
			changed = true

		default:
			upgraded = append(upgraded, e)
		}
	}

	if changed {
		WriteWatchFile(filePath, upgraded)
		Library.Progress("Watch file updated: %s", filePath)
	} else {
		Library.Progress("Watch file already up to date.")
	}
}

func doAssignOrcid(args []string) {
	if !openLibraryToUpdate() {
		return
	}
	loadNonDoubleContributorNamesFromDb(&Library)

	orcid := strings.ToUpper(strings.TrimSpace(args[1]))

	if !isValidORCID(orcid) {
		fmt.Fprintf(os.Stderr, "Invalid ORCID format %q — expected NNNN-NNNN-NNNN-NNN[0-9X]\n", orcid)
		return
	}

	id, _, ok := resolveContributorArg(args[0])
	if !ok {
		return
	}

	contrib := Library.ContributorByID[id]

	// Check for ownership conflict.
	if existingID := orcidToContributorID(&Library, orcid); existingID != "" && existingID != id {
		existing := Library.ContributorByID[existingID]
		fmt.Fprintf(os.Stderr, "ORCID %s is already assigned to contributor %s (%s)\n",
			orcid, existingID, existing.Name)
		return
	}

	canonical := contrib.ORCID == ""
	upsertContributorORCIDToDB(id, orcid, canonical)
	Library.ORCIDToContributorID[orcid] = id
	if canonical {
		contrib.ORCID = orcid
		Library.Progress("Set canonical ORCID %s for contributor %s (%s)", orcid, id, contrib.Name)
	} else {
		Library.Progress("Added additional ORCID %s for contributor %s (%s) — canonical is %s",
			orcid, id, contrib.Name, contrib.ORCID)
	}
}

func main() {
	fmt.Fprintf(os.Stderr, "%s %s\n", filepath.Base(os.Args[0]), AppVersion)

	baseFlag := flag.String("base", "", "path/basename of the library (required)")
	flag.BoolVar(&forceWrite, "force_write", false, "force write even if unchanged")

	flag.BoolVar(&cmdTrustHints, "trust_hints", false, "harvest: auto-accept key-hint matches without confirmation")
	flag.BoolVar(&cmdCollectKeys, "collect_keys", false, "harvest: add source entry keys to the hints DB when unambiguous")
	flag.StringVar(&cmdHarvestGroup, "group", "", "harvest: add all resolved entries to this group")

	var cmdVersion bool
	flag.BoolVar(&cmdVersion, "version", false, "print version and exit")
	flag.BoolVar(&cmdVersion, "v", false, "print version and exit")

	var (
		cmdSync               bool
		cmdGetPdfs            bool
		cmdFindEntries        bool
		cmdEntryKey           bool
		cmdEntryKeyAlias      bool
		cmdShowEntry          bool
		cmdFixEntries         bool
		cmdFixDuplicates        bool // -fix_duplicates: fix entries in unresolved title groups
		cmdFixCandidates        bool // -fix_candidates: link unmatched entries to DBLP
		cmdTriageAuthorMappings    bool // -triage_author_mappings: triage author/editor losing_field_values
		cmdTriageContributorAliases bool // -triage_contributor_aliases: generalise or keep entry-specific contributor aliases
		cmdDisambiguateContributors bool // -disambiguate_contributors: resolve ambiguous contributor-name assignments
		cmdUpgradeWatchActions      bool // -upgrade_watch_actions: rewrite watch file to contributor format
		cmdAssignOrcid              bool // -assign_orcid: assign an ORCID to a contributor
		cmdEnrichContributorData    bool // -enrich_contributor_data: run full contributor enrichment pipeline
		cmdMergeContributors     bool // -merge_contributors: merge two contributors into one
		cmdAddDblpEntry   bool
		cmdAddDblpEntries bool
		cmdWatch             bool
		cmdAddKeyMapping         bool
		cmdMergeEntries       bool
		cmdAddNameMapping     bool
		cmdCorrectName        bool
		cmdAddDoiAlias        bool
		cmdSetPreferredAlias  bool
		cmdNewKey             bool
		cmdDeleteEntry        bool
		cmdMap                bool
		cmdUseAliases         bool
		cmdAddToGroup         bool
		cmdRemoveFromGroup    bool
		cmdSetGroups          bool
		cmdSetField           bool
		cmdRenderAsBibTeX     bool
		cmdRenderGroup        bool
		cmdListGroupAliases   bool
		cmdRenderAsTex        bool
		cmdRenderAsHTML       bool
		cmdRenderAsText       bool
		cmdCheckPdfs                bool
		cmdAlignBooktitleCountries  bool
		cmdUpdateOrcidCache         bool
		cmdLoadDblpXml              bool
		cmdUpdateDblp               bool
		cmdRepairDblpManifest       bool
		cmdRebuildDblpCrossrefIndex bool
		cmdRebuildDblpTitleIndex    bool
		cmdRestoreKeyHints      bool
		restoreKeyHintsPath     string
		cmdDeleteGarbage            bool
		cmdRestoreFromDump          bool
		cmdRestoreBackupName        string // -restore <name>: restore named backup to home DB
		cmdApplyScript              bool
		// Unified table export/import (v23.0).
		// Bare flag (-export) means "all"; with value (-export t1,t2) means those tables.
		cmdExport tableListFlag
		cmdImport tableListFlag

		cmdSetSyncStatus string // -set_sync_status <status> <source_key> <stem>: set/clear a sync status flag

		cmdImportAllCSV bool // legacy migration helper; kept for migrate.sh compat
		cmdImportBib    bool

		cmdHarvest bool // -harvest: harvest entries from a bib file (path from args) or stdin
	)

	flag.BoolVar(&cmdSync, "sync", false, "sync library to bib file(s) via exchange config; optional arg narrows to one file")
	flag.BoolVar(&cmdPull, "pull", false, "with -sync: skip up-sync (phase 1) and re-import; only write bib output from DB")
	flag.BoolVar(&cmdGetPdfs, "get_pdfs", false, "download missing PDFs into the files folder")
	flag.BoolVar(&cmdFindEntries, "find_entries", false, "list entries matching field [value] (key TAB value per line)")
	flag.BoolVar(&cmdEntryKey, "entry_key", false, "resolve alias to canonical key")
	flag.BoolVar(&cmdEntryKeyAlias, "entry_key_alias", false, "get preferred alias for a key")
	flag.BoolVar(&cmdShowEntry, "show_entry", false, "print full entry content")
	flag.BoolVar(&cmdFixEntries, "fix_entries", false, "fix/check specific entries")
	flag.BoolVar(&cmdFixEntries, "fix_entry", false, "alias for -fix_entries")
	flag.BoolVar(&cmdFixDuplicates, "fix_duplicates", false, "interactively resolve title-duplicate pairs in the library")
	flag.BoolVar(&cmdFixDuplicates, "fix_all_entries", false, "alias for -fix_duplicates")
	flag.BoolVar(&cmdTriageAuthorMappings, "triage_author_mappings", false, "triage author/editor entries in losing_field_values")
	flag.BoolVar(&cmdTriageContributorAliases, "triage_contributor_aliases", false, "generalise or keep entry-specific contributor aliases")
	flag.BoolVar(&cmdDisambiguateContributors, "disambiguate_contributors", false, "resolve ambiguous contributor-name assignments in entry_contributor_names")
	flag.BoolVar(&cmdUpgradeWatchActions, "upgrade_watch_actions", false, "rewrite watch file replacing name/orcid entries with contributor EP_XXXX")
	flag.BoolVar(&cmdAssignOrcid, "assign_orcid", false, "assign an ORCID to a contributor: -assign_orcid <name-or-EP-id> <orcid>")
	flag.BoolVar(&cmdEnrichContributorData, "enrich_contributor_data", false, "run full contributor enrichment pipeline: absorb DBLP names+ORCIDs, enrich from ORCID profiles, merge ORCID duplicates")
	flag.BoolVar(&cmdMatchedOrcidDataOnly, "matched_orcid_data_only", false, "with -enrich_contributor_data: skip ORCID challenges in step 3, leaving them as homework")
	flag.BoolVar(&cmdMergeContributors, "merge_contributors", false, "merge second contributor into first: -merge_contributors <into> <from>")
	flag.BoolVar(&cmdAddDblpEntries, "update_all_dblp_entries", false, "update all library entries that have a DBLP key with fresh DBLP data")
	flag.BoolVar(&cmdFixCandidates, "fix_candidates", false, "interactively link library entries without a DBLP key to DBLP records")
	flag.BoolVar(&cmdFixCandidates, "extend_dblp_coverage", false, "alias for -fix_candidates")
	flag.BoolVar(&cmdFix, "fix", false, "apply full per-entry checks when combined with -sync or -harvest")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entry", false, "upsert DBLP data for one or more given entries (library or DBLP keys)")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entries", false, "alias for -add_dblp_entry")
	flag.BoolVar(&cmdWatch, "watch", false, "check watched persons/ORCIDs for missing publications")
	flag.BoolVar(&cmdAddKeyMapping, "add_key_mapping", false, "add key alias(es) to a canonical key: -add_key_mapping <alias>... <canonical>")
	flag.BoolVar(&cmdAddKeyMapping, "add_key_mappings", false, "alias for -add_key_mapping")
	flag.BoolVar(&cmdMergeEntries, "merge_entries", false, "merge entries into target: -merge_entries <key>... <target>")
	flag.BoolVar(&cmdAddNameMapping, "add_name_mapping", false, "add a name alias mapping: -add_name_mapping <canonical> <alias>")
	flag.BoolVar(&cmdCorrectName, "correct_name", false, "correct a name spelling everywhere: -correct_name <bad_name> <correct_name>")
	flag.BoolVar(&cmdAddDoiAlias, "add_doi_alias", false, "record a DOI as an alias for an entry: -add_doi_alias <entry_key> <doi>")
	flag.BoolVar(&cmdSetPreferredAlias, "set_preferred_alias", false, "set preferred alias for a key: -set_preferred_alias <key> <alias>")
	flag.BoolVar(&cmdNewKey, "new_key", false, "print a fresh canonical key and exit")
	flag.BoolVar(&cmdDeleteEntry, "delete_entry", false, "delete one or more library entries: -delete_entry <key>...")
	flag.BoolVar(&cmdMap, "map", false, "also record alias→key in the project map file (with -entry_key_alias)")
	flag.BoolVar(&cmdAddToGroup, "add_to_group", false, "add an entry to a group: -add_to_group <key> <group>")
	flag.BoolVar(&cmdRemoveFromGroup, "remove_from_group", false, "remove an entry from a group")
	flag.BoolVar(&cmdSetGroups, "set_groups", false, "set group membership: -set_groups <key> [+] <group>...")
	flag.BoolVar(&cmdSetField, "set_field", false, "set a field on an entry: -set_field <key> <field> [<value>] (omit value to clear)")
	flag.BoolVar(&cmdRenderGroup, "render_group", false, "render all entries in a group to pubs/citations folders")
	flag.BoolVar(&cmdListGroupAliases, "list_group_aliases", false, "list canonical|alias pairs for all entries in a group")
	flag.BoolVar(&cmdUseAliases, "use_aliases", false, "use preferred aliases as file names in -render_group")
	flag.BoolVar(&cmdRenderAsBibTeX, "render_as_bibtex", false, "render entry as self-contained BibTeX")
	flag.BoolVar(&cmdRenderAsTex, "render_as_tex", false, "render entry as TeX bibliography reference")
	flag.BoolVar(&cmdRenderAsHTML, "render_as_html", false, "render entry as HTML bibliography reference")
	flag.BoolVar(&cmdRenderAsText, "render_as_text", false, "render entry as plain-text bibliography reference")
	flag.BoolVar(&cmdCheckPdfs, "check_pdfs", false, "check PDF health, orphan files, and duplicates in the files folder")
flag.BoolVar(&cmdAlignBooktitleCountries, "align_booktitle_countries", false, "detect and fix unbraced country names in booktitle fields")
	flag.BoolVar(&cmdUpdateOrcidCache, "update_orcid", false, "refresh the ORCID disk cache for all known contributors (oldest-first, q+Enter to stop)")
	flag.BoolVar(&cmdLoadDblpXml, "load_dblp_xml", false, "load a DBLP .xml.gz export into the local DBLP file store")
	flag.BoolVar(&cmdUpdateDblp, "update_dblp", false, "download the latest DBLP XML export from dblp.uni-trier.de")
	flag.BoolVar(&cmdRepairDblpManifest, "repair_dblp_manifest", false, "rebuild DBLP manifest and title index from a .xml.gz export")
	flag.BoolVar(&cmdRebuildDblpCrossrefIndex, "rebuild_dblp_crossref_index", false, "rebuild DBLP crossref children index from stored data.json files")
	flag.BoolVar(&cmdRebuildDblpTitleIndex, "rebuild_dblp_title_index", false, "rebuild DBLP title index from stored data.json files (no XML needed; -base required for folder config)")
	flag.BoolVar(&cmdRestoreKeyHints, "restore_key_hints", false, "restore key hints from a backup CSV, remapping old keys via key_oldies")
	flag.StringVar(&restoreKeyHintsPath, "hints_csv", "", "path to the backup key_hints.csv for -restore_key_hints")
	flag.BoolVar(&cmdDeleteGarbage, "delete_garbage", false, "delete DBLP trash folder contents and exit")
	flag.BoolVar(&cmdRestoreFromDump, "restore_from_dump", false, "restore home database from $base.dump (use after corruption to rebuild from SQL dump)")
	flag.StringVar(&cmdRestoreBackupName, "restore", "", "restore home database from a named backup (e.g. -restore ErikProper_20260709_123456)")
	flag.BoolVar(&cmdNoGarbageCleaning, "no_garbage_cleaning", false, "skip background cleanup of the DBLP trash folder")
	flag.BoolVar(&cmdApplyScript, "do_entry_actions", false, "evaluate group assignment rules from <base>.scripts/entry_actions")
	// Unified table export / import (v23.0)
	flag.Var(&cmdExport, "export", "export tables to <base>.tables/ (bare = all; or comma-separated table names)")
	flag.Var(&cmdImport, "import", "import tables from <base>.tables/, replace-all with confirmation (bare = all; or comma-separated table names)")
	flag.StringVar(&cmdSetSyncStatus, "set_sync_status", "", "set/clear a sync status flag: -set_sync_status <status|''> <source_key> <stem>")
	flag.BoolVar(&cmdImportAllCSV, "import_all_csv", false, "import all mapping CSVs (migration helper for migrate.sh)")
	flag.BoolVar(&cmdImportBib, "import_bib", false, "import a bib file into the DB (requires filename argument; use to initialise or reinitialise bib_entries)")
	flag.BoolVar(&cmdHarvest, "harvest", false, "interactively ingest entries from a bib file (path from args) or stdin into the library")

	flag.Parse()
	args := flag.Args()

	// Go's flag parser stops at the first non-flag argument, so modifier flags
	// that appear after positional args (e.g. -entry_key_alias key -map) are
	// not parsed. Rescan args for known modifiers and strip them out.
	{
		var filtered []string
		for _, a := range args {
			switch a {
			case "-map", "--map":
				cmdMap = true
			case "-use_aliases", "--use_aliases":
				cmdUseAliases = true
			default:
				filtered = append(filtered, a)
			}
		}
		args = filtered
	}

	if cmdVersion {
		os.Exit(0)
	}

	// -set_sync_status operates on an explicit .sync file; no -base needed.
	if cmdSetSyncStatus != "" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: bibtex_check -set_sync_status <status|''> <source_key> <sync-file-stem>")
			os.Exit(1)
		}
		DoSetSyncStatus(cmdSetSyncStatus, args[0], args[1])
		os.Exit(0)
	}

	if *baseFlag == "" {
		fmt.Fprintln(os.Stderr, "Usage: bibtex_check -base <path/basename> [-command] [args...]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	if absBase, err := filepath.Abs(*baseFlag); err == nil {
		*baseFlag = stripKnownBaseExtension(absBase)
	}
	bibTeXFolder = filepath.Dir(*baseFlag) + "/"
	bibTeXBaseName = filepath.Base(*baseFlag)
	BibFile = bibTeXBaseName + BibFileExtension

	Reporting = TInteraction{}

	loadBibTeXFolders(bibTeXFolder + bibTeXBaseName + FoldersFileExtension)
	if backupFolder == "" {
		backupFolder = bibTeXFolder + bibTeXBaseName + ".backups/"
	}
	if globalFolder == "" {
		globalFolder = bibTeXFolder
	}
	loadUnicodeMap(globalFolder + "unicode_map.csv")
	loadHtmlCommandsMap(globalFolder + "html_commands_map.csv")
	loadHtmlCharacterMap(globalFolder + "html_character_map.csv")
	loadLatexIndexerMap(globalFolder + "latex_indexer.csv")

	if cmdNewKey {
		doNewKey()
		os.Exit(0)
	}

	// Acquired here, before cmdUpdateDblp, rather than further down: -update_dblp
	// does a slow (multi-minute) download+import and does not os.Exit() afterward —
	// it falls through into the normal session below. acquireDblpLock alone does
	// not stop a second, unrelated invocation (e.g. -watch) from acquiring this same
	// instance lock and running a full session against the same working DB while
	// the download is still in progress; when -update_dblp later reaches its own
	// DB work, both processes can be writing at once, producing SQLITE_BUSY_SNAPSHOT
	// ("database is locked (517)") on whichever write loses the race.
	acquireInstanceLock()

	if cmdUpdateDblp {
		acquireDblpLock()
		maybeStartDblpTrashCleanup()
		doUpdateDblp()
	}

	if cmdLoadDblpXml {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -load_dblp_xml <path.xml.gz>")
			os.Exit(1)
		}
		acquireDblpLock()
		maybeStartDblpTrashCleanup()
		doLoadDblpXml(args)
		os.Exit(0)
	}

	if cmdRepairDblpManifest {
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -repair_dblp_manifest <path.xml.gz>")
			os.Exit(1)
		}
		maybeStartDblpTrashCleanup()
		doRepairDblpManifest(args)
		os.Exit(0)
	}

	if cmdRebuildDblpCrossrefIndex {
		doRebuildDblpCrossrefIndex()
		os.Exit(0)
	}

	if cmdRebuildDblpTitleIndex {
		doRebuildDblpTitleIndex()
		os.Exit(0)
	}

	if cmdDeleteGarbage {
		des, err := os.ReadDir(dblpTrashFolder())
		if err != nil || len(des) == 0 {
			fmt.Fprintf(os.Stderr, "No DBLP trash to delete.\n")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Deleting DBLP trash...\n")
		start := time.Now()
		if err := os.RemoveAll(dblpTrashFolder()); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting trash: %s\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Done (%.0fs).\n", time.Since(start).Seconds())
		os.Exit(0)
	}

	if cmdRestoreFromDump {
		doRestoreFromDump()
		os.Exit(0)
	}

	if cmdRestoreBackupName != "" {
		doRestoreFromBackup(cmdRestoreBackupName)
		os.Exit(0)
	}

	maybeMigrateDbFile()
	maybeMigrateDblpFolder()
	maybeMigrateDblpNameFiles()
	maybeMigrateTablesFolder()
	maybeMigrateScriptFile()
	maybeMigrateToHomePath()
	connectToDatabase()

	if !cmdSync && !cmdFindEntries && !cmdEntryKey && !cmdEntryKeyAlias && !cmdShowEntry {
		maybeStartDblpTrashCleanup()
	}

	switch {
	case cmdSync:
		filter := ""
		if len(args) > 0 {
			filter = args[0]
		}
		doSync(filter)

	case cmdGetPdfs:
		doGetPdfs()

	case cmdFindEntries:
		if len(args) == 0 || len(args) > 2 {
			fmt.Fprintln(os.Stderr, "Usage: -find_entries <field> [<value>]")
			os.Exit(1)
		}
		doFindEntries(args)

	case cmdEntryKey:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -entry_key <alias>")
			os.Exit(1)
		}
		doEntryKey(args)

	case cmdEntryKeyAlias:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -entry_key_alias <key>")
			os.Exit(1)
		}
		doEntryKeyAlias(args, cmdMap)

	case cmdMap:
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: -map <alias>... <canonical>")
			os.Exit(1)
		}
		doAddKeyMapping(args)

	case cmdShowEntry:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -show_entry <key>")
			os.Exit(1)
		}
		doShowEntry(args)

	case cmdFixEntries:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -fix_entries <key>...")
			os.Exit(1)
		}
		doFixEntries(args)

	case cmdFixDuplicates:
		doFixDuplicates()

	case cmdTriageAuthorMappings:
		doTriageAuthorMappings()

	case cmdTriageContributorAliases:
		doTriageContributorAliases()

	case cmdDisambiguateContributors:
		doDisambiguateContributors()

	case cmdUpgradeWatchActions:
		doUpgradeWatchActions()

	case cmdAssignOrcid:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -assign_orcid <name-or-EP-id> <orcid>")
			os.Exit(1)
		}
		doAssignOrcid(args)

	case cmdUpdateOrcidCache:
		doUpdateOrcidCache()

	case cmdEnrichContributorData:
		doEnrichContributorData()

	case cmdMergeContributors:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -merge_contributors <into> <from>")
			os.Exit(1)
		}
		doMergeContributors(args)

	case cmdRestoreKeyHints:
		doRestoreKeyHints(restoreKeyHintsPath)

	case cmdUpdateDblp:
		doUpsertDblpEntries()
		if dbWriteSessionActive {
			runWatch()
			runScript()
		}

	case cmdFixCandidates:
		doFixCandidates()

	case cmdAddDblpEntries:
		requireNoDblpImport()
		doUpsertDblpEntries()

	case cmdAddDblpEntry:
		requireNoDblpImport()
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -add_dblp_entry <key>...")
			os.Exit(1)
		}
		// Route each argument: DBLP keys (containing "/") go to doAddDblpEntry;
		// library keys go to doFixDblpFor.
		var libKeys, dblpKeys []string
		for _, arg := range args {
			if strings.Contains(arg, "/") {
				dblpKeys = append(dblpKeys, arg)
			} else {
				libKeys = append(libKeys, arg)
			}
		}
		if len(libKeys) > 0 {
			doUpsertDblpFor(libKeys)
		}
		if len(dblpKeys) > 0 {
			doUpsertDblpEntryFromDblpKeys(dblpKeys)
		}

case cmdWatch:
		requireNoDblpImport()
		doWatch()

	case cmdAddKeyMapping:
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: -add_key_mapping <alias>... <canonical>")
			os.Exit(1)
		}
		doAddKeyMapping(args)

	case cmdMergeEntries:
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: -merge_entries <key>... <target>")
			os.Exit(1)
		}
		doMergeEntries(args)

	case cmdAddNameMapping:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -add_name_mapping <canonical> <alias>")
			os.Exit(1)
		}
		doAddNameMapping(args)

	case cmdCorrectName:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -correct_name <bad_name> <correct_name>")
			os.Exit(1)
		}
		doCorrectName(args)

	case cmdAddDoiAlias:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -add_doi_alias <entry_key> <doi>")
			os.Exit(1)
		}
		if !openLibraryToUpdate() {
			os.Exit(1)
		}
		addEntryDoiAlias(args[0], args[1])

	case cmdDeleteEntry:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -delete_entry <key>...")
			os.Exit(1)
		}
		if !openLibraryToUpdate() {
			os.Exit(1)
		}
		for _, rawKey := range args {
			key := Library.MapEntryKey(rawKey)
			if key == "" {
				key = rawKey
			}
			if !Library.EntryExists(key) {
				fmt.Fprintf(os.Stderr, "Entry not found: %s\n", rawKey)
				continue
			}
			fmt.Fprintf(os.Stderr, "\nEntry to delete:\n%s\n", Library.entryDisplayString(key))
			if !Library.ConfirmAction(fmt.Sprintf("Delete %s?", key)) {
				continue
			}
			Library.DeleteEntry(key)
			Library.Progress("Deleted: %s", key)
		}

	case cmdSetPreferredAlias:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -set_preferred_alias <key> <alias>")
			os.Exit(1)
		}
		doSetPreferredAlias(args)

	case cmdSetField:
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprintln(os.Stderr, "Usage: -set_field <key> <field> [<value>]")
			os.Exit(1)
		}
		doSetField(args)

	case cmdAddToGroup:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -add_to_group <key> <group>")
			os.Exit(1)
		}
		doAddToGroup(args)

	case cmdRemoveFromGroup:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -remove_from_group <key> <group>")
			os.Exit(1)
		}
		doRemoveFromGroup(args)

	case cmdSetGroups:
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: -set_groups <key> [+] <group>...")
			os.Exit(1)
		}
		doSetGroups(args)

	case cmdRenderGroup:
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "Usage: -render_group [-use_aliases] <group> <pubs_folder> <citations_folder>")
			os.Exit(1)
		}
		doRenderGroup(args, cmdUseAliases)

	case cmdListGroupAliases:
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "Usage: -list_group_aliases <group>")
			os.Exit(1)
		}
		doListGroupAliases(args)

	case cmdRenderAsBibTeX:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -render_as_bibtex <key>")
			os.Exit(1)
		}
		doRenderAsBibTeX(args)

	case cmdRenderAsTex:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -render_as_tex <key>")
			os.Exit(1)
		}
		doRenderAsTex(args)

	case cmdRenderAsHTML:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -render_as_html <key>")
			os.Exit(1)
		}
		doRenderAsHTML(args)

	case cmdRenderAsText:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -render_as_text <key>")
			os.Exit(1)
		}
		doRenderAsText(args)

	case cmdCheckPdfs:
		doCheckPDFs()

case cmdAlignBooktitleCountries:
		if openLibraryToUpdate() {
			Library.CheckAlignBooktitleCountries()
		}

	case cmdApplyScript:
		doApplyScript()

	case cmdExport != "":
		// IsBoolFlag=true means "-export table" parses as bare "-export" with "table" in args.
		// When the spec is the default "all" and positional args are present, treat the first
		// arg as the table spec so "-export bib_entries" works alongside "-export=bib_entries".
		exportSpec := string(cmdExport)
		if exportSpec == "all" && len(args) > 0 {
			exportSpec = strings.Join(args, ",")
		}
		if openLibraryToReport() {
			ExportTables(exportSpec)
		}

	case cmdImport != "":
		importSpec := string(cmdImport)
		if importSpec == "all" && len(args) > 0 {
			importSpec = strings.Join(args, ",")
		}
		skipStartupChecks = true
		if openLibraryToUpdate() {
			ImportTables(importSpec, &Library)
		}

	case cmdImportAllCSV:
		// Does not require ValidBibDb: mapping tables are imported independently of bib entries.
		if prepareWorkingDatabase() {
			maybeMigrateTableConstraints()
			if ImportAllCSVExchangeFiles() {
				dbInteraction.Progress("All CSV exchange files imported successfully.")
			}
		}

	case cmdImportBib:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -import_bib <file.bib>")
			os.Exit(1)
		}
		doImportBib(args[0])

	case cmdHarvest:
		path := ""
		if len(args) > 0 {
			path = args[0]
		}
		doHarvest(path)

	default:
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "Unexpected arguments (did you forget a command flag?):", args)
			os.Exit(1)
		}
		doDefaultRun()
	}

	Library.CheckDblpKeyMissingWarnings()

	// DB-primary (step 13.6 + 22.7): the bib file is never written during a normal run.
	// Bib file writes happen only via -sync (full mode). All entry changes are already
	// persisted to the working DB by setEntryField; finaliseWorkingDatabase flushes them home.
	if (forceWrite || bibEntriesModified) && !skipBibDbRefresh {
		clearTableDirty("bib_entries")
		refreshBibDbTimestamp()
	}

	saveKeyNonDoublesToDb(&Library)

	if !postCheckGate() {
		dbInteraction.Warning("Post-check gate failed — home database not updated")
		abandonWorkingDatabase()
	} else {
		finaliseWorkingDatabase()
	}
}

// gracefulQuit runs the normal post-check and DB finalisation sequence, then
// exits with code 0. Called when the user presses 'q' during interactive name
// resolution so that changes made earlier in the run are not lost.
func gracefulQuit() {
	forceCommitBibTransaction()
	saveKeyNonDoublesToDb(&Library)

	if !postCheckGate() {
		dbInteraction.Warning("Post-check gate failed — home database not updated")
		abandonWorkingDatabase()
	} else {
		finaliseWorkingDatabase()
	}
	os.Exit(0)
}
