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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// --- Subset state file (.subset) ---

// TSubsetEntry is one row of the .subset common-ancestor fingerprint file.
// Both db_hash and bib_hash are bibContentFingerprint values.
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

// dbHashForEntry computes the bibContentFingerprint for a library entry from the DB.
func dbHashForEntry(canonicalKey string) string {
	e := loadEntryFromDb(canonicalKey)
	if e == nil {
		return ""
	}
	return bibContentFingerprint(*e)
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
	fmt.Fprintf(os.Stderr, "\nSubset bib entry (changed):\n")
	printEntryFields(bibEntry.Fields[EntryTypeField], bibEntry.Key, bibEntry.Fields)
	fmt.Fprintf(os.Stderr, "Library entry:\n")
	fmt.Fprint(os.Stderr, Library.entryDisplayString(canonicalKey))

	if !Library.ConfirmAction(QuestionSubsetBibChanged) {
		return canonicalKey
	}
	tempKey := addHarvestEntry(&Library, bibEntry)
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

// runSubsetSync is called from doSync when mode == "subset". The library is already open.
// Maintains the .subset fingerprint file next to the source bib.
func runSubsetSync(cfg TBibGetConfig, baseDir string) {
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
	statePath := keysBasePath + SubsetStateExtension

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
		return
	}

	runSubsetUpdate(cfg, sourcePath, keysBasePath, statePath, existingState)
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
	rewriteKeysFile(keysBasePath, writtenPairs)
	Library.Progress("  Written .keys: %d entries", len(writtenPairs))

	// Build initial state: bib_hash == db_hash on a clean export.
	now := time.Now().Format(time.RFC3339)
	newState := TSubsetState{}
	for _, p := range writtenPairs {
		h := dbHashForEntry(p.canonicalKey)
		newState[p.canonicalKey] = TSubsetEntry{
			CanonicalKey: p.canonicalKey,
			OutputKey:    p.localKey,
			DBHash:       h,
			BibHash:      h,
			Timestamp:    now,
		}
	}
	writeSubsetState(statePath, newState)
	Library.Progress("  Written .subset state: %d entries", len(newState))
}

// runSubsetUpdate handles subsequent syncs: .subset file exists, detect three-way changes.
func runSubsetUpdate(cfg TBibGetConfig, sourcePath, keysBasePath, statePath string, existingState TSubsetState) {
	Library.Progress("  State  : %s (%d entries)", statePath, len(existingState))

	// Parse the externally-edited bib.
	bibEntries := Library.parseHarvestBib(sourcePath)
	Library.Progress("  Source : %d entries parsed", len(bibEntries))

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

		if canonical == "" || !Library.EntryExists(canonical) {
			toProcess = append(toProcess, categorizedEntry{bibEntry: e, status: statusNew})
			continue
		}

		bibSeenCanonicals[canonical] = true
		se := existingState[canonical]
		currentBibHash := bibContentFingerprint(e)
		currentDBHash := dbHashForEntry(canonical)

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

	// Re-export the bib (reflects all DB changes made above) and update the state.
	writtenPairs := writePullSync(cfg, "")
	if writtenPairs == nil {
		return
	}

	// Persist the updated keys list.
	rewriteKeysFile(keysBasePath, writtenPairs)

	// Recompute state from the freshly written pairs.
	now := time.Now().Format(time.RFC3339)
	newState := TSubsetState{}
	for _, p := range writtenPairs {
		h := dbHashForEntry(p.canonicalKey)
		newState[p.canonicalKey] = TSubsetEntry{
			CanonicalKey: p.canonicalKey,
			OutputKey:    p.localKey,
			DBHash:       h,
			BibHash:      h, // bib was just regenerated from DB
			Timestamp:    now,
		}
	}
	writeSubsetState(statePath, newState)
	Library.Progress("  Updated .subset state: %d entries", len(newState))
}
