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

// The general function call to DeAlias key specific field values.
func (l *TBibTeXLibrary) DeAliasNormalisedEntryFieldValue(key, field, valueRAW string) string {
	return l.DeAliasEntryFieldValue(key, field, l.NormaliseFieldValue(field, valueRAW))
}

// Rename. The previous should be called Normalised ...
func (l *TBibTeXLibrary) DeAliasEntryFieldValue(key, field, value string) string {
	if field == "crossref" {
		return l.DeAliasEntryKey(value)
	}

	if unAliased, hasAlias := l.GenericFieldSourceToTarget[field][value]; hasAlias {
		return unAliased
	}

	if unAliased, hasAlias := l.EntryFieldSourceToTarget[key][field][value]; hasAlias {
		return unAliased
	}

	return value
}

func (l *TBibTeXLibrary) DeAliasNormalisedFieldValue(field, valueRAW string) string {
	return l.DeAliasFieldValue(field, l.NormaliseFieldValue(field, valueRAW))
}

// The general function call to DeAlias field values.
func (l *TBibTeXLibrary) DeAliasFieldValue(field, value string) string {

	if field == "crossref" {
		return l.DeAliasEntryKey(value)
	}

	if unAliased, hasAlias := l.GenericFieldSourceToTarget[field][value]; hasAlias {
		return unAliased
	}

	return value
}
