/*
 *
 * Module: bibtex_library_mapping_fields
 *
 * This module is concerned with the mapping of field values.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 02.06.2024
 *
 */

//// HOW ABOUT THE CROSS FIELD MAPPING?

package main

// The general function call to Map key specific field values.
func (l *TBibTeXLibrary) MapNormalisedEntryFieldValue(key, field, valueRAW string) string {
	return l.MapEntryFieldValue(key, field, l.NormaliseFieldValue(field, valueRAW))
}

// Rename. The previous should be called Normalised ...
func (l *TBibTeXLibrary) MapEntryFieldValue(key, field, value string) string {
	if field == "crossref" {
		return l.MapEntryKey(value)
	}

	if unAliased, hasAlias := l.GenericFieldSourceToTarget[field][value]; hasAlias {
		return unAliased
	}

	if unAliased, hasAlias := l.EntryFieldSourceToTarget[key][field][value]; hasAlias {
		return unAliased
	}

	return value
}

func (l *TBibTeXLibrary) MapNormalisedFieldValue(field, valueRAW string) string {
	return l.MapFieldValue(field, l.NormaliseFieldValue(field, valueRAW))
}

// The general function call to Map field values.
func (l *TBibTeXLibrary) MapFieldValue(field, value string) string {

	if field == "crossref" {
		return l.MapEntryKey(value)
	}

	if unAliased, hasAlias := l.GenericFieldSourceToTarget[field][value]; hasAlias {
		return unAliased
	}

	return value
}
