package main

import (
	"encoding/base64"
	"regexp"
	"strings"
)

type TFieldProcessors = map[string]func(*TBibTeXLibrary, string) string

const (
	WarningMissingFile = "File %s for key %s seems not to exist"
	WarningBad         = "Found wrong "
	WarningForKey      = " for key %s"
	WarningBadISBN     = WarningBad + "ISBN \"%s\"" + WarningForKey
	WarningBadISSN     = WarningBad + "ISSN \"%s\"" + WarningForKey
	WarningBadYear     = WarningBad + "year \"%s\"" + WarningForKey
)

var fieldProcessors TFieldProcessors

func processDOIValue(library *TBibTeXLibrary, rawDOI string) string {
	var (
		trimDOIStart = regexp.MustCompile(`^(doi:|http://[a-z,.]*/)`)
		trimmedDOI   string
	)

	trimmedDOI = strings.TrimSpace(rawDOI)
	trimmedDOI = trimDOIStart.ReplaceAllString(trimmedDOI, "")
	trimmedDOI = strings.ReplaceAll(trimmedDOI, "\\_", "_")
	trimmedDOI = strings.ReplaceAll(trimmedDOI, "{$", "")
	trimmedDOI = strings.ReplaceAll(trimmedDOI, "$}", "")

	return trimmedDOI
}

func processISSNValue(library *TBibTeXLibrary, rawISSN string) string {
	var (
		trimISSNStart = regexp.MustCompile(`^ *ISSN[:]? *`)
		trimmedISSN   string
	)

	trimmedISSN = trimISSNStart.ReplaceAllString(rawISSN, "")
	trimmedISSN = strings.ReplaceAll(trimmedISSN, ",", " ")
	trimmedISSN = strings.TrimSpace(trimmedISSN)
	trimmedISSN = strings.Split(trimmedISSN, " ")[0]
	trimmedISSN = strings.ReplaceAll(trimmedISSN, "-", "")

	if CheckISSNValidity(trimmedISSN) {
		return trimmedISSN[0:4] + "-" + trimmedISSN[4:8]
	} else {
		if !library.legacyMode {
			library.Warning(WarningBadISSN, rawISSN, library.currentKey)
		}
		return strings.TrimSpace(rawISSN)
	}
}

func processISBNValue(library *TBibTeXLibrary, rawISBN string) string {
	var (
		trimmedISBN   string
		trimISBNStart = regexp.MustCompile(`^ *ISBN[-]?(10|13|)[:]? *`)
	)

	trimmedISBN = trimISBNStart.ReplaceAllString(rawISBN, "")
	trimmedISBN = strings.ReplaceAll(trimmedISBN, ",", " ")
	trimmedISBN = strings.TrimSpace(trimmedISBN)
	trimmedISBN = strings.Split(trimmedISBN, " ")[0]

	if CheckISBNValidity(trimmedISBN) {
		return trimmedISBN
	} else {
		if !library.legacyMode {
			library.Warning(WarningBadISBN, rawISBN, library.currentKey)
		}

		return strings.TrimSpace(rawISBN)
	}
}

func processYearValue(library *TBibTeXLibrary, rawYear string) string {
	trimmedYear := strings.TrimSpace(rawYear)

	if CheckYearValidity(trimmedYear) {
		return trimmedYear
	} else {
		if !library.legacyMode {
			library.Warning(WarningBadYear, rawYear, library.currentKey)
		}

		return strings.TrimSpace(rawYear)
	}
}

func processDBLPValue(library *TBibTeXLibrary, value string) string {
	if !library.legacyMode {
		library.AddKeyAlias("DBLP:"+value, library.currentKey, false)
	}

	return strings.TrimSpace(value)
}

func processCrossrefValue(library *TBibTeXLibrary, crossref string) string {
	trimmedCrossref := strings.TrimSpace(crossref)

	if library.legacyMode {
		// Note that the next call is hard-wired to the main Library.
		// Only needed while still allowing library.legacyMode mode.
		key, isKey := Library.LookupEntry(trimmedCrossref)

		if isKey {
			return key
		}
	}

	return trimmedCrossref
}

func processPagesValue(library *TBibTeXLibrary, pages string) string {
	var trimDashes = regexp.MustCompile(`-+`)

	trimedPageRanges := ""

	trimedPageRanges = strings.TrimSpace(pages)
	trimedPageRanges = strings.ReplaceAll(trimedPageRanges, " ", "")
	trimedPageRanges = trimDashes.ReplaceAllString(trimedPageRanges, "-")

	rangesList := ""
	comma := ""

	for _, pageRange := range strings.Split(trimedPageRanges, ",") {
		trimedPagesList := strings.Split(pageRange, "-")
		switch {
		case len(trimedPagesList) == 0:
			return pages

		case len(trimedPagesList) == 1:
			return trimedPagesList[0]

		case len(trimedPagesList) == 2:
			firstPagePair := strings.Split(trimedPagesList[0], ":")
			secondPagePair := strings.Split(trimedPagesList[1], ":")

			if len(firstPagePair) == 1 || len(secondPagePair) == 1 {
				rangesList += comma + trimedPagesList[0] + "--" + trimedPagesList[1]
			} else if len(firstPagePair) == 2 && len(secondPagePair) == 2 {
				if firstPagePair[0] == secondPagePair[0] {
					rangesList += comma + trimedPagesList[0] + "--" + secondPagePair[1]
				} else {
					rangesList += comma + trimedPagesList[0] + "--" + trimedPagesList[1]
				}
			} else {
				return pages
			}

		default:
			return pages
		}

		comma = ", "
	}

	return rangesList
}

func processFileValue(library *TBibTeXLibrary, rawFile string) string {
	var (
		trimFileStart = regexp.MustCompile(`^.*/Zotero/storage/`)
		trimFileEnd   = regexp.MustCompile(`.pdf:.*$`)
		trimmedFile   string
	)

	if library.legacyMode {
		trimmedFile = trimFileStart.ReplaceAllString(rawFile, "")
		trimmedFile = trimFileEnd.ReplaceAllString(trimmedFile, "") + ".pdf"

		// Hardwired ... legacy!!
		if FileExists("/Users/erikproper/BiBTeX/Zotero/" + trimmedFile) {
			return "/Users/erikproper/BiBTeX/Zotero/" + trimmedFile
		} else {
			return ""
		}
	} else {
		return ""
	}
}

func processBDSKFileValue(library *TBibTeXLibrary, value string) string {
	data, _ := base64.StdEncoding.DecodeString(value)
	payload := string(data)

	fileNameStart := strings.Index(payload, "relativePathXbookmark") + len("relativePathXbookmark") + 3
	fileNameEnd := strings.Index(payload, ".pdf") + 4
	fileName := payload

	if 0 <= fileNameStart && fileNameStart < fileNameEnd && fileNameEnd <= len(payload) {
		fileName = payload[fileNameStart:fileNameEnd]
	}

	if FileExists(library.files + fileName) {
		return value
	} else {
		if !library.legacyMode {
			library.Warning(WarningMissingFile, fileName, library.currentKey)
		}

		return ""
	}
}

// /////// Include the key here or not??
func (l *TBibTeXLibrary) ProcessFieldValue(field, value string) string {
	valueNormaliser, hasNormaliser := fieldProcessors[field]

	if hasNormaliser {
		return valueNormaliser(l, value)
	} else {
		return strings.TrimSpace(value)
	}
}

func init() {
	fieldProcessors = TFieldProcessors{}
	fieldProcessors["bdsk-file-1"] = processBDSKFileValue
	fieldProcessors["bdsk-file-2"] = processBDSKFileValue
	fieldProcessors["bdsk-file-3"] = processBDSKFileValue
	fieldProcessors["bdsk-file-4"] = processBDSKFileValue
	fieldProcessors["bdsk-file-5"] = processBDSKFileValue
	fieldProcessors["bdsk-file-6"] = processBDSKFileValue
	fieldProcessors["bdsk-file-7"] = processBDSKFileValue
	fieldProcessors["bdsk-file-8"] = processBDSKFileValue
	fieldProcessors["bdsk-file-9"] = processBDSKFileValue
	fieldProcessors["dblp"] = processDBLPValue
	fieldProcessors["crossref"] = processCrossrefValue // only needed while still allowing library.legacyMode
	fieldProcessors["doi"] = processDOIValue
	fieldProcessors["isbn"] = processISBNValue
	fieldProcessors["issn"] = processISSNValue
	fieldProcessors["file"] = processFileValue // only needed while still allowing library.legacyMode
	fieldProcessors["pages"] = processPagesValue
	fieldProcessors["year"] = processYearValue

	// "address"
	// "author"
	// "bdsk-url-1"
	// "bdsk-url-2"
	// "bdsk-url-3"
	// "bdsk-url-4"
	// "bdsk-url-5"
	// "bdsk-url-6"
	// "bdsk-url-7"
	// "bdsk-url-8"
	// "bdsk-url-9"
	// "booktitle"
	// "chapter"
	// "edition"
	// "editor"
	// "howpublished"
	// "institution"
	// "journal"
	// "key"
	// "langid"
	// "local-url"
	// "month"
	// "note"
	// "number"
	// "organization"
	// "publisher"
	// "researchgate"
	// "school"
	// "series"
	// "title"
	// "type"
	// "url"
	// "urldate"
	// "urloriginal"
	// "volume"
}
