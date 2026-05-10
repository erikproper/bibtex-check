/*
 *
 * Module: bibtex_library_checks
 *
 * This module is concerned with checks of fields and entries.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 03.05.2026
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
	var validKey = regexp.MustCompile(`^` + keyPrefix + `-[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]$`)

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

		if bibEntryExists(oldie) {
			l.Warning(WarningOldieIsKey, oldie)
		}
	}
}

func (l *TBibTeXLibrary) CheckURLRedundance(entry *TBibTeXEntry) {
	url := entry.FieldValue("url")

	if l.IsRedundantURL(url, entry.Key) {
		// CONSTANTS!!!
		l.Warning("Can empty url for " + entry.Key + ", which is " + url)

		l.setEntryField(entry, "url", "")
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
func (l *TBibTeXLibrary) CheckPreferredKey(entry *TBibTeXEntry) bool {
	var validPreferredKeyAlias = regexp.MustCompile(`^[a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*$`)

	alias := entry.FieldValue(PreferredAliasField)

	return alias == "" || validPreferredKeyAlias.MatchString(alias)
}

func (l *TBibTeXLibrary) CheckTitlePresence(entry *TBibTeXEntry) {
	if entry.FieldValue(TitleField) == "" {
		l.Warning(WarningEmptyTitle, entry.Key)
	}
}

func (l *TBibTeXLibrary) CheckDOIPresence(entry *TBibTeXEntry) {
	foundDOI := entry.FieldValue("doi")

	if foundDOI == "" {
		if l.tryGetDOIFromURL(entry.Key, "url", &foundDOI) {
			l.Warning("Found DOI in URL %s for %s", foundDOI, entry.Key)
			l.setEntryField(entry, "doi", foundDOI)
		}
	}
}

func (l *TBibTeXLibrary) CheckURLDateNeed(entry *TBibTeXEntry) {
	if entry.FieldValue("urldate") != "" {
		if entry.FieldValue("url") == "" ||
			entry.FieldValue(DBLPField) != "" ||
			entry.FieldValue("doi") != "" ||
			entry.FieldValue("isbn") != "" ||
			entry.FieldValue("issn") != "" {

			// In these cases, we do not need an urldate
			l.setEntryField(entry, "urldate", "")
		}
	}
}

func (l *TBibTeXLibrary) CheckBookishTitles(entry *TBibTeXEntry) {
	// SAFE??
	entryType := entry.EntryType()

	if BibTeXBookish.Contains(entryType) {
		newBookTitle := l.MaybeResolveFieldValue(entry.Key, entry.Key, "booktitle", entry.FieldValue(TitleField), entry.FieldValue("booktitle"))
		l.setEntryField(entry, "booktitle", newBookTitle)
		l.UpdateEntryFieldAlias(entry.Key, TitleField, entry.FieldValue(TitleField), entry.FieldValue("booktitle"))
		l.setEntryField(entry, TitleField, entry.FieldValue("booktitle"))
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
func (l *TBibTeXLibrary) CheckEPrint(entry *TBibTeXEntry) {
	EPrintTypeValueity := strings.ToLower(entry.FieldValue("eprinttype"))
	EPrintValueity := entry.FieldValue("eprint")

	DOIValueity := entry.FieldValue("doi")
	URLValueity := entry.FieldValue("url")

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
			l.Warning("Not able to find eprint data for %s", entry.Key)
		} else {
			if DOIValueity == "" {
				DOIValueity = "10.48550/arXiv." + EPrintValue
			}
		}

		l.setEntryField(entry, "eprinttype", EPrintTypeValue)
		l.setEntryField(entry, "eprint", EPrintValue)
		l.setEntryField(entry, "doi", DOIValueity)

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
							l.Warning("Not able to find eprint data for %s", entry.Key)
						}
					}
				}
			}
		}

		l.setEntryField(entry, "eprinttype", EPrintTypeValue)
		l.setEntryField(entry, "eprint", EPrintValue)

	default:
		if (EPrintTypeValueity != "" && EPrintValueity == "") || (EPrintTypeValueity == "" && EPrintValueity != "") {
			l.setEntryField(entry, "eprinttype", "")
			l.setEntryField(entry, "eprint", "")
		}
	}
}

func (l *TBibTeXLibrary) CheckISBNFromDOI(entry *TBibTeXEntry) {
	DOIValueity := entry.FieldValue("doi")
	if !strings.HasPrefix(DOIValueity, "10.1007/978-") {
		return
	}

	ISBNCandidate := strings.ReplaceAll(DOIValueity, "10.1007/", "")
	if !IsValidISBN(ISBNCandidate) {
		return
	}

	crossrefRAW := entry.FieldValue("crossref")
	if crossrefRAW == "" {
		l.UpdateEntryFieldAlias(entry.Key, "isbn", entry.FieldValue("isbn"), ISBNCandidate)
		l.setEntryField(entry, "isbn", ISBNCandidate)
		return
	}

	// The doi is a book-level Springer doi; isbn belongs on the parent, not this child.
	crossrefKey := l.MapEntryKey(crossrefRAW)
	if crossrefKey == "" {
		crossrefKey = crossrefRAW
	}
	crossrefEntry := l.buildEntry(crossrefKey)
	if !crossrefEntry.Exists() {
		return
	}
	parentISBN := crossrefEntry.FieldValue("isbn")
	switch {
	case parentISBN == "":
		l.UpdateEntryFieldAlias(crossrefKey, "isbn", "", ISBNCandidate)
		l.setEntryField(crossrefEntry, "isbn", ISBNCandidate)
		l.deleteEntryField(entry, "doi")
	case parentISBN == ISBNCandidate:
		// doi already accounted for on parent; child doi will be cleaned by CheckCrossrefDOI
	default:
		l.Warning(WarningISBNMismatchFromCrossrefDOI, entry.Key, crossrefKey, ISBNCandidate, parentISBN)
	}
}

func (l *TBibTeXLibrary) CheckCrossrefInheritableField(crossrefEntry, entry *TBibTeXEntry, field string) {
	if BibTeXMustInheritFields.Contains(field) {
		if challenge, hasChallenge := entry.Fields[field]; hasChallenge {
			target := l.MaybeResolveFieldValue(crossrefEntry.Key, entry.Key, field, challenge, crossrefEntry.FieldValue(field))

			l.setEntryField(crossrefEntry, field, target)

			if field == "booktitle" {
				currentTitle := crossrefEntry.FieldValue(TitleField)
				newTitle := l.MaybeResolveFieldValue(crossrefEntry.Key, entry.Key, field, target, currentTitle)

				if currentTitle != newTitle {
					l.TitleIndex.DeleteValueFromStringSetMap(TeXStringIndexer(currentTitle), crossrefEntry.Key)

					/// Refactor this into a function. We need this more often.
					l.setEntryField(crossrefEntry, TitleField, newTitle)
					l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(newTitle), crossrefEntry.Key)
				}
			}

			for otherChallenger := range l.EntryFieldSourceToTarget[entry.Key][field] {
				l.AddEntryFieldAlias(crossrefEntry.Key, field, otherChallenger, target, false)
			}

			if target != "" {
				l.deleteEntryField(entry, field)
				delete(l.EntryFieldSourceToTarget[entry.Key], field)
			}
		}
	} else if BibTeXMayInheritFields.Contains(field) {
		if crossrefValue, hasCrossrefValue := crossrefEntry.Fields[field]; hasCrossrefValue {
			// No need to override the child value, when it is the same as the parent's value
			if crossrefValue == entry.Fields[field] {
				l.setEntryField(entry, field, "")
			}
		}
	}
}

// CheckCrossrefDOI drops the child's DOI when it is the parent's ISBN-based Springer DOI.
// The may-inherit logic handles the case where the parent explicitly stores the same DOI;
// this covers the case where the parent has an isbn but no doi field.
func (l *TBibTeXLibrary) CheckCrossrefDOI(crossrefEntry, entry *TBibTeXEntry) {
	childDOI := entry.FieldValue("doi")
	if childDOI == "" {
		return
	}
	parentISBN := crossrefEntry.FieldValue("isbn")
	if parentISBN != "" && childDOI == "10.1007/"+parentISBN {
		l.deleteEntryField(entry, "doi")
	}
}

func (l *TBibTeXLibrary) CheckCrossref(entry *TBibTeXEntry) {
	entryType := entry.EntryType()
	crossrefetyRAW := entry.FieldValue("crossref")

	crossrefety := l.MapEntryKey(crossrefetyRAW)
	if crossrefety == "" {
		crossrefety = crossrefetyRAW
	}

	if crossrefety == entry.Key {
		l.Warning("Found self referencing crossref for %s. Cleaning this up.", entry.Key)
		l.setEntryField(entry, "crossref", "")
	}

	if allowedCrossrefType, hasAllowedCrossrefType := BibTeXCrossrefType[entryType]; hasAllowedCrossrefType {
		if crossrefety != "" {
			if CrossrefType := l.EntryType(crossrefety); CrossrefType != "" {
				if allowedCrossrefType == CrossrefType || CrossrefType == "incollection" { // MAKE THIS CLEANER
					crossrefEntry := l.buildEntry(crossrefety)
					for field := range BibTeXInheritableFields.Elements() {
						l.CheckCrossrefInheritableField(crossrefEntry, entry, field)
					}

					l.CheckCrossrefDOI(crossrefEntry, entry)
					l.CheckBookishTitles(crossrefEntry)
				} else {
					l.Warning("Crossref from %s %s to %s %s does not comply to the typing rules.", entryType, entry.Key, CrossrefType, crossrefety)
				}
			} else {
				l.Warning("Target %s of crossref from %s does not exist.", crossrefety, entry.Key)
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckFileReferences(key, otherKey string) {
	upsertBibEntryField(key, LocalURLField, l.ResolveFileReferences(key, otherKey))
}

func (l *TBibTeXLibrary) CheckFileReference(entry *TBibTeXEntry) {
	l.setEntryField(entry, LocalURLField, l.ResolveFileReferences(entry.Key, entry.Key))
}

func (l *TBibTeXLibrary) CheckISSN(entry *TBibTeXEntry) {
	issn := entry.FieldValue("issn")

	if issn == "" || IsValidISSN(issn) {
		return
	}

	l.Warning(WarningBadISSN, issn, entry.Key)
}

func (l *TBibTeXLibrary) CheckISBN(entry *TBibTeXEntry) {
	isbn := entry.FieldValue("isbn")

	if isbn == "" || IsValidISBN(isbn) {
		return
	}

	l.Warning(WarningBadISBN, isbn, entry.Key)
}

func (l *TBibTeXLibrary) CheckYear(entry *TBibTeXEntry) {
	year := entry.FieldValue("year")

	if year == "" || IsValidYear(year) {
		return
	}

	l.Warning(WarningBadYear, year, entry.Key)
}

func (l *TBibTeXLibrary) CheckURLDate(entry *TBibTeXEntry) {
	date := entry.FieldValue("urldate")

	if date == "" || IsValidDate(date) {
		return
	}

	l.Warning(WarningBadDate, date, entry.Key)
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

				upsertBibEntryField(crossrefKey, TitleField, bookTitle)
				upsertBibEntryField(crossrefKey, "booktitle", bookTitle)
				l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(bookTitle), crossrefKey)
				l.SetEntryType(crossrefKey, crossrefType)

				l.SetEntryFieldValue(key, "crossref", crossrefKey)

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

func (l *TBibTeXLibrary) CheckKeyValidity(entry *TBibTeXEntry) {
	if !IsValidKey(entry.Key) {
		l.Warning(WarningInvalidKey, entry.Key)
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
			l.SetEntryFieldValue(key, "crossref", parentKey)
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

func (l *TBibTeXLibrary) CheckEntry(entry *TBibTeXEntry) {
	if entry.Exists() {
		l.CheckKeyValidity(entry)

		// CheckCrossref can lead to a merger of entries for now ...
		if entry.Exists() && l.EntryExists(entry.Key) {
			l.CheckDOIPresence(entry)
			l.CheckEPrint(entry)
			l.CheckCrossref(entry)
			l.CheckPreferredKey(entry)
			l.CheckBookishTitles(entry)
			l.CheckISBNFromDOI(entry)
			l.CheckTitlePresence(entry)
			l.CheckURLRedundance(entry)
			l.CheckURLDateNeed(entry)
			l.CheckFileReference(entry)

			// Simple conformity checks
			l.CheckISSN(entry)
			l.CheckISBN(entry)
			l.CheckYear(entry)
			l.CheckURLDate(entry)
		}
	}
}

func (l *TBibTeXLibrary) CheckEntries() {
	l.Progress(ProgressCheckingConsistencyOfEntries)

	forEachBibEntryKey(func(key string) bool {
		l.CheckEntry(l.buildEntry(key))
		return true
	})
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
