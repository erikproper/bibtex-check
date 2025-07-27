/*
 *
 * Module: bibtex_library_checks
 *
 * This module is concerned with checks of fields and entries.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

///// Are these really all "checks"??? The actual checks might even be done while reading the entries.

package main

import (
	"os"
	"regexp"
	"strings"
)

/*
 *
 * BibTeX field value conformity checks
 *
 */

func (l *TBibTeXLibrary) IsRedundantURL(url, key string) bool {
	foundURL := strings.ToLower(url)

	return foundURL == strings.ToLower("https://doi.org/"+l.EntryFieldValueity(key, "doi"))
}

func IsValidKey(key string) bool {
	var validKey = regexp.MustCompile(`^` + KeyPrefix + `-[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]$`)

	return validKey.MatchString(key)
}

// Checks if a given ISSN fits the desired format
func IsValidISSN(ISSN string) bool {
	var validISSN = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9,X]$`)

	return validISSN.MatchString(ISSN)
}

// Checks if a given ISBN fits the desired format
func IsValidISBN(ISBN string) bool {
	var validISBN = regexp.MustCompile(`^([0-9][-]?[0-9][-]?[0-9][-]?|)[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9,X]$`)

	return validISBN.MatchString(ISBN)
}

// Checks if a given year is indeed a year
func IsValidYear(year string) bool {
	var validYear = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]$`)

	return validYear.MatchString(year)
}

// Checks if a given date is indeed a date
func IsValidDate(date string) bool {
	var validDate = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$`)

	return validDate.MatchString(date)
}

/*
 *
 * Check if a field is allowed for a given entry.
 *
 */
func (l *TBibTeXLibrary) EntryAllowsForField(entry, field string) bool {
	return BibTeXAllowedEntryFields[l.EntryType(entry)].Set().Contains(field)
}

/*
 *
 * Basic correctness checks of mappings
 *
 */

func (l *TBibTeXLibrary) checkValueMapping(valueMap TStringMap, inverseMap TStringSetMap, keyety string) {
	for source, target := range valueMap {
		if _, targetAlreadyUsedAsSource := valueMap[target]; targetAlreadyUsedAsSource {
			l.Warning(WarningTargetAlreadyUsedAsSource+keyety, target)
		}

		if _, sourceAlreadyUsedAsTarget := inverseMap[source]; sourceAlreadyUsedAsTarget {
			l.Warning(WarningSourceAlreadyUsedAsTarget, source)
		}
	}
}

func (l *TBibTeXLibrary) CheckFieldMappings() {
	l.Progress(ProgressCheckingFieldMappings)

	for field, valueMapping := range l.GenericFieldSourceToTarget {
		l.checkValueMapping(valueMapping, l.GenericFieldTargetToSource[field], ".")
	}

	for key, fieldValueMapping := range l.EntryFieldSourceToTarget {
		for field, valueMapping := range fieldValueMapping {
			l.checkValueMapping(valueMapping, l.EntryFieldTargetToSource[key][field], WarningMappingForKey+key+".")
		}
	}
}

/*
 *
 * General library/entry level checks & updates
 *
 */

// Check all oldies
func (l *TBibTeXLibrary) CheckKeyOldiesConsistency() {
	l.Progress(ProgressCheckingConsistencyOfKeyOldies)

	for oldie, key := range l.KeyToKey {
		if !l.EntryExists(key) {
			l.Warning(WarningTargetOfOldieNotExists, key, oldie)
		}

		if _, oldieIsActuallyKeyToEntry := l.EntryFields[oldie]; oldieIsActuallyKeyToEntry {
			l.Warning(WarningOldieIsKey, oldie)
		}
	}
}

func (l *TBibTeXLibrary) CheckURLRedundance(key string) {
	url := l.EntryFieldValueity(key, "url")

	if l.IsRedundantURL(url, key) {
		// CONSTANTS!!!
		l.Warning("Can empty url for " + key + ", which is " + url)

		// Call??
		l.EntryFields[key]["url"] = ""
	}
}

func (l *TBibTeXLibrary) tryGetDOIFromURL(key, field string, foundDOI *string) bool {
	if *foundDOI == "" {
		if URL := l.EntryFieldValueity(key, field); URL != "" {
			var DOIURL = regexp.MustCompile(`^(doi:|http(s|)://(doi.org|dx.doi.org|hdl.handle.net|doi.acm.org|doi.ieeecomputersociety.org|dl.acm.org/doi|onlinelibrary.wiley.com/doi|publications.amsus.org/doi/abs|press.endocrine.org/doi/abs|doi.apa.org/index.cfm?doi=|www.crcnetbase.com/doi/abs|publications.amsus.org/doi/abs|econtent.hogrefe.com/doi/abs|www.mitpressjournals.org/doi/abs|www.atsjournals.org/doi/abs)/)`)

			DOICandidate := DOIURL.ReplaceAllString(URL, "")

			if DOICandidate != URL {
				*foundDOI = DOICandidate

				return true
			}
		}
	}

	return false
}

// Checks if a given preferred alias fits the desired format of 	[a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*
// Examples: gordijn2002e3value, overbeek2010matchmaking, ...
func (l *TBibTeXLibrary) CheckPreferredKey(key string) bool {
	var validPreferredKeyAlias = regexp.MustCompile(`^[a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*$`)

	alias := l.EntryFieldValueity(key, PreferredAliasField)

	return alias == "" || validPreferredKeyAlias.MatchString(alias)
}

func (l *TBibTeXLibrary) CheckTitlePresence(key string) {
	if l.EntryFieldValueity(key, TitleField) == "" {
		l.Warning(WarningEmptyTitle, key)
	}
}

func (l *TBibTeXLibrary) CheckDOIPresence(key string) {
	foundDOI := l.EntryFieldValueity(key, "doi")

	if foundDOI == "" {
		if l.tryGetDOIFromURL(key, "url", &foundDOI) {
			l.Warning("Found DOI in URL %s for %s", foundDOI, key)
			l.EntryFields[key]["doi"] = foundDOI
		}
	}
}

func (l *TBibTeXLibrary) CheckURLDateNeed(key string) {
	if l.EntryFieldValueity(key, "urldate") != "" {
		if l.EntryFieldValueity(key, "url") == "" ||
			l.EntryFieldValueity(key, DBLPField) != "" ||
			l.EntryFieldValueity(key, "doi") != "" ||
			l.EntryFieldValueity(key, "isbn") != "" ||
			l.EntryFieldValueity(key, "issn") != "" {

			// In these cases, we do not need an urldate
			l.EntryFields[key]["urldate"] = ""
		}
	}
}

func (l *TBibTeXLibrary) CheckBookishTitles(key string) {
	// SAFE??
	entryType := l.EntryType(key)

	if BibTeXBookish.Contains(entryType) {
		l.EntryFields[key]["booktitle"] = l.MaybeResolveFieldValue(key, key, "booktitle", l.EntryFieldValueity(key, TitleField), l.EntryFieldValueity(key, "booktitle"))
		l.UpdateEntryFieldAlias(key, TitleField, l.EntryFields[key][TitleField], l.EntryFields[key]["booktitle"])
		l.EntryFields[key][TitleField] = l.EntryFields[key]["booktitle"]
	}
	// if strings.Contains(l.EntryFields[key]["booktitle"], "proc.") || strings.Contains(l.EntryFields[key]["booktitle"], "Proc.") ||
	//
	//		strings.Contains(l.EntryFields[key]["booktitle"], "proceedings") || strings.Contains(l.EntryFields[key]["booktitle"], "Proceedings") ||
	//		strings.Contains(l.EntryFields[key]["booktitle"], "workshop") || strings.Contains(l.EntryFields[key]["booktitle"], "Workshop") ||
	//		strings.Contains(l.EntryFields[key]["booktitle"], "conference") || strings.Contains(l.EntryFields[key]["booktitle"], "Conference") {
	//		if l.EntryFields[key][TitleField] == l.EntryFields[key]["booktitle"] {
	//			if entryType != "proceedings" {
	//				fmt.Println("Expected a proceedings", key)
	//				l.SetEntryType(key, "proceedings")
	//			}
	//		} else {
	//			if entryType != "inproceedings" {
	//				fmt.Println("Expected an inproceedings", key)
	//				l.SetEntryType(key, "inproceedings")
	//			}
	//		}
	//	}
}

// Harmonize with tryGetDOIFromURL ???
// Config based ... needs a bit of work I guess ....
func (l *TBibTeXLibrary) CheckEPrint(key string) {
	EPrintTypeValueity := strings.ToLower(l.EntryFieldValueity(key, "eprinttype"))
	EPrintValueity := l.EntryFieldValueity(key, "eprint")

	DOIValueity := l.EntryFieldValueity(key, "doi")
	URLValueity := l.EntryFieldValueity(key, "url")

	DOIValueLower := strings.ToLower(DOIValueity)
	URLValueLower := strings.ToLower(URLValueity)

	OnArXive := EPrintTypeValueity == "arxiv" ||
		/*   */ strings.HasPrefix(DOIValueLower, "10.48550/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://arxiv.org/abs/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://doi.org/10.48550/")

	OnJstor := EPrintTypeValueity == "jstor" ||
		/*   */ strings.HasPrefix(DOIValueLower, "10.2307/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://doi.org/10.2307/") ||
		/*   */ strings.HasPrefix(URLValueLower, "http://www.jstor.org/stable/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://www.jstor.org/stable/")

	switch {
	case OnArXive:
		EPrintTypeValue := "arXiv"
		EPrintValue := EPrintValueity

		if EPrintValue != "" {
			EPrintValue = strings.ReplaceAll(strings.ToLower(EPrintValue), "arxiv:", "")
		}

		if EPrintValue == "" && DOIValueLower != "" {
			EPrintValue = strings.ReplaceAll(DOIValueLower, "10.48550/arxiv.", "")

			if EPrintValue == DOIValueLower {
				EPrintValue = ""
			}
		}

		if EPrintValue == "" && URLValueLower != "" {
			EPrintValue = strings.ReplaceAll(URLValueLower, "https://arxiv.org/abs/", "")

			if EPrintValue == URLValueLower {
				EPrintValue = ""
			}
		}

		if EPrintValue == "" {
			l.Warning("Not able to find eprint data for %s", key)
		} else {
			if DOIValueity == "" {
				DOIValueity = "10.48550/arXiv." + EPrintValue
			}
		}

		l.EntryFields[key]["eprinttype"] = EPrintTypeValue
		l.EntryFields[key]["eprint"] = EPrintValue
		l.EntryFields[key]["doi"] = DOIValueity

	case OnJstor:
		EPrintTypeValue := "jstor"
		EPrintValue := EPrintValueity

		if EPrintValue == "" {
			EPrintValue = strings.ReplaceAll(DOIValueLower, "10.2307/", "")

			if EPrintValue == "" {
				EPrintValue = strings.ReplaceAll(URLValueLower, "https://doi.org/10.2307/", "")

				if EPrintValue == "" {
					EPrintValue = strings.ReplaceAll(EPrintValue, "http://www.jstor.org/stable/", "")

					if EPrintValue == "" {
						EPrintValue = strings.ReplaceAll(EPrintValue, "https://www.jstor.org/stable/", "")

						if EPrintValue == "" {
							l.Warning("Not able to find eprint data for %s", key)
						}
					}
				}
			}
		}

		l.EntryFields[key]["eprinttype"] = EPrintTypeValue
		l.EntryFields[key]["eprint"] = EPrintValue

	default:
		if (EPrintTypeValueity != "" && EPrintValueity == "") || (EPrintTypeValueity == "" && EPrintValueity != "") {
			l.EntryFields[key]["eprinttype"] = ""
			l.EntryFields[key]["eprint"] = ""
		}
	}
}

func (l *TBibTeXLibrary) CheckISBNFromDOI(key string) {
	DOIValueity := l.EntryFieldValueity(key, "doi")

	if strings.HasPrefix(DOIValueity, "10.1007/978-") {
		ISBNCandidate := strings.ReplaceAll(DOIValueity, "10.1007/", "")
		if IsValidISBN(ISBNCandidate) {
			l.UpdateEntryFieldAlias(key, "isbn", l.EntryFields[key]["isbn"], ISBNCandidate)
			l.EntryFields[key]["isbn"] = ISBNCandidate
		}
	}
}

func (l *TBibTeXLibrary) CheckCrossrefInheritableField(crossrefKey, key, field string) {
	if BibTeXMustInheritFields.Contains(field) {
		if challenge, hasChallenge := l.EntryFields[key][field]; hasChallenge {
			target := l.MaybeResolveFieldValue(crossrefKey, key, field, challenge, l.EntryFieldValueity(crossrefKey, field))

			l.EntryFields[crossrefKey][field] = target

			if field == "booktitle" {
				currentTitle := l.EntryFieldValueity(crossrefKey, TitleField)
				newTitle := l.MaybeResolveFieldValue(crossrefKey, key, field, target, currentTitle)

				if currentTitle != newTitle {
					l.TitleIndex.DeleteValueFromStringSetMap(TeXStringIndexer(currentTitle), crossrefKey)

					/// Refactor this into a function. We need this more often.
					l.EntryFields[crossrefKey][TitleField] = newTitle
					l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(newTitle), crossrefKey)
				}
			}

			for otherChallenger := range l.EntryFieldSourceToTarget[key][field] {
				l.AddEntryFieldAlias(crossrefKey, field, otherChallenger, target, false)
			}

			if target != "" {
				delete(l.EntryFields[key], field)
				delete(l.EntryFieldSourceToTarget[key], field)
			}
		}
	} else if BibTeXMayInheritFields.Contains(field) {
		if crossrefValue, hasCrossrefValue := l.EntryFields[crossrefKey][field]; hasCrossrefValue {
			// No need to override the child value, when it is the same as the parent's value
			if crossrefValue == l.EntryFields[key][field] {
				l.EntryFields[key][field] = ""
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckCrossref(key string) {
	entryType := l.EntryType(key)
	crossrefetyRAW := l.EntryFieldValueity(key, "crossref")

	crossrefety := l.MapEntryKey(crossrefetyRAW)
	if crossrefety == "" {
		crossrefety = crossrefetyRAW
	}

	if crossrefety == key {
		l.Warning("Found self referencing crossref for %s. Cleaning this up.", key)
		l.EntryFields[key]["crossref"] = ""
	}

	if allowedCrossrefType, hasAllowedCrossrefType := BibTeXCrossrefType[entryType]; hasAllowedCrossrefType {
		if crossrefety != "" {
			if CrossrefType := l.EntryType(crossrefety); CrossrefType != "" {
				if allowedCrossrefType == CrossrefType || CrossrefType == "incollection" { // MAKE THIS CLEANER
					for field := range BibTeXInheritableFields.Elements() {
						l.CheckCrossrefInheritableField(crossrefety, key, field)
					}

					l.CheckBookishTitles(CrossrefType)
				} else {
					l.Warning("Crossref from %s %s to %s %s does not comply to the typing rules.", entryType, key, CrossrefType, crossrefety)
				}
			} else {
				l.Warning("Target %s of crossref from %s does not exist.", crossrefety, key)
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckFileReferences(key, otherKey string) {
	l.EntryFields[key][LocalURLField] = l.ResolveFileReferences(key, otherKey)
}

func (l *TBibTeXLibrary) CheckFileReference(key string) {
	l.CheckFileReferences(key, key)
}

func (l *TBibTeXLibrary) CheckISSN(key string) {
	issn := l.EntryFieldValueity(key, "issn")

	if issn == "" || IsValidISSN(issn) {
		return
	}

	l.Warning(WarningBadISSN, issn, key)
}

func (l *TBibTeXLibrary) CheckISBN(key string) {
	isbn := l.EntryFieldValueity(key, "isbn")

	if isbn == "" || IsValidISBN(isbn) {
		return
	}

	l.Warning(WarningBadISBN, isbn, key)
}

func (l *TBibTeXLibrary) CheckYear(key string) {
	year := l.EntryFieldValueity(key, "year")

	if year == "" || IsValidYear(year) {
		return
	}

	l.Warning(WarningBadYear, year, key)
}

func (l *TBibTeXLibrary) CheckURLDate(key string) {
	date := l.EntryFieldValueity(key, "urldate")

	if date == "" || IsValidDate(date) {
		return
	}

	l.Warning(WarningBadDate, date, key)
}

func (l *TBibTeXLibrary) CheckNeedToSplitBookishEntry(keyRAW string) string {
	key := l.MapEntryKey(keyRAW) // Dealias, while we are likely to do this immediately after a merge (for now)
	// After merging all doubles, we can do this as part of the consistency check and CheckCrossref in particular, and then don't need to dealias.

	if BibTeXCrossreffer.Contains(l.EntryType(key)) {
		crossrefKey := l.EntryFieldValueity(l.MapEntryKey(key), "crossref")
		if crossrefKey == "" {
			entryType := l.EntryType(key)

			bookTitle := l.EntryFieldValueity(l.MapEntryKey(key), "booktitle")
			if bookTitle == "" {
				l.Warning("Empty booktitle for a bookish entry %s of type %s", key, entryType)
			} else {
				crossrefType := BibTeXCrossrefType[entryType]
				crossrefKey = l.NewKey()
				l.KeyIsTemporary.Add(crossrefKey)

				// refactor this with func (l *TBibTeXLibrary) AssignField(field, value string) and StartRecordingEntry
				l.EntryFields[crossrefKey] = TStringMap{}
				l.EntryFields[crossrefKey][TitleField] = bookTitle
				l.EntryFields[crossrefKey]["booktitle"] = bookTitle
				l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(bookTitle), crossrefKey)
				l.SetEntryType(crossrefKey, crossrefType)

				l.EntryFields[key]["crossref"] = crossrefKey

				return crossrefKey
			}
		}
	}

	return ""
}

func (l *TBibTeXLibrary) CheckNeedToMergeForEqualTitles(key string) {
	// Why not do l.MapEntryKey(key) always as part of l.EntryFieldValueity ???
	title := l.EntryFieldValueity(l.MapEntryKey(key), TitleField)
	if title != "" {
		// Should be via a function!
		Keys := l.TitleIndex[TeXStringIndexer(title)]
		if Keys.Size() > 1 {
			sortedKeys := Keys.ElementsSorted()
			for _, a := range sortedKeys {
				if a == l.MapEntryKey(a) {
					for _, b := range sortedKeys {
						if b == l.MapEntryKey(b) {
							l.MaybeMergeEntries(l.MapEntryKey(a), l.MapEntryKey(b))
						}
					}
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckKeyValidity(key string) {
	if !IsValidKey(key) {
		l.Warning(WarningInvalidKey, key)
	}
}

func (l *TBibTeXLibrary) CheckDBLP(keyRAW string) {
	key := l.MapEntryKey(keyRAW) // Needed??

	l.MaybeSyncDBLPEntry(key)

	entryType := l.EntryType(key)
	entryDBLP := l.EntryFieldValueity(key, DBLPField)

	if BibTeXCrossreffer.Contains(entryType) {
		crossrefKey := l.EntryFieldValueity(key, "crossref")
		crossrefDBLP := l.EntryFieldValueity(crossrefKey, DBLPField)

		parentDBLP := l.MaybeGetDBLPCrossref(entryDBLP)
		parentKey := l.LookupDBLPKey(parentDBLP)

		if parentKey != "" && crossrefKey != parentKey {
			l.EntryFields[key]["crossref"] = parentKey
			crossrefKey = parentKey
			crossrefDBLP = parentDBLP
		}

		if crossrefDBLP == "" && parentDBLP != "" {
			l.SetEntryFieldValue(crossrefKey, DBLPField, parentDBLP)
			crossrefDBLP = parentDBLP
		}

		if crossrefKey == "" {
			l.Warning("Crossref entry type without a crossref %s", key)
		}

		if entryDBLP != "" && crossrefDBLP == "" {
			l.Warning("Parent entry %s does not have a dblp key, while the child %s does have dblp key %s", crossrefKey, key, entryDBLP)
		}

		// Is allowed ..
		//if entryDBLP == "" && crossrefDBLP != "" {
		//	l.Warning("Child entry %s does not have a dblp key, while the parent %s does have dblp key %s", key, crossrefKey, crossrefDBLP)
		//}
	}

	// Add parent to child check for bookish
	if BibTeXBookish.Contains(entryType) {
		l.Progress("Checking children of %s", entryDBLP)
		l.ForEachChildOfDBLPKey(entryDBLP, func(childDBLP string) {
			childKey := l.LookupDBLPKey(childDBLP)

			if childKey != "" {
				l.SetEntryFieldValue(childKey, "crossref", key)
			} else {
				l.MaybeAddDBLPChildEntry(childDBLP, key)
			}
		})
	}
}

func (l *TBibTeXLibrary) CheckEntry(key string) {
	if l.EntryExists(key) {
		l.CheckKeyValidity(key)

		// CheckCrossref can lead to a merger of entries for now ...
		if l.EntryExists(key) {
			l.CheckDOIPresence(key)
			l.CheckEPrint(key)
			l.CheckCrossref(key)
			l.CheckPreferredKey(key)
			l.CheckBookishTitles(key)
			l.CheckISBNFromDOI(key)
			l.CheckTitlePresence(key)
			l.CheckURLRedundance(key)
			l.CheckURLDateNeed(key)
			l.CheckFileReference(key)

			// Simple conformity checks
			l.CheckISSN(key)
			l.CheckISBN(key)
			l.CheckYear(key)
			l.CheckURLDate(key)
		}
	}
}

func (l *TBibTeXLibrary) CheckEntries() {
	l.Progress(ProgressCheckingConsistencyOfEntries)

	for key := range l.EntryFields {
		l.CheckEntry(key)
	}
}

func (l *TBibTeXLibrary) CheckFiles() {
	// CONSTANT!!!!
	l.Progress("Checking for superfluous and duplicate files.")
	filePath := l.FilesRoot + FilesFolder

	files, err := os.ReadDir(filePath)
	if err != nil {
		return
	}

	for _, e := range files {
		fileName := e.Name()
		if strings.HasSuffix(fileName, ".pdf") {
			key := strings.TrimSuffix(fileName, ".pdf")
			if l.EntryExists(key) {
				l.FileMD5Index.AddValueToStringSetMap(MD5ForFile(filePath+fileName), key)
			} else {
				l.Warning("File %s is not associated to any entry", fileName)
			}
		}
	}

	for _, Keys := range l.FileMD5Index {
		if Keys.Size() > 1 {
			l.Warning("File, with same content, is used by multiple different files: %s", Keys.String())
			l.MaybeMergeEntrySet(Keys)
		}
	}
}
