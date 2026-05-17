/*
 *
 * Module: bibtex_entry
 *
 * This module defines TBibTeXEntry, a value snapshot of a single BibTeX entry.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 03.05.2026
 *
 */

package main

// TBibTeXEntry holds a snapshot of field values for a single BibTeX entry,
// loaded from the DB on demand. Mutations go through library helpers that
// keep the snapshot and the DB in sync.
type TBibTeXEntry struct {
	Key    string
	Fields map[string]string
}

func (e *TBibTeXEntry) FieldValue(field string) string {
	if e.Fields == nil {
		return ""
	}
	return e.Fields[field]
}

func (e *TBibTeXEntry) EntryType() string {
	return e.FieldValue(EntryTypeField)
}

// Exists returns true when the entry has a non-empty entry type.
func (e *TBibTeXEntry) Exists() bool {
	return e.EntryType() != ""
}
