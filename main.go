package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var (
	Library   TBibTeXLibrary
	Reporting TInteraction
)

const AppVersion = "14.18"

// Run-state flags consumed by the write tail in main.
var (
	skipBibDbRefresh  bool
	skipBibValidation bool
	forceWrite        bool
	cmdStep           bool
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
	Library.ReadKeyHintsFile()
	Library.ReadNameMappingsFile()
	Library.ReadGenericFieldMappingsFile()
	Library.ReadEntryFieldMappingsFile()
	Library.ReadCrossFieldMappingsFile()
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

func doAllChecks(key string) {
	doC1Checks(key)
	doC2Checks(key)
	doC3Checks(key)
}

func cleanKey(rawKey string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(rawKey, "\\cite{", ""), "cite{", ""), "}", ""))
}

var BibFile string

var instanceLockFile *os.File // held open for the lifetime of the process to maintain the flock

// acquireInstanceLock obtains an exclusive flock on <basename>.lock.
// Exits immediately if another instance already holds the lock for the same base.
// The OS releases the lock automatically when the process exits.
func acquireInstanceLock() {
	lockPath := bibTeXFolder + bibTeXBaseName + LockFileExtension

	var err error
	instanceLockFile, err = os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open lock file %s: %s\n", lockPath, err)
		os.Exit(1)
	}

	if err := syscall.Flock(int(instanceLockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintf(os.Stderr, "Another instance is already running for %s\n", bibTeXBaseName)
		os.Exit(1)
	}
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

func doTempFix() {
	if openLibraryToUpdate() {
		Library.repairBadPreferredAliases()
	}
}

func doDefaultRun() {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
	}
}

func doCheckPDFs() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		Library.ReadPDFConfirmedOkFile()
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

func doRenderGroup(args []string) {
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
			writeFile(pubsFolder+key+".bib", bib)
			writeFile(citationsFolder+key+".html", Library.renderAsHTML(key)+"\n")
			writeFile(citationsFolder+key+".tex", Library.renderAsTeX(key)+"\n")

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
			fmt.Printf("%s|%s|%s|%s\n", year, key, rg, dblp)
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
		key := Library.MapEntryKey(cleanKey(args[0]))
		alias := Library.PreferredKey(key)
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
		for _, key := range cleanKeys(args) {
			doAllChecks(key)
		}
	}
}

func doFixAllEntries() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		total := countBibEntries()
		count := 0
		forEachBibEntryKey(func(key string) bool {
			count++
			Library.Progress(ProgressEntryProgress, count, total, float64(count)*100/float64(total))
			Reporting.ResetQuestionFlag()
			doAllChecks(key)
			if cmdStep && Library.OutputWasProduced() {
				fmt.Printf("--- Press Enter to continue ---")
				bufio.NewReader(os.Stdin).ReadString('\n')
			}
			if Reporting.QuestionWasAsked() && Reporting.AskContinueOrQuit() {
				return false
			}
			return true
		})
	}
}

func doFixDblpEntries() {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		total := countBibEntries()
		scanned := 0
		forEachBibEntryKey(func(key string) bool {
			scanned++
			if Library.EntryFieldValueity(key, DBLPField) != "" {
				Library.Progress(ProgressEntryProgress, scanned, total, float64(scanned)*100/float64(total))
				Reporting.ResetQuestionFlag()
				doAllChecks(key)
				Reporting.PressEnterToContinue()
				if Reporting.QuestionWasAsked() && Reporting.AskContinueOrQuit() {
					return false
				}
			}
			return true
		})
	}
}

func doNewKey() {
	fmt.Println(KeyFromTime(time.Now()))
}

func doFixDblpFor(args []string) {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		for _, key := range cleanKeys(args) {
			doAllChecks(key)
		}
	}
}

func doAddDblpEntry(args []string) {
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		for _, dblpKey := range args {
			if Library.LookupDBLPKey(dblpKey) == "" {
				if added := Library.MaybeAddDBLPEntry(dblpKey); added != "" {
					doAllChecks(added)
				}
			}
		}
	}
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

func doMergeEntries(args []string) {
	keys := cleanKeys(args)
	if openLibraryToUpdate() {
		Library.ReadKeyNonDoublesFile()
		target := Library.MapEntryKey(keys[len(keys)-1])
		for _, alias := range keys[:len(keys)-1] {
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

	flag.BoolVar(&cmdStep, "step", false, "pause for Enter after each entry in for-all loops")

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
		cmdAddDblpEntry       bool
		cmdAddKeyMapping      bool
		cmdMergeEntries       bool
		cmdAddNameMapping     bool
		cmdSetPreferredAlias  bool
		cmdNewKey             bool
		cmdMap                bool
		cmdAddToGroup         bool
		cmdRemoveFromGroup    bool
		cmdRenderAsBibTeX     bool
		cmdRenderGroup        bool
		cmdRenderAsTex        bool
		cmdRenderAsHTML       bool
		cmdRenderAsText       bool
		cmdCheckPdfs          bool
		cmdTempFix            bool
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
	flag.BoolVar(&cmdFixDblpEntries, "fix_dblp_entries", false, "fix/check all entries that have a DBLP key")
	flag.BoolVar(&cmdFixDblpFor, "fix_dblp_for", false, "fix/check specific entries with DBLP")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entry", false, "add one or more new entries from DBLP")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entries", false, "alias for -add_dblp_entry")
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
	flag.BoolVar(&cmdRenderAsBibTeX, "render_as_bibtex", false, "render entry as self-contained BibTeX")
	flag.BoolVar(&cmdRenderAsTex, "render_as_tex", false, "render entry as TeX bibliography reference")
	flag.BoolVar(&cmdRenderAsHTML, "render_as_html", false, "render entry as HTML bibliography reference")
	flag.BoolVar(&cmdRenderAsText, "render_as_text", false, "render entry as plain-text bibliography reference")
	flag.BoolVar(&cmdCheckPdfs, "check_pdfs", false, "check PDF health, orphan files, and duplicates in the files folder")
	flag.BoolVar(&cmdTempFix, "temp_fix", false, "re-derive preferred aliases with single-letter suffix or unicode substring")

	flag.Parse()
	args := flag.Args()
	Reporting.SetStepMode(cmdStep)

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

	loadBibTeXConfig(bibTeXFolder + bibTeXBaseName + ConfigFileExtension)

	if cmdNewKey {
		doNewKey()
		os.Exit(0)
	}

	acquireInstanceLock()
	connectToDatabase()

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

	case cmdFixDblpEntries:
		doFixDblpEntries()

	case cmdFixDblpFor:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -fix_dblp_for <key>...")
			os.Exit(1)
		}
		doFixDblpFor(args)

	case cmdAddDblpEntry:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -add_dblp_entry <dblp-key>...")
			os.Exit(1)
		}
		doAddDblpEntry(args)

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
			fmt.Fprintln(os.Stderr, "Usage: -render_group <group> <pubs_folder> <citations_folder>")
			os.Exit(1)
		}
		doRenderGroup(args)

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

	case cmdTempFix:
		doTempFix()

	default:
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "Unexpected arguments (did you forget a command flag?):", args)
			os.Exit(1)
		}
		doDefaultRun()
	}

	wroteFiles := false

	if forceWrite || Library.pdfConfirmedOkModified {
		Library.WritePDFConfirmedOkFile()
		wroteFiles = true
	}
	if forceWrite || Library.keyNonDoublesModified {
		Library.WriteKeyNonDoublesFile()
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
