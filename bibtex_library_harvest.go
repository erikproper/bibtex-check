/*
 *
 * Module: bibtex_library_harvest
 *
 * Harvest mode (step 14.2): ingest entries from an external bib file as candidates
 * for the main library. Entries are processed sequentially and interactively.
 *
 * Three input sources:
 *   - Watch file: mode="harvest" in bib.config; triggered by -sync; state in .sync DB.
 *   - One-off:    -harvest <path>; no state written.
 *   - Stdin:      -harvest (no path); no state written.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Delta log types and I/O ---

// harvestContentFingerprint returns an MD5 of all non-empty field=value pairs, sorted.
// Used for skip-content tracking in the .sync DB.
func harvestContentFingerprint(e TBibTeXEntry) string {
	return bibContentFingerprint(e)
}

// harvestSkipStatus checks the sync state for e and returns whether it should be
// skipped, and if it was already resolved, returns the canonical key for re-run work.
// Returns (skip, resolvedCanon): skip=true means skip the entry; resolvedCanon is
// non-empty only when the entry was previously resolved (for PDF/hint re-registration).
func harvestSkipStatus(e TBibTeXEntry, syncState *TSyncState, l *TBibTeXLibrary) (skip bool, resolvedCanon string) {
	if syncState == nil || e.Key == "" {
		return false, ""
	}
	status := syncState.GetStatus(e.Key)
	if status == "" {
		return false, ""
	}
	switch {
	case status == SyncStatusWaived:
		return true, ""
	case strings.HasPrefix(status, "skip-content:"):
		fp := strings.TrimPrefix(status, "skip-content:")
		return harvestContentFingerprint(e) == fp, ""
	default:
		canon := l.MapEntryKey(status)
		if l.EntryExists(canon) {
			return true, canon
		}
		return false, ""
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


// transferHarvestKey appends localKey;finalKey to the harvest_transfer target's keys file.
func transferHarvestKey(localKey, finalKey string) {
	if cmdHarvestTransferKeysPath != "" {
		appendPairToKeysFile(cmdHarvestTransferKeysPath, localKey, finalKey)
	}
}

// addToHarvestGroup adds finalKey to cmdHarvestGroup when the flag is set.
func addToHarvestGroup(l *TBibTeXLibrary, finalKey string) {
	if cmdHarvestGroup == "" || finalKey == "" {
		return
	}
	if members := l.GroupEntries[cmdHarvestGroup]; !members.Contains(finalKey) {
		l.GroupEntries.AddValueToStringSetMap(cmdHarvestGroup, finalKey)
		if err := bibExec(`INSERT INTO bib_groups (group_name, entry_key) VALUES (?, ?) ON CONFLICT DO NOTHING;`, cmdHarvestGroup, finalKey); err != nil {
			l.Warning("Group add failed (%s → %q): %s", finalKey, cmdHarvestGroup, err)
		} else {
			l.Progress("Added %s to group %q", finalKey, cmdHarvestGroup)
		}
	}
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

// --- Local groups I/O ---

// readLocalGroups loads a .groups file (group;canonicalKey CSV) into a TStringSetMap.
func readLocalGroups(path string) TStringSetMap {
	result := TStringSetMap{}
	processFile(path, func(line string) {
		parts := strings.SplitN(line, csvDelimiter, 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			result.AddValueToStringSetMap(parts[0], parts[1])
		}
	})
	return result
}

// writeLocalGroups writes a TStringSetMap to a .groups file (group;canonicalKey CSV).
func writeLocalGroups(path string, groups TStringSetMap) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, g := range groupNames {
		members := groups[g] // copy so Elements() can be called (pointer method)
		for key := range members.Elements() {
			fmt.Fprintf(f, "%s%s%s\n", g, csvDelimiter, key)
		}
	}
}

// maybeHarvestGroups imports the JabRef per-entry groups field from a harvested entry.
// Groups in l.harvestSyncGroups are written to the main bib_groups DB table.
// All other groups are added to l.harvestLocalGroups (persisted at end of harvest run).
// The groups field value is a comma-separated list of group names. Idempotent.
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
		if l.harvestSyncGroups.Contains(group) {
			l.GroupEntries.AddValueToStringSetMap(group, canonicalKey)
			if err := bibExec(
				`INSERT INTO bib_groups (group_name, entry_key) VALUES (?, ?) ON CONFLICT DO NOTHING;`,
				group, canonicalKey); err != nil {
				l.Warning("Could not persist group %q for %s: %s", group, canonicalKey, err)
			}
		} else {
			l.harvestLocalGroups.AddValueToStringSetMap(group, canonicalKey)
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
// Writes the outcome to syncState when non-nil (watch mode).
// Returns the resolved canonical key (empty if not resolved) and whether the user quit.
func (l *TBibTeXLibrary) runHarvestEntry(e TBibTeXEntry, syncState *TSyncState) (string, bool) {
	recordStatus := func(status string) {
		if syncState != nil && e.Key != "" {
			syncState.SetStatus(e.Key, status)
		}
	}
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

	// Always show the source entry first.
	fmt.Fprintf(os.Stderr, "\nSource entry:\n")
	printEntryFields(e.Fields[EntryTypeField], e.Key, e.Fields)

	// Step 1: key hint / alias match.
	if keyMatch := l.harvestKeyMatch(e); keyMatch != "" {
		fmt.Fprintf(os.Stderr, "Key match:\n")
		fmt.Fprint(os.Stderr, l.entryDisplayString(keyMatch))
		if cmdTrustHints || l.ConfirmAction(QuestionHarvestKeyMatch) {
			finalKey := mergeAndCheck(addHarvestEntry(l, e), keyMatch)
			maybeCollectKeyHint(l, e.Key, finalKey)
			l.maybeHarvestPDF(e, finalKey)
			l.maybeHarvestGroups(e, finalKey)
			addToHarvestGroup(l, finalKey)
			transferHarvestKey(e.Key, finalKey)
			recordStatus(finalKey)
			return finalKey, false
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
			maybeCollectKeyHint(l, e.Key, finalKey)
			l.maybeHarvestPDF(e, finalKey)
			l.maybeHarvestGroups(e, finalKey)
			addToHarvestGroup(l, finalKey)
			transferHarvestKey(e.Key, finalKey)
			recordStatus(finalKey)
			return finalKey, false
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
			finalKey = l.MapEntryKey(finalKey)
			maybeCollectKeyHint(l, e.Key, finalKey)
			l.maybeHarvestPDF(e, finalKey)
			addToHarvestGroup(l, finalKey)
			transferHarvestKey(e.Key, finalKey)
			recordStatus(finalKey)
			return finalKey, false
		}
	}

	// Step 4: no match — offer add, skip, waive, or quit.
	validActions := TStringSetNew()
	validActions.Add("a").Add("s").Add("w").Add("q")
	switch l.WarningQuestion(QuestionHarvestAction, validActions, "") {
	case "q":
		return "", true
	case "w":
		recordStatus(SyncStatusWaived)
		return "", false
	case "s":
		recordStatus("skip-content:" + harvestContentFingerprint(e))
		return "", false
	default: // "a"
		newKey := addHarvestEntry(l, e)
		l.Progress("Checking %s", l.MapEntryKey(newKey))
		doAllChecks(newKey)
		finalKey := l.MapEntryKey(newKey)
		fixEntry(finalKey)
		maybeCollectKeyHint(l, e.Key, finalKey)
		l.maybeHarvestPDF(e, finalKey)
		addToHarvestGroup(l, finalKey)
		transferHarvestKey(e.Key, finalKey)
		recordStatus(finalKey)
		return finalKey, false
	}
}

// runHarvestLoop processes entries interactively in source-file order.
// Entries already handled (resolved, waived, or skip-content unchanged) are silently
// skipped when syncState is non-nil. For previously resolved entries, PDF sync and
// key-hint registration are re-run on each pass.
func (l *TBibTeXLibrary) runHarvestLoop(entries []TBibTeXEntry, syncState *TSyncState) {
	stepN := int(cmdStep)
	questionCounter := 0

	for _, e := range entries {
		skip, resolvedCanon := harvestSkipStatus(e, syncState, l)
		if skip {
			if resolvedCanon != "" {
				l.maybeHarvestPDF(e, resolvedCanon)
				l.maybeHarvestGroups(e, resolvedCanon)
				if e.Key != "" {
					l.AddKeyHint(e.Key, resolvedCanon)
					transferHarvestKey(e.Key, resolvedCanon)
				}
			}
			continue
		}
		_, quit := l.runHarvestEntry(e, syncState)
		if quit {
			return
		}
		if stepN > 0 {
			questionCounter++
			if questionCounter >= stepN {
				if l.AskContinueOrQuit() {
					return
				}
				questionCounter = 0
			}
		}
	}
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

	Library.runHarvestLoop(entries, nil)
}

// runHarvestSync is called from doSync when mode == "harvest". The library is already open.
// State (resolved/waived/skip-content) is tracked in the .sync DB next to the source bib.
func runHarvestSync(cfg TBibGetConfig, baseDir string) {
	if cfg.TrustHints {
		cmdTrustHints = true
	}
	if cfg.CollectKeys {
		cmdCollectKeys = true
	}

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
	localGroupsPath := keysBasePath + LocalGroupsExtension

	syncState := openSyncState(keysBasePath)
	defer syncState.close()

	syncGroups := TStringSetNew()
	for _, g := range cfg.SyncGroups {
		syncGroups.Add(g)
	}
	Library.harvestSyncGroups = syncGroups
	Library.harvestLocalGroups = readLocalGroups(localGroupsPath)

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

	entries, parseOK := Library.parseHarvestBib(sourcePath)
	if !parseOK {
		Library.Progress("  Harvest sync aborted: fix the source bib and re-run.")
		return
	}
	if len(entries) == 0 {
		Library.Progress("  Source: no entries found")
		return
	}

	skipped := 0
	for _, e := range entries {
		if skip, _ := harvestSkipStatus(e, syncState, &Library); skip {
			skipped++
		}
	}
	pending := len(entries) - skipped
	Library.Progress("  Source: %d entr%s total; %d synced, %d pending",
		len(entries), map[bool]string{true: "y", false: "ies"}[len(entries) == 1],
		skipped, pending)

	pdfsBefore := len(Library.PDFFiles)
	hintsBefore := len(Library.newKeyHints)
	Library.runHarvestLoop(entries, syncState)
	pdfsHarvested := len(Library.PDFFiles) - pdfsBefore
	hintsAdded := len(Library.newKeyHints) - hintsBefore

	resolved, skippedContent, waived, stillPending := 0, 0, 0, 0
	for _, e := range entries {
		if e.Key == "" {
			continue
		}
		status := syncState.GetStatus(e.Key)
		switch {
		case status == "":
			stillPending++
		case status == SyncStatusWaived:
			waived++
		case strings.HasPrefix(status, "skip-content:"):
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
	Library.Progress("  Waived                  : %d", waived)
	Library.Progress("  Pending (not yet seen)  : %d", stillPending)
	Library.Progress("  PDFs harvested          : %d", pdfsHarvested)
	Library.Progress("  Key hints added         : %d", hintsAdded)
	writeLocalGroups(localGroupsPath, Library.harvestLocalGroups)
	Library.harvestSyncGroups = TStringSetNew()
	Library.harvestLocalGroups = TStringSetMap{}
}

// --- Config patching ---

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
