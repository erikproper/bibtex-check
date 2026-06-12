/*
 *
 * Module: bibtex_library_subset
 *
 * Subset sync mode (step 14.3): bidirectional sync between the main library and an
 * externally edited bib file covering a subset of entries.
 *
 * Three-way change detection using a .subset fingerprint file (common-ancestor snapshot).
 * With trusted_subset: true, changes are applied without confirmation.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// subsetSyncBailOut finalises the working database and exits cleanly.
// Called when the user explicitly rejects a merge that would silently drop fields.
func subsetSyncBailOut() {
	Library.Progress("Bailing out — finalising database.")
	finaliseWorkingDatabase()
	os.Exit(0)
}

// --- Subset state file (.subset) ---

// TSubsetEntry is one row of the .subset common-ancestor fingerprint file.
// db_hash is computed by subsetDBFingerprint; bib_hash by subsetBibFingerprint.
// On a fresh export they are equal; they diverge when either side is edited.
type TSubsetEntry struct {
	CanonicalKey string // canonical library key
	OutputKey    string // local key written to the bib file
	DBHash       string // fingerprint of the DB entry at last sync
	BibHash      string // fingerprint of the bib entry at last sync
	Timestamp    string // RFC3339 timestamp of last sync
}

// TSubsetState is the in-memory snapshot of the .subset file, keyed by canonical key.
type TSubsetState map[string]TSubsetEntry

// readSubsetState reads a .subset file; returns an empty state when absent.
func readSubsetState(statePath string) TSubsetState {
	state := TSubsetState{}
	processFile(statePath, func(line string) {
		parts := strings.SplitN(line, csvDelimiter, 5)
		if len(parts) != 5 {
			return
		}
		state[parts[0]] = TSubsetEntry{
			CanonicalKey: parts[0],
			OutputKey:    parts[1],
			DBHash:       parts[2],
			BibHash:      parts[3],
			Timestamp:    parts[4],
		}
	})
	return state
}

// writeSubsetState writes the .subset file from the in-memory state, sorted by canonical key.
func writeSubsetState(statePath string, state TSubsetState) {
	lines := make([]string, 0, len(state))
	for _, e := range state {
		lines = append(lines, strings.Join([]string{
			e.CanonicalKey, e.OutputKey, e.DBHash, e.BibHash, e.Timestamp,
		}, csvDelimiter))
	}
	sort.Strings(lines)
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(statePath, []byte(content), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "Cannot write subset state file:", err)
	}
}

// subsetDBFingerprint computes the fingerprint of a DB entry for subset change detection.
// Excludes editor-noise fields (same as bibEditorNoiseFields) but includes all content
// fields — including preferredalias and local-url — to match what the subset bib writes.
func subsetDBFingerprint(canonicalKey string) string {
	e := loadEntryFromDb(canonicalKey)
	if e == nil {
		return ""
	}
	fields := make([]string, 0, len(e.Fields))
	for f, v := range e.Fields {
		if v != "" && !bibEditorNoiseFields.Contains(f) {
			fields = append(fields, f+"="+v)
		}
	}
	sort.Strings(fields)
	h := md5.Sum([]byte(strings.Join(fields, ";")))
	return hex.EncodeToString(h[:])
}

// subsetBibFingerprint computes the fingerprint of a parsed bib entry for subset change
// detection. Normalizes the crossref field from the local output key to the canonical key
// using outputToCanonical, making it comparable with subsetDBFingerprint.
func subsetBibFingerprint(e TBibTeXEntry, outputToCanonical map[string]string) string {
	fields := make([]string, 0, len(e.Fields))
	for f, v := range e.Fields {
		if v == "" || bibEditorNoiseFields.Contains(f) {
			continue
		}
		if f == "crossref" {
			if canonical, ok := outputToCanonical[v]; ok {
				v = canonical
			}
		}
		fields = append(fields, f+"="+v)
	}
	sort.Strings(fields)
	h := md5.Sum([]byte(strings.Join(fields, ";")))
	return hex.EncodeToString(h[:])
}

// --- Update helpers ---

// applySubsetBibToDb merges bibEntry fields into the canonical library entry, either
// interactively (via MergeEntries field challenges) or silently (trusted_subset).
// Returns the final canonical key after the merge.
func applySubsetBibToDb(bibEntry TBibTeXEntry, canonicalKey string, trusted bool) string {
	if trusted {
		dbEntry := loadEntryFromDb(canonicalKey)
		if dbEntry == nil {
			return canonicalKey
		}
		for field, value := range bibEntry.Fields {
			if !bibEditorNoiseFields.Contains(field) && field != EntryTypeField {
				Library.setEntryField(dbEntry, field, value)
			}
		}
		return canonicalKey
	}

	// Interactive: add bib entry as a temp library entry, merge via field challenges.
	// Strip bibEditorNoiseFields (including local-url) before display and merge so
	// that file-derived fields never appear as challenges against the DB entry.
	cleanEntry := TBibTeXEntry{Key: bibEntry.Key, Fields: make(map[string]string, len(bibEntry.Fields))}
	for f, v := range bibEntry.Fields {
		if !bibEditorNoiseFields.Contains(f) {
			cleanEntry.Fields[f] = v
		}
	}

	// Check for fields in the bib entry that are not allowed for the entry type.
	// MergeEntries only challenges fields in BibTeXAllowedEntryFields[entryType],
	// so any others would be silently discarded. Offer a bail-out before proceeding.
	entryType := cleanEntry.Fields[EntryTypeField]
	if allowedFields, known := BibTeXAllowedEntryFields[entryType]; known {
		var illegalFields []string
		for f := range cleanEntry.Fields {
			if f != EntryTypeField && !allowedFields.Set().Contains(f) {
				illegalFields = append(illegalFields, f)
			}
		}
		if len(illegalFields) > 0 {
			sort.Strings(illegalFields)
			for _, f := range illegalFields {
				fmt.Fprintf(os.Stderr, "WARNING: entry %s has field %q which is not allowed for entry type %q — it will be ignored during merge.\n", canonicalKey, f, entryType)
			}
			if !Library.ConfirmAction("Proceed anyway (illegal fields will be dropped)") {
				subsetSyncBailOut()
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\nSubset bib entry (changed):\n")
	printEntryFields(cleanEntry.Fields[EntryTypeField], cleanEntry.Key, cleanEntry.Fields)
	fmt.Fprintf(os.Stderr, "Library entry:\n")
	fmt.Fprint(os.Stderr, Library.entryDisplayString(canonicalKey))

	if !Library.ConfirmAction(QuestionSubsetBibChanged) {
		return canonicalKey
	}
	tempKey := addHarvestEntry(&Library, cleanEntry)
	Library.MergeEntries(tempKey, canonicalKey)
	return Library.MapEntryKey(canonicalKey)
}

// deleteSubsetEntry removes a library entry that was deleted from the subset bib.
// With trusted_subset, deletes silently; otherwise prompts.
func deleteSubsetEntry(canonicalKey string, trusted bool) bool {
	if !trusted {
		fmt.Fprintf(os.Stderr, "\nEntry deleted from subset bib:\n")
		fmt.Fprint(os.Stderr, Library.entryDisplayString(canonicalKey))
		if !Library.ConfirmAction(QuestionSubsetDeleteEntry) {
			return false
		}
	}
	deleteBibEntry(canonicalKey)
	return true
}

// --- Top-level subset sync ---

// resolveSubsetPaths derives the bib source path, keys base, and state file path from cfg.
func resolveSubsetPaths(cfg TBibGetConfig, baseDir string) (sourcePath, keysBasePath, statePath string) {
	sourcePath = cfg.FileName
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
	keysBasePath = strings.TrimSuffix(sourcePath, filepath.Ext(sourcePath))
	statePath = keysBasePath + SubsetStateExtension
	return
}

// runSubsetPhase1 is called from doSync phase 1: parses the subset bib and merges any
// bib-side changes into the DB. Returns true when phase 2 must be skipped: either a
// fresh export was performed (bib already written) or the up-sync was aborted (e.g.
// parse error — bib must not be overwritten).
func runSubsetPhase1(cfg TBibGetConfig, baseDir string) bool {
	sourcePath, keysBasePath, statePath := resolveSubsetPaths(cfg, baseDir)

	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	Library.Progress("Sync subset: %s", cfg.FileName)
	Library.Progress("  trusted_subset=%-3s  pdf_files=%q", on(cfg.TrustedSubset), cfg.PDFFiles)

	existingState := readSubsetState(statePath)

	if len(existingState) == 0 {
		runSubsetFreshExport(cfg, baseDir, sourcePath, keysBasePath, statePath)
		return true
	}

	aborted := runSubsetUpSync(cfg, sourcePath, keysBasePath, statePath, existingState)
	return aborted
}

// buildSubsetState constructs the .subset state after a bib write. It re-parses the
// written bib to compute BibHash from the actual file content, so field exclusions
// in entryGetString (dblp, repositum, etc.) don't cause spurious bib-changed detections
// on subsequent syncs.
func buildSubsetState(writtenPairs []TBibGetPair, sourcePath string) TSubsetState {
	outputToCanonical := map[string]string{}
	for _, p := range writtenPairs {
		outputToCanonical[p.localKey] = p.canonicalKey
	}

	bibEntries, _ := Library.parseHarvestBib(sourcePath)
	bibHashByLocalKey := map[string]string{}
	for _, e := range bibEntries {
		bibHashByLocalKey[e.Key] = subsetBibFingerprint(e, outputToCanonical)
	}

	now := time.Now().Format(time.RFC3339)
	state := TSubsetState{}
	for _, p := range writtenPairs {
		dbHash := subsetDBFingerprint(p.canonicalKey)
		bibHash := bibHashByLocalKey[p.localKey]
		if bibHash == "" {
			bibHash = dbHash
		}
		state[p.canonicalKey] = TSubsetEntry{
			CanonicalKey: p.canonicalKey,
			OutputKey:    p.localKey,
			DBHash:       dbHash,
			BibHash:      bibHash,
			Timestamp:    now,
		}
	}
	return state
}

// runSubsetPhase2 is called from doSync phase 2: re-exports the subset bib from the
// (now fully updated) DB and writes the new .subset state file.
func runSubsetPhase2(cfg TBibGetConfig, baseDir string) {
	sourcePath, keysBasePath, statePath := resolveSubsetPaths(cfg, baseDir)

	writtenPairs := writePullSync(cfg, baseDir)
	if writtenPairs == nil {
		return
	}
	rewriteKeysFile(keysBasePath, writtenPairs, cfg.KeyMapping)

	newState := buildSubsetState(writtenPairs, sourcePath)
	writeSubsetState(statePath, newState)
	Library.Progress("  Updated .subset state: %d entries", len(newState))
}

// runSubsetFreshExport handles the first-run case: no prior .subset file.
func runSubsetFreshExport(cfg TBibGetConfig, baseDir, sourcePath, keysBasePath, statePath string) {
	Library.Progress("  State  : %s (new)", statePath)

	writtenPairs := writePullSync(cfg, baseDir)
	if writtenPairs == nil {
		return // user declined to overwrite the bib
	}

	// Write all resolved pairs (explicit .keys + .select + auto-parents) back to the
	// .keys file so it becomes the complete, stable record for subsequent syncs.
	rewriteKeysFile(keysBasePath, writtenPairs, cfg.KeyMapping)
	Library.Progress("  Written .keys: %d entries", len(writtenPairs))

	// Build initial state by re-parsing the written bib for accurate BibHash values.
	newState := buildSubsetState(writtenPairs, sourcePath)
	writeSubsetState(statePath, newState)
	Library.Progress("  Written .subset state: %d entries", len(newState))
}

// runSubsetUpSync handles phase 1 of subsequent syncs: parses the bib, detects three-way
// changes, and merges bib-side edits into the DB. Does not write back the bib or state.
// Returns true if the sync was aborted (e.g. parse error) — caller must skip phase 2.
func runSubsetUpSync(cfg TBibGetConfig, sourcePath, keysBasePath, statePath string, existingState TSubsetState) bool {
	Library.Progress("  State  : %s (%d entries)", statePath, len(existingState))

	// Parse the externally-edited bib. Abort if the parse is incomplete — a partial
	// result would cause healthy entries to be misclassified as deleted.
	bibEntries, parseOK := Library.parseHarvestBib(sourcePath)
	Library.Progress("  Source : %d entries parsed", len(bibEntries))
	if !parseOK {
		Library.Progress("  Subset sync aborted: fix the bib file and re-run.")
		return true
	}

	// Build reverse map: output key → state entry, for matching bib entries back to
	// canonical library keys.
	outputToCanonical := map[string]string{}
	for canonical, se := range existingState {
		outputToCanonical[se.OutputKey] = canonical
	}

	type subsetStatus int
	const (
		statusUnchanged  subsetStatus = iota
		statusBibChanged              // bib edited, DB same
		statusDBChanged               // DB updated, bib same
		statusBothChanged             // both edited
		statusNew                     // in bib but not in state
	)

	type categorizedEntry struct {
		bibEntry       TBibTeXEntry
		canonicalKey   string
		stateEntry     TSubsetEntry
		currentBibHash string
		currentDBHash  string
		status         subsetStatus
	}

	var toProcess []categorizedEntry
	bibSeenCanonicals := map[string]bool{}
	stepN := int(cmdStep)
	questions := 0

	for _, e := range bibEntries {
		// Match bib entry back to its canonical library key via output key or alias lookup.
		canonical := outputToCanonical[e.Key]
		if canonical == "" {
			canonical = Library.harvestKeyMatch(e)
		}

		// The canonical from state may have become an alias since the last sync
		// (key added to key_oldies). Resolve to the current canonical so the entry
		// is not misclassified as new/deleted.
		stateKey := canonical
		if canonical != "" && !Library.EntryExists(canonical) {
			if resolved := Library.MapEntryKey(canonical); Library.EntryExists(resolved) {
				canonical = resolved
			}
		}

		if canonical == "" || !Library.EntryExists(canonical) {
			toProcess = append(toProcess, categorizedEntry{bibEntry: e, status: statusNew})
			continue
		}

		// Mark the original state key as seen so it isn't treated as deleted.
		if stateKey != "" {
			bibSeenCanonicals[stateKey] = true
		}
		bibSeenCanonicals[canonical] = true

		// Entry is a known library entry but not yet in the subset state — it was
		// auto-included as a crossref parent, not edited by the user. Accept silently;
		// phase 2 will write it to the bib and add it to the state.
		se, inState := existingState[stateKey]
		if !inState {
			se, inState = existingState[canonical]
		}
		if !inState {
			toProcess = append(toProcess, categorizedEntry{
				bibEntry:     e,
				canonicalKey: canonical,
				status:       statusUnchanged,
			})
			continue
		}
		currentBibHash := subsetBibFingerprint(e, outputToCanonical)
		currentDBHash := subsetDBFingerprint(canonical)

		bibChanged := currentBibHash != se.BibHash
		dbChanged := currentDBHash != se.DBHash

		var status subsetStatus
		switch {
		case !bibChanged && !dbChanged:
			status = statusUnchanged
		case bibChanged && !dbChanged:
			status = statusBibChanged
		case !bibChanged && dbChanged:
			status = statusDBChanged
		default:
			status = statusBothChanged
		}

		toProcess = append(toProcess, categorizedEntry{
			bibEntry:       e,
			canonicalKey:   canonical,
			stateEntry:     se,
			currentBibHash: currentBibHash,
			currentDBHash:  currentDBHash,
			status:         status,
		})
	}

	// Identify deleted entries: in state but not seen in parsed bib.
	var deletedCanonicals []string
	for canonical := range existingState {
		if !bibSeenCanonicals[canonical] {
			deletedCanonicals = append(deletedCanonicals, canonical)
		}
	}
	sort.Strings(deletedCanonicals)

	// Summary.
	counts := map[subsetStatus]int{}
	for _, c := range toProcess {
		counts[c.status]++
	}
	Library.Progress("  Changes: %d unchanged, %d bib-changed, %d db-changed, %d both-changed, %d new, %d deleted",
		counts[statusUnchanged], counts[statusBibChanged], counts[statusDBChanged],
		counts[statusBothChanged], counts[statusNew], len(deletedCanonicals))

	quit := false

	// Process bib-changed entries.
	for _, c := range toProcess {
		if quit || (stepN > 0 && questions >= stepN) {
			break
		}
		if c.status != statusBibChanged && c.status != statusBothChanged {
			continue
		}
		if c.status == statusBothChanged && !cfg.TrustedSubset {
			fmt.Fprintf(os.Stderr, "\nBoth bib and DB changed for %s.\n", c.canonicalKey)
			fmt.Fprintf(os.Stderr, "DB entry:\n")
			fmt.Fprint(os.Stderr, Library.entryDisplayString(c.canonicalKey))
			fmt.Fprintf(os.Stderr, "Bib entry:\n")
			printEntryFields(c.bibEntry.Fields[EntryTypeField], c.bibEntry.Key, c.bibEntry.Fields)
			if !Library.ConfirmAction(QuestionSubsetBothChanged) {
				continue // keep DB version
			}
		}
		applySubsetBibToDb(c.bibEntry, c.canonicalKey, cfg.TrustedSubset)
		questions++
	}

	// Process new entries via harvest pipeline.
	if !quit {
		for _, c := range toProcess {
			if quit || (stepN > 0 && questions >= stepN) {
				break
			}
			if c.status != statusNew {
				continue
			}
			var updated THarvestLog
			updated, quit = Library.runHarvestEntry(c.bibEntry, nil, false)
			// If the entry was added/merged, its canonical key landed in the log action.
			if len(updated) > 0 && updated[0].Action != harvestActionSkipContent && updated[0].Action != harvestActionSkipNever {
				bibSeenCanonicals[updated[0].Action] = true
			}
			questions++
		}
	}

	// Process deleted entries.
	if !quit {
		for _, canonical := range deletedCanonicals {
			if quit || (stepN > 0 && questions >= stepN) {
				break
			}
			if deleteSubsetEntry(canonical, cfg.TrustedSubset) {
				delete(existingState, canonical)
			}
			if !cfg.TrustedSubset {
				questions++
			}
		}
	}

	return false
}
