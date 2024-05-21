/*
 *
 * Module: bibtex_library_normalise_fields
 *
 * This module is concerned with the Normalisation of field values.
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
	// "fmt"
)

// Definition of the map for field Normalisers
type TFieldNormalisers = map[string]func(*TBibTeXLibrary, string) string

var fieldNormalisers TFieldNormalisers

// TXT
func NormaliseAliassableFieldValue(fieldAliasToAlias TStringMap, value string) string {
	if normalised, isMapped := fieldAliasToAlias[value]; isMapped {
		return normalised
	} else {
		return value
	}
}

// Normalise the name of a person based on the aliases
func NormalisePersonNameValue(l *TBibTeXLibrary, name string) string {
	return NormaliseAliassableFieldValue(l.NameAliasToName, name)
}

// TXT
func NormaliseAliassableTitleFieldValue(l *TBibTeXLibrary, fieldAliasToAlias TStringMap, value string) string {
	return NormaliseAliassableFieldValue(fieldAliasToAlias, NormaliseTitleString(l, value))
}

// Normalise the name of a journal based on the aliases
func NormaliseJournalValue(l *TBibTeXLibrary, journal string) string {
	return NormaliseAliassableTitleFieldValue(l, l.JournalAliasToJournal, journal)
}

// Normalise the name of a school based on the aliases
func NormaliseSchoolValue(l *TBibTeXLibrary, school string) string {
	return NormaliseAliassableTitleFieldValue(l, l.SchoolAliasToSchool, school)
}

// Normalise the name of an institution based on the aliases
func NormaliseInstitutionValue(l *TBibTeXLibrary, institution string) string {
	return NormaliseAliassableTitleFieldValue(l, l.InstitutionAliasToInstitution, institution)
}

// Normalise the name of an organisation based on the aliases
func NormaliseOrganisationValue(l *TBibTeXLibrary, organisation string) string {
	return NormaliseAliassableTitleFieldValue(l, l.OrganisationAliasToOrganisation, organisation)
}

// Normalise the name of a publisher based on the aliases
func NormalisePublisherValue(l *TBibTeXLibrary, publisher string) string {
	return NormaliseAliassableTitleFieldValue(l, l.PublisherAliasToPublisher, publisher)
}

// Normalise the name of a series based on the aliases
func NormaliseSeriesValue(l *TBibTeXLibrary, series string) string {
	return NormaliseAliassableTitleFieldValue(l, l.SeriesAliasToSeries, series)
}

// Normalize number values
func NormaliseNumberValue(l *TBibTeXLibrary, rawNumber string) string {
	var (
		numberRange = regexp.MustCompile(`^[0-9]+-+[0-9]+$`)
		minuses     = regexp.MustCompile(`-+`)
	)

	number := strings.TrimSpace(rawNumber)
	if numberRange.MatchString(number) {
		number = minuses.ReplaceAllString(number, "--")
	} else {
		number = minuses.ReplaceAllString(number, "-")
	}

	return number
}

// Normalize DOI values
func NormaliseDOIValue(l *TBibTeXLibrary, rawDOI string) string {
	var (
		trimDOIStart = regexp.MustCompile(`^(doi:|http://[a-z,.]*/)`)
		trimmedDOI   string
	)

	// Remove leading/trailing spaces
	trimmedDOI = strings.TrimSpace(rawDOI)
	// Remove doi: or http://XXX from the start
	trimmedDOI = trimDOIStart.ReplaceAllString(trimmedDOI, "")
	// Some publishers of BibTeX files use a "{$\_$}" in the doi. We prefer not to.
	trimmedDOI = strings.ReplaceAll(trimmedDOI, "\\_", "_")
	trimmedDOI = strings.ReplaceAll(trimmedDOI, "{$", "")
	trimmedDOI = strings.ReplaceAll(trimmedDOI, "$}", "")

	return trimmedDOI
}

// Normalize URL values
func NormaliseURLValue(l *TBibTeXLibrary, rawURL string) string {
	var trimmedURL string

	// Remove leading/trailing spaces
	trimmedURL = strings.TrimSpace(rawURL)
	// Some publishers of BibTeX files use a "{$\_$}" in the URL. We prefer not to.
	trimmedURL = strings.ReplaceAll(trimmedURL, "\\_", "_")
	trimmedURL = strings.ReplaceAll(trimmedURL, "{$", "")
	trimmedURL = strings.ReplaceAll(trimmedURL, "$}", "")
	// Same with "%5C" which is an encoded \_
	trimmedURL = strings.ReplaceAll(trimmedURL, "%5C", "_")

	return trimmedURL
}

// Normalize DOI values to the format 1234-5678
func NormaliseISSNValue(l *TBibTeXLibrary, rawISSN string) string {
	var (
		trimISSNStart = regexp.MustCompile(`^ *ISSN[:]? *`)
		trimmedISSN   string
	)

	// Remove ISSN: from the start
	trimmedISSN = trimISSNStart.ReplaceAllString(rawISSN, "")
	// Sometimes we have multiple ISSN's provided. We only include first one.
	// Sometimes these ISSN's are separated by a space and sometimes by a ","
	trimmedISSN = strings.ReplaceAll(trimmedISSN, ",", " ")
	// Remove remaining leading/trailing spaces
	trimmedISSN = strings.TrimSpace(trimmedISSN)
	// Select the first ISSN number
	trimmedISSN = strings.Split(trimmedISSN, " ")[0]
	// Remove any "-" from the ISSN number, so we can properly re-insert it later.
	trimmedISSN = strings.ReplaceAll(trimmedISSN, "-", "")

	if len(trimmedISSN) == 8 {
		// Add the "-" in the middle again
		trimmedISSN = trimmedISSN[0:4] + "-" + trimmedISSN[4:8]

		// A final check if we have a proper ISSN
		if CheckISSNValidity(trimmedISSN) {
			return trimmedISSN
		}
	}

	// If we get here, we have a bad ISSN on our hand.
	if !l.legacyMode {
		l.Warning(WarningBadISSN, rawISSN, l.currentKey)
	}

	return strings.TrimSpace(rawISSN)
}

// Normalize DOI values to an ISBN10 or ISBN13 format.
func NormaliseISBNValue(l *TBibTeXLibrary, rawISBN string) string {
	var (
		trimmedISBN   string
		trimISBNStart = regexp.MustCompile(`^ *ISBN[-]?(10|13|)[:]? *`)
	)

	// Remove ISBN: from the start
	trimmedISBN = trimISBNStart.ReplaceAllString(rawISBN, "")
	// Sometimes we have multiple ISBN's provided. We only include first one.
	// Sometimes these ISBN's are separated by a space and sometimes by a ","
	trimmedISBN = strings.ReplaceAll(trimmedISBN, ",", " ")
	// Remove remaining leading/trailing spaces
	trimmedISBN = strings.TrimSpace(trimmedISBN)
	// Select the first ISBN number
	trimmedISBN = strings.Split(trimmedISBN, " ")[0]

	if CheckISBNValidity(trimmedISBN) {
		return trimmedISBN
	}

	// If we get here, we have a bad ISBN on our hand.
	if !l.legacyMode {
		l.Warning(WarningBadISBN, rawISBN, l.currentKey)
	}

	return strings.TrimSpace(rawISBN)
}

func NormaliseYearValue(l *TBibTeXLibrary, rawYear string) string {
	// Remove leading/trailing spaces
	trimmedYear := strings.TrimSpace(rawYear)

	if CheckYearValidity(trimmedYear) {
		return trimmedYear
	}

	// If we get here, we have a bad year on our hand.
	if !l.legacyMode {
		l.Warning(WarningBadYear, rawYear, l.currentKey)
	}

	return strings.TrimSpace(rawYear)
}

func NormaliseCrossrefValue(l *TBibTeXLibrary, crossref string) string {
	// Remove leading/trailing spaces
	trimmedCrossref := strings.TrimSpace(crossref)

	if l.legacyMode {
		// Note that the next call is hard-wired to the main Library.
		// Only needed while still allowing l.legacyMode mode.
		key, isKey := Library.LookupEntry(trimmedCrossref)

		if isKey {
			return key
		}
	}

	return trimmedCrossref
}

func NormalisePagesValue(l *TBibTeXLibrary, pages string) string {
	var trimDashes = regexp.MustCompile(`-+`)

	trimedPageRanges := ""
	// Remove leading/trailing spaces
	trimedPageRanges = strings.TrimSpace(pages)
	// There should be no spaces in page ranges.
	trimedPageRanges = strings.ReplaceAll(trimedPageRanges, " ", "")
	// We use "--" between start and end page. However, during the Normalisation, we first reduce this to only one "-" to that way Normalise ---, -- and - to a single -.
	trimedPageRanges = trimDashes.ReplaceAllString(trimedPageRanges, "-")

	rangesList := "" // We use this to collect the page range(s)
	comma := ""      // Potentially a comma separating page ranges.

	// We can actually have page ranges, such as 3--5, 8--9
	// In addition, sometimes numbers are prefixed with section/paper numbers: 2:3--4:5
	// We start by splitting the page field based on the comma separated provided page ranges.
	for _, pageRange := range strings.Split(trimedPageRanges, ",") {
		// Within a singular page range, a "-" distinguishes between a starting page and ending page.
		// So we split again.
		trimedPagesList := strings.Split(pageRange, "-")

		switch {
		case len(trimedPagesList) == 0:
			// If we cannot split the page range, we will leave it as is
			rangesList += comma + pageRange

		case len(trimedPagesList) == 1:
			// If we only have one page, we're done with this page reave
			rangesList += comma + trimedPagesList[0]

		case len(trimedPagesList) == 2:
			// So, we have a start and end page.
			// Now we also need to cater for the fact that the start and end page may be prefixed by a section/paper number.
			// Therefore, we split both page numbers based on the ":" between the section/paper number and the actual page.
			firstPagePair := strings.Split(trimedPagesList[0], ":")
			secondPagePair := strings.Split(trimedPagesList[1], ":")

			if len(firstPagePair) <= 2 && len(secondPagePair) == 1 {
				// If the second page number only contains one number, we can add these page number pars to the ranges
				rangesList += comma + trimedPagesList[0] + "--" + trimedPagesList[1]

			} else if len(firstPagePair) == 2 && len(secondPagePair) == 2 {
				// If both first and last page are prefixed, we need to check of the actually have the same prefix.
				if firstPagePair[0] == secondPagePair[0] {
					// If we have the same prefix, we can drop te second occurrence.
					// So, we prefer e.g. 3:1--10 over 3:1--3:10
					rangesList += comma + trimedPagesList[0] + "--" + secondPagePair[1]

				} else {
					// In the case we have different prefixes, we just include both.
					// For example, 3:5--4:10
					rangesList += comma + trimedPagesList[0] + "--" + trimedPagesList[1]

				}
			} else {
				// Some other page specifications we don't recognise.
				rangesList += comma + pageRange
			}

		default:
			// Some other page specifications we don't recognise.
			rangesList += comma + pageRange
		}

		// If we loop to another page pair, we need to actually add a , when adding the next page pair
		comma = ", "
	}

	return rangesList
}

// Legacy ... will be removed once we have migrated all legacy and files.
func NormaliseFileValue(l *TBibTeXLibrary, rawFile string) string {
	var (
		trimFileStart = regexp.MustCompile(`^.*/Zotero/storage/`)
		trimFileEnd   = regexp.MustCompile(`.pdf:.*$`)
		trimmedFile   string
	)

	if l.legacyMode {
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

// Check if the provided BibDesk file (in base64 encoded format) is present.
// If not present, we should just ignore the field.
// But still give a warning.
func NormaliseBDSKFileValue(l *TBibTeXLibrary, value string) string {
	// Decode the provided value, and get the payload as a string.
	data, _ := base64.StdEncoding.DecodeString(value)
	payload := string(data)

	// Find start of filename.
	fileNameStart := strings.Index(payload, "relativePathXbookmark") + len("relativePathXbookmark") + 3
	// Find the end of the filename
	fileNameEnd := strings.Index(payload, ".pdf") + 4

	// We use the raw payload as the default filename
	fileName := payload
	// But if we have a correct "cutout" of the filename we will use that:
	if 0 <= fileNameStart && fileNameStart < fileNameEnd && fileNameEnd <= len(payload) {
		fileName = payload[fileNameStart:fileNameEnd]
	}

	// See if the file exists
	if FileExists(l.FilesRoot + fileName) {
		// If it's there, we can return the original value as-is
		return value
	} else {
		// If it is not there, create a warning, and return empty
		if !l.legacyMode {
			l.Warning(WarningMissingFile, fileName, l.currentKey)
		}

		return ""
	}
}

// The general function call to Normalise field values.
// If a field specific Normalisation function exists, then it is applied.
// Otherwise, we only remove leading/trailing spaces.
func (l *TBibTeXLibrary) NormaliseFieldValue(field, value string) string {
	valueNormaliser, hasNormaliser := fieldNormalisers[field]

	if hasNormaliser {
		return valueNormaliser(l, value)
	} else {
		return strings.TrimSpace(value)
	}
}

func init() {
	// Define the Normaliser functions.
	fieldNormalisers = TFieldNormalisers{}
	fieldNormalisers["author"] = NormaliseNamesString
	fieldNormalisers["address"] = NormaliseTitleString
	fieldNormalisers["bdsk-file-1"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-2"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-3"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-4"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-5"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-6"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-7"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-8"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-file-9"] = NormaliseBDSKFileValue
	fieldNormalisers["bdsk-url-1"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-2"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-3"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-4"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-5"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-6"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-7"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-8"] = NormaliseURLValue
	fieldNormalisers["bdsk-url-9"] = NormaliseURLValue
	fieldNormalisers["booktitle"] = NormaliseTitleString
	fieldNormalisers["crossref"] = NormaliseCrossrefValue // only needed while still allowing l.legacyMode
	fieldNormalisers["doi"] = NormaliseDOIValue
	fieldNormalisers["editor"] = NormaliseNamesString
	fieldNormalisers["file"] = NormaliseFileValue // only needed while still allowing l.legacyMode
	fieldNormalisers["howpublished"] = NormaliseTitleString
	fieldNormalisers["institution"] = NormaliseInstitutionValue
	fieldNormalisers["isbn"] = NormaliseISBNValue
	fieldNormalisers["issn"] = NormaliseISSNValue
	fieldNormalisers["journal"] = NormaliseJournalValue
	fieldNormalisers["number"] = NormaliseNumberValue
	fieldNormalisers["organization"] = NormaliseOrganisationValue
	fieldNormalisers["pages"] = NormalisePagesValue
	fieldNormalisers["publisher"] = NormalisePublisherValue
	fieldNormalisers["series"] = NormaliseSeriesValue
	fieldNormalisers["school"] = NormaliseSchoolValue
	fieldNormalisers["title"] = NormaliseTitleString
	fieldNormalisers["url"] = NormaliseURLValue
	fieldNormalisers["year"] = NormaliseYearValue
}
