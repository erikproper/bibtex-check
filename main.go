package main

import (
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

const AppVersion = "10.0"

// Run-state flags set by command functions; consumed by the write tail in main.
var (
	writeAliases     bool
	writeMappings    bool
	writeBibFile     bool
	skipBibDbRefresh bool
	forceWrite       bool
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
	clearBibTables()
	beginBibTransaction()
	if !Library.ReadBib(BibFile) {
		rollbackBibTransaction()
		return false
	}
	saveBibGroupsToDb(&Library)
	saveBibCommentsToDb(&Library)
	commitBibTransaction()
	initEntryCache()
	reportCacheMode()
	Library.WriteCache()
	refreshBibDbTimestamp()
	return true
}

func openLibraryToUpdate() bool {
	initialiseLibrary()
	Library.ReadKeyOldiesFile()
	loadMappingFiles()

	if Library.ValidBibDb() {
		buildTitleIndexFromDb(&Library)
		loadBibFromDb()
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

	if Library.ValidBibDb() {
		loadBibFromDb()
	} else {
		loadMappingFiles()
		if !parseBibIntoDb() {
			return false
		}
	}

	Library.ReportLibrarySize()
	return true
}

func FIXThatShouldBeChecks(key string) {
	Library.CheckNeedToMergeForEqualTitles(key)
	Library.CheckNeedToSplitBookishEntry(key)
	Library.CheckDBLP(key)
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

func doDefaultRun() {
	if openLibraryToUpdate() {
		writeBibFile = true
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		Library.CheckFiles()
		if bibEntriesModified || forceWrite {
			writeAliases = true
			writeMappings = true
		}
	}
}

func doGetPdfs() {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		forEachBibEntryKey(func(key string) bool {
			filePath := Library.FilesRoot + FilesFolder + key + ".pdf"
			if !FileExists(filePath) {
				URL := Library.EntryFieldValueity(key, "url")
				if URL != "" && URL[len(URL)-4:] == ".pdf" {
					fmt.Println("get direct", filePath, "\""+URL+"\"")
				}

				DOI := Library.EntryFieldValueity(key, "doi")
				if strings.HasPrefix(DOI, "10.1007/") {
					fmt.Println("get springer", filePath, "\"https://link.springer.com/chapter/"+DOI+"#preview\"")
				}
			}
			return true
		})
	}
}

func doEntryKey(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		fmt.Println(Library.MapEntryKey(cleanKey(args[0])))
	}
}

func doEntryKeyAlias(args []string) {
	Reporting.SetInteractionOff()
	if openLibraryToReport() {
		alias := Library.PreferredKey(Library.MapEntryKey(cleanKey(args[0])))
		if alias != "" {
			fmt.Println(alias)
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
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		for _, key := range cleanKeys(args) {
			FIXThatShouldBeChecks(key)
		}
		writeBibFile = true
		writeAliases = true
		writeMappings = true
	}
}

func doFixAllEntries() {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		count := 0
		forEachBibEntryKey(func(key string) bool {
			count++
			fmt.Println("Entry count: ", count)
			Reporting.ResetQuestionFlag()
			FIXThatShouldBeChecks(key)
			if Reporting.QuestionWasAsked() && Reporting.AskContinueOrQuit() {
				return false
			}
			return true
		})
		writeBibFile = true
		writeAliases = true
		writeMappings = true
	}
}

func doSyncAllDblpEntries() {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		count := 0
		forEachBibEntryKey(func(key string) bool {
			if Library.EntryFieldValueity(key, DBLPField) != "" {
				count++
				fmt.Println("Entry count: ", count)
				Reporting.ResetQuestionFlag()
				Library.CheckDBLP(key)
				if Reporting.QuestionWasAsked() && Reporting.AskContinueOrQuit() {
					return false
				}
			}
			return true
		})
		writeBibFile = true
		writeAliases = true
		writeMappings = true
	}
}

func doNewKey() {
	fmt.Println(KeyFromTime(time.Now()))
}

func doSyncDblpFor(args []string) {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		for _, key := range cleanKeys(args) {
			Library.CheckDBLP(key)
		}
		writeBibFile = true
		writeAliases = true
		writeMappings = true
	}
}

func doAddDblpEntry(args []string) {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		writeBibFile = true
		writeAliases = true
		writeMappings = true
		for _, dblpKey := range args {
			if Library.LookupDBLPKey(dblpKey) == "" {
				if added := Library.MaybeAddDBLPEntry(dblpKey); added != "" {
					FIXThatShouldBeChecks(added)
				}
			}
		}
	}
}

func doAddKeyMapping(args []string) {
	if openLibraryToUpdate() {
		Library.CheckEntries()
		writeBibFile = true
		writeAliases = true
		writeMappings = true
		target := Library.MapEntryKey(cleanKey(args[len(args)-1]))
		for _, alias := range args[:len(args)-1] {
			fmt.Println("Mapping", cleanKey(alias), "to", target)
			Library.AddKeyAlias(cleanKey(alias), target)
		}
		Library.CheckEntries()
		FIXThatShouldBeChecks(target)
	}
}

func doMergeEntries(args []string) {
	keys := cleanKeys(args)
	if openLibraryToUpdate() {
		Library.CheckEntries()
		Library.ReadKeyNonDoublesFile()
		writeBibFile = true
		writeAliases = true
		writeMappings = true
		target := Library.MapEntryKey(keys[len(keys)-1])
		for _, alias := range keys[:len(keys)-1] {
			Library.MergeEntries(alias, target)
		}
		for _, key := range keys {
			if Library.MapEntryKey(key) == key {
				FIXThatShouldBeChecks(key)
			}
		}
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

	var cmdVersion bool
	flag.BoolVar(&cmdVersion, "version", false, "print version and exit")
	flag.BoolVar(&cmdVersion, "v", false, "print version and exit")

	var (
		cmdGet                  bool
		cmdGetPdfs              bool
		cmdEntryKey             bool
		cmdEntryKeyAlias        bool
		cmdShowEntry            bool
		cmdFixEntries           bool
		cmdFixAllEntries        bool
		cmdSyncAllDblpEntries   bool
		cmdSyncDblpFor          bool
		cmdAddDblpEntry         bool
		cmdAddKeyMapping        bool
		cmdMergeEntries         bool
		cmdAddNameMapping       bool
		cmdNewKey               bool
	)

	flag.BoolVar(&cmdGet, "get", false, "generate local .bib from bib.config + .map in CWD")
	flag.BoolVar(&cmdGetPdfs, "get_pdfs", false, "print shell get-commands for missing PDFs")
	flag.BoolVar(&cmdEntryKey, "entry_key", false, "resolve alias to canonical key")
	flag.BoolVar(&cmdEntryKeyAlias, "entry_key_alias", false, "get preferred alias for a key")
	flag.BoolVar(&cmdShowEntry, "show_entry", false, "print full entry content")
	flag.BoolVar(&cmdFixEntries, "fix_entries", false, "fix/check specific entries")
	flag.BoolVar(&cmdFixEntries, "fix_entry", false, "alias for -fix_entries")
	flag.BoolVar(&cmdFixAllEntries, "fix_all_entries", false, "fix/check all entries")
	flag.BoolVar(&cmdSyncAllDblpEntries, "sync_all_dblp_entries", false, "DBLP-sync all entries that have a DBLP key")
	flag.BoolVar(&cmdSyncDblpFor, "sync_dblp_for", false, "DBLP-sync specific entries")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entry", false, "add one or more new entries from DBLP")
	flag.BoolVar(&cmdAddDblpEntry, "add_dblp_entries", false, "alias for -add_dblp_entry")
	flag.BoolVar(&cmdAddKeyMapping, "add_key_mapping", false, "add key alias(es) to a canonical key")
	flag.BoolVar(&cmdAddKeyMapping, "add_key_mappings", false, "alias for -add_key_mapping")
	flag.BoolVar(&cmdMergeEntries, "merge_entries", false, "merge entries into target")
	flag.BoolVar(&cmdAddNameMapping, "add_name_mapping", false, "add a name alias mapping")
	flag.BoolVar(&cmdNewKey, "new_key", false, "print a fresh canonical key and exit")

	flag.Parse()
	args := flag.Args()

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
		doEntryKeyAlias(args)

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

	case cmdSyncAllDblpEntries:
		doSyncAllDblpEntries()

	case cmdSyncDblpFor:
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: -sync_dblp_for <key>...")
			os.Exit(1)
		}
		doSyncDblpFor(args)

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

	default:
		if len(args) > 0 {
			fmt.Fprintln(os.Stderr, "Unexpected arguments (did you forget a command flag?):", args)
			os.Exit(1)
		}
		doDefaultRun()
	}

	wroteFiles := false

	if Library.keyNonDoublesModified || forceWrite {
		Library.WriteKeyNonDoublesFile()
		wroteFiles = true
	}
	if Library.nameMappingsModified {
		Library.WriteNameMappingFile()
		wroteFiles = true
	}
	if writeAliases {
		Library.WriteAllMappingsFiles()
		wroteFiles = true
	}
	if writeMappings {
		Library.WriteCrossFieldMappingsFile()
		wroteFiles = true
	}
	if writeBibFile && (bibEntriesModified || forceWrite) {
		Library.CheckEntries()
		Library.WriteBibTeXFile()
		Library.WriteCache()
		wroteFiles = true
	}
	if wroteFiles && !skipBibDbRefresh {
		refreshBibDbTimestamp()
	}
}
