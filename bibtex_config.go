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
	BibTeXImportFields       TStringSet            // Set of fields we would consider importing
	BibTeXAllowedFields      TStringSet            // Aggregation of all allowed fields
	BibTeXAllowedEntries     TStringSet            // Aggregation of the allowed entry types
	BibTeXBookish            TStringSet            // All book-alike entry types
	BibTeXFieldMap           TStringMap            // Mapping of field names, to enable aliases and automatic corrections
	BibTeXEntryMap           TStringMap            // Mapping of entry names, to enable automatic corrections
	BibTeXDefaultStrings     TStringMap            // The default string definitions that will be used when opening a BibTeX file
	BibTeXCrossrefType       TStringMap            // Entry type mapping for crossrefs
)

const (
	// The prefix used for the generated keys
	// (*) Should go into a config file.
	KeyPrefix = "EP" // (*)

	BibFileExtension                 = ".bib"
	KeyAliasesFileExtension          = ".keys"
	PreferredKeyAliasesFileExtension = ".preferred"
	NameAliasesFileExtension         = ".names"
	JournalAliasesFileExtension      = ".journals"
	PublisherAliasesFileExtension    = ".publishers"
	InstitutionAliasesFileExtension  = ".institutions"
	OrganisationAliasesFileExtension = ".organisations"
	SchoolAliasesFileExtension       = ".schools"
	SeriesAliasesFileExtension       = ".series"
	ChallengesFileExtension          = ".challenges"
	AddressesFileExtension           = ".addresses"
	ISSNFileExtension                = ".issn"
)

// When dealing with the resolution of ambiguities regarding fields of entries, we also want to treat the type of the entry as a field
// To avoid confusion with normal fields, use "illegal" field names
const EntryTypeField = "$entry-type$"

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

// Add an alias/correction for the provided entry
func AddEntryAlias(entry, alias string) {
	BibTeXEntryMap[entry] = alias
}

// Add an alias/correction for the provided field
func FieldAlias(field, alias string) {
	BibTeXFieldMap[field] = alias
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
	BibTeXImportFields.Initialise()
	BibTeXBookish.Initialise()

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
		"month", "year", "note", "annote", "doi", "key", "author", "title",
		"alias", "dblp", "researchgate",
		"eprinttype", "eprint",
		"local-url", "langid",
		"url", "urldate", "urloriginal")

	BibTeXImportFields.Unite(BibTeXAllowedFields)

	AddAllowedFields(
		"date-added", "date-modified",
		"bdsk-url-1", "bdsk-url-2", "bdsk-url-3", "bdsk-url-4", "bdsk-url-5",
		"bdsk-url-6", "bdsk-url-7", "bdsk-url-8", "bdsk-url-9",
		"bdsk-file-1", "bdsk-file-2", "bdsk-file-3", "bdsk-file-4", "bdsk-file-5",
		"bdsk-file-6", "bdsk-file-7", "bdsk-file-8", "bdsk-file-9")
	// (*) The above ones are the ones needed for my purposes in the BiBDesk contect.
	// It makes sense to allow a config file to add to these, and move some of the above to this config file as well.
	// For instance "researchgate" and "urloriginal"

	BibTeXBookish.Add("proceedings", "book")

	BibTeXCrossrefType = TStringMap{}
	BibTeXCrossrefType["inproceedings"] = "proceedings"
	BibTeXCrossrefType["incollection"] = "book"
	BibTeXCrossrefType["inbook"] = "book"

	BibTeXEntryMap = TStringMap{}
	BibTeXEntryMap["conference"] = "inproceedings"
	// (*) The above one is an official alias.
	// The ones below are not, and should be moved to a config file.
	BibTeXEntryMap["softmisc"] = "misc"
	BibTeXEntryMap["patent"] = "misc"
	BibTeXEntryMap["unpublished"] = "misc"

	BibTeXFieldMap = TStringMap{}
	// (*) The ones below should all be moved to a config file.
	BibTeXFieldMap["editors"] = "editor"
	BibTeXFieldMap["authors"] = "author"
	BibTeXFieldMap["contributor"] = "author"
	BibTeXFieldMap["contributors"] = "author"
	BibTeXFieldMap["ee"] = "url"
	BibTeXFieldMap["language"] = "langid"

	// We probably want to get rid of these, as soon as we're finished with the legacy migration.
	// Although the "_" seems to occur in "harvested" libraries as well.
	if AllowLegacy {
		AddAllowedFields("file")

		for field := range BibTeXAllowedFields.Elements() {
			BibTeXFieldMap["x"+field] = field
			BibTeXFieldMap["_"+field] = field
		}
	}
}
