package main

import (
	"encoding/base64"
	"os"
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

func processISSNValue(library *TBibTeXLibrary, rawISSN string) string {
	var (
		trimISSNStart = regexp.MustCompile(`^ *ISSN[:]? *`)
		validISSN     = regexp.MustCompile(`^[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9,X]$`)
		trimmedISSN   string
	)

	trimmedISSN = trimISSNStart.ReplaceAllString(rawISSN, "")
	trimmedISSN = strings.ReplaceAll(trimmedISSN, ",", " ")
	trimmedISSN = strings.TrimSpace(trimmedISSN)
	trimmedISSN = strings.Split(trimmedISSN, " ")[0]
	trimmedISSN = strings.ReplaceAll(trimmedISSN, "-", "")

	if validISSN.MatchString(trimmedISSN) {
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
		validISBN     = regexp.MustCompile(`^([0-9][-]?[0-9][-]?[0-9][-]?|)[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9,X]$`)
	)

	trimmedISBN = trimISBNStart.ReplaceAllString(rawISBN, "")
	trimmedISBN = strings.ReplaceAll(trimmedISBN, ",", " ")
	trimmedISBN = strings.TrimSpace(trimmedISBN)
	trimmedISBN = strings.Split(trimmedISBN, " ")[0]

	if validISBN.MatchString(trimmedISBN) {
		return trimmedISBN
	} else {
		if !library.legacyMode {
			library.Warning(WarningBadISBN, rawISBN, library.currentKey)
		}
		return strings.TrimSpace(rawISBN)
	}
}

func processYearValue(library *TBibTeXLibrary, rawYear string) string {
	var validYear = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]$`)

	trimmedYear := strings.TrimSpace(rawYear)

	if !validYear.MatchString(trimmedYear) && !library.legacyMode {
		library.Warning(WarningBadYear, trimmedYear, library.currentKey)
	}

	return trimmedYear
}

func processDBLPValue(library *TBibTeXLibrary, value string) string {
	if !library.legacyMode {
		library.AddKeyAlias("DBLP:"+value, library.currentKey, false)
	}

	return value
}

func processPagesValue(library *TBibTeXLibrary, pages string) string {
	trimedPageRanges := ""

	trimedPageRanges = strings.TrimSpace(pages)
	trimedPageRanges = strings.ReplaceAll(trimedPageRanges, " ", "")
	trimedPageRanges = strings.ReplaceAll(trimedPageRanges, "--", "-")

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

func processBDSKFileValue(library *TBibTeXLibrary, value string) string {
	data, _ := base64.StdEncoding.DecodeString(value)
	payload := string(data)

	fileNameStart := strings.Index(payload, "relativePathXbookmark") + len("relativePathXbookmark") + 3
	fileNameEnd := strings.Index(payload, ".pdf") + 4
	fileName := payload

	if 0 <= fileNameStart && fileNameStart < fileNameEnd && fileNameEnd <= len(payload) {
		fileName = payload[fileNameStart:fileNameEnd]
	}

	_, err := os.Stat(library.files + fileName)
	if err != nil && !library.legacyMode {
		library.Warning(WarningMissingFile, fileName, library.currentKey)
	}

	return value
}

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
	fieldProcessors["isbn"] = processISBNValue
	fieldProcessors["issn"] = processISSNValue
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
	// "crossref"
	// "edition"
	// "editor"
	// "file"
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
