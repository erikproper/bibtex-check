/*
 *
 * Module: bibtex_library_dealias_fields
 *
 * This module is concerned with the DeAlias of field values.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 02.06.2024
 *
 */

package main

// The general function call to DeAlias entry specific field values.
func (l *TBibTeXLibrary) DeAliasEntryFieldValue(entry, field, value string) string {
	if field == "crossref" {
		return l.DeAliasEntryKey(value)
	}

	if unAliased, hasAlias := l.GenericFieldAliasToTarget[field][value]; hasAlias {
		return unAliased
	}

	if unAliased, hasAlias := l.EntryFieldAliasToTarget[entry][field][value]; hasAlias {
		return unAliased
	}

	return value
}

// The general function call to DeAlias field values.
func (l *TBibTeXLibrary) DeAliasFieldValue(field, value string) string {
	if field == "crossref" {
		return l.DeAliasEntryKey(value)
	}

	if unAliased, hasAlias := l.GenericFieldAliasToTarget[field][value]; hasAlias {
		return unAliased
	}

	return value
}
