/*
 *
 * Module: bibtex_library_repair
 *
 * Repairs garbled author/editor fields in bib_entries.
 * Garbling (e.g. "Ae, Chun, Soon" instead of "Chun, Soon Ae") was caused by
 * incorrect name_mappings aliases that have since been fixed.  This module
 * re-derives the correct value from DBLP (for entries with a dblp field) or
 * from a reference SQLite database (for entries without a DBLP key).
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"database/sql"
	"strings"
)

// hasGarbledName returns true when any individual name in a BibTeX "and"-list
// contains two or more commas, which is the signature of the garbling pattern
// (e.g. "Ae, Chun, Soon", "Marco, Leimeister, Jan").
func hasGarbledName(names string) bool {
	for _, name := range strings.Split(names, " and ") {
		if strings.Count(strings.TrimSpace(name), ",") >= 2 {
			return true
		}
	}
	return false
}

// repairFieldFromValue normalises rawValue for field and writes it to key when
// the raw value is non-empty, clean (no garbling), and differs from current.
// Returns true when a change was made.
func (l *TBibTeXLibrary) repairFieldFromValue(key, field, rawValue string) bool {
	if rawValue == "" {
		return false
	}
	normalised := l.NormaliseFieldValue(field, rawValue)
	if normalised == "" || hasGarbledName(normalised) {
		return false
	}
	current := l.EntryFieldValueity(key, field)
	if current == normalised {
		return false
	}
	l.SetEntryFieldValue(key, field, normalised)
	return true
}

// repairFieldFromDblp reads the DBLP file store for dblpKey and uses it to
// repair the author or editor field of key.  Returns true on any change.
func (l *TBibTeXLibrary) repairFieldFromDblp(key, dblpKey, field string) bool {
	current := l.EntryFieldValueity(key, field)
	if !hasGarbledName(current) {
		return false
	}
	entry := dblpEntryFromFile(dblpKey)
	if entry == nil {
		return false
	}
	return l.repairFieldFromValue(key, field, entry.Fields[field])
}

// repairFieldFromRepairDb queries repairDb for the author/editor value of key
// (and its key_oldies aliases when the direct lookup fails).
// Returns the raw field value found, or "".
func repairFieldFromRepairDb(repairDb *sql.DB, key, field string) string {
	// Try the current key first.
	var value string
	err := repairDb.QueryRow(
		`SELECT value FROM bib_entries WHERE key = ? AND field = ?`, key, field,
	).Scan(&value)
	if err == nil && value != "" {
		return value
	}

	// Walk key_oldies of the current working DB to find old keys that might exist in repairDb.
	rows, err := db.Query(`SELECT alias FROM key_oldies WHERE key = ?`, key)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var oldKey string
		if rows.Scan(&oldKey) != nil {
			continue
		}
		err = repairDb.QueryRow(
			`SELECT value FROM bib_entries WHERE key = ? AND field = ?`, oldKey, field,
		).Scan(&value)
		if err == nil && value != "" {
			return value
		}
	}
	return ""
}

// RepairGarbledNames iterates every entry in the library and repairs garbled
// author/editor fields.  For DBLP-linked entries it uses the DBLP file store;
// for others it falls back to repairDb (may be nil to skip non-DBLP entries).
// Returns the number of fields repaired.
func (l *TBibTeXLibrary) RepairGarbledNames(repairDb *sql.DB) int {
	total := countBibEntries()
	count := 0
	repaired := 0

	forEachBibEntryKey(func(key string) bool {
		count++
		l.Progress(ProgressEntryProgress, count, total, float64(count)*100/float64(total))

		for _, field := range []string{"author", "editor"} {
			current := l.EntryFieldValueity(key, field)
			if current == "" || !hasGarbledName(current) {
				continue
			}

			dblpKey := l.EntryFieldValueity(key, DBLPField)
			fixed := false

			if dblpKey != "" {
				fixed = l.repairFieldFromDblp(key, dblpKey, field)
			}

			if !fixed && repairDb != nil {
				raw := repairFieldFromRepairDb(repairDb, key, field)
				if raw != "" {
					fixed = l.repairFieldFromValue(key, field, raw)
				}
			}

			if fixed {
				repaired++
				bibEntriesModified = true
			}
		}
		return true
	})

	return repaired
}
