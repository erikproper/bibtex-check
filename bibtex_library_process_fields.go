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
type TFieldProcessors = map[string]func(*TBibTeXLibrary, string)

var plainFieldProcessors,
	cacheFieldProcessors TFieldProcessors

func processDBLPValue(l *TBibTeXLibrary, value string) {
	l.AddKeyAlias("DBLP:"+value, l.currentKey)
}

func processPreferredKeyValue(l *TBibTeXLibrary, value string) {
	l.AddKeyAlias(value, l.currentKey)
}

func processTitleValue(l *TBibTeXLibrary, value string) {
	l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(value), l.currentKey)
}

// The general function call to process field values.
// It always, first, conducts a normalisation of the field value.
// If a field specific process function exists, then it is applied on the normalise value.
// Otherwise, we simply return the normalised value.
func (l *TBibTeXLibrary) ProcessPlainEntryFieldValue(entry, field, value string) string {
	normalisedValue := l.DeAliasEntryFieldValue(entry, field, l.NormaliseFieldValue(field, value))

	valueProcessor, hasProcessor := plainFieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, normalisedValue)
	}

	return normalisedValue
}

func (l *TBibTeXLibrary) ProcessCacheEntryFieldValue(entry, field, value string) string {
	// This needs serious fixing!
	l.currentKey = entry
	
	valueProcessor, hasProcessor := cacheFieldProcessors[field]
	if hasProcessor {
		valueProcessor(l, value)
	}

	return value
}

func init() {
	// Define the processing functions.
	plainFieldProcessors = TFieldProcessors{}
	plainFieldProcessors["dblp"] = processDBLPValue
	plainFieldProcessors["preferredkey"] = processPreferredKeyValue
	plainFieldProcessors["title"] = processTitleValue
	
	cacheFieldProcessors = TFieldProcessors{}
	cacheFieldProcessors["dblp"] = processDBLPValue
	cacheFieldProcessors["preferredkey"] = processPreferredKeyValue
}
