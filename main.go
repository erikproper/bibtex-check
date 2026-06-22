package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// stepFlag implements flag.Value for -step [N].
// -step alone means step size 1; -step=N or -step N means step size N.
type stepFlag int

func (f *stepFlag) String() string   { return strconv.Itoa(int(*f)) }
func (f *stepFlag) IsBoolFlag() bool { return true }
func (f *stepFlag) Set(s string) error {
	if s == "true" {
		*f = 1
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return fmt.Errorf("step: expected a positive integer, got %q", s)
	}
	*f = stepFlag(n)
	return nil
}

var (
	Library   TBibTeXLibrary
	Reporting TInteraction
)

const AppVersion = "26.1"

// Run-state flags consumed by the write tail in main.
var (
	skipBibDbRefresh      bool
	skipBibValidation     bool
	skipStartupChecks     bool // set by -import so the import runs before consistency checks
	forceWrite            bool
	cmdStep               stepFlag
	cmdNoGarbageCleaning  bool
	cmdTrustHints              bool   // -trust_hints: auto-accept key-hint matches in harvest
	cmdCollectKeys             bool   // -collect_keys: add source keys to hints DB when unambiguous
	cmdHarvestGroup            string         // -group: add all resolved harvest entries to this group
	cmdHarvestTransferKeysPath string         // resolved .keys path for harvest_transfer target; "" = disabled
	cmdHarvestWeaveEntries     []TBibTeXEntry // ignored entries accumulated during this harvest run; flushed to follow .sync DB
	cmdFix                bool // -fix: apply full per-entry checks when combined with -sync or -harvest
	cmdPull               bool // -pull: with -sync, skip up-sync (phase 1); only write bib output from DB
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
		if Library.EntryFieldValueity(source, DBLPField) != "" || Library.EntryFieldValueity(target, DBLPField) != "" {
			return
		}
		maybeFindDBLPCandidates(source)
		// If source just acquired a DBLP key, exclude it from target's search so the
		// same candidate is not offered twice in one PreMergeCheck call.
		excl := TStringSetNew()
		if d := Library.EntryFieldValueity(Library.MapEntryKey(source), DBLPField); d != "" {
			excl.Add(normalizeDblpKey(d))
		}
		maybeFindDBLPCandidatesExcluding(target, excl)
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
	initEntryCache()
	reportCacheMode()
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
	maybeMigrateToLosingFieldValues()
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
	clearBibTables()
	beginBibTransaction()
	if !Library.ParseBibFile(path) {
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
	clearBibTables()
	beginBibTransaction()
	if !Library.ReadBib(BibFile) {
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

func openLibraryToUpdate() bool {
	if !prepareWorkingDatabase() {
		return false
	}
	maybeMigrateTableConstraints()
	maybeMigrateToLosingFieldValues()
	maybeMigrateStripLocalURL()
	preCheckRepair()
	maybeMigrateToFKSchema()
	initialiseLibrary()
	Library.ReadKeyOldiesFile()
	loadMappingFiles()

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

	Library.LoadPDFFiles()
	Library.ReportLibrarySize()
	if !skipStartupChecks {
		Library.CheckDblpDuplicates()
		Library.CheckKeyOldiesConsistency()
		Library.CheckKeyHintsConsistency()
		Library.CheckDblpWaivedConsistency()
		Library.CheckEntryFieldMappingWinners()
	}
	return true
}

func openLibraryToReport() bool {
	initialiseLibrary()
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
		if Library.NonDoubles[mappedKey].Set().Contains(peer) {
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
	existing := Library.NonDoubles[key]
	entryYear, hasYear := dblpFilterYear(key)
	var candidates []string
	var yearFiltered []string
	for _, c := range allCandidates {
		if existing.Contains(KeyForDBLP(c)) {
			continue
		}
		if extraExclusions.Contains(c) {
			Library.AddNonDoubles(key, KeyForDBLP(c))
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
			Library.AddNonDoubles(key, KeyForDBLP(c))
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
			Library.AddNonDoubles(key, KeyForDBLP(c))
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

// --- Command functions ---

// doTriageAuthorMappings interactively reviews all author/editor pairs in losing_field_values.
// Each pair is classified and resolved: brace-wrap (auto-fixed), name-equal-after-mapping
// (auto-retired), single-name variant (name-mapping offered), missing-authors (prompt),
// multi-name variant (kept), or unclassifiable (flagged).
func doTriageAuthorMappings() {
	if !openLibraryToUpdate() {
		return
	}
	defer reportHomework()

	rows, err := db.Query(`SELECT entry_key, field, value FROM losing_field_values WHERE field IN ('author', 'editor') AND triage_status IS NULL ORDER BY entry_key, field`)
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
		db.Exec(`DELETE FROM losing_field_values WHERE entry_key=? AND field=? AND value=?`, key, field, loser) //nolint:errcheck
	}

	markKept := func(key, field, loser string) {
		db.Exec(`UPDATE losing_field_values SET triage_status='kept' WHERE entry_key=? AND field=? AND value=?`, key, field, loser) //nolint:errcheck
	}

	splitOnAnd := func(value string) []string {
		parts := strings.Split(value, " and ")
		for i, p := range parts {
			parts[i] = strings.TrimSpace(p)
		}
		return parts
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

	stepN := int(cmdStep)
	questionCounter := 0
outer:
	for _, p := range pairs {
		winner := Library.EntryFieldValueity(p.key, p.field)
		if winner == "" {
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
				options := TStringSetNew()
				options.Add("w", "l", "s", "q")
				answer := Library.WarningQuestion(
					"Map to winner-canonical (w), loser-canonical (l), skip (s), quit (q)?",
					options,
					"Entry: %s / field: %s\n  Winner: %s\n  Loser:  %s\n  Pos %d: winner=%q loser=%q",
					p.key, p.field, winner, p.loser, pos+1, wNames[pos], lNames[pos])
				switch answer {
				case "w":
					Library.AddNameMapping(wNames[pos], lNames[pos])
					retireLoser(p.key, p.field, p.loser)
				case "l":
					Library.AddNameMapping(lNames[pos], wNames[pos])
					retireLoser(p.key, p.field, p.loser)
				case "q":
					break outer
				}
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
				Library.Warning("Unclassifiable pair for %s %s — flag for manual review\n  Winner: %s\n  Loser:  %s",
					p.key, p.field, winner, p.loser)
			}
		}

		if stepN > 0 && Library.QuestionWasAsked() {
			questionCounter++
			if questionCounter >= stepN {
				if Library.AskContinueOrQuit() {
					break outer
				}
				questionCounter = 0
			}
		}
	}
}

// reportHomework prints a summary of remaining work:
//   - number of title-hash groups that contain at least one unresolved potential
//     duplicate pair (title-equal, not in non_doubles, no divergent DBLP/DOI evidence)
//   - number of entries without a dblp field that have at least one unresolved DBLP
//     title-index candidate (not already in non_doubles)
func reportHomework() {
	// A title group is unresolved when it contains at least two canonical keys
	// whose pair has neither a non_doubles declaration nor field-value evidence
	// of being different entries (divergent DBLP key or DOI).
	unresolvedGroups := 0
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
				if Library.NonDoubles[a].Set().Contains(b) {
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
			unresolvedGroups++
		}
	}

	// Entries with at least one unresolved DBLP candidate not already in non-doubles.
	// Year-range filtering is skipped here (it requires disk I/O per candidate); the
	// count may be a slight overestimate but avoids file reads on every run.
	dblpCandidates := 0
	forEachBibEntryKey(func(key string) bool {
		if Library.EntryFieldValueity(key, DBLPField) != "" {
			return true
		}
		hash := libraryTitleHash(Library.EntryFieldValueity(key, TitleField))
		if hash == "" {
			return true
		}
		existing := Library.NonDoubles[key]
		for _, c := range readDblpTitleLinks(hash) {
			if !existing.Set().Contains(KeyForDBLP(c)) {
				dblpCandidates++
				return true
			}
		}
		return true
	})

	// Lone proceedings: proceedings with no library crossref children, no DBLP children
	// in the file store, and no waive flag. Excludes DBLP-backed entries (they are safe).
	// Also excludes proceedings that have a PDF file — CheckLoneProceedings silently skips
	// those too, so they should not be counted as outstanding work.
	loneProceedings := 0
	crossrefTargets := TStringSetNew()
	forEachBibEntryKey(func(key string) bool {
		if cr := Library.EntryFieldValueity(key, "crossref"); cr != "" {
			crossrefTargets.Add(Library.MapEntryKey(cr))
		}
		return true
	})
	filesDir := Library.FilesRoot + Library.FilesFolder
	forEachBibEntryKey(func(key string) bool {
		if Library.EntryFieldValueity(key, EntryTypeField) != "proceedings" {
			return true
		}
		if crossrefTargets.Contains(key) {
			return true
		}
		if Library.EntryHasFlag(key, FlagLoneProceedingsWaived) {
			return true
		}
		if Library.EntryFieldValueity(key, DBLPField) != "" {
			return true // DBLP-backed: not spurious
		}
		if FileExists(filesDir + key + ".pdf") {
			return true // has PDF content — not a problem
		}
		loneProceedings++
		return true
	})

	// Entries with url but no urldate/doi/isbn/issn that haven't been URL-checked yet.
	urlUnchecked := 0
	forEachBibEntryKey(func(key string) bool {
		entry := Library.buildEntry(key)
		if !URLCheckNeeded(&Library, entry) {
			return true
		}
		if Library.GetMetadata(key, MetaPropUrlCheckStatus) == "" {
			urlUnchecked++
		}
		return true
	})

	authorEditorPairs := 0
	db.QueryRow(`SELECT COUNT(*) FROM losing_field_values WHERE field IN ('author', 'editor') AND triage_status IS NULL`).Scan(&authorEditorPairs)

	Library.Progress("Homework:\n  %d title group(s) with unresolved duplicate(s)\n  %d entry/ies with unresolved DBLP candidate(s)\n  %d lone proceedings\n  %d url(s) not yet checked\n  %d author/editor value(s) needing triage",
		unresolvedGroups, dblpCandidates, loneProceedings, urlUnchecked, authorEditorPairs)
}

// doFixCandidates interactively links library entries that have no DBLP key yet
// to DBLP records. Entries are processed per title-index bucket so that DBLP
// keys already claimed by one variation in the bucket are not offered to another
// (variation-aware exclusion). Within each bucket, entries that have no DBLP key
// are offered candidates; entries that already have one are skipped.
func doFixCandidates() {
	if openLibraryToUpdate() {
		defer reportHomework()
		stepN := Reporting.StepSize()
		questionCounter := 0
		entryCountAtStepStart := countBibEntries()
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
			if stepN > 0 && Library.QuestionWasAsked() {
				questionCounter++
				if questionCounter >= stepN {
					now := countBibEntries()
					if added := now - entryCountAtStepStart; added > 0 {
						Library.Progress("Step added %d new entr%s to the library.", added,
							map[bool]string{true: "y", false: "ies"}[added == 1])
					}
					if Library.AskContinueOrQuit() {
						break outer
					}
					entryCountAtStepStart = countBibEntries()
					questionCounter = 0
				}
			}
		}
	}
}

func doDefaultRun() {
	if openLibraryToUpdate() {
		defer reportHomework()
		clearEntryWarnings()
		Library.ReadURLsIgnoreFile()
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

func doRepairGarbledNames(repairBibPath string) {
	if !openLibraryToUpdate() {
		return
	}
	Library.Progress("Cleaning bad name mappings")
	n := Library.cleanBadNameMappings()
	Library.Progress("Cleaned %d bad name_mapping canonical(s)", n)

	var bibMap map[string]map[string]string
	if repairBibPath != "" {
		Library.Progress("Parsing repair bib: %s", repairBibPath)
		var err error
		bibMap, err = parseBibForAuthorEditor(repairBibPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not parse repair bib %s: %s\n", repairBibPath, err)
		} else {
			Library.Progress("Loaded %d entries from repair bib", len(bibMap))
		}
	}
	Library.Progress("Scanning entries for garbled names")
	n = Library.RepairGarbledNames(bibMap)
	Library.Progress("Repaired %d garbled author/editor field(s)", n)
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
			if Library.NonDoubles[a].Set().Contains(b) {
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
		stepN := int(cmdStep)
		questionCounter := 0
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
			if stepN > 0 && Library.QuestionWasAsked() {
				questionCounter++
				if questionCounter >= stepN {
					if Library.AskContinueOrQuit() {
						break outer
					}
					questionCounter = 0
				}
			}
		}
	}
}

func doUpsertDblpEntries() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		total := countBibEntries()

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

		quit := false
		scanned := 0
		stepN := int(cmdStep)
		questionCounter := 0
		spinner := Library.NewSpinner(ProgressFixingDblpEntries)
		beginBibTransaction()
		processKey := func(key string) {
			scanned++
			spinner.Update(scanned, total)
			if Library.EntryFieldValueity(key, DBLPField) != "" {
				Library.ResetQuestionFlag()
				doC1Checks(key)
				Library.MaybeFixDBLPEntry(key)
				doC3Checks(key)
				if stepN > 0 && Library.QuestionWasAsked() {
					questionCounter++
					if questionCounter >= stepN {
						if Library.AskContinueOrQuit() {
							quit = true
						}
						questionCounter = 0
					}
				}
			}
		}
		for _, key := range bookishKeys {
			if quit {
				break
			}
			processKey(key)
		}
		// Drain: fix any entries added during the bookish pass (children created by
		// CheckEntry). Loop until stable — grandchildren are non-bookish so depth is
		// bounded, but looping is safer than assuming exactly one level.
		for !quit {
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
				if quit {
					break
				}
				processKey(key)
			}
		}
		for _, key := range otherKeys {
			if quit {
				break
			}
			processKey(key)
		}
		commitBibTransaction()
		spinner.Stop()
		bibEntriesModified = true
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
	}
	return keys
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

	stepN := int(cmdStep)
	questionCounter := 0
	stopped := false

	for _, w := range entries {
		if stopped {
			break
		}
		keys := watchEntryDblpKeys(w)

		label := w.Tag
		if label == "" {
			label = w.Value
		}

		if len(keys) == 0 {
			Library.Warning("No DBLP entries found for %s %q — person may not be in file store", w.EntryType, w.Value)
			continue
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

			if stepN > 0 && Library.QuestionWasAsked() {
				questionCounter++
				if questionCounter >= stepN {
					if Library.AskContinueOrQuit() {
						stopped = true
						break
					}
					questionCounter = 0
				}
			}
		}

		if newCount == 0 {
			Library.Progress("Watching %s: all publications present (%d total)", label, len(keys))
		} else {
			Library.Progress("Watching %s: %d missing publication(s) checked", label, newCount)
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

func doAddNameMapping(args []string) {
	if openLibraryToUpdate() {
		Library.AddNameMapping(args[0], args[1])
		Library.RenormaliseNameFields()
	}
}

func main() {
	fmt.Fprintf(os.Stderr, "%s %s\n", filepath.Base(os.Args[0]), AppVersion)

	baseFlag := flag.String("base", "", "path/basename of the library (required)")
	flag.BoolVar(&forceWrite, "force_write", false, "force write even if unchanged")

	flag.Var(&cmdStep, "step", "pause every N entries in for-all loops (default N=1 when flag is given)")
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
		cmdTriageAuthorMappings bool // -triage_author_mappings: triage author/editor losing_field_values
		cmdAddDblpEntry   bool
		cmdAddDblpEntries bool
		cmdWatch             bool
		cmdAddKeyMapping         bool
		cmdMergeEntries       bool
		cmdAddNameMapping     bool
		cmdSetPreferredAlias  bool
		cmdNewKey             bool
		cmdDeleteEntry        bool
		cmdMap                bool
		cmdUseAliases         bool
		cmdAddToGroup         bool
		cmdRemoveFromGroup    bool
		cmdSetField           bool
		cmdRenderAsBibTeX     bool
		cmdRenderGroup        bool
		cmdListGroupAliases   bool
		cmdRenderAsTex        bool
		cmdRenderAsHTML       bool
		cmdRenderAsText       bool
		cmdCheckPdfs                bool
		cmdAlignBooktitleCountries  bool
		cmdLoadDblpXml              bool
		cmdUpdateDblp               bool
		cmdRepairDblpManifest       bool
		cmdRebuildDblpCrossrefIndex bool
		cmdRebuildDblpTitleIndex    bool
		cmdRepairGarbledNames   bool
		repairBibPath           string
		cmdRestoreKeyHints      bool
		restoreKeyHintsPath     string
		cmdDeleteGarbage            bool
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
	flag.BoolVar(&cmdSetPreferredAlias, "set_preferred_alias", false, "set preferred alias for a key: -set_preferred_alias <key> <alias>")
	flag.BoolVar(&cmdNewKey, "new_key", false, "print a fresh canonical key and exit")
	flag.BoolVar(&cmdDeleteEntry, "delete_entry", false, "delete one or more library entries: -delete_entry <key>...")
	flag.BoolVar(&cmdMap, "map", false, "also record alias→key in the project map file (with -entry_key_alias)")
	flag.BoolVar(&cmdAddToGroup, "add_to_group", false, "add an entry to a group: -add_to_group <key> <group>")
	flag.BoolVar(&cmdRemoveFromGroup, "remove_from_group", false, "remove an entry from a group")
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
	flag.BoolVar(&cmdLoadDblpXml, "load_dblp_xml", false, "load a DBLP .xml.gz export into the local DBLP file store")
	flag.BoolVar(&cmdUpdateDblp, "update_dblp", false, "download the latest DBLP XML export from dblp.uni-trier.de")
	flag.BoolVar(&cmdRepairDblpManifest, "repair_dblp_manifest", false, "rebuild DBLP manifest and title index from a .xml.gz export")
	flag.BoolVar(&cmdRebuildDblpCrossrefIndex, "rebuild_dblp_crossref_index", false, "rebuild DBLP crossref children index from stored data.json files")
	flag.BoolVar(&cmdRebuildDblpTitleIndex, "rebuild_dblp_title_index", false, "rebuild DBLP title index from stored data.json files (no XML needed; -base required for folder config)")
	flag.BoolVar(&cmdRepairGarbledNames, "repair_garbled_names", false, "clean bad name_mappings and repair garbled author/editor fields")
	flag.StringVar(&repairBibPath, "repair_bib", "", "path to a reference .bib file for -repair_garbled_names (non-DBLP entries)")
	flag.BoolVar(&cmdRestoreKeyHints, "restore_key_hints", false, "restore key hints from a backup CSV, remapping old keys via key_oldies")
	flag.StringVar(&restoreKeyHintsPath, "hints_csv", "", "path to the backup key_hints.csv for -restore_key_hints")
	flag.BoolVar(&cmdDeleteGarbage, "delete_garbage", false, "delete DBLP trash folder contents and exit")
	flag.BoolVar(&cmdNoGarbageCleaning, "no_garbage_cleaning", false, "skip background cleanup of the DBLP trash folder")
	flag.BoolVar(&cmdApplyScript, "do_entry_actions", false, "evaluate group assignment rules from <base>.scripts/entry_actions")
	// Unified table export / import (v23.0)
	flag.Var(&cmdExport, "export", "export tables to <base>.tables/ (bare = all; or comma-separated table names)")
	flag.Var(&cmdImport, "import", "import tables from <base>.tables/, replace-all with confirmation (bare = all; or comma-separated table names)")
	flag.StringVar(&cmdSetSyncStatus, "set_sync_status", "", "set/clear a sync status flag: -set_sync_status <status|''> <source_key> <stem>")
	flag.BoolVar(&cmdImportAllCSV, "import_all_csv", false, "import all mapping CSVs (migration helper for migrate.sh)")
	flag.BoolVar(&cmdImportBib, "import_bib", false, "import a bib file into the DB (requires filename argument; use to initialise or reinitialise bib_entries)")
	flag.BoolVar(&cmdHarvest, "harvest", false, "interactively ingest entries from a bib file (path from args) or stdin into the library")


	// Normalise "-step N" (space-separated) to "-step=N" before flag.Parse so
	// that IsBoolFlag-style parsing handles "-step" (no value) correctly too.
	for i := 1; i < len(os.Args)-1; i++ {
		if os.Args[i] == "-step" || os.Args[i] == "--step" {
			if _, err := strconv.Atoi(os.Args[i+1]); err == nil {
				os.Args[i] = os.Args[i] + "=" + os.Args[i+1]
				os.Args = append(os.Args[:i+1], os.Args[i+2:]...)
				break
			}
		}
	}
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
	Reporting.SetStepSize(int(cmdStep))

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

	acquireInstanceLock()

	maybeMigrateDbFile()
	maybeMigrateDblpFolder()
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

	case cmdRestoreKeyHints:
		doRestoreKeyHints(restoreKeyHintsPath)

	case cmdRepairGarbledNames:
		doRepairGarbledNames(repairBibPath)

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
			maybeMigrateToLosingFieldValues()
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

	if !postCheckGate() {
		dbInteraction.Warning("Post-check gate failed — home database not updated")
		abandonWorkingDatabase()
	} else {
		finaliseWorkingDatabase()
	}
}
