package main

var (
	BiBTeXAllowedEntryFields map[string]TStringSet
	BiBTeXAllowedFields,
	BiBTeXAllowedEntries TStringSet
	BiBTeXFieldNameMap,
	BiBTeXEntryNameMap,
	BiBTeXDefaultStrings TStringMap
)

// / Set / Get !!
func AllowedEntryFields(entry string, fields ...string) {
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

func AllowedFields(fields ...string) {
	for entry := range BiBTeXAllowedEntries.Elements() {
		AllowedEntryFields(entry, fields...)
	}
}

func EntryTypeAlias(entry, alias string) {
	BiBTeXEntryNameMap[entry] = alias
}

func FieldTypeAlias(entry, alias string) {
	BiBTeXEntryNameMap[entry] = alias
}

func init() {
	// Some if this should move into a settings file.
	// Settings should be an environment variable ...
	// see https://gobyexample.com/environment-variables
	// If settings file does not exist, then create one and push this as default into ib.

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

	AllowedEntryFields(
		"article", "journal", "volume", "number", "pages", "month", "issn")

	AllowedEntryFields(
		"book", "editor", "publisher", "volume", "number", "series", "address",
		"edition", "issn", "isbn")
	AllowedEntryFields(
		"inbook", "editor", "chapter", "pages", "publisher", "volume", "number",
		"series", "type", "address", "edition", "issn", "isbn", "crossref")
	AllowedEntryFields(
		"incollection", "booktitle", "publisher", "editor", "volume", "number", "series",
		"type", "chapter", "pages", "address", "edition", "issn", "isbn", "crossref")
	AllowedEntryFields(
		"inproceedings", "booktitle", "editor", "volume", "number", "series", "pages",
		"address", "organization", "publisher", "issn", "isbn", "crossref")
	AllowedEntryFields(
		"manual", "organization", "address", "edition", "issn", "isbn")
	AllowedEntryFields(
		"mastersthesis", "school", "type", "address", "issn", "isbn")
	AllowedEntryFields(
		"misc", "howpublished")
	AllowedEntryFields(
		"phdthesis", "school", "type", "address", "issn", "isbn")
	AllowedEntryFields(
		"proceedings", "booktitle", "editor", "volume", "number", "series", "address",
		"organization", "publisher", "issn", "isbn")
	AllowedEntryFields(
		"techreport", "institution", "type", "number", "address", "issn", "isbn")
	AllowedEntryFields(
		"unpublished")
	AllowedEntryFields(
		"booklet", "howpublished", "address", "issn", "isbn")

	AllowedFields(
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

	BiBTeXEntryNameMap = TStringMap{}
	BiBTeXEntryNameMap["conference"] = "inproceedings"
	BiBTeXEntryNameMap["softmisc"] = "misc"
	BiBTeXEntryNameMap["patent"] = "misc"
	BiBTeXEntryNameMap["unpublished"] = "misc"

	BiBTeXFieldNameMap = TStringMap{}
	BiBTeXFieldNameMap["editors"] = "editor"
	BiBTeXFieldNameMap["authors"] = "author"
	BiBTeXFieldNameMap["contributor"] = "author"
	BiBTeXFieldNameMap["contributors"] = "author"
	BiBTeXFieldNameMap["ee"] = "url"

	for field := range BiBTeXAllowedFields.Elements() {
		BiBTeXFieldNameMap["x"+field] = field
		BiBTeXFieldNameMap["_"+field] = field
	}
}
