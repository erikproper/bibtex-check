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
	NoKey = ""

	BibFileExtension    = ".bib"
	cacheFileExtension  = ".cache"
	ConfigFileExtension = ".config"
	LockFileExtension   = ".lock"

	// Mapping/hint/oldie tables live in a <basename>.tables/ subfolder as CSV files.
	NameMappingsFilePath          = ".tables/filter_name_mappings.csv"
	EntryFieldMappingsFilePath    = ".tables/filter_entry_field_mappings.csv"
	GenericFieldMappingsFilePath  = ".tables/filter_generic_field_mappings.csv"
	CrossFieldMappingsFilePath    = ".tables/filter_cross_field_mappings.csv"
	StateNamesFilePath                = ".tables/filter_state_names.csv"
	StateCountriesFilePath            = ".tables/filter_state_countries.csv"
	CountryNamesFilePath              = ".tables/filter_country_names.csv"
	BooktitleCountryNamesFilePath     = ".tables/filter_booktitle_country_names.csv"
	KeyNonDoublesFilePath  = ".tables/key_non_doubles.csv"
	KeyOldiesFilePath      = ".tables/key_oldies.csv"
	KeyHintsFilePath       = ".tables/key_hints.csv"
	WatchFilePath           = ".tables/watch.csv"
	ScriptFilePath          = ".script"
	DblpParentFilePath      = ".tables/dblp_parent.csv"
	DblpWaivedFilePath      = ".tables/dblp_waived.csv"
	EntryMetadataFilePath   = ".tables/entry_metadata.json"

	ShortenMappingsFilePath = "shorten_mappings.csv"
	EntryFlagsFilePath      = ".tables/entry_flags.csv"

	URLsIgnoreFilePath = ".tables/urls_ignore.csv"
	URLsFailedFilePath = "urls_failed.csv"

	DefaultLanguage = "eng"
)

// Entry flag values stored in entry_flags.csv / the entry_flags SQLite table.
const (
	EntryFlagNoDBLPChildren = "no_dblp_children"
)

// When dealing with the resolution of ambiguities regarding fields of entries, we also want to treat the type of the entry as a field
// To avoid confusion with normal fields, use "illegal" field names
const (
	EntryTypeField      = "entrytype"
	IgnoreField         = ""
	PreferredAliasField = "preferredalias"
	DBLPField           = "dblp"
	TitleField          = "title"
	GroupsField         = "groups"
	JabrefFileField     = "file"
	LocalURLField       = "local-url"
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
		"misc", "howpublished", "crossref")
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
		"month", "year", "note", "doi", "key", "author", TitleField,
		DBLPField, "researchgate", "abstract", "ketwords",
		"eprinttype", "eprint", "langid",
		"url", "urldate", "urloriginal",
		"withdrawn")

	// Needed for what?? Legacy? Import??
	//BibTeXImportFields.Unite(BibTeXAllowedFields)

	// Handle for repositum of TU Wien
	AddAllowedFields(
		"repositum")

	// Jabref
	AddAllowedFields(
		"owner", "creationdate", "modificationdate", "groups", JabrefFileField)

	// BibDesk
	AddAllowedFields(
		LocalURLField,
		"date-added", "date-modified",
		"bdsk-url-1", "bdsk-url-2", "bdsk-url-3", "bdsk-url-4", "bdsk-url-5",
		"bdsk-url-6", "bdsk-url-7", "bdsk-url-8", "bdsk-url-9",
		"bdsk-file-1", "bdsk-file-2", "bdsk-file-3", "bdsk-file-4", "bdsk-file-5",
		"bdsk-file-6", "bdsk-file-7", "bdsk-file-8", "bdsk-file-9")

	AddAllowedFields(
		PreferredAliasField, EntryTypeField)
	// Own fields

	// Refactor ...
	BibTeXBookish.Add("proceedings", "book")
	BibTeXCrossreffer.Add("inproceedings", "incollection", "inbook", "misc")
	BibTeXCrossrefType = TStringMap{}
	// Fill Crossreffer. Target must be Bookish
	BibTeXCrossrefType["inproceedings"] = "proceedings"
	BibTeXCrossrefType["incollection"] = "book"
	BibTeXCrossrefType["inbook"] = "book"
	BibTeXCrossrefType["misc"] = "misc"

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
	BibTeXFieldMap["organisation"] = "organization"
	BibTeXFieldMap["institute"] = "institution"
	BibTeXFieldMap["group"] = "groups"
	BibTeXFieldMap["issue"] = "number"
	BibTeXFieldMap["editors"] = "editor"
	BibTeXFieldMap["authors"] = "author"
	BibTeXFieldMap["contributor"] = "author"
	BibTeXFieldMap["contributors"] = "author"
	BibTeXFieldMap["ee"] = "url"
	BibTeXFieldMap["language"] = "langid"
}
