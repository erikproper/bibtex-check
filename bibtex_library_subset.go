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

// subsetFingerprintExclude is the set of fields excluded from subset fingerprints.
// local-url is excluded because it is derived from the filesystem (PDFFiles map),
// not stored in the DB, so it never contributes to a genuine content change.
// EntryTypeField is intentionally included so that changing e.g. @inproceedings to
// @incollection in the subset bib is detected as a real edit.
var subsetFingerprintExclude = func() TStringSet {
	s := TStringSetNew()
	s.Set().Unite(bibEditorNoiseFields)
	s.Add(LocalURLField)
	return s
}()

// dealiasCanonical resolves a crossref value to its current canonical key.
// Tries outputToCanonical first (covers keys in the current subset state), then
// falls back to MapEntryKey (covers crossref targets whose canonical changed after
// the state was written, e.g. due to a merge between syncs).
func dealiasCanonical(v string, outputToCanonical map[string]string) string {
	if canonical, ok := outputToCanonical[v]; ok {
		return canonical
	}
	if resolved := Library.MapEntryKey(v); resolved != "" && Library.EntryExists(resolved) {
		return resolved
	}
	return v
}

// subsetDBFingerprint computes the fingerprint of a DB entry for subset change detection.
// The crossref field is de-aliased to the current canonical key so that a crossref
// parent being merged does not spuriously diverge the DB fingerprint.
func subsetDBFingerprint(canonicalKey string) string {
	e := loadEntryFromDb(canonicalKey)
	if e == nil {
		return ""
	}
	fields := make([]string, 0, len(e.Fields))
	for f, v := range e.Fields {
		if v == "" || subsetFingerprintExclude.Set().Contains(f) {
			continue
		}
		if f == "crossref" {
			v = dealiasCanonical(v, nil)
		}
		fields = append(fields, f+"="+v)
	}
	sort.Strings(fields)
	h := md5.Sum([]byte(strings.Join(fields, ";")))
	return hex.EncodeToString(h[:])
}

// subsetBibFingerprint computes the fingerprint of a parsed bib entry for subset change
// detection. The crossref field is de-aliased to the current canonical key using both
// outputToCanonical (state-based) and MapEntryKey (covers post-state merges), making it
// directly comparable with subsetDBFingerprint.
func subsetBibFingerprint(e TBibTeXEntry, outputToCanonical map[string]string) string {
	fields := make([]string, 0, len(e.Fields))
	for f, v := range e.Fields {
		if v == "" || subsetFingerprintExclude.Set().Contains(f) {
			continue
		}
		if f == "crossref" {
			v = dealiasCanonical(v, outputToCanonical)
		}
		fields = append(fields, f+"="+v)
	}
	sort.Strings(fields)
	h := md5.Sum([]byte(strings.Join(fields, ";")))
	return hex.EncodeToString(h[:])
}

// bibReflectsDB returns true when the parsed bib entry's content matches what
// entryGetString would produce from the current DB — i.e. the bib was faithfully
// generated from the DB and no user edit has occurred. Used to suppress "bib changed"
// prompts caused by stale state fingerprints (e.g. after a bib output format change).
//
// The DB side applies MapEntryFieldValue (same mapping entryGetString uses) so that
// both sides are in the same normalised form.
func bibReflectsDB(bibEntry TBibTeXEntry, canonicalKey string, outputToCanonical map[string]string) bool {
	dbEntry := loadEntryFromDb(canonicalKey)
	if dbEntry == nil {
		return false
	}

	bibFields := make([]string, 0, len(bibEntry.Fields))
	for f, v := range bibEntry.Fields {
		if v == "" || subsetFingerprintExclude.Set().Contains(f) {
			continue
		}
		if f == "crossref" {
			v = dealiasCanonical(v, outputToCanonical)
		}
		bibFields = append(bibFields, f+"="+v)
	}
	sort.Strings(bibFields)

	dbFields := make([]string, 0, len(dbEntry.Fields))
	for f, v := range dbEntry.Fields {
		if v == "" || subsetFingerprintExclude.Set().Contains(f) {
			continue
		}
		if f == "crossref" {
			v = dealiasCanonical(v, nil)
		} else {
			v = Library.MapEntryFieldValue(canonicalKey, f, v)
		}
		dbFields = append(dbFields, f+"="+v)
	}
	sort.Strings(dbFields)

	return strings.Join(bibFields, ";") == strings.Join(dbFields, ";")
}

// --- Update helpers ---

// subsetFieldsToClear returns the sorted list of fields that are non-empty in the DB
// entry but absent or empty in the bib entry. These are fields the user intentionally
// removed from the bib; they should be cleared from the DB as part of the merge.
// Noise fields and structural fields (EntryType, LocalURL) are excluded.
func subsetFieldsToClear(cleanFields map[string]string, dbEntry *TBibTeXEntry) []string {
	var clear []string
	for field, dbValue := range dbEntry.Fields {
		if dbValue == "" {
			continue
		}
		if field == EntryTypeField || field == LocalURLField {
			continue
		}
		if bibEditorNoiseFields.Contains(field) {
			continue
		}
		if cleanFields[field] == "" {
			clear = append(clear, field)
		}
	}
	sort.Strings(clear)
	return clear
}

// applySubsetEntryType applies a type change from the subset bib directly to the DB
// entry, bypassing MergeEntries priority logic. The lineage for EntryTypeField is
// cleared so a subsequent DBLP sync presents the type as a normal challenge rather
// than silently winning on priority.
func applySubsetEntryType(bibType, canonicalKey string, dbEntry *TBibTeXEntry) {
	if bibType == "" || bibType == Library.EntryType(canonicalKey) {
		return
	}
	Library.Progress("Entry type changed: %s → %s for %s (subset bib edit)", Library.EntryType(canonicalKey), bibType, canonicalKey)
	Library.SetEntryFieldValue(canonicalKey, EntryTypeField, bibType)
	Library.setLineage(canonicalKey, EntryTypeField, "", false) // clear lineage
	if dbEntry != nil {
		dbEntry.Fields[EntryTypeField] = bibType
	}
}

// applySubsetBibToDb merges bibEntry fields into the canonical library entry, either
// interactively (via MergeEntries field challenges) or silently (trusted_subset).
// Fields absent from the bib but present in the DB are cleared — they were intentionally
// removed by the user.
// Returns the final canonical key after the merge.
func applySubsetBibToDb(bibEntry TBibTeXEntry, canonicalKey string, trusted bool) string {
	if trusted {
		dbEntry := loadEntryFromDb(canonicalKey)
		if dbEntry == nil {
			return canonicalKey
		}
		// Apply entry type directly (excluded from the field loop below).
		applySubsetEntryType(bibEntry.Fields[EntryTypeField], canonicalKey, dbEntry)
		cleanFields := map[string]string{}
		for field, value := range bibEntry.Fields {
			if !bibEditorNoiseFields.Contains(field) && field != EntryTypeField {
				Library.setEntryField(dbEntry, field, value)
				cleanFields[field] = value
			}
		}
		for _, field := range subsetFieldsToClear(cleanFields, dbEntry) {
			Library.deleteEntryField(dbEntry, field)
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

	dbEntry := loadEntryFromDb(canonicalKey)

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

	toClear := subsetFieldsToClear(cleanEntry.Fields, dbEntry)
	newType := cleanEntry.Fields[EntryTypeField]

	fmt.Fprintf(os.Stderr, "\nSubset bib entry (changed):\n")
	printEntryFields(cleanEntry.Fields[EntryTypeField], cleanEntry.Key, cleanEntry.Fields)
	fmt.Fprintf(os.Stderr, "Library entry:\n")
	fmt.Fprint(os.Stderr, Library.entryDisplayString(canonicalKey))
	if len(toClear) > 0 {
		fmt.Fprintf(os.Stderr, "Fields to clear: %s\n", strings.Join(toClear, ", "))
	}

	if !Library.ConfirmAction(QuestionSubsetBibChanged) {
		return canonicalKey
	}
	// Apply type change before MergeEntries — otherwise priority logic silently
	// keeps a higher-priority (e.g. DBLP) type and never asks the user.
	applySubsetEntryType(newType, canonicalKey, dbEntry)
	tempKey := addHarvestEntry(&Library, cleanEntry)
	Library.MergeEntries(tempKey, canonicalKey)
	finalKey := Library.MapEntryKey(canonicalKey)
	if len(toClear) > 0 && dbEntry != nil {
		for _, field := range toClear {
			Library.Progress("Clearing field %q from %s (removed from subset bib)", field, finalKey)
			deleteBibEntryField(finalKey, field)
		}
	}
	return finalKey
}

// deleteSubsetEntry removes a library entry that was deleted from the subset bib.
// With trusted_subset, deletes silently; otherwise prompts.
// When localFilesDir and outputKey are non-empty, the local PDF copy (if any) is
// moved to <stem>.trash/ alongside the local .files/ folder.
func deleteSubsetEntry(canonicalKey, localFilesDir, outputKey string, trusted bool) bool {
	if !trusted {
		fmt.Fprintf(os.Stderr, "\nEntry deleted from subset bib:\n")
		fmt.Fprint(os.Stderr, Library.entryDisplayString(canonicalKey))
		if !Library.ConfirmAction(QuestionSubsetDeleteEntry) {
			return false
		}
	}
	deleteBibEntry(canonicalKey)
	if localFilesDir != "" && outputKey != "" {
		pdfPath := localFilesDir + outputKey + ".pdf"
		if FileExists(pdfPath) {
			trashDir := strings.TrimSuffix(strings.TrimSuffix(localFilesDir, "/"), ".files") + ".trash/"
			_ = os.MkdirAll(trashDir, 0755)
			dest := trashDir + outputKey + ".pdf"
			if FileExists(dest) {
				dest = trashDir + outputKey + "-" + time.Now().Format("20060102-150405") + ".pdf"
			}
			if err := os.Rename(pdfPath, dest); err != nil {
				Library.Warning("Could not move local PDF %s.pdf to trash: %s", outputKey, err)
			}
		}
	}
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
// runSubsetPhase1 is called from doSync phase 1: parses the subset bib and merges any
// bib-side changes into the DB. Returns (skipPhase2, syncState).
// skipPhase2 is true when a fresh export was performed or the up-sync was aborted.
// syncState must be passed to runSubsetPhase2 so it can be populated and closed there.
func runSubsetPhase1(cfg TBibGetConfig, baseDir string) (bool, *TSyncState) {
	sourcePath, keysBasePath, statePath := resolveSubsetPaths(cfg, baseDir)
	// Reset per-file metadata blocks; populated by parseHarvestBib or transition parse.
	Library.jabrefGroupingBlock = ""
	Library.jabrefMetaBlocks = nil
	Library.bibdeskMetaBlocks = nil
	logPath := keysBasePath + HarvestLogExtension
	maybeMigrateHarvestToSubset(&cfg, keysBasePath, statePath, logPath)

	on := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	Library.Progress("Sync subset: %s", cfg.FileName)
	Library.Progress("  trusted_subset=%-3s  pdf_files=%-8q  fix=%-3s", on(cfg.TrustedSubset), cfg.PDFFiles, on(cmdFix))

	syncState := openSyncState(keysBasePath)

	existingState := readSubsetState(statePath)

	if len(existingState) == 0 && (syncState == nil || len(syncState.entries) == 0) {
		runSubsetFreshExport(cfg, baseDir, sourcePath, keysBasePath, statePath, syncState)
		return true, syncState
	}

	aborted := runSubsetUpSync(cfg, sourcePath, keysBasePath, statePath, existingState, syncState)
	return aborted, syncState
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

// buildEntryGroupsFromSyncState builds the runtime entryGroups map from the sync
// state: canonical key → sorted slice of all group names (managed + local).
// Called before writePullSync so entryGetString can emit all groups per entry.
func buildEntryGroupsFromSyncState(syncState *TSyncState) map[string][]string {
	result := make(map[string][]string)
	if syncState == nil {
		return result
	}
	for _, canonKey := range syncState.keys() {
		se := syncState.get(canonKey)
		if se == nil {
			continue
		}
		var groups []string
		for g := range se.Groups.Elements() {
			groups = append(groups, g)
		}
		sort.Strings(groups)
		result[canonKey] = groups
	}
	return result
}

// runSubsetPhase2 is called from doSync phase 2: re-exports the subset bib from the
// (now fully updated) DB, writes the new .subset state file, and closes the sync state.
func runSubsetPhase2(cfg TBibGetConfig, baseDir string, syncState *TSyncState) {
	sourcePath, keysBasePath, statePath := resolveSubsetPaths(cfg, baseDir)

	cfg.entryGroups = buildEntryGroupsFromSyncState(syncState)
	writtenPairs := writePullSync(cfg, baseDir)
	if writtenPairs == nil {
		syncState.close()
		return
	}
	rewriteKeysFile(keysBasePath, writtenPairs, cfg.KeyMapping)

	newState := buildSubsetState(writtenPairs, sourcePath)
	writeSubsetState(statePath, newState)
	Library.Progress("  Updated .subset state: %d entries", len(newState))

	buildSyncStateSnapshot(cfg, writtenPairs, syncState)
	syncState.close()
}

// runSubsetFreshExport handles the first-run case: no prior .subset or .sync state.
func runSubsetFreshExport(cfg TBibGetConfig, baseDir, sourcePath, keysBasePath, statePath string, syncState *TSyncState) {
	Library.Progress("  State  : %s (new)", statePath)

	// No bib has been read yet so there are no local groups; entryGroups stays nil
	// and entryGetString falls back to Library.GroupEntries for managed groups.
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

	buildSyncStateSnapshot(cfg, writtenPairs, syncState)
	syncState.close()
}

// applyGroupSync updates the sync state with all group assignments from the bib,
// then performs a three-way merge of managed group memberships using the previous
// sync state snapshot as the common ancestor.
//
// Step 1 — record all groups (managed + local) from the bib into the sync state.
// Step 2 — for each managed group and each entry:
//   - bib added it (not in snap, not in DB) → push to DB
//   - bib removed it (was in snap, still in DB) → remove from DB
//   - DB added it (not in snap, not in bib) → add to sync state so phase 2 writes it to bib
//   - DB removed it (was in snap, not in DB, not in bib) → no action
//
// When syncState is nil every snapshot set is treated as empty: additions still
// propagate but removals are suppressed (safe default for first run / open failed).
func applyGroupSync(cfg TBibGetConfig, bibEntries []TBibTeXEntry, outputToCanonical map[string]string, syncState *TSyncState) {
	mappings := parseGroupMappings(cfg.SyncGroups)

	// Log the effective DB→local group mapping once so the user can verify scope.
	if len(mappings) > 0 {
		var managedPairs []string
		seen := map[string]bool{}
		for dbGroup := range Library.GroupEntries {
			localGroup := dbGroupToLocal(dbGroup, mappings)
			if localGroup == "" || seen[dbGroup] {
				continue
			}
			seen[dbGroup] = true
			if dbGroup == localGroup {
				managedPairs = append(managedPairs, dbGroup)
			} else {
				managedPairs = append(managedPairs, dbGroup+" → "+localGroup)
			}
		}
		sort.Strings(managedPairs)
		if len(managedPairs) > 0 {
			Library.Progress("  Managed groups: %s", strings.Join(managedPairs, ", "))
		} else {
			Library.Progress("  Managed groups: none match config patterns")
		}
	}

	for _, e := range bibEntries {
		canon := outputToCanonical[e.Key]
		if canon == "" {
			canon = Library.harvestKeyMatch(e)
		}
		if canon == "" {
			continue
		}
		if resolved := Library.MapEntryKey(canon); resolved != "" {
			canon = resolved
		}
		if !Library.EntryExists(canon) {
			continue
		}

		// Save the previous snapshot BEFORE overwriting with current bib state.
		snapGroups := TStringSetNew()
		if se := syncState.get(canon); se != nil {
			for g := range se.Groups.Elements() {
				snapGroups.Add(g)
			}
		}

		// Step 1: record ALL groups from bib into sync state (preserving non-group fields).
		allBibGroups := TStringSetNew()
		for _, g := range strings.Split(e.Fields["groups"], ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				allBibGroups.Add(g)
			}
		}
		if se := syncState.get(canon); se != nil {
			updated := *se
			updated.Groups = allBibGroups
			syncState.set(updated)
		} else {
			syncState.set(TSyncEntry{
				CanonicalKey: canon,
				Groups:       allBibGroups,
				Fields:       make(map[string]string),
			})
		}

		if len(cfg.SyncGroups) == 0 {
			continue
		}

		// Step 2: three-way merge for managed groups using snapGroups as common ancestor.
		// Expansion is purely DB-side: iterate Library.GroupEntries and map to local names.
		for dbGroup := range Library.GroupEntries {
			localGroup := dbGroupToLocal(dbGroup, mappings)
			if localGroup == "" {
				continue // not in scope
			}
			dbMembers := Library.GroupEntries[dbGroup]
			dbHas := dbMembers.Contains(canon)
			bibHas := allBibGroups.Contains(localGroup)
			snapHas := snapGroups.Contains(localGroup)

			switch {
			case bibHas && !dbHas && !snapHas:
				// Bib added this entry to an existing DB group → push to DB.
				Library.GroupEntries.AddValueToStringSetMap(dbGroup, canon)
				if err := bibExec(`INSERT INTO bib_groups (group_name, entry_key) VALUES (?, ?) ON CONFLICT DO NOTHING;`, dbGroup, canon); err != nil {
					Library.Warning("Group sync insert failed (%s → %q): %s", canon, dbGroup, err)
				} else {
					Library.Progress("Group sync: +%s → %q (local: %q)", canon, dbGroup, localGroup)
				}
			case !bibHas && dbHas && snapHas:
				// Bib removed → remove from DB.
				Library.GroupEntries.DeleteValueFromStringSetMap(dbGroup, canon)
				if err := bibExec(`DELETE FROM bib_groups WHERE group_name=? AND entry_key=?`, dbGroup, canon); err != nil {
					Library.Warning("Group sync delete failed (%s → %q): %s", canon, dbGroup, err)
				} else {
					Library.Progress("Group sync: -%s → %q (local: %q)", canon, dbGroup, localGroup)
				}
			case !bibHas && dbHas && !snapHas:
				// DB added independently → add local name to sync state so phase 2 writes it to bib.
				if se := syncState.get(canon); se != nil {
					updated := *se
					updated.Groups.Add(localGroup)
					syncState.set(updated)
				}
			}
		}
	}
}

// buildSyncStateSnapshot records the output key, DB fingerprint, and group
// memberships for every entry written in this sync cycle. The Groups field is
// already correct from applyGroupSync (all managed + local groups from bib,
// plus any DB-added managed groups); we preserve it and only update the
// identity/hash fields.
func buildSyncStateSnapshot(cfg TBibGetConfig, writtenPairs []TBibGetPair, syncState *TSyncState) {
	if syncState == nil {
		return
	}
	now := time.Now().Unix()
	written := map[string]bool{}
	for _, p := range writtenPairs {
		written[p.canonicalKey] = true
		groups := TStringSetNew()
		if se := syncState.get(p.canonicalKey); se != nil {
			// Preserve all groups recorded during applyGroupSync (managed + local).
			groups = se.Groups
		}
		syncState.set(TSyncEntry{
			CanonicalKey: p.canonicalKey,
			OutputKey:    p.localKey,
			Groups:       groups,
			DBHash:       subsetDBFingerprint(p.canonicalKey),
			Fields:       make(map[string]string),
			SyncTime:     now,
		})
	}
	for _, k := range syncState.keys() {
		if !written[k] {
			syncState.delete(k)
		}
	}
}

// TGroupMapping holds one group synchronisation rule from the config.
// DBPattern is a glob matched against DB (bib_groups) group names.
// LocalPattern is the name used in the local bib file; a * in LocalPattern is
// replaced by the portion of the DB group name captured by * in DBPattern.
// When DBPattern has no *, LocalPattern is used verbatim (direct rename).
type TGroupMapping struct {
	DBPattern    string
	LocalPattern string
}

// parseGroupMappings converts cfg.SyncGroups into []TGroupMapping.
// Each element may be "DBPattern" (same local name) or "DBPattern:LocalPattern".
func parseGroupMappings(patterns []string) []TGroupMapping {
	m := make([]TGroupMapping, 0, len(patterns))
	for _, p := range patterns {
		if idx := strings.Index(p, ":"); idx >= 0 {
			m = append(m, TGroupMapping{p[:idx], p[idx+1:]})
		} else {
			m = append(m, TGroupMapping{p, p})
		}
	}
	return m
}

// dbGroupToLocal maps a DB group name to its local bib name using the first
// matching mapping. Returns "" when no mapping matches (group not in scope).
// Expansion is always DB-side: only DB groups that exist are considered.
func dbGroupToLocal(dbGroup string, mappings []TGroupMapping) string {
	for _, m := range mappings {
		if ok, _ := filepath.Match(m.DBPattern, dbGroup); !ok {
			continue
		}
		if !strings.Contains(m.DBPattern, "*") {
			return m.LocalPattern
		}
		parts := strings.SplitN(m.DBPattern, "*", 2)
		prefix, suffix := parts[0], parts[1]
		if !strings.HasPrefix(dbGroup, prefix) || !strings.HasSuffix(dbGroup, suffix) {
			continue
		}
		captured := dbGroup[len(prefix) : len(dbGroup)-len(suffix)]
		return strings.Replace(m.LocalPattern, "*", captured, 1)
	}
	return ""
}

// groupInScope reports whether group g matches any pattern in patterns.
// Patterns support the same wildcards as filepath.Match: * matches any sequence
// of non-separator characters, ? matches any single character.
// Used for non-subset modes where DB and local group names are identical.
func groupInScope(g string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, g); matched {
			return true
		}
	}
	return false
}

// computeCurrentScope returns the set of canonical keys that the .keys and .select files
// currently select. It mirrors the scope-building logic of writePullSync without writing
// anything. Used by runSubsetUpSync to tell apart intentional user deletions from entries
// that simply fell out of scope (e.g. via only_these; group "ISE"; after a group change).
func computeCurrentScope(cfg TBibGetConfig, keysBasePath string) map[string]bool {
	scope := map[string]bool{}
	pairs, _, ok := readKeysFile(keysBasePath)
	if !ok {
		return scope
	}
	selectStmts, _ := readSelectFile(keysBasePath)
	if selectOnlyThese(selectStmts) {
		pairs = nil
	}
	explicitKeys := map[string]bool{}
	for _, p := range pairs {
		resolved := Library.MapEntryKey(p.canonicalKey)
		if resolved == "" {
			resolved = p.canonicalKey
		}
		if resolved != "" {
			scope[resolved] = true
			explicitKeys[resolved] = true
		}
	}
	for _, canonical := range expandSelectStmts(selectStmts, explicitKeys) {
		scope[canonical] = true
	}
	return scope
}

// runSubsetUpSync handles phase 1 of subsequent syncs: parses the bib, detects three-way
// changes, and merges bib-side edits into the DB. Does not write back the bib or state.
// Returns true if the sync was aborted (e.g. parse error) — caller must skip phase 2.
func runSubsetUpSync(cfg TBibGetConfig, sourcePath, keysBasePath, statePath string, existingState TSubsetState, syncState *TSyncState) bool {
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

	// Three-way group merge using sync state snapshot as common ancestor.
	applyGroupSync(cfg, bibEntries, outputToCanonical, syncState)

	// Build a second reverse map: current canonical → stale state key.
	// Covers the case where the bib was already regenerated to the new canonical key
	// (phase 2 ran after a merge) but the state file still has the old key. Without
	// this map, the new canonical isn't found in outputToCanonical and harvestKeyMatch
	// returns it directly — but the old state key is never marked as seen, so it fires
	// a spurious deletion prompt.
	resolvedToStateKey := map[string]string{}
	for stateCanonical := range existingState {
		if resolved := Library.MapEntryKey(stateCanonical); resolved != stateCanonical && Library.EntryExists(resolved) {
			resolvedToStateKey[resolved] = stateCanonical
		}
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
			// Silently skip entries that were explicitly deleted from the library.
			// Mark the state key as seen so it doesn't also fire a deletion prompt.
			if isDeletedEntry(e.Key) || (canonical != "" && isDeletedEntry(canonical)) {
				if stateKey != "" {
					bibSeenCanonicals[stateKey] = true
				}
				bibSeenCanonicals[e.Key] = true
				if canonical != "" {
					bibSeenCanonicals[canonical] = true
				}
				continue
			}
			toProcess = append(toProcess, categorizedEntry{bibEntry: e, status: statusNew})
			continue
		}

		// Mark the original state key as seen so it isn't treated as deleted.
		if stateKey != "" {
			bibSeenCanonicals[stateKey] = true
		}
		bibSeenCanonicals[canonical] = true
		// Also mark any stale state entry whose canonical has since been resolved to
		// this one (covers the case where phase 2 already updated the bib to the new
		// canonical but the state file still holds the old key from before the merge).
		if oldKey, ok := resolvedToStateKey[canonical]; ok {
			bibSeenCanonicals[oldKey] = true
		}

		// Entry is a known library entry but not yet in the subset state — it was
		// auto-included as a crossref parent, not edited by the user. Accept silently;
		// phase 2 will write it to the bib and add it to the state.
		se, inState := existingState[stateKey]
		if !inState {
			se, inState = existingState[canonical]
		}
		if !inState {
			if oldKey, ok := resolvedToStateKey[canonical]; ok {
				se, inState = existingState[oldKey]
			}
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

		// When the stored state is stale (e.g. bib output format changed between
		// syncs), bibChanged may fire even though the bib accurately reflects the
		// current DB. Downgrade to unchanged when the bib content matches what
		// entryGetString would produce from the DB right now.
		if bibChanged && bibReflectsDB(e, canonical, outputToCanonical) {
			bibChanged = false
		}

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
	// Entries that were explicitly deleted from the library (recorded in deleted_entries)
	// are silently accepted — no prompt needed, phase 2 will drop them from the state.
	//
	// Entries that fell out of scope (e.g. only_these; group "ISE"; no longer includes
	// them because they were removed from the group) are also silently pruned — they are
	// NOT treated as user-initiated DB deletions. Phase 2 rebuilds the state from scratch
	// so no explicit cleanup is needed here.
	currentScope := computeCurrentScope(cfg, keysBasePath)
	var deletedCanonicals []string
	outOfScope := 0
	for canonical := range existingState {
		if !bibSeenCanonicals[canonical] {
			if isDeletedEntry(canonical) {
				continue
			}
			if !currentScope[canonical] {
				outOfScope++
				continue
			}
			deletedCanonicals = append(deletedCanonicals, canonical)
		}
	}
	sort.Strings(deletedCanonicals)

	// Summary.
	counts := map[subsetStatus]int{}
	for _, c := range toProcess {
		counts[c.status]++
	}
	Library.Progress("  Changes: %d unchanged, %d bib-changed, %d db-changed, %d both-changed, %d new, %d deleted, %d out-of-scope",
		counts[statusUnchanged], counts[statusBibChanged], counts[statusDBChanged],
		counts[statusBothChanged], counts[statusNew], len(deletedCanonicals), outOfScope)

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

	// Compute local files dir for pdf_files="local" (used when trashing deleted entries' PDFs).
	localFilesDir := ""
	if cfg.PDFFiles == "local" {
		localFilesDir = strings.TrimSuffix(sourcePath, filepath.Ext(sourcePath)) + ".files/"
	}

	// Process deleted entries.
	if !quit {
		for _, canonical := range deletedCanonicals {
			if quit || (stepN > 0 && questions >= stepN) {
				break
			}
			outputKey := existingState[canonical].OutputKey
			if deleteSubsetEntry(canonical, localFilesDir, outputKey, cfg.TrustedSubset) {
				delete(existingState, canonical)
			}
			if !cfg.TrustedSubset {
				questions++
			}
		}
	}

	return false
}
