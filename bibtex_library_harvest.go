/*
 *
 * Module: bibtex_library_harvest
 *
 * Harvest mode (step 14.2): ingest entries from an external bib file as candidates
 * for the main library. Entries are processed sequentially and interactively.
 *
 * Three input sources:
 *   - Watch file: mode="harvest" in bib.config; triggered by -sync; .harvest log maintained.
 *   - One-off:    -harvest <path>; no .harvest log written.
 *   - Stdin:      -harvest (no path); no .harvest log written.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Delta log types and I/O ---

// THarvestLogEntry is one row of the .harvest CSV delta log.
type THarvestLogEntry struct {
	OriginalKey        string // key from source bib (empty if absent)
	TitleHash          string // MD5 of TeXStringIndexer(title)
	ContentFingerprint string // MD5 of sorted field=value pairs
	Action             string // library canonical key, "skip-content", or "skip-never"
}

const (
	harvestActionSkipContent = "skip-content"
	harvestActionSkipNever   = "skip-never"
)

// THarvestLog is the in-memory delta log for one source bib.
type THarvestLog []THarvestLogEntry

// harvestContentFingerprint returns an MD5 of all non-empty field=value pairs, sorted.
func harvestContentFingerprint(e TBibTeXEntry) string {
	return bibContentFingerprint(e)
}

// harvestTitleHash returns MD5 of the TeXStringIndexer-normalised title.
func harvestTitleHash(e TBibTeXEntry) string {
	title := e.Fields[TitleField]
	if title == "" {
		return ""
	}
	h := md5.Sum([]byte(TeXStringIndexer(title)))
	return hex.EncodeToString(h[:])
}

// readHarvestLog reads a .harvest CSV file; returns an empty log when absent.
func readHarvestLog(logPath string) THarvestLog {
	var log THarvestLog
	processFile(logPath, func(line string) {
		parts := strings.SplitN(line, csvDelimiter, 4)
		if len(parts) != 4 {
			return
		}
		log = append(log, THarvestLogEntry{
			OriginalKey:        parts[0],
			TitleHash:          parts[1],
			ContentFingerprint: parts[2],
			Action:             parts[3],
		})
	})
	return log
}

// writeHarvestLog writes the log to logPath.
func writeHarvestLog(logPath string, log THarvestLog) {
	f, err := os.Create(logPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write harvest log:", err)
		return
	}
	defer f.Close()
	for _, e := range log {
		fmt.Fprintf(f, "%s%s%s%s%s%s%s\n",
			e.OriginalKey, csvDelimiter,
			e.TitleHash, csvDelimiter,
			e.ContentFingerprint, csvDelimiter,
			e.Action)
	}
}

// harvestLogLookup finds the log entry for e. Checks OriginalKey first, then TitleHash.
func harvestLogLookup(e TBibTeXEntry, log THarvestLog) *THarvestLogEntry {
	if e.Key != "" {
		for i := range log {
			if log[i].OriginalKey == e.Key {
				return &log[i]
			}
		}
	}
	titleHash := harvestTitleHash(e)
	if titleHash != "" {
		for i := range log {
			if log[i].TitleHash == titleHash {
				return &log[i]
			}
		}
	}
	return nil
}

// harvestShouldSkip returns true when e should be silently skipped this run.
func harvestShouldSkip(e TBibTeXEntry, log THarvestLog, l *TBibTeXLibrary) bool {
	entry := harvestLogLookup(e, log)
	if entry == nil {
		return false
	}
	switch entry.Action {
	case harvestActionSkipNever:
		return true
	case harvestActionSkipContent:
		return harvestContentFingerprint(e) == entry.ContentFingerprint
	default:
		// Action is a library canonical key: skip if that key still exists.
		return l.EntryExists(l.MapEntryKey(entry.Action))
	}
}

// --- Parsing ---

// countHarvestBibEntries does a fast line scan to count anticipated bib entries:
// any line whose first non-space character is '@' followed by a letter, excluding
// @string, @comment, and @preamble (which are not library entries).
func countHarvestBibEntries(path string) int {
	n := 0
	processFile(path, func(line string) {
		trimmed := strings.TrimLeft(line, " \t")
		if len(trimmed) < 2 || trimmed[0] != '@' {
			return
		}
		rest := strings.ToLower(trimmed[1:])
		if strings.HasPrefix(rest, "string") ||
			strings.HasPrefix(rest, "comment") ||
			strings.HasPrefix(rest, "preamble") {
			return
		}
		if len(rest) > 0 && rest[0] >= 'a' && rest[0] <= 'z' {
			n++
		}
	})
	return n
}

// parseHarvestBib parses a bib file using the existing streaming parser with the
// capturedHarvestEntries mechanism: entries are collected in memory and never written
// to the main DB. Returns the entries and true on a complete parse; false when fewer
// entries were captured than anticipated (malformed source, parsing stopped early).
func (l *TBibTeXLibrary) parseHarvestBib(path string) ([]TBibTeXEntry, bool) {
	if !FileExists(path) {
		return nil, true
	}
	anticipated := countHarvestBibEntries(path)

	entries := make([]TBibTeXEntry, 0, anticipated)
	l.capturedHarvestEntries = &entries
	l.harvestCapturePDFFields = true
	l.harvestSourceDir = filepath.Dir(path)
	l.ParseRawBibFile(path)
	l.harvestCapturePDFFields = false
	// harvestSourceDir is kept set so maybeHarvestPDF can resolve relative paths
	// during the harvest loop that follows. Overwritten on the next parseHarvestBib call.
	l.capturedHarvestEntries = nil
	l.capturedDBLPEntry = nil // guard: clean up if last entry was never finished

	if len(entries) < anticipated {
		l.Warning("Parsed %d of %d anticipated entries from %s — source file may be malformed (parsing stopped early)",
			len(entries), anticipated, path)
		return entries, false
	}
	return entries, true
}

// --- Entry matching pipeline ---

// harvestKeyMatch returns the canonical library key matching e's source key via
// KeyToKey aliases or HintToKey. Returns "" when no real library entry is found.
// MapEntryKey returns the key unchanged when not in KeyToKey, so EntryExists guards
// against false positives from key-hint-only keys.
func (l *TBibTeXLibrary) harvestKeyMatch(e TBibTeXEntry) string {
	if e.Key == "" {
		return ""
	}
	if canon := l.MapEntryKey(e.Key); l.EntryExists(canon) {
		return canon
	}
	if hint := l.HintToKey[e.Key]; hint != "" {
		if canon := l.MapEntryKey(hint); l.EntryExists(canon) {
			return canon
		}
	}
	return ""
}

// harvestTitleMatches returns all canonical library keys whose normalised title
// matches e's title via TitleIndex. Returns nil when no match is found.
func (l *TBibTeXLibrary) harvestTitleMatches(e TBibTeXEntry) []string {
	title := e.Fields[TitleField]
	if title == "" {
		return nil
	}
	if peers := l.TitleIndex[TeXStringIndexer(title)]; peers.Size() > 0 {
		return peers.ElementsSorted()
	}
	return nil
}

// askHarvestLibraryChoice asks the user to pick one of n numbered library candidates.
// Returns the 1-based index, or 0 for none.
func (l *TBibTeXLibrary) askHarvestLibraryChoice(n int) int {
	options := TStringSetNew()
	options.Add("0")
	for i := 1; i <= n; i++ {
		options.Add(fmt.Sprintf("%d", i))
	}
	answer := l.WarningQuestion(QuestionHarvestLibraryChoice, options, "")
	result := 0
	fmt.Sscanf(answer, "%d", &result)
	return result
}

// harvestFindDblpCandidates runs the DBLP title-hash search (step 3 of the pipeline)
// for a harvested entry that has no dblp field. Returns the chosen DBLP key or "".
func (l *TBibTeXLibrary) harvestFindDblpCandidates(e TBibTeXEntry) string {
	title := e.Fields[TitleField]
	if title == "" {
		return ""
	}
	hash := libraryTitleHash(title)
	if hash == "" {
		return ""
	}
	candidates := readDblpTitleLinks(hash)
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) > 9 {
		candidates = candidates[:9]
	}
	fmt.Fprintf(os.Stderr, "\n")
	for i, c := range candidates {
		fmt.Fprintf(os.Stderr, "  [%d] %s", i+1, c)
		if entry := dblpEntryFromFile(c); entry != nil {
			for _, field := range []string{"title", "year", "author", "booktitle", "journal"} {
				if v := entry.Fields[field]; v != "" {
					fmt.Fprintf(os.Stderr, "\n      %-12s: %s", field, v)
				}
			}
		}
		fmt.Fprintln(os.Stderr)
	}
	options := TStringSetNew()
	options.Add("0", "k")
	for i := range candidates {
		options.Add(fmt.Sprintf("%d", i+1))
	}
	answer := l.WarningQuestion(QuestionHarvestDblpChoose, options,
		WarningHarvestDblpCandidatesFound, e.Fields[TitleField], len(candidates))
	if answer == "k" {
		if dblpKey, err := Reporting.AskForInput("DBLP key"); err == nil && dblpKey != "" {
			if dblpEntryFromFile(dblpKey) != nil {
				return dblpKey
			}
			l.Warning("DBLP key %q not found in file store", dblpKey)
		}
		return ""
	}
	n := 0
	fmt.Sscanf(answer, "%d", &n)
	if n <= 0 || n > len(candidates) {
		return ""
	}
	return candidates[n-1]
}

// --- Display ---

// printEntryFields prints all non-empty fields of a TBibTeXEntry to stderr.
func printEntryFields(entryType, key string, fields map[string]string) {
	fmt.Fprintf(os.Stderr, "  @%s{%s,\n", entryType, key)
	sorted := make([]string, 0, len(fields))
	for f := range fields {
		if f != EntryTypeField {
			sorted = append(sorted, f)
		}
	}
	sort.Strings(sorted)
	for _, field := range sorted {
		if v := fields[field]; v != "" {
			fmt.Fprintf(os.Stderr, "    %-*s = {%s},\n", BibTeXFieldColumnWidth, field, v)
		}
	}
	fmt.Fprintf(os.Stderr, "  }\n")
}


// maybeCollectKeyHint adds sourceKey → finalKey to the hints DB when -collect_keys
// is active and the mapping is unambiguous (not already pointing elsewhere).
func maybeCollectKeyHint(l *TBibTeXLibrary, sourceKey, finalKey string) {
	if !cmdCollectKeys || sourceKey == "" || finalKey == "" {
		return
	}
	if existing := l.HintToKey[sourceKey]; existing != "" && l.MapEntryKey(existing) != finalKey {
		return
	}
	l.AddKeyHint(sourceKey, finalKey)
}

// --- PDF harvesting ---

// jabrefPDFPath extracts the path to the first PDF-typed file from a JabRef file
// field value. Returns "" when no PDF entry is found. Format:
//
//	description:path:type[;description:path:type…]
//
// with ':', ';', and '\' each escaped by '\'.
func jabrefPDFPath(ref string) string {
	// Temporarily replace escape sequences so we can split on bare delimiters.
	safe := strings.ReplaceAll(ref, `\\`, "\x01")  // protect escaped backslash
	safe = strings.ReplaceAll(safe, `\:`, "\x02")  // protect escaped colon
	safe = strings.ReplaceAll(safe, `\;`, "\x03")  // protect escaped semicolon

	restore := func(s string) string {
		s = strings.ReplaceAll(s, "\x03", ";")
		s = strings.ReplaceAll(s, "\x02", ":")
		s = strings.ReplaceAll(s, "\x01", `\`)
		return s
	}

	for _, entry := range strings.Split(safe, ";") {
		parts := strings.SplitN(entry, ":", 3)
		if len(parts) < 3 {
			continue
		}
		if !strings.EqualFold(restore(parts[2]), "pdf") {
			continue
		}
		if p := restore(parts[1]); p != "" {
			return p
		}
	}
	return ""
}

// maybeHarvestPDF copies a PDF referenced by a harvested entry into the library files
// folder under canonicalKey.pdf. Handles both JabRef (file = {:path:PDF}) and
// BibDesk (local-url = {/abs/path}) formats. Relative JabRef paths are resolved
// against l.harvestSourceDir (set during parseHarvestBib, kept through the loop).
func (l *TBibTeXLibrary) maybeHarvestPDF(e TBibTeXEntry, canonicalKey string) {
	if l.PDFFiles[canonicalKey] {
		return // library already has a PDF for this entry
	}
	destPath := l.FilesRoot + l.FilesFolder + canonicalKey + ".pdf"

	var srcPath string
	if ref := e.Fields[JabrefFileField]; ref != "" {
		if raw := jabrefPDFPath(ref); raw != "" {
			if !filepath.IsAbs(raw) && l.harvestSourceDir != "" {
				raw = filepath.Join(l.harvestSourceDir, raw)
			}
			srcPath = raw
		}
	} else if ref := e.Fields[LocalURLField]; ref != "" {
		srcPath = ref
	}

	if srcPath == "" || !FileExists(srcPath) {
		return
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
		l.Warning("Could not create PDF directory %s: %s", filepath.Dir(destPath), err)
		return
	}
	if err := copyFile(srcPath, destPath); err != nil {
		l.Warning("Could not copy PDF %s → %s: %s", srcPath, destPath, err)
		return
	}
	l.PDFFiles[canonicalKey] = true
	l.Progress("Harvested PDF: %s → %s", filepath.Base(srcPath), canonicalKey+".pdf")
}

// maybeHarvestGroups imports the JabRef per-entry groups field into the library's
// GroupEntries and persists each membership to bib_groups. The field value is a
// comma-separated list of group names. Idempotent: re-runs add nothing new.
func (l *TBibTeXLibrary) maybeHarvestGroups(e TBibTeXEntry, canonicalKey string) {
	raw := e.Fields["groups"]
	if raw == "" {
		return
	}
	for _, group := range strings.Split(raw, ",") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		l.GroupEntries.AddValueToStringSetMap(group, canonicalKey)
		if err := bibExec(
			`INSERT INTO bib_groups (group_name, entry_key) VALUES (?, ?) ON CONFLICT DO NOTHING;`,
			group, canonicalKey); err != nil {
			l.Warning("Could not persist group %q for %s: %s", group, canonicalKey, err)
		}
	}
}

// --- Adding entries ---

// addHarvestEntry inserts a harvested TBibTeXEntry as a new library entry and returns
// the new canonical key.
func addHarvestEntry(l *TBibTeXLibrary, e TBibTeXEntry) string {
	key := l.NewKey()
	l.SetEntryType(key, e.Fields[EntryTypeField])
	for field, value := range e.Fields {
		if field == EntryTypeField || value == "" {
			continue
		}
		switch field {
		case DBLPField, PreferredAliasField:
			// Write directly without registering alias/hint mappings for this
			// temp key — it will be merged immediately and the canonical entry
			// already owns these mappings.
			l.SetEntryFieldValue(key, field, value)
		default:
			l.ProcessRawEntryFieldValue(key, field, value)
		}
	}
	return key
}

// --- Interactive loop ---

// runHarvestEntry processes one harvested entry through the 4-step pipeline.
// Returns the updated log and true when the user chose to quit.
func (l *TBibTeXLibrary) runHarvestEntry(e TBibTeXEntry, log THarvestLog, withLog bool) (THarvestLog, bool) {
	titleHash := harvestTitleHash(e)
	contentFP := harvestContentFingerprint(e)

	appendLog := func(action string) THarvestLog {
		if withLog {
			return append(log, THarvestLogEntry{e.Key, titleHash, contentFP, action})
		}
		return log
	}
	// fixEntry runs the full per-entry fix when -fix is set: DBLP update for
	// entries that already have a DBLP key, peer+candidate search for those that don't.
	fixEntry := func(key string) {
		if !cmdFix {
			return
		}
		key = l.MapEntryKey(key)
		if dblpKey := l.EntryFieldValueity(key, DBLPField); dblpKey != "" {
			l.MaybeMergeDBLPEntry(dblpKey, key, true)
		} else {
			if !findLibraryEqualWithDblp(key) {
				maybeFindDBLPCandidates(key)
			}
		}
	}
	mergeAndCheck := func(newKey, matchKey string) string {
		l.MergeEntries(l.MapEntryKey(newKey), matchKey)
		finalKey := l.MapEntryKey(matchKey)
		l.Progress("Checking %s", finalKey)
		doAllChecks(finalKey)
		fixEntry(finalKey)
		return l.MapEntryKey(finalKey)
	}
	collectKey := func(sourceKey, finalKey string) {
		maybeCollectKeyHint(l, sourceKey, finalKey)
	}

	// Always show the source entry first.
	fmt.Fprintf(os.Stderr, "\nSource entry:\n")
	printEntryFields(e.Fields[EntryTypeField], e.Key, e.Fields)

	// Step 1: key hint / alias match.
	if keyMatch := l.harvestKeyMatch(e); keyMatch != "" {
		fmt.Fprintf(os.Stderr, "Key match:\n")
		fmt.Fprint(os.Stderr, l.entryDisplayString(keyMatch))
		if cmdTrustHints || l.ConfirmAction(QuestionHarvestKeyMatch) {
			finalKey := mergeAndCheck(addHarvestEntry(l, e), keyMatch)
			collectKey(e.Key, finalKey)
			l.maybeHarvestPDF(e, finalKey)
			l.maybeHarvestGroups(e, finalKey)
			return appendLog(finalKey), false
		}
	}

	// Step 2: title match in library (may find multiple).
	if titleMatches := l.harvestTitleMatches(e); len(titleMatches) > 0 {
		fmt.Fprintf(os.Stderr, "Title match(es) in library:\n")
		for i, k := range titleMatches {
			fmt.Fprintf(os.Stderr, "[%d]\n", i+1)
			fmt.Fprint(os.Stderr, l.entryDisplayString(k))
		}
		if pick := l.askHarvestLibraryChoice(len(titleMatches)); pick > 0 {
			finalKey := mergeAndCheck(addHarvestEntry(l, e), titleMatches[pick-1])
			collectKey(e.Key, finalKey)
			l.maybeHarvestPDF(e, finalKey)
			l.maybeHarvestGroups(e, finalKey)
			return appendLog(finalKey), false
		}
	}

	// Step 3: DBLP title match (only when source has no dblp field yet).
	if e.Fields[DBLPField] == "" {
		if chosen := l.harvestFindDblpCandidates(e); chosen != "" {
			newKey := addHarvestEntry(l, e)
			l.AssociateDblpKey(newKey, chosen)
			finalKey := l.MapEntryKey(newKey)
			l.Progress("Checking %s", finalKey)
			doAllChecks(finalKey)
			fixEntry(finalKey)
			collectKey(e.Key, l.MapEntryKey(finalKey))
			l.maybeHarvestPDF(e, l.MapEntryKey(finalKey))
			return appendLog(l.MapEntryKey(finalKey)), false
		}
	}

	// Step 4: no match — offer add or skip.
	validActions := TStringSetNew()
	validActions.Add("a").Add("s").Add("w").Add("q")
	switch l.WarningQuestion(QuestionHarvestAction, validActions, "") {
	case "q":
		return log, true
	case "w":
		return appendLog(harvestActionSkipNever), false
	case "s":
		return appendLog(harvestActionSkipContent), false
	default: // "a"
		newKey := addHarvestEntry(l, e)
		l.Progress("Checking %s", l.MapEntryKey(newKey))
		doAllChecks(newKey)
		finalKey := l.MapEntryKey(newKey)
		fixEntry(finalKey)
		collectKey(e.Key, finalKey)
		l.maybeHarvestPDF(e, finalKey)
		return appendLog(finalKey), false
	}
}

// runHarvestLoop processes entries interactively in source-file order.
// Entries already resolved in the log are silently skipped when withLog is true.
// Returns the updated log.
func (l *TBibTeXLibrary) runHarvestLoop(entries []TBibTeXEntry, log THarvestLog, withLog bool) THarvestLog {
	stepN := int(cmdStep)
	questionCounter := 0

	for _, e := range entries {
		if withLog && harvestShouldSkip(e, log, l) {
			// Always register confirmed log entries as key hints and group memberships —
			// both are curated approved data, not gated on -collect_keys.
			if entry := harvestLogLookup(e, log); entry != nil &&
				entry.Action != harvestActionSkipContent &&
				entry.Action != harvestActionSkipNever {
				canon := l.MapEntryKey(entry.Action)
				if e.Key != "" {
					l.AddKeyHint(e.Key, entry.Action)
				}
				l.maybeHarvestGroups(e, canon)
			}
			continue
		}
		var quit bool
		log, quit = l.runHarvestEntry(e, log, withLog)
		if quit {
			return log
		}
		if stepN > 0 {
			questionCounter++
			if questionCounter >= stepN {
				if l.AskContinueOrQuit() {
					return log
				}
				questionCounter = 0
			}
		}
	}
	return log
}

// --- Top-level harvest commands ---

// doHarvest opens the library, parses the source bib, and runs the interactive
// harvest loop. path="" reads from stdin; withLog=false for one-off / stdin runs.
func doHarvest(sourcePath string) {
	// When reading from stdin: drain bib data to a temp file first, then reopen
	// /dev/tty as stdin so all subsequent interactive prompts work normally.
	tmpPath := ""
	if sourcePath == "" {
		tmpFile, err := os.CreateTemp("", "harvest_*.bib")
		if err != nil {
			fmt.Fprintln(os.Stderr, "harvest: cannot create temp file:", err)
			return
		}
		tmpPath = tmpFile.Name()
		if _, err := io.Copy(tmpFile, os.Stdin); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			fmt.Fprintln(os.Stderr, "harvest: cannot read stdin:", err)
			return
		}
		tmpFile.Close()

		// Reconnect stdin to the terminal so crash recovery and all interactive
		// prompts can read from the user rather than the exhausted pipe.
		if tty, err := os.Open("/dev/tty"); err == nil {
			os.Stdin = tty
		} else {
			fmt.Fprintln(os.Stderr, "harvest: cannot open /dev/tty for interactive input:", err)
			os.Remove(tmpPath)
			return
		}
	}

	if !openLibraryToUpdate() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
		return
	}
	Library.ReadKeyNonDoublesFile()

	var entries []TBibTeXEntry

	if tmpPath != "" {
		defer os.Remove(tmpPath)
		entries, _ = Library.parseHarvestBib(tmpPath)
	} else {
		entries, _ = Library.parseHarvestBib(sourcePath)
	}

	if len(entries) == 0 {
		Library.Progress(ProgressHarvestSkipped)
		return
	}
	plural := "ies"
	if len(entries) == 1 {
		plural = "y"
	}
	Library.Progress(ProgressHarvestParsed, len(entries), plural, sourcePath)

	Library.runHarvestLoop(entries, nil, false)
}

// runHarvestSync is called from doSync when mode == "harvest". The library is
// already open. Maintains the .harvest delta log next to the source bib.
func runHarvestSync(cfg TBibGetConfig, baseDir string) {
	// Config values are OR'd with CLI flags: either source enables the feature.
	if cfg.TrustHints {
		cmdTrustHints = true
	}
	if cfg.CollectKeys {
		cmdCollectKeys = true
	}

	// Resolve source path to absolute using the same logic as writePullSync.
	sourcePath := cfg.FileName
	if filepath.Ext(sourcePath) == "" {
		sourcePath += BibFileExtension
	}
	if !filepath.IsAbs(sourcePath) {
		if baseDir != "" {
			sourcePath = filepath.Join(baseDir, sourcePath)
		} else if cwd, err := os.Getwd(); err == nil {
			sourcePath = filepath.Join(cwd, sourcePath)
		}
	}
	keysBasePath := strings.TrimSuffix(sourcePath, filepath.Ext(sourcePath))
	logPath := keysBasePath + HarvestLogExtension

	maybeMigrateSubsetToHarvest(sourcePath, keysBasePath, logPath)

	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	Library.Progress("Sync harvest: %s", cfg.FileName)
	Library.Progress("  trust_hints=%-3s  collect_keys=%-3s  fix=%-3s",
		on(cmdTrustHints), on(cmdCollectKeys), on(cmdFix))
	Library.Progress("  Source: %s", sourcePath)

	entries, _ := Library.parseHarvestBib(sourcePath)
	if len(entries) == 0 {
		Library.Progress("  Source: no entries found")
		return
	}

	log := readHarvestLog(logPath)

	// Count how many entries the log already covers (will be skipped).
	skipped := 0
	for _, e := range entries {
		if harvestShouldSkip(e, log, &Library) {
			skipped++
		}
	}
	pending := len(entries) - skipped
	Library.Progress("  Source: %d entr%s total; %d logged, %d pending",
		len(entries), map[bool]string{true: "y", false: "ies"}[len(entries) == 1],
		skipped, pending)

	if pending == 0 {
		// Re-run PDF harvest and key-hint registration for all resolved entries.
		// PDFs may have been added to the source bib since the first logged run,
		// and key hints must be registered regardless of whether -collect_keys
		// was active during the original harvest run.
		pdfsBefore := len(Library.PDFFiles)
		hintsBefore := len(Library.newKeyHints)
		for _, e := range entries {
			entry := harvestLogLookup(e, log)
			if entry == nil || entry.Action == harvestActionSkipContent || entry.Action == harvestActionSkipNever {
				continue
			}
			canon := Library.MapEntryKey(entry.Action)
			Library.maybeHarvestPDF(e, canon)
			Library.maybeHarvestGroups(e, canon)
			if e.Key != "" {
				Library.AddKeyHint(e.Key, entry.Action)
			}
		}
		pdfsHarvested := len(Library.PDFFiles) - pdfsBefore
		hintsAdded := len(Library.newKeyHints) - hintsBefore
		if pdfsHarvested > 0 || hintsAdded > 0 {
			Library.Progress("  PDFs harvested : %d  Key hints added : %d (re-run)", pdfsHarvested, hintsAdded)
		} else {
			Library.Progress("  All entries already logged — nothing to do")
		}
		return
	}

	pdfsBefore := len(Library.PDFFiles)
	hintsBefore := len(Library.newKeyHints)
	log = Library.runHarvestLoop(entries, log, true)
	writeHarvestLog(logPath, log)
	pdfsHarvested := len(Library.PDFFiles) - pdfsBefore
	hintsAdded := len(Library.newKeyHints) - hintsBefore

	// Final summary: count by outcome across the full log.
	resolved, skippedContent, waived, stillPending := 0, 0, 0, 0
	for _, e := range entries {
		entry := harvestLogLookup(e, log)
		if entry == nil {
			stillPending++
			continue
		}
		switch entry.Action {
		case harvestActionSkipNever:
			waived++
		case harvestActionSkipContent:
			skippedContent++
		default:
			resolved++
		}
	}
	Library.Progress("Harvest result: %s", cfg.FileName)
	Library.Progress("  Total  : %d source entr%s",
		len(entries), map[bool]string{true: "y", false: "ies"}[len(entries) == 1])
	Library.Progress("  Resolved (merged/added) : %d", resolved)
	Library.Progress("  Skipped (skip-content)  : %d", skippedContent)
	Library.Progress("  Waived  (skip-never)    : %d", waived)
	Library.Progress("  Pending (not yet seen)  : %d", stillPending)
	Library.Progress("  PDFs harvested          : %d", pdfsHarvested)
	Library.Progress("  Key hints added         : %d", hintsAdded)
}

// --- Mode transitions ---

// patchConfigField reads the JSON config at cfgPath, sets rawKey to rawVal, and writes back.
func patchConfigField(cfgPath, rawKey string, rawVal json.RawMessage) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return
	}
	m[rawKey] = rawVal
	if out, err := json.MarshalIndent(m, "", "  "); err == nil {
		os.WriteFile(cfgPath, append(out, '\n'), 0644) //nolint:errcheck
	}
}

// maybeMigrateHarvestToSubset handles a harvest→subset mode transition. When the
// user changes the config from mode="harvest" to mode="subset", the existing harvest
// log is used to seed the initial .keys file so the subset bib keeps the source
// keys as output keys. key_mapping is enabled in the config and in cfg so that the
// current sync run uses local keys immediately.
//
// Detection: subset state file absent + harvest log file present + no .keys file yet.
func maybeMigrateHarvestToSubset(cfg *TBibGetConfig, keysBasePath, statePath, logPath string) {
	keysPath := keysBasePath + KeysFileExtension
	if FileExists(statePath) || !FileExists(logPath) || FileExists(keysPath) {
		return
	}
	log := readHarvestLog(logPath)
	if len(log) == 0 {
		return
	}
	var pairs []TBibGetPair
	for _, e := range log {
		if e.OriginalKey == "" || e.Action == harvestActionSkipContent || e.Action == harvestActionSkipNever {
			continue
		}
		canon := Library.MapEntryKey(e.Action)
		if !Library.EntryExists(canon) {
			continue
		}
		pairs = append(pairs, TBibGetPair{localKey: e.OriginalKey, canonicalKey: canon})
	}
	if len(pairs) == 0 {
		Library.Progress("Harvest→subset: no resolved entries in harvest log — skipping .keys creation")
		return
	}
	cfg.KeyMapping = true
	rewriteKeysFile(keysBasePath, pairs, true)
	patchConfigField(keysBasePath+ConfigFileExtension, "key_mapping", json.RawMessage("true"))
	os.Rename(logPath, logPath+".bak") //nolint:errcheck
	Library.Progress("Harvest→subset transition: wrote %d key pairs from harvest log; harvest log archived", len(pairs))
}

// maybeMigrateSubsetToHarvest handles a subset→harvest mode transition. When the
// user changes the config from mode="subset" to mode="harvest", the existing .keys
// file is used to pre-populate the harvest log so previously absorbed entries are
// not presented again. The .keys file is archived to .keys.bak.
//
// Detection: harvest log absent + .keys file present.
func maybeMigrateSubsetToHarvest(sourcePath, keysBasePath, logPath string) {
	keysPath := keysBasePath + KeysFileExtension
	if FileExists(logPath) || !FileExists(keysPath) {
		return
	}
	pairs, _, ok := readKeysFile(keysBasePath)
	if !ok || len(pairs) == 0 {
		return
	}
	localToCanon := make(map[string]string, len(pairs))
	for _, p := range pairs {
		localToCanon[p.localKey] = p.canonicalKey
	}
	entries, _ := Library.parseHarvestBib(sourcePath)
	var log THarvestLog
	for _, e := range entries {
		canon, found := localToCanon[e.Key]
		if !found {
			continue
		}
		canon = Library.MapEntryKey(canon)
		if !Library.EntryExists(canon) {
			continue
		}
		log = append(log, THarvestLogEntry{
			OriginalKey:        e.Key,
			TitleHash:          harvestTitleHash(e),
			ContentFingerprint: harvestContentFingerprint(e),
			Action:             canon,
		})
	}
	writeHarvestLog(logPath, log)
	os.Rename(keysPath, keysPath+".bak") //nolint:errcheck
	Library.Progress("Subset→harvest transition: pre-logged %d entries from .keys file", len(log))
}
