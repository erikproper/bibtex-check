//
// Module: bibtexconfig
//
// This module is concerned with general configuration parameters for handling BiBTeX libraries.
// Some of the things as presently set may be (partially) moved to a config file that is read when the application is started.
// These are marked with (*)
//
// Creator: Henderik A. Proper (erikproper@fastmail.com)
//
// Version of: 24.04.2024
//

package main

var (
	BiBTeXAllowedEntryFields map[string]TStringSet // Per entry type, the allowed field
	BiBTeXAllowedFields      TStringSet            // Aggregation of all allowed fields
	BiBTeXAllowedEntries     TStringSet            // Aggregation of the allowed entries.
	BiBTeXFieldMap           TStringMap            // Mapping of field names, to enable aliases and automatic corrections
	BiBTeXEntryMap           TStringMap            // Mapping of entry names, to enable automatic corrections
	BiBTeXDefaultStrings     TStringMap            // The default string definitions that will be used when opening a BiBTeX file
)

// The prefix used for the generated keys
// (*) Should go into a config file.
const KeyPrefix = "EP"

// Add the allowed fields for an entry, while updating the aggregations of allowed entries and fields.
func AddAllowedEntryFields(entry string, fields ...string) {
	BiBTeXAllowedEntries.Add(entry)

	for _, field := range fields {
		BiBTeXAllowedFields.Add(field)

		_, exists := BiBTeXAllowedEntryFields[entry]
		if !exists {
			BiBTeXAllowedEntryFields[entry] = TStringSetNew()
		}
		BiBTeXAllowedEntryFields[entry].Set().Add(field)
	}
}

// Add the provided allowed fields to all of the (known so-far) allowed entries
func AddAllowedFields(fields ...string) {
	for entry := range BiBTeXAllowedEntries.Elements() {
		AddAllowedEntryFields(entry, fields...)
	}
}

// Add an alias/correction for the provided entry
func AddEntryAlias(entry, alias string) {
	BiBTeXEntryMap[entry] = alias
}

// Add an alias/correction for the provided field
func FieldAlias(field, alias string) {
	BiBTeXFieldMap[field] = alias
}

func init() {
	BiBTeXDefaultStrings = TStringMap{
		"jan": "January",
		"feb": "February",
		"mar": "March",
		"apr": "April",
		"may": "May",
		"jun": "June",
		"jul": "July",
		"aug": "August",
		"sep": "September",
		"oct": "October",
		"nov": "November",
		"dec": "December",
	}

	BiBTeXAllowedEntryFields = map[string]TStringSet{}

	BiBTeXAllowedEntries.Initialise()
	BiBTeXAllowedFields.Initialise()

	AddAllowedEntryFields(
		"article", "journal", "volume", "number", "pages", "month", "issn")
	AddAllowedEntryFields(
		"book", "editor", "publisher", "volume", "number", "series", "address",
		"edition", "issn", "isbn")
	AddAllowedEntryFields(
		"inbook", "editor", "chapter", "pages", "publisher", "volume", "number",
		"series", "type", "address", "edition", "issn", "isbn", "crossref")
	AddAllowedEntryFields(
		"incollection", "booktitle", "publisher", "editor", "volume", "number", "series",
		"type", "chapter", "pages", "address", "edition", "issn", "isbn", "crossref")
	AddAllowedEntryFields(
		"inproceedings", "booktitle", "editor", "volume", "number", "series", "pages",
		"address", "organization", "publisher", "issn", "isbn", "crossref")
	AddAllowedEntryFields(
		"manual", "organization", "address", "edition", "issn", "isbn")
	AddAllowedEntryFields(
		"mastersthesis", "school", "type", "address", "issn", "isbn")
	AddAllowedEntryFields(
		"misc", "howpublished")
	AddAllowedEntryFields(
		"phdthesis", "school", "type", "address", "issn", "isbn")
	AddAllowedEntryFields(
		"proceedings", "booktitle", "editor", "volume", "number", "series", "address",
		"organization", "publisher", "issn", "isbn")
	AddAllowedEntryFields(
		"techreport", "institution", "type", "number", "address", "issn", "isbn")
	AddAllowedEntryFields(
		"unpublished")
	AddAllowedEntryFields(
		"booklet", "howpublished", "address", "issn", "isbn")
	// (*) The above ones are the official ones. It makes sense to allow a config file to add to these.

	AddAllowedFields(
		"month", "year", "note", "annote", "doi", "key", "author", "title",
		"alias", "dblp", "researchgate",
		"eprinttype", "eprint",
		"local-url", "langid",
		"url", "urldate", "urloriginal",
		"date-added", "date-modified",
		"bdsk-url-1", "bdsk-url-2", "bdsk-url-3", "bdsk-url-4", "bdsk-url-5",
		"bdsk-url-6", "bdsk-url-7", "bdsk-url-8", "bdsk-url-9",
		"bdsk-file-1", "bdsk-file-2", "bdsk-file-3", "bdsk-file-4", "bdsk-file-5",
		"bdsk-file-6", "bdsk-file-7", "bdsk-file-8", "bdsk-file-9")
	// (*) The above ones are the ones needed for my purposes in the BiBDesk contect.
	// It makes sense to allow a config file to add to these, and move some of the above to this config file as well.
	// For instance "researchgate" and "urloriginal"

	BiBTeXEntryMap = TStringMap{}
	BiBTeXEntryMap["conference"] = "inproceedings"
	// (*) The above one is an official alias.
	// The ones below are not, and should be moved to a config file.
	BiBTeXEntryMap["softmisc"] = "misc"
	BiBTeXEntryMap["patent"] = "misc"
	BiBTeXEntryMap["unpublished"] = "misc"

	BiBTeXFieldMap = TStringMap{}
	// (*) These ones should all be moved to a config file.
	BiBTeXFieldMap["editors"] = "editor"
	BiBTeXFieldMap["authors"] = "author"
	BiBTeXFieldMap["contributor"] = "author"
	BiBTeXFieldMap["contributors"] = "author"
	BiBTeXFieldMap["ee"] = "url"

	// We probably want to get rid of these, as soon as we're finished with the legacy migration.
	// Although the "_" seems to occur in "harvested" libraries as well.
	if AllowLegacy {
		for field := range BiBTeXAllowedFields.Elements() {
			BiBTeXFieldMap["x"+field] = field
			BiBTeXFieldMap["_"+field] = field
		}
	}
}
