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

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// Definition of the map for field processors
type TFieldProcessors = map[string]func(*TBibTeXLibrary, string, string) (string, string)

var fieldProcessors TFieldProcessors

func processDBLPValue(l *TBibTeXLibrary, key, value string) (string, string) {
	l.AddKeyAlias(KeyForDBLP(value), key)
	l.AddKeyHint(KeyForDBLP(value), key)

	return DBLPField, value
}

func processPreferredAliasValue(l *TBibTeXLibrary, key, value string) (string, string) {
	l.AddKeyAlias(value, key)
	l.AddKeyHint(value, key)

	return PreferredAliasField, value
}

func processTitleValue(l *TBibTeXLibrary, key, value string) (string, string) {
	l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(value), key)

	return TitleField, value
}

func processGroupsValue(l *TBibTeXLibrary, key, value string) (string, string) {
	for _, group := range strings.Split(value, ",") {
		l.GroupEntries.AddValueToStringSetMap(strings.TrimSpace(group), key)
	}

	return GroupsField, ""
}

func processJabrefFileValue(l *TBibTeXLibrary, key, value string) (string, string) {
	var (
		trimFileReferenceStart = regexp.MustCompile(`^[~:]*:`)
		trimFileReferenceEnd   = regexp.MustCompile(`:[^:]*$`)
	)

	// Remove leading/trailing stuff
	value = trimFileReferenceStart.ReplaceAllString(value, "")
	value = trimFileReferenceEnd.ReplaceAllString(value, "")

	return LocalURLField, value
}

func processBDSKFileValue(l *TBibTeXLibrary, key, value string) (string, string) {
	if value != "" {
		// Decode the provided value, and get the payload as a string.
		data, _ := base64.StdEncoding.DecodeString(value)
		payload := string(data)

		// Find start of filename.
		valueStart := strings.Index(payload, "relativePathXbookmark") + len("relativePathXbookmark") + 3
		// Find the end of the filename
		valueEnd := strings.Index(payload, ".pdf") + 4

		// If we cannot find the ".pdf", there is not really a file.
		if valueEnd <= 4 {
			return LocalURLField, ""
		}

		// We use the raw payload as the default filename
		value := payload
		// But if we have a correct "cutout" of the filename we will use that:
		if 0 <= valueStart && valueStart < valueEnd && valueEnd <= len(payload) {
			value = payload[valueStart:valueEnd]
		}

		return LocalURLField, value
	} else {
		return LocalURLField, ""
	}
}

func processFieldToIgnoreValue(l *TBibTeXLibrary, key, value string) (string, string) {
	return IgnoreField, ""
}

// The general function call to process field values.
// It always, first, conducts a normalisation of the field value.
// If a field specific process function exists, then it is applied on the normalised value.
// Otherwise, we simply return the normalised value.
func (l *TBibTeXLibrary) ProcessRawEntryFieldValue(key, field, value string) {
	value = l.MapNormalisedEntryFieldValue(key, field, value)

	l.ProcessEntryFieldValue(key, field, value)
}

// The general function call to process field values when reading from the cache.
// This should not need any normalisation of the field valie.

func (l *TBibTeXLibrary) ProcessEntryFieldValue(key, field, value string) {
	valueProcessor, hasProcessor := fieldProcessors[field]
	if hasProcessor {
		field, value = valueProcessor(l, key, value)
	}

	if value != "" {
		l.EntryFields.SetValueForStringPairMap(key, field, value)
	}
}

func init() {
	// Declare the processing functions.
	fieldProcessors = TFieldProcessors{}

	// General
	fieldProcessors[DBLPField] = processDBLPValue
	fieldProcessors[PreferredAliasField] = processPreferredAliasValue
	fieldProcessors[TitleField] = processTitleValue
	fieldProcessors[GroupsField] = processGroupsValue
	fieldProcessors[IgnoreField] = processFieldToIgnoreValue

	///// NUANCE when doing syncs ...
	fieldProcessors["abstract"] = processFieldToIgnoreValue
	fieldProcessors["keywords"] = processFieldToIgnoreValue

	// Jabref
	fieldProcessors[JabrefFileField] = processJabrefFileValue
	fieldProcessors["owner"] = processFieldToIgnoreValue
	fieldProcessors["creationdate"] = processFieldToIgnoreValue
	fieldProcessors["modificationdate"] = processFieldToIgnoreValue
	fieldProcessors["groups"] = processFieldToIgnoreValue

	// BibDesk
	fieldProcessors["bdsk-file-1"] = processBDSKFileValue
	fieldProcessors["bdsk-file-2"] = processBDSKFileValue
	fieldProcessors["bdsk-file-3"] = processBDSKFileValue
	fieldProcessors["bdsk-file-4"] = processBDSKFileValue
	fieldProcessors["bdsk-file-5"] = processBDSKFileValue
	fieldProcessors["bdsk-file-6"] = processBDSKFileValue
	fieldProcessors["bdsk-file-7"] = processBDSKFileValue
	fieldProcessors["bdsk-file-8"] = processBDSKFileValue
	fieldProcessors["bdsk-file-9"] = processBDSKFileValue
	fieldProcessors["bdsk-url-1"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-2"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-3"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-4"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-5"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-6"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-7"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-8"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-url-9"] = processFieldToIgnoreValue
	fieldProcessors["date-added"] = processFieldToIgnoreValue
	fieldProcessors["date-modified"] = processFieldToIgnoreValue
}
