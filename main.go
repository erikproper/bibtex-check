package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

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

const AppVersion = "20.29"

// Run-state flags consumed by the write tail in main.
var (
	skipBibDbRefresh      bool
	skipBibValidation     bool
	forceWrite            bool
	cmdStep               stepFlag
	cmdNoGarbageCleaning  bool
	cmdAutoFixAlignTitles bool
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

	nameMappingsRepaired, _ := repairDirtyMappingTables()

	// If name_mappings was repaired, the normalisation pass for cross-field and
	// generic-field mappings may have used stale aliases — force a fresh reload.
	if nameMappingsRepaired {
		setTableDate("filter_cross_field_mappings", 0)
		setTableDate("filter_generic_field_mappings", 0)
	}
}

func loadMappingFiles() {
	Library.ReadAddressMappings()
	Library.CheckAddressMappings()
	Library.ReadKeyHintsFile()
	Library.ReadNameMappingsFile()
	Library.ReadGenericFieldMappingsFile()
	Library.ReadEntryFieldMappingsFile()
	Library.ReadCrossFieldMappingsFile()
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

func parseBibIntoDb() bool {
	Library.Progress(ProgressReparsingBibFile)
	safeOk := beginSafeParse()
	if !safeOk {
		Library.Warning("Proceeding without safe-parse backup; database not protected during reparse.")
	}

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
	initialiseLibrary()
	Library.ReadKeyOldiesFile()
	loadMappingFiles()

	if skipBibValidation || Library.ValidBibDb() {
		buildTitleIndexFromDb(&Library)
		loadBibFromDb()
		if skipBibValidation {
			Library.WriteBibTeXFile()
			clearTableDirty("bib_entries")
			refreshBibDbTimestamp()
			skipBibValidation = false
		}
	} else if !parseBibIntoDb() {
		return false
	}

	Library.ReportLibrarySize()
	Library.CheckKeyOldiesConsistency()
	Library.CheckEntryFieldMappingWinners()
	return true
}

func openLibraryToReport() bool {
	initialiseLibrary()
	Library.ReadKeyOldiesFile()

	if skipBibValidation || Library.ValidBibDb() {
		loadBibFromDb()
		if skipBibValidation {
			Library.WriteBibTeXFile()
			clearTableDirty("bib_entries")
			refreshBibDbTimestamp()
			skipBibValidation = false
		}
	} else {
		loadMappingFiles()
		if !parseBibIntoDb() {
			return false
		}
	}

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
// caller can re-run DBLP checks for the now-associated entry.
func maybeFindDBLPCandidates(key string) bool {
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
	yearStr := Library.EntryFieldValueity(key, "year")
	if yearStr == "" {
		if crossref := Library.EntryFieldValueity(key, "crossref"); crossref != "" {
			yearStr = Library.EntryFieldValueity(crossref, "year")
		}
	}
	entryYear, entryHasYear := strconv.Atoi(yearStr)
	var candidates []string
	for _, c := range allCandidates {
		if existing.Contains(KeyForDBLP(c)) {
			continue
		}
		if entryHasYear == nil {
			if ce := dblpEntryFromFile(c); ce != nil {
				if candYear, err := strconv.Atoi(ce.Fields["year"]); err == nil {
					diff := candYear - entryYear
					if diff < -3 || diff > 3 {
						continue
					}
				}
			}
		}
		candidates = append(candidates, c)
	}
	if len(candidates) == 0 {
		return false
	}
	if len(candidates) > 9 {
		candidates = candidates[:9]
	}
	chosen := Library.AskCandidateDblpKey(key, candidates)
	if chosen == "" {
		for _, c := range candidates {
			Library.AddNonDoubles(key, KeyForDBLP(c))
		}
		return false
	}
	Library.AssociateDblpKey(key, chosen)
	return true
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

// reportHomework prints a summary of remaining work:
//   - number of unresolved potential duplicate pairs (title-equal, not in non_doubles,
//     no divergent DBLP/DOI evidence)
//   - number of entries without a dblp field that have at least one unresolved DBLP
//     title-index candidate (not already in non_doubles)
func reportHomework() {
	// Unresolved potential duplicate pairs.
	type pair struct{ a, b string }
	counted := map[pair]bool{}
	unresolvedPairs := 0
	for _, keys := range Library.TitleIndex {
		sorted := keys.ElementsSorted()
		for i, a := range sorted {
			if a != Library.MapEntryKey(a) {
				continue
			}
			for _, b := range sorted[i+1:] {
				if b != Library.MapEntryKey(b) {
					continue
				}
				if a == b {
					continue
				}
				p := pair{a, b}
				if counted[p] {
					continue
				}
				counted[p] = true
				if Library.NonDoubles[a].Set().Contains(b) {
					continue
				}
				if Library.EvidenceForBeingDifferentEntries(a, b) {
					continue
				}
				unresolvedPairs++
			}
		}
	}

	// Entries with at least one unresolved DBLP candidate.
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

	Library.Progress("Homework: %d unresolved duplicate pair(s), %d entry/ies with unresolved DBLP candidate(s)", unresolvedPairs, dblpCandidates)
}

func doDefaultRun() {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		Library.CheckAlignTitles(false)
		reportHomework()
	}
}

func doCheckPDFs() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.CheckPDFHealth()
	}
}

func doGetPdfs() {
	if openLibraryToReport() {
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
		key := Library.MapEntryKey(cleanKey(args[0]))
		fmt.Print(Library.renderAsBibTeX(key))
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
			bib := Library.renderAsBibTeX(key)
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

			fileKey := key
			if useAliases {
				if alias := Library.PreferredKey(key); alias != "" {
					fileKey = alias
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
		key := Library.MapEntryKey(cleanKey(args[0]))
		fmt.Println(Library.renderAsTeX(key))
	}
}

func doRenderAsHTML(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		key := Library.MapEntryKey(cleanKey(args[0]))
		fmt.Println(Library.renderAsHTML(key))
	}
}

func doRenderAsText(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		key := Library.MapEntryKey(cleanKey(args[0]))
		fmt.Println(Library.renderAsText(key))
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

func doEntryKey(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.MapEntryKey(cleanKey(args[0])))
	}
}

func doEntryKeyAlias(args []string, withMap bool) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		rawKey := cleanKey(args[0])
		key := Library.MapEntryKey(rawKey)
		alias := Library.PreferredKey(key)
		if alias == "" && key != rawKey && validPreferredKeyAlias.MatchString(rawKey) {
			// Input is a valid preferred-alias that resolved to a canonical key;
			// use it directly so -map works before preferredalias is set on the entry.
			alias = rawKey
		}
		if alias != "" {
			fmt.Println(alias)
			if withMap {
				appendToMapFile(alias, key)
			}
		}
	}
}

func doShowEntry(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.EntryString(Library.MapEntryKey(cleanKey(args[0])), ""))
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

func doFixAllEntries() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		total := countBibEntries()
		count := 0
		stepN := int(cmdStep)
		questionCounter := 0
		forEachBibEntryKey(func(key string) bool {
			count++
			Library.Progress(ProgressEntryProgress, count, total, float64(count)*100/float64(total))
			Library.ResetQuestionFlag()
			doAllChecks(key)
			if stepN > 0 && Library.QuestionWasAsked() {
				questionCounter++
				if questionCounter >= stepN {
					if Library.AskContinueOrQuit() {
						return false
					}
					questionCounter = 0
				}
			}
			return true
		})
	}
}

func doFixDblpEntries() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.FixDblpHierarchy()
		if cmdAutoFixAlignTitles {
			Library.CheckAlignTitles(true)
		}
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

func doFixDblpFor(args []string) {
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


func doAddDblpEntry(args []string) {
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

// doWatchDblp reads watch.csv and for each watched person/ORCID checks whether
// all their publications are in the library.  Missing entries are displayed and
// the user is asked whether to add each one (same flow as -add_dblp_entry).
func doWatchDblp() {
	if !openLibraryToUpdate() {
		return
	}
	Library.ReadKeyNonDoublesFile()

	filePath := bibTeXFolder + bibTeXBaseName + WatchFilePath
	entries := ReadWatchFile(filePath)
	if len(entries) == 0 {
		Library.Progress("No watch entries found in %s", filePath)
		return
	}

	stepN := int(cmdStep)
	questionCounter := 0
	stopped := false

	for _, w := range entries {
		if stopped {
			break
		}
		var keys []string
		switch w.EntryType {
		case "name":
			if orcid := resolveNameToORCID(w.Value); orcid != "" {
				Library.Progress("Resolved %q to ORCID %s", w.Value, orcid)
				keys = readDblpORCIDEntries(orcid)
			} else {
				keys = readDblpPersonEntries(w.Value)
			}
		case "orcid":
			keys = readDblpORCIDEntries(w.Value)
		}

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
}

// doExtendDblpCoverage visits every entry without a dblp field and tries to
// associate a DBLP key in two stages:
//  1. Look for a title-equal library entry that already has a dblp field and
//     offer an interactive merge (inheriting the DBLP key via the merge).
//  2. If no intra-library peer is found, search the external DBLP title index
//     and offer the user numbered candidates.
//
// Only when a DBLP key is successfully associated does the function run a full
// doAllChecks on the resulting entry to absorb any other equal entries.
func doExtendDblpCoverage() {
	if !openLibraryToUpdate() {
		return
	}
	Library.ReadKeyNonDoublesFile()
	Library.FixDblpHierarchy()
	stepN := int(cmdStep)
	questionCounter := 0
	forEachBibEntryKey(func(key string) bool {
		if Library.EntryFieldValueity(key, DBLPField) != "" {
			return true
		}
		Library.ResetQuestionFlag()
		found := findLibraryEqualWithDblp(key)
		if !found {
			found = maybeFindDBLPCandidates(key)
		}
		if found {
			doAllChecks(Library.MapEntryKey(key))
		}
		if stepN > 0 && Library.QuestionWasAsked() {
			questionCounter++
			if questionCounter >= stepN {
				if Library.AskContinueOrQuit() {
					return false
				}
				questionCounter = 0
			}
		}
		return true
	})
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
	canon := Library.MapEntryKey(rawKey)
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
		for _, rawKey := range keys {
			resolved := resolveOrImportKey(rawKey, &importedCount)
			if resolved == "" {
				fmt.Fprintf(os.Stderr, "Unknown key: %s\n", rawKey)
				return
			}
			resolvedKeys = append(resolvedKeys, resolved)
		}
		if importedCount > 1 {
			fmt.Fprintln(os.Stderr, "Error: -merge_entries accepts at most one not-yet-imported DBLP entry")
			return
		}
		target := resolvedKeys[len(resolvedKeys)-1]
		for _, alias := range resolvedKeys[:len(resolvedKeys)-1] {
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
		if target, inUse := Library.HintToKey[alias]; inUse && target != key {
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
		Library.ApplyScript(scriptPath)
	}
}

func doAddNameMapping(args []string) {
	Library = TBibTeXLibrary{}
	Library.Initialise(Reporting, bibTeXFolder, bibTeXBaseName)
	Library.ReadNameMappingsFile()
	Library.AddNameMapping(args[0], args[1])
	skipBibDbRefresh = true
}

func main() {
	fmt.Fprintf(os.Stderr, "%s %s\n", filepath.Base(os.Args[0]), AppVersion)

	baseFlag := flag.String("base", "", "path/basename of the library (required)")
	flag.BoolVar(&forceWrite, "force_write", false, "force write even if unchanged")

	flag.Var(&cmdStep, "step", "pause every N entries in for-all loops (default N=1 when flag is given)")

	var cmdVersion bool
	flag.BoolVar(&cmdVersion, "version", false, "print version and exit")
	flag.BoolVar(&cmdVersion, "v", false, "print version and exit")

	var (
		cmdGet                bool
		cmdGetPdfs            bool
		cmdFindEntries        bool
		cmdEntryKey           bool
		cmdEntryKeyAlias      bool
		cmdShowEntry          bool
		cmdFixEntries         bool
		cmdFixAllEntries      bool
		cmdFixDblpEntries     bool
		cmdFixDblpFor         bool
		cmdAddDblpEntry          bool
		cmdExtendDblpCoverage    bool
		cmdDblpWatch             bool
		cmdAddKeyMapping         bool
		cmdMergeEntries       bool
		cmdAddNameMapping     bool
		cmdSetPreferredAlias  bool
		cmdNewKey             bool
		cmdMap                bool
		cmdUseAliases         bool
		cmdAddToGroup         bool
		cmdRemoveFromGroup    bool
		cmdRenderAsBibTeX     bool
		cmdRenderGroup        bool
		cmdListGroupAliases   bool
		cmdRenderAsTex        bool
		cmdRenderAsHTML       bool
		cmdRenderAsText       bool
		cmdCheckPdfs        bool
		cmdLoadDblpXml        bool
		cmdUpdateDblp         bool
		cmdRepairDblpManifest        bool
		cmdRebuildDblpCrossrefIndex  bool
		cmdDeleteGarbage                bool
		cmdApplyScript bool
	)

	flag.BoolVar(&cmdGet, "get", false, "generate local .bib from bib.config + .map in CWD")
	flag.BoolVar(&cmdGetPdfs, "get_pdfs", false, "download missing PDFs into the files folder")
	flag.BoolVar(&cmdFindEntries, "find_entries", false, "list entries matching field [value] (key TAB value per line)")
	flag.BoolVar(&cmdEntryKey, "entry_key", false, "resolve alias to canonical key")
	flag.BoolVar(&cmdEntryKeyAlias, "entry_key_alias", false, "get preferred alias for a key")
	flag.BoolVar(&cmdShowEntry, "show_entry", false, "print full entry content")
	flag.BoolVar(&cmdFixEntries, "fix_entries", false, "fix/check specific entries")
	flag.BoolVar(&cmdFixEntries, "fix_entry", false, "alias for -fix_entries")
	flag.BoolVar(&cmdFixAllEntries, "fix_all_entries", false, "fix/check all entries")
	flag.BoolVar(&cmdFixDblpEntries, "fix_all_dblp_entries", false, "fix/check all entries that have a DBLP key")
	flag.BoolVar(&cmdFixDblpFor, "fix_dblp_entry", false, "fix/check specific entries with DBLP")
	flag.BoolVar(&cmdFixDblpFor, "fix_dblp_entries", false, "alias for -fix_dblp_entry")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entry", false, "add one or more new entries from DBLP")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entries", false, "alias for -add_dblp_entry")
	flag.BoolVar(&cmdExtendDblpCoverage, "extend_dblp_coverage", false, "interactively associate DBLP keys with entries that have none")
	flag.BoolVar(&cmdDblpWatch, "dblp_watch", false, "check watched persons/ORCIDs in watch.csv for missing publications")
	flag.BoolVar(&cmdAddKeyMapping, "add_key_mapping", false, "add key alias(es) to a canonical key")
	flag.BoolVar(&cmdAddKeyMapping, "add_key_mappings", false, "alias for -add_key_mapping")
	flag.BoolVar(&cmdMergeEntries, "merge_entries", false, "merge entries into target")
	flag.BoolVar(&cmdAddNameMapping, "add_name_mapping", false, "add a name alias mapping")
	flag.BoolVar(&cmdSetPreferredAlias, "set_preferred_alias", false, "set preferred alias for a key")
	flag.BoolVar(&cmdNewKey, "new_key", false, "print a fresh canonical key and exit")
	flag.BoolVar(&cmdMap, "map", false, "also record alias→key in the project map file (with -entry_key_alias)")
	flag.BoolVar(&cmdAddToGroup, "add_to_group", false, "add an entry to a group")
	flag.BoolVar(&cmdRemoveFromGroup, "remove_from_group", false, "remove an entry from a group")
	flag.BoolVar(&cmdRenderGroup, "render_group", false, "render all entries in a group to pubs/citations folders")
	flag.BoolVar(&cmdListGroupAliases, "list_group_aliases", false, "list canonical|alias pairs for all entries in a group")
	flag.BoolVar(&cmdUseAliases, "use_aliases", false, "use preferred aliases as file names in -render_group")
	flag.BoolVar(&cmdRenderAsBibTeX, "render_as_bibtex", false, "render entry as self-contained BibTeX")
	flag.BoolVar(&cmdRenderAsTex, "render_as_tex", false, "render entry as TeX bibliography reference")
	flag.BoolVar(&cmdRenderAsHTML, "render_as_html", false, "render entry as HTML bibliography reference")
	flag.BoolVar(&cmdRenderAsText, "render_as_text", false, "render entry as plain-text bibliography reference")
	flag.BoolVar(&cmdCheckPdfs, "check_pdfs", false, "check PDF health, orphan files, and duplicates in the files folder")
	flag.BoolVar(&cmdLoadDblpXml, "load_dblp_xml", false, "load a DBLP .xml.gz export into the local DBLP file store")
	flag.BoolVar(&cmdUpdateDblp, "update_dblp", false, "download the latest DBLP XML export from dblp.uni-trier.de")
	flag.BoolVar(&cmdRepairDblpManifest, "repair_dblp_manifest", false, "rebuild DBLP manifest and title index from a .xml.gz export")
	flag.BoolVar(&cmdRebuildDblpCrossrefIndex, "rebuild_dblp_crossref_index", false, "rebuild DBLP crossref children index from stored data.json files")
	flag.BoolVar(&cmdDeleteGarbage, "delete_garbage", false, "delete DBLP trash folder contents and exit")
	flag.BoolVar(&cmdNoGarbageCleaning, "no_garbage_cleaning", false, "skip background cleanup of the DBLP trash folder")
	flag.BoolVar(&cmdApplyScript, "apply_script", false, "evaluate group assignment rules from <base>.script")
flag.BoolVar(&cmdAutoFixAlignTitles, "fix", false, "auto-accept all title/volume/edition alignment suggestions (use with -fix_all_dblp_entries)")

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

	loadBibTeXConfig(bibTeXFolder + bibTeXBaseName + ConfigFileExtension)
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
	connectToDatabase()

	if !cmdGet && !cmdFindEntries && !cmdEntryKey && !cmdEntryKeyAlias && !cmdShowEntry {
		maybeStartDblpTrashCleanup()
	}

	switch {
	case cmdGet:
		Reporting.SetInteractionOff()
		if openLibraryToReport() {
			doGet()
		}

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

	case cmdFixAllEntries:
		doFixAllEntries()

	case cmdUpdateDblp:
		doFixDblpEntries()

	case cmdFixDblpEntries:
		requireNoDblpImport()
		doFixDblpEntries()

case cmdFixDblpFor:
		requireNoDblpImport()
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -fix_dblp_entry <key>...")
			os.Exit(1)
		}
		doFixDblpFor(args)

	case cmdAddDblpEntry:
		// Step 10.4.3: add proper read-vs-write DBLP locking here.
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -add_dblp_entry <dblp-key>...")
			os.Exit(1)
		}
		doAddDblpEntry(args)

	case cmdExtendDblpCoverage:
		requireNoDblpImport()
		doExtendDblpCoverage()

	case cmdDblpWatch:
		requireNoDblpImport()
		doWatchDblp()

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

	case cmdSetPreferredAlias:
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: -set_preferred_alias <key> <alias>")
			os.Exit(1)
		}
		doSetPreferredAlias(args)

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


	case cmdApplyScript:
		doApplyScript()

	default:
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "Unexpected arguments (did you forget a command flag?):", args)
			os.Exit(1)
		}
		doDefaultRun()
	}

	Library.CheckDblpKeyMissingWarnings()

	wroteFiles := false

	if forceWrite || Library.metadataModified {
		Library.WriteMetadataFile()
		wroteFiles = true
	}
	if forceWrite || Library.entryFlagsModified {
		Library.WriteEntryFlagsFile()
		wroteFiles = true
	}
	if forceWrite || Library.keyNonDoublesModified {
		Library.WriteKeyNonDoublesFile()
		wroteFiles = true
	}
	if forceWrite || Library.dblpParentModified {
		Library.WriteDblpParentFile()
		wroteFiles = true
	}
	if forceWrite || Library.dblpWaivedModified {
		Library.WriteDblpWaivedFile()
		wroteFiles = true
	}
	if forceWrite || Library.keyOldiesModified {
		Library.WriteKeyOldiesFile()
		wroteFiles = true
	}
	if forceWrite || Library.keyHintsModified {
		Library.WriteKeyHintsFile()
		wroteFiles = true
	}
	if forceWrite || Library.nameMappingsModified {
		Library.WriteNameMappingFile()
		wroteFiles = true
	}
	if forceWrite || Library.genericFieldMappingsModified {
		Library.WriteGenericFieldMappingsFile()
		wroteFiles = true
	}
	if forceWrite || Library.entryFieldMappingsModified {
		Library.WriteEntryFieldMappingsFile()
		wroteFiles = true
	}
	if forceWrite || Library.crossFieldMappingsModified {
		Library.WriteCrossFieldMappingsFile()
		wroteFiles = true
	}
	if forceWrite || bibEntriesModified {
		Library.WriteBibTeXFile()
		clearTableDirty("bib_entries")
		wroteFiles = true
	}
	if wroteFiles && !skipBibDbRefresh {
		refreshBibDbTimestamp()
	}
}
