/*
 *
 * Module: bibtex_config
 *
 * This module is concerned with general configuration parameters for handling BibTeX libraries.
 * Some of the things as presently set may be (partially) moved to a config file that is read when the application is started.
 * These are marked with (*)
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

var (
	BibTeXAllowedEntryFields map[string]TStringSet // Per entry type, the allowed field
	//BibTeXImportFields       TStringSet            // Set of fields we would consider importing
	BibTeXAllowedFields     TStringSet // Aggregation of all allowed fields
	BibTeXMustInheritFields TStringSet //
	BibTeXMayInheritFields  TStringSet //
	BibTeXInheritableFields TStringSet //
	BibTeXAllowedEntries    TStringSet // Aggregation of the allowed entry types
	BibTeXBookish           TStringSet // All book-alike entry types
	BibTeXCrossreffer       TStringSet //
	BibTeXFieldMap          TStringMap // Mapping of field names, to enable aliases and automatic corrections
	BibTeXEntryMap          TStringMap // Mapping of entry names, to enable automatic corrections
	BibTeXDefaultStrings    TStringMap // The default string definitions that will be used when opening a BibTeX file
	BibTeXCrossrefType      TStringMap // Entry type mapping for crossrefs
)

const (
	// The prefix used for the generated keys
	// (*) Should go into a config file.
	KeyPrefix   = "EP" // (*)
	FilesFolder = "Files/"

	CacheCommentsSeparator = "@@@@@@@@"

	FieldsCacheExtension   = ".cache_fields"
	CommentsCacheExtension = ".cache_comments"

	BibFileExtension = ".bib"

	NameMappingsFileExtension         = ".filter_name_mappings"
	EntryFieldMappingsFileExtension   = ".filter_entry_field_mappings"
	GenericFieldMappingsFileExtension = ".filter_generic_field_mappings"
	FieldMappingsFileExtension        = ".filter_field_mappings"

	NonDoublesFileExtension = ".non_double"
	KeyOldiesFileExtension  = ".key_oldies"
	KeyHintsFileExtension   = ".key_hints"
)

// When dealing with the resolution of ambiguities regarding fields of entries, we also want to treat the type of the entry as a field
// To avoid confusion with normal fields, use "illegal" field names
const (
	EntryTypeField      = "entrytype"
	PreferredAliasField = "preferredalias"
	DBLPField           = "dblp"
)

// Add the allowed fields for an entry, while updating the aggregations of allowed entries and fields.
func AddAllowedEntryFields(entry string, fields ...string) {
	BibTeXAllowedEntries.Add(entry)

	for _, field := range fields {
		BibTeXAllowedFields.Add(field)

		_, exists := BibTeXAllowedEntryFields[entry]
		if !exists {
			BibTeXAllowedEntryFields[entry] = TStringSetNew()
		}
		BibTeXAllowedEntryFields[entry].Set().Add(field)
	}
}

// Add the provided allowed fields to all of the (known so-far) allowed entries
func AddAllowedFields(fields ...string) {
	for entry := range BibTeXAllowedEntries.Elements() {
		AddAllowedEntryFields(entry, fields...)
	}
}

func init() {
	BibTeXDefaultStrings = TStringMap{
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

	BibTeXAllowedEntryFields = map[string]TStringSet{}

	// Use SAFE options?
	BibTeXAllowedEntries.Initialise()
	BibTeXAllowedFields.Initialise()
	//BibTeXImportFields.Initialise()
	BibTeXBookish.Initialise()
	BibTeXCrossreffer.Initialise()
	BibTeXMustInheritFields.Initialise()
	BibTeXMayInheritFields.Initialise()
	BibTeXInheritableFields.Initialise()

	AddAllowedEntryFields(
		"article", "journal", "volume", "number", "pages", "month", "issn")
	AddAllowedEntryFields(
		"book", "booktitle", "editor", "publisher", "volume", "number", "series", "address",
		"edition", "issn", "isbn")
	AddAllowedEntryFields(
		"inbook", "booktitle", "editor", "chapter", "pages", "publisher", "volume", "number",
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
		"month", "year", "note", "doi", "key", "author", "title",
		DBLPField, "researchgate",
		"eprinttype", "eprint", "langid",
		"url", "urldate", "urloriginal")

	// Needed for what?? Legacy? Import??
	//BibTeXImportFields.Unite(BibTeXAllowedFields)

	// Jabref
	AddAllowedFields(
		"creationdate", "modificationdate", "groups", "file", "owner")

	AddAllowedFields(
		PreferredAliasField, EntryTypeField)
	// Own fields

	AddAllowedFields(
		"repositum")
	// Handle for repositum of TU Wien

	// Refactor ...
	BibTeXBookish.Add("proceedings", "book")
	BibTeXCrossreffer.Add("inproceedings", "incollection", "inbook")
	BibTeXCrossrefType = TStringMap{}
	// Fill Crossreffer. Target must be Bookish
	BibTeXCrossrefType["inproceedings"] = "proceedings"
	BibTeXCrossrefType["incollection"] = "book"
	BibTeXCrossrefType["inbook"] = "book"

	BibTeXMustInheritFields.Add("booktitle", "year", "editor", "publisher", "volume", "number", "series",
		"address", "month", "edition", "issn", "isbn", "address", "type", "organization")
	BibTeXMayInheritFields.Add("doi", "url")
	BibTeXInheritableFields.Unite(BibTeXMustInheritFields).Unite(BibTeXMayInheritFields)
	// Consistency checks:
	// - Target of BibTeXCrossrefType must always be bookish
	// - BibTeXMustInheritFields must be among the fields of bookish entries.

	BibTeXEntryMap = TStringMap{}
	BibTeXEntryMap["conference"] = "inproceedings"
	// (*) The above one is an official alias.
	// The ones below are not, and should be moved to a config file.
	BibTeXEntryMap["softmisc"] = "misc"
	BibTeXEntryMap["online"] = "misc"
	BibTeXEntryMap["patent"] = "misc"
	BibTeXEntryMap["unpublished"] = "misc"

	BibTeXFieldMap = TStringMap{}
	// (*) The ones below should all be moved to a config file.
	BibTeXFieldMap["issue"] = "number"
	BibTeXFieldMap["editors"] = "editor"
	BibTeXFieldMap["authors"] = "author"
	BibTeXFieldMap["contributor"] = "author"
	BibTeXFieldMap["contributors"] = "author"
	BibTeXFieldMap["ee"] = "url"
	BibTeXFieldMap["language"] = "langid"
}
