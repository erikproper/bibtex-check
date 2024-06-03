/*
 *
 * Module: bibtex_library_process_fields
 *
 * This module is concerned with field specific processing.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 06.05.2024
 *
 */

package main

// Definition of the map for field processors
type TFieldProcessors = map[string]func(*TBibTeXLibrary, string)

var fieldProcessors TFieldProcessors

// When we have a DBLP field, we can use this as an alias
func processDBLPValue(l *TBibTeXLibrary, value string) {
	if !l.legacyMode {
		l.AddKeyAlias("DBLP:"+value, l.currentKey)
	}
}

// The general function call to process field values.
// It always, first, conducts a normalisation of the field value.
// If a field specific process function exists, then it is applied on the normalise value.
// Otherwise, we simply return the normalised value.
func (l *TBibTeXLibrary) ProcessEntryFieldValue(entry, field, value string) string {
	normalisedValue := l.DeAliasEntryFieldValue(entry, field, l.NormaliseFieldValue(field, value))

	valueProcessor, hasProcessor := fieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, normalisedValue)
	}

	return normalisedValue
}

func init() {
	// Define the processing functions.
	fieldProcessors = TFieldProcessors{}
	fieldProcessors["dblp"] = processDBLPValue
}
