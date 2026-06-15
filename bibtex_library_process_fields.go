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
type TFieldProcessors = map[string]func(*TBibTeXLibrary, string, string) (string, string)

var fieldProcessors TFieldProcessors

func processDBLPValue(l *TBibTeXLibrary, key, value string) (string, string) {
	l.AddKeyAlias(KeyForDBLP(value), key)
	addDblpKeyHintTransient(l, KeyForDBLP(value), key)

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



func processFieldToIgnoreValue(l *TBibTeXLibrary, key, value string) (string, string) {
	return IgnoreField, ""
}

// processFieldCapturePDFRef passes the value through when harvestCapturePDFFields is
// active (so harvest can copy the referenced PDF), otherwise ignores it like
// processFieldToIgnoreValue.
func processFieldCapturePDFRef(l *TBibTeXLibrary, key, value string) (string, string) {
	if l.harvestCapturePDFFields {
		return key, value
	}
	return IgnoreField, ""
}

// The general function call to process field values.
// It always, first, conducts a normalisation of the field value.
// If a field specific process function exists, then it is applied on the normalised value.
// Otherwise, we simply return the normalised value.
// When capturedDBLPEntry is active the raw pre-normalisation value is captured directly,
// so that verify and fix comparisons work against the as-scraped data.
func (l *TBibTeXLibrary) ProcessRawEntryFieldValue(key, field, value string) {
	if l.capturedDBLPEntry != nil {
		if value != "" {
			l.capturedDBLPEntry.Fields[field] = value
		}
		return
	}

	value = l.MapNormalisedEntryFieldValue(key, field, value)

	l.ProcessEntryFieldValue(key, field, value)
}

// The general function call to process field values when reading from the cache.
// This should not need any normalisation of the field value.

func (l *TBibTeXLibrary) ProcessEntryFieldValue(key, field, value string) {
	if l.capturedDBLPEntry != nil {
		if value != "" {
			l.capturedDBLPEntry.Fields[field] = value
		}
		return
	}
	valueProcessor, hasProcessor := fieldProcessors[field]
	if hasProcessor {
		field, value = valueProcessor(l, key, value)
	}
	if value != "" {
		upsertBibEntryField(key, field, value)
	}
}

func init() {
	// Declare the processing functions.
	fieldProcessors = TFieldProcessors{}

	// General
	fieldProcessors[DBLPField] = processDBLPValue
	fieldProcessors[PreferredAliasField] = processPreferredAliasValue
	fieldProcessors[TitleField] = processTitleValue

	fieldProcessors[IgnoreField] = processFieldToIgnoreValue

	///// NUANCE when doing syncs ...
	fieldProcessors["abstract"] = processFieldToIgnoreValue
	fieldProcessors["keywords"] = processFieldToIgnoreValue

	// Jabref
	fieldProcessors[JabrefFileField] = processFieldCapturePDFRef // captured during harvest; otherwise ignored
	fieldProcessors["owner"] = processFieldToIgnoreValue
	fieldProcessors["creationdate"] = processFieldToIgnoreValue
	fieldProcessors["modificationdate"] = processFieldToIgnoreValue
	fieldProcessors["groups"] = processFieldToIgnoreValue

	// BibDesk
	// bdsk-file-* are ignored; PDF presence is tracked via PDFFiles, not DB.
	fieldProcessors["bdsk-file-1"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-2"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-3"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-4"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-5"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-6"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-7"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-8"] = processFieldToIgnoreValue
	fieldProcessors["bdsk-file-9"] = processFieldToIgnoreValue
	fieldProcessors[LocalURLField] = processFieldCapturePDFRef // captured during harvest; otherwise ignored
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
