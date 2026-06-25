/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: Tables
 *
 * Unified export / import for all user-editable tables.
 *
 * All tables live as CSV files in <basename>.tables/. The table registry maps
 * each table name to its file path, internal export/import helpers, and the
 * cascade normalisation step (if any) that must run after import.
 *
 * Cascade principle: importing a mapping table (name_mappings, generic field
 * mappings, etc.) may change how bib_entries values are normalised. After
 * importing such a table the cascade loader reloads the mapping into memory and
 * re-normalises every affected field in bib_entries in-place, so the database
 * stays internally consistent without requiring a separate run.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 03.06.2026
 *
 */

package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// ── Table registry ────────────────────────────────────────────────────────────

// tTableEntry describes one exportable/importable table.
type tTableEntry struct {
	name      string
	filePath  string                  // relative to bibTeXFolder+bibTeXBaseName
	exportFn  func()                  // nil if not exportable
	importFn  func()                  // nil if not importable; always replace-all semantics
	cascadeFn func(l *TBibTeXLibrary) // re-normalise bib_entries after import; nil if none
}

// tableRegistry is the ordered list of all user-visible tables.
var tableRegistry []tTableEntry

func init() {
	tableRegistry = []tTableEntry{
		// Geographic primitives — most foundational; other address tables depend on these.
		{"state_names", StateNamesFilePath,
			ExportStateNames, func() { importStateNamesFromCSV(true) },
			func(l *TBibTeXLibrary) { cascadeRenormaliseFields(l, "address") }},
		{"country_names", CountryNamesFilePath,
			ExportCountryNames, func() { importCountryNamesFromCSV(true) },
			func(l *TBibTeXLibrary) { cascadeRenormaliseFields(l, "address") }},
		{"state_countries", StateCountriesFilePath,
			ExportStateCountries, func() { importStateCountriesFromCSV(true) },
			func(l *TBibTeXLibrary) { cascadeRenormaliseFields(l, "address") }},

		// Contributor identity and name-alias tables (replace legacy name_mappings).
		// contributors must be imported before contributor_names (FK dependency).
		{"contributors", ContributorsFilePath,
			ExportContributors, func() { importContributorsFromCSV(true) },
			func(l *TBibTeXLibrary) {
				loadContributorsFromDb(l)
				cascadeRenormaliseFields(l, "author", "editor")
			}},
		{"contributor_names", ContributorNamesFilePath,
			ExportContributorNames, func() { importContributorNamesFromCSV(true) },
			func(l *TBibTeXLibrary) {
				loadContributorsFromDb(l)
				cascadeRenormaliseFields(l, "author", "editor")
			}},

		// Field mapping tables — all cascade into bib_entries.
		// generic_field_mappings and cross_field_mappings import to their legacy tables;
		// the cascade syncs rows into the unified field_mappings table and reloads the
		// in-memory maps before renormalising, so newly imported mappings take effect.
		{"generic_field_mappings", GenericFieldMappingsFilePath,
			ExportGenericFieldMappings, func() { importGenericFieldMappingsFromCSV(true) },
			func(l *TBibTeXLibrary) {
				db.Exec(`INSERT OR IGNORE INTO field_mappings
				           (source_field, source_value, target_field, target_value)
				         SELECT field, challenger, field, winner FROM generic_field_mappings`)
				loadFieldMappingsFromDb(l)
				cascadeRenormaliseAllFields(l)
			}},
		{"cross_field_mappings", CrossFieldMappingsFilePath,
			ExportCrossFieldMappings, func() { importCrossFieldMappingsFromCSV(true) },
			func(l *TBibTeXLibrary) {
				db.Exec(`INSERT OR IGNORE INTO field_mappings
				           (source_field, source_value, target_field, target_value)
				         SELECT source_field, source_value, target_field, target_value FROM cross_field_mappings`)
				loadFieldMappingsFromDb(l)
				cascadeRenormaliseAllFields(l)
			}},
		{"losing_field_values", LosingFieldValuesFilePath,
			ExportLosingFieldValues, func() { importLosingFieldValuesFromCSV(true) },
			func(l *TBibTeXLibrary) { cascadeRenormaliseAllFields(l) }},

		// Core entry data — comprehensive renormalisation using all maps above.
		{"bib_entries", BibEntriesFilePath,
			exportBibEntries, importBibEntriesReplace,
			func(l *TBibTeXLibrary) { cascadeRenormaliseAllFields(l) }},

		// Booktitle country bracing — post-pass after bib_entries normalisation.
		{"booktitle_country_names", BooktitleCountryNamesFilePath,
			ExportBooktitleCountryNames, func() { importBooktitleCountryNamesFromCSV(true) },
			func(l *TBibTeXLibrary) { cascadeRenormaliseFields(l, "booktitle", "journal") }},

		// Key tables (no cascade)
		{"key_hints", KeyHintsFilePath,
			ExportKeyHints, func() { importKeyHintsFromCSV(true) }, nil},
		{"key_oldies", KeyOldiesFilePath,
			ExportKeyOldies, func() { importKeyOldiesFromCSV(true) }, nil},
		{"non_double_entries", KeyNonDoublesFilePath,
			ExportKeyNonDoubles, func() { importKeyNonDoublesFromCSV(true) }, nil},

		// DBLP overrides and flags (no cascade)
		{"dblp_parent", DblpParentFilePath,
			ExportDblpParent, func() { importDblpParentFromCSV(true) }, nil},
		{"dblp_waived", DblpWaivedFilePath,
			ExportDblpWaived, func() { importDblpWaivedFromCSV(true) }, nil},

		// Metadata and miscellaneous (no cascade)
		{"entry_metadata", EntryMetadataFilePath,
			exportEntryMetadataCSV, importEntryMetadataCSV, nil},
		// shorten_mappings lives in globalFolder — not in the per-library .tables/ dir.
		// It is imported directly via ImportAllCSVExchangeFiles; no registry entry needed.
		{"urls_ignore", URLsIgnoreFilePath,
			ExportURLsIgnore, func() { importURLsIgnoreFromCSV(true) }, nil},
		{"config", ConfigCSVFilePath,
			ExportConfig, func() { importConfigFromCSV(true) }, nil},
	}
}

// tableByName returns the registry entry for name, or nil.
func tableByName(name string) *tTableEntry {
	for i := range tableRegistry {
		if tableRegistry[i].name == name {
			return &tableRegistry[i]
		}
	}
	return nil
}

// allTableNames returns every registered table name in registry order.
func allTableNames() []string {
	names := make([]string, len(tableRegistry))
	for i, t := range tableRegistry {
		names[i] = t.name
	}
	return names
}

// resolveTableNames expands "all" (or empty) to the full registry, validates
// individual names, and deduplicates. Returns nil + error message on bad input.
func resolveTableNames(spec string) ([]string, string) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "all" {
		return allTableNames(), ""
	}
	var out []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(spec, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if tableByName(name) == nil {
			return nil, fmt.Sprintf("unknown table %q — valid tables: %s",
				name, strings.Join(allTableNames(), ", "))
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return allTableNames(), ""
	}
	return out, ""
}

// ── Unified export ────────────────────────────────────────────────────────────

func exportMdatePath(csvPath string) string { return csvPath + ".mdate" }

// isExportCurrent returns true when the CSV file exists, the dirty bit is clear,
// and the .mdate file records the same modification timestamp as the DB table.
// A table with no tracked modification time (tableModTime == 0) or a set dirty
// bit is always considered out of date so it gets exported and stamped.
func isExportCurrent(csvPath, tableName string) bool {
	if _, err := os.Stat(csvPath); err != nil {
		return false
	}
	if isTableDirty(tableName) {
		return false
	}
	modTime := tableModTime(tableName)
	if modTime == 0 {
		return false
	}
	data, err := os.ReadFile(exportMdatePath(csvPath))
	if err != nil {
		return false
	}
	var fileTime int64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &fileTime); err != nil {
		return false
	}
	return fileTime == modTime
}

// writeExportMdate writes the current DB modification timestamp for tableName
// alongside the exported CSV so that the next export can skip it when unchanged.
// When the table has no tracked modification time yet (tableModTime == 0), a
// timestamp is recorded now so future changes detected via setTableDate will
// trigger re-exports correctly. Also clears the dirty bit and updates last_written_time.
func writeExportMdate(csvPath, tableName string) {
	modTime := tableModTime(tableName)
	if modTime == 0 {
		modTime = time.Now().UnixMicro()
		setTableDate(tableName, modTime)
	}
	data := fmt.Sprintf("%d\n", modTime)
	if err := os.WriteFile(exportMdatePath(csvPath), []byte(data), 0644); err != nil {
		dbInteraction.Warning("Could not write export mdate %s: %s", exportMdatePath(csvPath), err)
		return
	}
	clearTableDirty(tableName)
	setTableLastWritten(tableName)
}

// ExportTables exports the listed tables (or all when spec is "" / "all")
// to <basename>.tables/.  A table is skipped when its CSV and accompanying
// .mdate file are both present and the recorded timestamp matches the DB.
func ExportTables(spec string) {
	names, errMsg := resolveTableNames(spec)
	if errMsg != "" {
		dbInteraction.Warning("export: %s", errMsg)
		return
	}
	ensureTablesDir()
	for _, name := range names {
		t := tableByName(name)
		if t.exportFn == nil {
			dbInteraction.Progress("Table %s: export not supported — skipped", name)
			continue
		}
		csvPath := tablesFilePath(t.filePath)
		if isExportCurrent(csvPath, t.name) {
			dbInteraction.Progress("Table %s: up to date — skipped", t.name)
			continue
		}
		t.exportFn()
		writeExportMdate(csvPath, t.name)
	}
}

// ── Unified import ────────────────────────────────────────────────────────────

// ImportTables imports the listed tables (or all when spec is "" / "all") from
// <basename>.tables/, always asking for confirmation first (replace-all semantics).
// After all imports, runs cascade normalisation for any table that requires it.
func ImportTables(spec string, l *TBibTeXLibrary) {
	names, errMsg := resolveTableNames(spec)
	if errMsg != "" {
		dbInteraction.Warning("import: %s", errMsg)
		return
	}

	// Collect importable tables that have a file present.
	var toImport []tTableEntry
	for _, name := range names {
		t := tableByName(name)
		if t.importFn == nil {
			dbInteraction.Progress("Table %s: import not supported — skipped", name)
			continue
		}
		if _, err := os.Stat(tablesFilePath(t.filePath)); err != nil {
			dbInteraction.Progress("Table %s: file not found — skipped", name)
			continue
		}
		toImport = append(toImport, *t)
	}
	if len(toImport) == 0 {
		dbInteraction.Progress("Nothing to import.")
		return
	}

	tableList := make([]string, len(toImport))
	for i, t := range toImport {
		tableList[i] = t.name
	}
	if !dbInteraction.WarningYesNoQuestion(
		"Proceed with import? (replaces ALL existing content)",
		"About to import %d table(s): %s",
		len(toImport), strings.Join(tableList, ", ")) {
		dbInteraction.Progress("Import cancelled.")
		return
	}

	// Import phase.
	needsCascade := map[string]bool{}
	for _, t := range toImport {
		t.importFn()
		if t.cascadeFn != nil {
			needsCascade[t.name] = true
		}
	}

	// Cascade normalisation — always runs in registry dependency order.
	// Once the first imported table that needs a cascade is reached, all
	// subsequent tables with a cascadeFn are also cascaded (propagation),
	// so downstream normalisations always see a fully consistent upstream.
	if l != nil {
		cascading := false
		for i := range tableRegistry {
			t := &tableRegistry[i]
			if t.cascadeFn == nil {
				continue
			}
			if !cascading && needsCascade[t.name] {
				cascading = true
			}
			if cascading {
				dbInteraction.Progress("Cascading normalisation for %s...", t.name)
				t.cascadeFn(l)
			}
		}
	}
}

// ── bib_entries export / import ───────────────────────────────────────────────

func exportBibEntries() {
	ensureTablesDir()
	path := tablesFilePath(BibEntriesFilePath)
	rows, err := db.Query(
		`SELECT entry_key, field, value FROM bib_entries ORDER BY entry_key, field`)
	if err != nil {
		dbInteraction.Warning("Could not query bib_entries: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 3)
}

func importBibEntriesReplace() {
	path := tablesFilePath(BibEntriesFilePath)
	upsert := `INSERT INTO bib_entries (entry_key, field, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, field) DO UPDATE SET value = excluded.value`
	validate := func(f []string) bool { return len(f) >= 3 && f[0] != "" && f[1] != "" }
	clearFn := func() { db.Exec(`DELETE FROM bib_entries`) }
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1], f[2])
	})
	if ok {
		importReport("Imported", path, n)
	}
}

// ── folders export (virtual table) ───────────────────────────────────────────

// exportFolders writes the bootstrap settings (global_folder, cache_folder) to
// folders.csv as a key;value CSV. Import is not supported here — edit .folders
// directly to change bootstrap paths.
func exportFolders() {
	ensureTablesDir()
	path := tablesFilePath(FoldersCSVFilePath)
	f, err := os.Create(path)
	if err != nil {
		dbInteraction.Warning("Could not create %s: %s", path, err)
		return
	}
	defer f.Close()
	for _, row := range [][2]string{
		{"global_folder", globalFolder},
		{"cache_folder", cacheFolder},
	} {
		f.WriteString(csvLine(row[0], row[1]) + "\n")
	}
	dbInteraction.Progress("Exported folders settings to %s", path)
}

// ── entry_metadata CSV export / import ───────────────────────────────────────

func exportEntryMetadataCSV() {
	ensureTablesDir()
	path := tablesFilePath(EntryMetadataFilePath)
	rows, err := db.Query(
		`SELECT entry_key, property, value FROM entry_metadata ORDER BY entry_key, property`)
	if err != nil {
		dbInteraction.Warning("Could not query entry_metadata: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 3)
}

func importEntryMetadataCSV() {
	path := tablesFilePath(EntryMetadataFilePath)
	upsert := `INSERT INTO entry_metadata (entry_key, property, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, property) DO UPDATE SET value = excluded.value`
	validate := func(f []string) bool { return len(f) >= 3 && f[0] != "" && f[1] != "" }
	clearFn := func() { db.Exec(`DELETE FROM entry_metadata`) }
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1], f[2])
	})
	if ok {
		importReport("Imported", path, n)
	}
}

// ── Cascade normalisation ─────────────────────────────────────────────────────

// cascadeRenormaliseFields re-normalises the listed fields in bib_entries.
func cascadeRenormaliseFields(l *TBibTeXLibrary, fields ...string) {
	for _, field := range fields {
		renormaliseField(l, field)
	}
}

// cascadeRenormaliseAllFields re-normalises every distinct field in bib_entries.
func cascadeRenormaliseAllFields(l *TBibTeXLibrary) {
	rows, err := db.Query(`SELECT DISTINCT field FROM bib_entries`)
	if err != nil {
		return
	}
	var fields []string
	for rows.Next() {
		var f string
		rows.Scan(&f)
		fields = append(fields, f)
	}
	rows.Close()
	for _, field := range fields {
		renormaliseField(l, field)
	}
}

// renormaliseField applies NormaliseFieldValue followed by MapFieldValue to every
// bib_entries row for field, writing back changed values in a single transaction.
func renormaliseField(l *TBibTeXLibrary, field string) {
	rows, err := db.Query(
		`SELECT entry_key, value FROM bib_entries WHERE field = ?`, field)
	if err != nil {
		return
	}
	type kv struct{ key, val string }
	var updates []kv
	for rows.Next() {
		var key, oldVal string
		rows.Scan(&key, &oldVal)
		newVal := l.MapFieldValue(field, l.NormaliseFieldValue(field, oldVal))
		if newVal != oldVal {
			updates = append(updates, kv{key, newVal})
		}
	}
	rows.Close()
	if len(updates) == 0 {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		return
	}
	for _, u := range updates {
		tx.Exec(`UPDATE bib_entries SET value = ? WHERE entry_key = ? AND field = ?`,
			u.val, u.key, field)
	}
	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return
	}
	dbInteraction.Progress("  Renormalised %q: %d value(s) updated", field, len(updates))
}
