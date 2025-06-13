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

//import "fmt"

// Definition of the map for field processors
type TFieldProcessors = map[string]func(*TBibTeXLibrary, string, string)

var fieldProcessors TFieldProcessors

func processDBLPValue(l *TBibTeXLibrary, key, value string) {
	l.AddKeyAlias(KeyForDBLP(value), key)
	l.AddKeyHint(KeyForDBLP(value), key)
}

func processPreferredKeyValue(l *TBibTeXLibrary, key, value string) {
	l.AddKeyAlias(value, key)
	l.AddKeyHint(value, key)
}

func processTitleValue(l *TBibTeXLibrary, key, value string) {
	l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(value), key)
}

// The general function call to process field values.
// It always, first, conducts a normalisation of the field value.
// If a field specific process function exists, then it is applied on the normalised value.
// Otherwise, we simply return the normalised value.
func (l *TBibTeXLibrary) ProcessEntryFieldValue(key, field, value string) string {
	normalisedValue := l.DeAliasEntryFieldValue(key, field, l.NormaliseFieldValue(field, key, value))

	valueProcessor, hasProcessor := fieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, key, normalisedValue)
	}

	return normalisedValue
}

// The general function call to process field values when reading from the cache.
// This should not need any normalisation of the field valie.

func (l *TBibTeXLibrary) ProcessCachedEntryFieldValue(key, field, value string) string {
	valueProcessor, hasProcessor := fieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, key, value)
	}

	return value
}

func init() {
	// Define the processing functions.
	fieldProcessors = TFieldProcessors{}
	fieldProcessors[DBLPField] = processDBLPValue
	fieldProcessors[PreferredAliasField] = processPreferredKeyValue
	fieldProcessors[TitleField] = processTitleValue
}
