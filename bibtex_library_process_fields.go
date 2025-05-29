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

var plainFieldProcessors,
	cachedFieldProcessors TFieldProcessors

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

	valueProcessor, hasProcessor := plainFieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, key, normalisedValue)
	}

	return normalisedValue
}

func (l *TBibTeXLibrary) ProcessCachedEntryFieldValue(key, field, value string) string {
	valueProcessor, hasProcessor := cachedFieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, key, value)
	}

	return value
}

func init() {
	// Define the processing functions.

	// NOTE ... if thet are the same ... why not use the same ....

	plainFieldProcessors = TFieldProcessors{}
	plainFieldProcessors["dblp"] = processDBLPValue
	plainFieldProcessors["preferredkey"] = processPreferredKeyValue
	plainFieldProcessors["title"] = processTitleValue

	cachedFieldProcessors = TFieldProcessors{}
	cachedFieldProcessors["dblp"] = processDBLPValue
	cachedFieldProcessors["preferredkey"] = processPreferredKeyValue
	cachedFieldProcessors["title"] = processTitleValue
}
