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

import "strings"

//import "fmt"

// Definition of the map for field processors
type TFieldProcessors = map[string]func(*TBibTeXLibrary, string, string) string

var fieldProcessors TFieldProcessors

func processDBLPValue(l *TBibTeXLibrary, key, value string) string {
	l.AddKeyAlias(KeyForDBLP(value), key)
	l.AddKeyHint(KeyForDBLP(value), key)

	return value
}

func processPreferredKeyValue(l *TBibTeXLibrary, key, value string) string {
	l.AddKeyAlias(value, key)
	l.AddKeyHint(value, key)

	return value
}

func processTitleValue(l *TBibTeXLibrary, key, value string) string {
	l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(value), key)

	return value
}

func processGroupsValue(l *TBibTeXLibrary, key, value string) string {
	for _, group := range strings.Split(value, ",") {
		l.EntryGroups.AddValueToStringSetMap(key, strings.TrimSpace(group))
	}

	return ""
}

// The general function call to process field values.
// It always, first, conducts a normalisation of the field value.
// If a field specific process function exists, then it is applied on the normalised value.
// Otherwise, we simply return the normalised value.
func (l *TBibTeXLibrary) ProcessEntryFieldValue(key, field, valueRAW string) string {
	value := l.DeAliasNormalisedEntryFieldValue(key, field, valueRAW)

	valueProcessor, hasProcessor := fieldProcessors[field]
	if hasProcessor {
		return valueProcessor(l, key, value)
	}

	return value
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
	fieldProcessors[GroupsField] = processGroupsValue
}
