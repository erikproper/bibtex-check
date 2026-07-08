/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: Tables
 *
 * Internal per-table export and import helpers. The public interface is
 * ExportTables / ImportTables in bibtex_library_tables.go.
 *
 * All imports follow the two-phase streaming protocol (ARCHITECTURE.md §10):
 *   Phase 1 — validate: stream through the file, count errors; abort if any found.
 *   Phase 2 — write: stream again; for import (replace) delete existing rows first.
 * Neither phase loads all rows into memory simultaneously.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 03.06.2026
 *
 */

package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
)

// tablesFilePath returns the full path for a table's CSV file.
func tablesFilePath(suffix string) string {
	return bibTeXFolder + bibTeXBaseName + suffix
}

// ensureTablesDir creates the tables directory if it does not exist.
func ensureTablesDir() {
	if err := os.MkdirAll(tablesFilePath(tablesFolderSuffix), 0o755); err != nil {
		dbInteraction.Warning("Could not create tables folder: %s", err)
	}
}

// importTwoPhaseFiltered is the generic two-phase streaming import helper.
// skip, if non-nil, returns true for rows to silently ignore (blank lines, comments).
// validate returns true when a non-skipped record is structurally valid.
// write inserts/upserts the record inside the provided transaction.
// Returns the number of rows written and whether the import succeeded.
func importTwoPhaseFiltered(path string, skip func([]string) bool, validate func([]string) bool, clearFn func(), write func(*sql.Tx, []string)) (int, bool) {
	errors := 0
	rowCount := 0
	processCSVFile(path, func(fields []string) {
		// Skip records that look like trailing CSV artifacts: a single field
		// that is blank, all-whitespace, or just a bare quote character.
		// These arise when a file ends with an unclosed quoted field at EOF.
		if len(fields) == 1 && strings.TrimSpace(strings.Trim(fields[0], `"`)) == "" {
			return
		}
		if skip != nil && skip(fields) {
			return
		}
		if !validate(fields) {
			errors++
		} else {
			rowCount++
		}
	})
	if errors > 0 {
		dbInteraction.Warning("Import aborted: %d invalid rows in %s — table unchanged", errors, path)
		return 0, false
	}
	if rowCount == 0 {
		dbInteraction.Warning("Nothing to import from %s", path)
		return 0, false
	}
	tx, err := db.Begin()
	if err != nil {
		dbInteraction.Warning("Could not begin import transaction: %s", err)
		return 0, false
	}
	if clearFn != nil {
		clearFn()
	}
	processCSVFile(path, func(fields []string) {
		if skip != nil && skip(fields) {
			return
		}
		if validate(fields) {
			write(tx, fields)
		}
	})
	if err := tx.Commit(); err != nil {
		dbInteraction.Warning("Could not commit import: %s", err)
		return 0, false
	}
	return rowCount, true
}

// importTwoPhase calls importTwoPhaseFiltered with no skip predicate.
func importTwoPhase(path string, validate func([]string) bool, clearFn func(), write func(*sql.Tx, []string)) (int, bool) {
	return importTwoPhaseFiltered(path, nil, validate, clearFn, write)
}

// importReport logs the result of an import operation.
func importReport(verb, path string, rows int) {
	dbInteraction.Progress("%s %d rows from %s", verb, rows, path)
}

// ── contributors ─────────────────────────────────────────────────────────────
// CSV format: id;name;orcid  (orcid may be empty)

func ExportContributors() {
	ensureTablesDir()
	path := tablesFilePath(ContributorsFilePath)
	rows, err := db.Query(`SELECT id, name, COALESCE(orcid, '') FROM contributors ORDER BY id`)
	if err != nil {
		dbInteraction.Warning("Could not query contributors: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 3)
}

func importContributorsFromCSV(replace bool) {
	path := tablesFilePath(ContributorsFilePath)
	upsert := `INSERT INTO contributors (id, name, orcid) VALUES (?, ?, ?)
	             ON CONFLICT(id) DO UPDATE SET name = excluded.name, orcid = excluded.orcid`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM contributors`) } //nolint:errcheck
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		orcid := ""
		if len(f) >= 3 {
			orcid = f[2]
		}
		tx.Exec(upsert, f[0], f[1], orcid) //nolint:errcheck
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── contributor_names ─────────────────────────────────────────────────────────
// CSV format: id;name

func ExportContributorNames() {
	ensureTablesDir()
	path := tablesFilePath(ContributorNamesFilePath)
	rows, err := db.Query(`SELECT id, name FROM contributor_names ORDER BY id, name`)
	if err != nil {
		dbInteraction.Warning("Could not query contributor_names: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importContributorNamesFromCSV(replace bool) {
	path := tablesFilePath(ContributorNamesFilePath)
	insert := `INSERT OR IGNORE INTO contributor_names (id, name) VALUES (?, ?)`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM contributor_names`) } //nolint:errcheck
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(insert, f[0], f[1]) //nolint:errcheck
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── contributor_id_oldies ────────────────────────────────────────────────────
// CSV format: absorbed_id;canonical_id

func ExportContributorIDOldies() {
	ensureTablesDir()
	path := tablesFilePath(ContributorIDOldiesFilePath)
	rows, err := db.Query(`SELECT absorbed_id, canonical_id FROM contributor_id_oldies ORDER BY absorbed_id`)
	if err != nil {
		dbInteraction.Warning("Could not query contributor_id_oldies: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importContributorIDOldiesFromCSV(replace bool) {
	path := tablesFilePath(ContributorIDOldiesFilePath)
	upsert := `INSERT INTO contributor_id_oldies (absorbed_id, canonical_id) VALUES (?, ?)
	             ON CONFLICT(absorbed_id) DO UPDATE SET canonical_id = excluded.canonical_id`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM contributor_id_oldies`) } //nolint:errcheck
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1]) //nolint:errcheck
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── contributor_orcid_seen ───────────────────────────────────────────────────
// CSV format: contributor_id;orcid;canonical;credit_name;declared_name;other_names

func ExportContributorORCIDSeen() {
	ensureTablesDir()
	path := tablesFilePath(ContributorORCIDSeenFilePath)
	rows, err := db.Query(
		`SELECT contributor_id, orcid, canonical, credit_name, declared_name, other_names
		   FROM contributor_orcid_seen ORDER BY contributor_id, orcid`)
	if err != nil {
		dbInteraction.Warning("Could not query contributor_orcid_seen: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 6)
}

func importContributorORCIDSeenFromCSV(replace bool) {
	path := tablesFilePath(ContributorORCIDSeenFilePath)
	upsert := `INSERT INTO contributor_orcid_seen
	             (contributor_id, orcid, canonical, credit_name, declared_name, other_names)
	             VALUES (?, ?, ?, ?, ?, ?)
	             ON CONFLICT(contributor_id, orcid) DO UPDATE SET
	               canonical = excluded.canonical,
	               credit_name = excluded.credit_name,
	               declared_name = excluded.declared_name,
	               other_names = excluded.other_names`
	validate := func(f []string) bool { return len(f) >= 6 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM contributor_orcid_seen`) } //nolint:errcheck
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1], f[2], f[3], f[4], f[5]) //nolint:errcheck
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── key_hints ────────────────────────────────────────────────────────────────
// CSV format: key;hint  (elements[0]=key, elements[1]=hint)

func ExportKeyHints() {
	ensureTablesDir()
	path := tablesFilePath(KeyHintsFilePath)
	rows, err := db.Query(`SELECT key, hint FROM key_hints ORDER BY key, hint`)
	if err != nil {
		dbInteraction.Warning("Could not query key_hints: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importKeyHintsFromCSV(replace bool) {
	path := tablesFilePath(KeyHintsFilePath)
	upsert := `INSERT INTO key_hints (hint, key) VALUES (?, ?)
	             ON CONFLICT(hint) DO UPDATE SET key = excluded.key`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM key_hints`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[1], f[0]) // hint=f[1], key=f[0]
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── key_oldies ───────────────────────────────────────────────────────────────
// CSV format: key;alias  (elements[0]=key, elements[1]=alias)

func ExportKeyOldies() {
	ensureTablesDir()
	path := tablesFilePath(KeyOldiesFilePath)
	rows, err := db.Query(`SELECT key, alias FROM key_oldies ORDER BY alias`)
	if err != nil {
		dbInteraction.Warning("Could not query key_oldies: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importKeyOldiesFromCSV(replace bool) {
	path := tablesFilePath(KeyOldiesFilePath)
	upsert := `INSERT INTO key_oldies (alias, key) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET key = excluded.key`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM key_oldies`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[1], f[0]) // alias=f[1], key=f[0]
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── non_double_entries ──────────────────────────────────────────────────────────
// CSV format: key1;key2

func ExportKeyNonDoubles() {
	ensureTablesDir()
	path := tablesFilePath(KeyNonDoublesFilePath)
	rows, err := db.Query(`SELECT key1, key2 FROM non_double_entries ORDER BY key1, key2`)
	if err != nil {
		dbInteraction.Warning("Could not query non_double_entries: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importKeyNonDoublesFromCSV(replace bool) {
	path := tablesFilePath(KeyNonDoublesFilePath)
	insert := `INSERT INTO non_double_entries (key1, key2) VALUES (?, ?) ON CONFLICT(key1, key2) DO NOTHING`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM non_double_entries`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(insert, f[0], f[1])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── generic_field_mappings ───────────────────────────────────────────────────
// CSV format: field;winner;challenger

func ExportGenericFieldMappings() {
	ensureTablesDir()
	path := tablesFilePath(GenericFieldMappingsFilePath)
	rows, err := db.Query(`SELECT field, winner, challenger FROM generic_field_mappings ORDER BY field, winner, challenger`)
	if err != nil {
		dbInteraction.Warning("Could not query generic_field_mappings: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 3)
}

func importGenericFieldMappingsFromCSV(replace bool) {
	path := tablesFilePath(GenericFieldMappingsFilePath)
	upsert := `INSERT INTO generic_field_mappings (field, winner, challenger) VALUES (?, ?, ?)
	             ON CONFLICT(field, challenger) DO UPDATE SET winner = excluded.winner`
	validate := func(f []string) bool { return len(f) >= 3 && f[0] != "" && f[1] != "" && f[2] != "" }
	skip := func(f []string) bool {
		if len(f) < 1 || (f[0] != "author" && f[0] != "editor") {
			return false
		}
		winner, challenger := "", ""
		if len(f) >= 2 {
			winner = f[1]
		}
		if len(f) >= 3 {
			challenger = f[2]
		}
		dbInteraction.Warning(WarningGenericFieldMappingAuthorEditor, f[0], winner, challenger)
		return true
	}
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM generic_field_mappings`) }
	}
	n, ok := importTwoPhaseFiltered(path, skip, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1], f[2])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── losing_field_values ──────────────────────────────────────────────────────
// CSV format: entry_key;field;value

func ExportLosingFieldValues() {
	ensureTablesDir()
	path := tablesFilePath(LosingFieldValuesFilePath)
	rows, err := db.Query(`SELECT entry_key, field, value FROM losing_field_values ORDER BY entry_key, field, value`)
	if err != nil {
		dbInteraction.Warning("Could not query losing_field_values: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 3)
}

func importLosingFieldValuesFromCSV(replace bool) {
	path := tablesFilePath(LosingFieldValuesFilePath)
	upsert := `INSERT INTO losing_field_values (entry_key, field, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, field, value) DO NOTHING`
	validate := func(f []string) bool { return len(f) >= 3 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM losing_field_values`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1], f[2])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── cross_field_mappings ─────────────────────────────────────────────────────
// CSV format: source_field;source_value;target_field;target_value
// source_field may be "field:entrytype" (e.g. "author:techreport") to restrict
// the rule to entries of the given type. This keeps all rules for a given field
// grouped together when the CSV is sorted by source_field.

func ExportCrossFieldMappings() {
	ensureTablesDir()
	path := tablesFilePath(CrossFieldMappingsFilePath)
	rows, err := db.Query(`SELECT source_field, source_value, target_field, target_value FROM cross_field_mappings ORDER BY source_field, source_value, target_field`)
	if err != nil {
		dbInteraction.Warning("Could not query cross_field_mappings: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 4)
}

func importCrossFieldMappingsFromCSV(replace bool) {
	path := tablesFilePath(CrossFieldMappingsFilePath)
	upsert := `INSERT INTO cross_field_mappings
	             (source_field, source_value, target_field, target_value) VALUES (?, ?, ?, ?)
	             ON CONFLICT(source_field, source_value, target_field) DO UPDATE SET target_value = excluded.target_value`
	validate := func(f []string) bool { return len(f) >= 4 && f[0] != "" && f[2] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM cross_field_mappings`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1], f[2], f[3])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── dblp_parent ──────────────────────────────────────────────────────────────
// CSV format: child_key;parent_key

func ExportDblpParent() {
	ensureTablesDir()
	path := tablesFilePath(DblpParentFilePath)
	rows, err := db.Query(`SELECT child_key, parent_key FROM dblp_parent ORDER BY child_key`)
	if err != nil {
		dbInteraction.Warning("Could not query dblp_parent: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importDblpParentFromCSV(replace bool) {
	path := tablesFilePath(DblpParentFilePath)
	insert := `INSERT INTO dblp_parent (child_key, parent_key) VALUES (?, ?)
	             ON CONFLICT(child_key) DO UPDATE SET parent_key = excluded.parent_key`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM dblp_parent`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(insert, f[0], f[1])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── dblp_waived ──────────────────────────────────────────────────────────────
// CSV format: key

func ExportDblpWaived() {
	ensureTablesDir()
	path := tablesFilePath(DblpWaivedFilePath)
	rows, err := db.Query(`SELECT key FROM dblp_waived ORDER BY key`)
	if err != nil {
		dbInteraction.Warning("Could not query dblp_waived: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 1)
}

func importDblpWaivedFromCSV(replace bool) {
	path := tablesFilePath(DblpWaivedFilePath)
	insert := `INSERT INTO dblp_waived (key) VALUES (?) ON CONFLICT(key) DO NOTHING`
	validate := func(f []string) bool { return len(f) >= 1 && f[0] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM dblp_waived`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(insert, f[0])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── entry_flags (legacy migration) ───────────────────────────────────────────
// entry_flags is now stored inside entry_metadata (value = 'true').
// ExportEntryFlags is kept for backward compatibility but reads from entry_metadata.
// importEntryFlagsFromCSV migrates a legacy entry_flags.csv into entry_metadata.

func ExportEntryFlags() {
	ensureTablesDir()
	path := tablesFilePath(EntryFlagsFilePath)
	flags := knownEntryFlags()
	placeholders := strings.Repeat("?,", len(flags))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(flags))
	for i, f := range flags {
		args[i] = f
	}
	rows, err := db.Query(
		`SELECT entry_key, property FROM entry_metadata WHERE value = 'true' AND property IN (`+placeholders+`) ORDER BY entry_key, property`,
		args...)
	if err != nil {
		dbInteraction.Warning("Could not query entry_flags from entry_metadata: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importEntryFlagsFromCSV(replace bool) {
	path := tablesFilePath(EntryFlagsFilePath)
	upsert := `INSERT OR IGNORE INTO entry_metadata (entry_key, property, value) VALUES (?, ?, 'true')`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		flags := knownEntryFlags()
		placeholders := strings.Repeat("?,", len(flags))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(flags))
		for i, f := range flags {
			args[i] = f
		}
		clearFn = func() {
			db.Exec(`DELETE FROM entry_metadata WHERE value = 'true' AND property IN (`+placeholders+`)`, args...)
		}
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── urls_ignore ──────────────────────────────────────────────────────────────
// CSV format: url  (one per line; also accepts url;reason;date — only url is stored)

func ExportURLsIgnore() {
	ensureTablesDir()
	path := tablesFilePath(URLsIgnoreFilePath)
	rows, err := db.Query(`SELECT url FROM urls_ignore ORDER BY url`)
	if err != nil {
		dbInteraction.Warning("Could not query urls_ignore: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 1)
}

func importURLsIgnoreFromCSV(replace bool) {
	path := tablesFilePath(URLsIgnoreFilePath)
	insert := `INSERT INTO urls_ignore (url) VALUES (?) ON CONFLICT(url) DO NOTHING`
	validate := func(f []string) bool { return len(f) >= 1 && f[0] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM urls_ignore`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(insert, strings.TrimSpace(f[0]))
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── shorten_mappings ─────────────────────────────────────────────────────────
// CSV format: field;original;shortened  (global file, not per-library)

func ExportShortenMappings() {
	path := globalFolder + ShortenMappingsFilePath
	rows, err := db.Query(`SELECT field, original, shortened FROM shorten_mappings ORDER BY field, original`)
	if err != nil {
		dbInteraction.Warning("Could not query shorten_mappings: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 3)
}

func importShortenMappingsFromCSV(replace bool) {
	path := globalFolder + ShortenMappingsFilePath
	upsert := `INSERT INTO shorten_mappings (field, original, shortened) VALUES (?, ?, ?)
	             ON CONFLICT(field, original) DO UPDATE SET shortened = excluded.shortened`
	// shorten_mappings.csv uses '#' comment lines and blank section separators — skip them.
	skip := func(f []string) bool {
		return len(f) == 0 || strings.TrimSpace(f[0]) == "" || strings.HasPrefix(strings.TrimSpace(f[0]), "#")
	}
	validate := func(f []string) bool { return len(f) >= 3 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM shorten_mappings`) }
	}
	n, ok := importTwoPhaseFiltered(path, skip, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, strings.TrimSpace(f[0]), strings.TrimSpace(f[1]), strings.TrimSpace(f[2]))
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── state_names ──────────────────────────────────────────────────────────────
// CSV format: canonical;alias  (elements[0]=canonical, elements[1]=alias)

func ExportStateNames() {
	ensureTablesDir()
	path := tablesFilePath(StateNamesFilePath)
	rows, err := db.Query(`SELECT canonical, alias FROM state_names ORDER BY canonical, alias`)
	if err != nil {
		dbInteraction.Warning("Could not query state_names: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importStateNamesFromCSV(replace bool) {
	path := tablesFilePath(StateNamesFilePath)
	upsert := `INSERT INTO state_names (alias, canonical) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET canonical = excluded.canonical`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM state_names`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[1], f[0]) // alias=f[1], canonical=f[0]
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── state_countries ──────────────────────────────────────────────────────────
// CSV format: state;country

func ExportStateCountries() {
	ensureTablesDir()
	path := tablesFilePath(StateCountriesFilePath)
	rows, err := db.Query(`SELECT state, country FROM state_countries ORDER BY state`)
	if err != nil {
		dbInteraction.Warning("Could not query state_countries: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importStateCountriesFromCSV(replace bool) {
	path := tablesFilePath(StateCountriesFilePath)
	upsert := `INSERT INTO state_countries (state, country) VALUES (?, ?)
	             ON CONFLICT(state) DO UPDATE SET country = excluded.country`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM state_countries`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[0], f[1])
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── country_names ────────────────────────────────────────────────────────────
// CSV format: canonical;alias

func ExportCountryNames() {
	ensureTablesDir()
	path := tablesFilePath(CountryNamesFilePath)
	rows, err := db.Query(`SELECT canonical, alias FROM country_names ORDER BY canonical, alias`)
	if err != nil {
		dbInteraction.Warning("Could not query country_names: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importCountryNamesFromCSV(replace bool) {
	path := tablesFilePath(CountryNamesFilePath)
	upsert := `INSERT INTO country_names (alias, canonical) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET canonical = excluded.canonical`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM country_names`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[1], f[0]) // alias=f[1], canonical=f[0]
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── booktitle_country_names ──────────────────────────────────────────────────
// CSV format: canonical;alias

func ExportBooktitleCountryNames() {
	ensureTablesDir()
	path := tablesFilePath(BooktitleCountryNamesFilePath)
	rows, err := db.Query(`SELECT canonical, alias FROM booktitle_country_names ORDER BY canonical, alias`)
	if err != nil {
		dbInteraction.Warning("Could not query booktitle_country_names: %s", err)
		return
	}
	defer rows.Close()
	writeCSVExport(path, rows, 2)
}

func importBooktitleCountryNamesFromCSV(replace bool) {
	path := tablesFilePath(BooktitleCountryNamesFilePath)
	upsert := `INSERT INTO booktitle_country_names (alias, canonical) VALUES (?, ?)
	             ON CONFLICT(alias) DO UPDATE SET canonical = excluded.canonical`
	validate := func(f []string) bool { return len(f) >= 2 && f[0] != "" && f[1] != "" }
	var clearFn func()
	if replace {
		clearFn = func() { db.Exec(`DELETE FROM booktitle_country_names`) }
	}
	n, ok := importTwoPhase(path, validate, clearFn, func(tx *sql.Tx, f []string) {
		tx.Exec(upsert, f[1], f[0]) // alias=f[1], canonical=f[0]
	})
	if ok {
		importReport(map[bool]string{true: "Imported", false: "Added"}[replace], path, n)
	}
}

// ── entry_metadata ───────────────────────────────────────────────────────────
// Stored as entry_metadata.json (JSON, not CSV). Format: map[entryKey]map[property]value.

func ExportEntryMetadata() {
	ensureTablesDir()
	path := tablesFilePath(EntryMetadataFilePath)
	rows, err := db.Query(`SELECT entry_key, property, value FROM entry_metadata`)
	if err != nil {
		dbInteraction.Warning("Could not query entry_metadata: %s", err)
		return
	}
	defer rows.Close()
	meta := TEntryMetadata{}
	for rows.Next() {
		var key, prop, val string
		if err := rows.Scan(&key, &prop, &val); err != nil {
			dbInteraction.Warning("Could not scan entry_metadata row: %s", err)
			continue
		}
		if _, ok := meta[key]; !ok {
			meta[key] = map[string]string{}
		}
		meta[key][prop] = val
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		dbInteraction.Warning("Could not marshal entry_metadata: %s", err)
		return
	}
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		dbInteraction.Warning("Could not write %s: %s", path, writeErr)
		return
	}
	dbInteraction.Progress("Exported %d entry keys to %s", len(meta), path)
}

func importEntryMetadataFromJSON(replace bool) {
	path := tablesFilePath(EntryMetadataFilePath)
	data, err := os.ReadFile(path)
	if err != nil {
		dbInteraction.Warning("Nothing to import from %s: %s", path, err)
		return
	}
	var meta TEntryMetadata
	if jsonErr := json.Unmarshal(data, &meta); jsonErr != nil {
		dbInteraction.Warning("Could not parse %s: %s", path, jsonErr)
		return
	}
	if len(meta) == 0 {
		dbInteraction.Warning("Nothing to import from %s", path)
		return
	}
	ensureEntryMetadataTableExists()
	if replace {
		db.Exec(`DELETE FROM entry_metadata`)
	}
	upsert := `INSERT INTO entry_metadata (entry_key, property, value) VALUES (?, ?, ?)
	             ON CONFLICT(entry_key, property) DO UPDATE SET value = excluded.value`
	tx, txErr := db.Begin()
	if txErr != nil {
		dbInteraction.Warning("Could not begin entry_metadata import: %s", txErr)
		return
	}
	rowCount := 0
	for key, props := range meta {
		for prop, val := range props {
			tx.Exec(upsert, key, prop, val)
			rowCount++
		}
	}
	if commitErr := tx.Commit(); commitErr != nil {
		dbInteraction.Warning("Could not commit entry_metadata import: %s", commitErr)
		return
	}
	verb := map[bool]string{true: "Imported", false: "Added"}[replace]
	dbInteraction.Progress("%s %d entry_metadata rows from %s", verb, rowCount, path)
}

// ── bulk import (for migrate.sh) ─────────────────────────────────────────────

// ImportAllCSVExchangeFiles imports all mapping CSVs found in the exchange folder
// into the DB. Each file is imported with replace=true (authoritative import).
// Used by migrate.sh as the one-time migration step from CSV-primary to DB-primary.
// Returns true if all present files imported successfully; false if any failed.
func ImportAllCSVExchangeFiles() bool {
	type importFn struct {
		name string
		path string
		fn   func(bool)
	}
	tables := []importFn{
		{"key_hints", tablesFilePath(KeyHintsFilePath), importKeyHintsFromCSV},
		{"key_oldies", tablesFilePath(KeyOldiesFilePath), importKeyOldiesFromCSV},
		{"non_double_entries", tablesFilePath(KeyNonDoublesFilePath), importKeyNonDoublesFromCSV},
		{"generic_field_mappings", tablesFilePath(GenericFieldMappingsFilePath), importGenericFieldMappingsFromCSV},
		{"losing_field_values", tablesFilePath(LosingFieldValuesFilePath), importLosingFieldValuesFromCSV},
		{"cross_field_mappings", tablesFilePath(CrossFieldMappingsFilePath), importCrossFieldMappingsFromCSV},
		{"dblp_parent", tablesFilePath(DblpParentFilePath), importDblpParentFromCSV},
		{"dblp_waived", tablesFilePath(DblpWaivedFilePath), importDblpWaivedFromCSV},
		{"entry_flags", tablesFilePath(EntryFlagsFilePath), importEntryFlagsFromCSV},
		{"urls_ignore", tablesFilePath(URLsIgnoreFilePath), importURLsIgnoreFromCSV},
		{"state_names", tablesFilePath(StateNamesFilePath), importStateNamesFromCSV},
		{"state_countries", tablesFilePath(StateCountriesFilePath), importStateCountriesFromCSV},
		{"country_names", tablesFilePath(CountryNamesFilePath), importCountryNamesFromCSV},
		{"booktitle_country_names", tablesFilePath(BooktitleCountryNamesFilePath), importBooktitleCountryNamesFromCSV},
	}
	ok := true
	for _, t := range tables {
		if _, statErr := os.Stat(t.path); statErr != nil {
			continue // file absent — skip silently
		}
		dbInteraction.Progress("Importing %s from %s", t.name, t.path)
		t.fn(true)
	}
	// shorten_mappings lives in globalFolder, not exchange folder
	if _, err := os.Stat(globalFolder + ShortenMappingsFilePath); err == nil {
		dbInteraction.Progress("Importing shorten_mappings from %s", globalFolder+ShortenMappingsFilePath)
		importShortenMappingsFromCSV(true)
	}
	if _, err := os.Stat(tablesFilePath(EntryMetadataFilePath)); err == nil {
		dbInteraction.Progress("Importing entry_metadata from %s", tablesFilePath(EntryMetadataFilePath))
		importEntryMetadataCSV()
	}
	return ok
}

// ── writeCSVExport ───────────────────────────────────────────────────────────

// writeCSVExport writes the result of a DB query to a CSV exchange file.
// cols is the expected column count (used to allocate the scan buffer).
func writeCSVExport(path string, rows *sql.Rows, cols int) {
	var lines []string
	scanBuf := make([]interface{}, cols)
	vals := make([]string, cols)
	for i := range scanBuf {
		scanBuf[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(scanBuf...); err != nil {
			dbInteraction.Warning("Export scan error: %s", err)
			continue
		}
		lines = append(lines, csvLine(vals...))
	}
	f, err := os.Create(path)
	if err != nil {
		dbInteraction.Warning("Could not create export file %s: %s", path, err)
		return
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
	dbInteraction.Progress("Exported %d rows to %s", len(lines), path)
}
