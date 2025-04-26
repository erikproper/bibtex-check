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
	"fmt"
	"os"
	"regexp"
	"strings"
)

/*
 *
 * BibTeX field value conformity checks
 *
 */

// Checks if a given alias fits the desired format of [a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*
// Examples: gordijn2002e3value, overbeek2010matchmaking, ...
func IsValidPreferredKeyAlias(alias string) bool {
	var validPreferredKeyAlias = regexp.MustCompile(`^[a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*$`)

	return validPreferredKeyAlias.MatchString(alias)
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
	return BibTeXAllowedEntryFields[l.EntryTypes[entry]].Set().Contains(field)
}

/*
 *
 * Basic correctness checks of mappings
 *
 */

// ALways same error message ....
func (l *TBibTeXLibrary) checkAliasesMapping(aliasMap TStringMap, inverseMap TStringSetMap, warningUsedAsAlias, warningTargetIsAlias string) {
	// Note: when we find an issue, we will not immediately stop as we want to list all issues.
	for alias, target := range aliasMap {
		if aliasedTarget, targetIsUsedAsAlias := aliasMap[target]; targetIsUsedAsAlias {
			// We cannot alias aliases
			fmt.Println("Pong")
			l.Warning(warningTargetIsAlias, alias, target, aliasedTarget)
		}

		if _, aliasIsUsedAsTargetForAlias := inverseMap[alias]; aliasIsUsedAsTargetForAlias {
			// Aliases should not be keys themselves.
			l.Warning(warningUsedAsAlias, alias)
		}
	}
}

func (l *TBibTeXLibrary) CheckAliases() {
	l.Progress(ProgressCheckingKeyAliasesMapping)

	l.checkAliasesMapping(l.KeyAliasToKey, l.KeyToAliases, WarningAliasIsKey, WarningAliasTargetKeyIsAlias)

	l.Progress(ProgressCheckingFieldAliasesMapping)
	for field, aliasMapping := range l.GenericFieldAliasToTarget {
		l.checkAliasesMapping(aliasMapping, l.GenericFieldTargetToAliases[field], WarningAliasIsKey, WarningAliasTargetKeyIsAlias)
	}

	for key, fieldAliasMapping := range l.EntryFieldAliasToTarget {
		for field, aliasMapping := range fieldAliasMapping {
			l.checkAliasesMapping(aliasMapping, l.EntryFieldTargetToAliases[key][field], WarningAliasIsKey, WarningAliasTargetKeyIsAlias)
		}
	}
}

/*
 *
 * General library/entry level checks & updates
 *
 */

// Check all key aliases of the library
func (l *TBibTeXLibrary) CheckKeyAliasesConsistency() {
	l.Progress(ProgressCheckingConsistencyOfKeyAliases)

	for alias, key := range l.KeyAliasToKey {
		// WORK!!
		// Once we're not in legacy mode anymore, then we need to enforce l.EntryExists(key)
		if l.EntryExists(key) {
			// Each "DBLP:" pre-fixed alias should be consistent with the dblp field of the referenced key.
			if strings.Index(alias, "DBLP:") == 0 {
				dblpAlias := alias[5:]
				dblpValue := l.EntryFieldValueity(key, "dblp")
				if dblpAlias != dblpValue {
					if dblpValue == "" {
						// If we have a dblp alias, and we have no dblp key, we can safely add this as the dblp value for this key.
						l.SetEntryFieldValue(key, "dblp", dblpAlias)
					} else {
						l.Warning(WarningDBLPMismatch, dblpAlias, dblpValue, key)
					}
				}
			}
		}

		if !l.EntryExists(key) {
			l.Warning("Target %s of alias %s does not exist", key, alias)
		}

		if _, aliasIsActuallyKeyToEntry := l.EntryFields[alias]; aliasIsActuallyKeyToEntry {
			// Aliases cannot be keys themselves.
			l.Warning(WarningAliasIsKey, alias)
		}
	}
}

func (l *TBibTeXLibrary) CheckURLPresence(key string) {
	if foundURL := l.EntryFieldValueity(key, "url"); foundURL == "" {
		if foundDOI := l.EntryFieldValueity(key, "doi"); foundDOI != "" {
			l.EntryFields[key]["url"] = "https://doi.org/" + foundDOI
		}
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

func (l *TBibTeXLibrary) CheckTitlePresence(key string) {
	if l.EntryFieldValueity(key, "title") == "" {
		l.Warning(WarningEmptyTitle, key)
	}
}

func (l *TBibTeXLibrary) CheckDOIPresence(key string) {
	foundDOI := l.EntryFieldValueity(key, "doi")

	if foundDOI == "" {
		if l.tryGetDOIFromURL(key, "url", &foundDOI) {

			// If we found a doi in the URL, then assign it
			fmt.Println("Found DOI", foundDOI, "for", key)
			l.EntryFields[key]["doi"] = foundDOI
		}
	}
}

func (l *TBibTeXLibrary) CheckURLDateNeed(key string) {
	if l.EntryFieldValueity(key, "urldate") != "" {
		if l.EntryFieldValueity(key, "url") == "" ||
			l.EntryFieldValueity(key, "dblp") != "" ||
			l.EntryFieldValueity(key, "doi") != "" ||
			l.EntryFieldValueity(key, "isbn") != "" ||
			l.EntryFieldValueity(key, "issn") != "" {

			// In these cases, we do not need an urldate
			l.EntryFields[key]["urldate"] = ""
		}
	}
}

func (l *TBibTeXLibrary) CheckPreferredKeyAliasesConsistency(key string) {
	if !l.PreferredKeyAliasExists(key) {
		///// CLEANER
		for alias := range l.KeyToAliases[key].Set().Elements() {
			if IsValidPreferredKeyAlias(alias) {
				// If we have no defined preferred alias, then we can use this one if it would be a valid preferred alias
				l.AddPreferredKeyAlias(alias)
			} else {
				// If we have no defined preferred alias, and the current alias is not valid, we can still try to lower the case and see if this works.
				loweredAlias := strings.ToLower(alias)

				// We do have to make sure the new alias is not already in use, and if it is then a valid alias.
				if !l.AliasExists(loweredAlias) && IsValidPreferredKeyAlias(loweredAlias) {
					l.AddKeyAlias(loweredAlias, key)
					l.AddPreferredKeyAlias(loweredAlias)
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckBookishTitles(key string) {
	// SAFE??
	if BibTeXBookish.Contains(l.EntryTypes[key]) {
		l.EntryFields[key]["booktitle"] = l.MaybeResolveFieldValue(key, key, "booktitle", l.EntryFieldValueity(key, "title"), l.EntryFieldValueity(key, "booktitle"))
		l.UpdateEntryFieldAlias(key, "title", l.EntryFields[key]["title"], l.EntryFields[key]["booktitle"])
		l.EntryFields[key]["title"] = l.EntryFields[key]["booktitle"]
	}
	if strings.Contains(l.EntryFields[key]["booktitle"], "proc.") || strings.Contains(l.EntryFields[key]["booktitle"], "Proc.") ||
		strings.Contains(l.EntryFields[key]["booktitle"], "proceedings") || strings.Contains(l.EntryFields[key]["booktitle"], "Proceedings") ||
		strings.Contains(l.EntryFields[key]["booktitle"], "workshop") || strings.Contains(l.EntryFields[key]["booktitle"], "Workshop") ||
		strings.Contains(l.EntryFields[key]["booktitle"], "conference") || strings.Contains(l.EntryFields[key]["booktitle"], "Conference") {
		if l.EntryFields[key]["title"] == l.EntryFields[key]["booktitle"] {
			if l.EntryTypes[key] != "proceedings" {
				fmt.Println("Expected an proceedings", key)
				l.EntryTypes[key] = "proceedings"
			}
		} else {
			if l.EntryTypes[key] != "inproceedings" {
				fmt.Println("Expected an inproceedings", key)
				l.EntryTypes[key] = "inproceedings"
			}
		}
	}
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
				currentTitle := l.EntryFieldValueity(crossrefKey, "title")
				newTitle := l.MaybeResolveFieldValue(crossrefKey, key, field, target, currentTitle)

				if currentTitle != newTitle {
					l.TitleIndex.DeleteValueFromStringSetMap(TeXStringIndexer(currentTitle), crossrefKey)

					/// Refactor this into a function. We need this more often.
					l.EntryFields[crossrefKey]["title"] = newTitle
					l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(newTitle), crossrefKey)
				}
			}

			for otherChallenger := range l.EntryFieldAliasToTarget[key][field] {
				l.AddEntryFieldAlias(crossrefKey, field, otherChallenger, target, false)
			}

			if target != "" {
				delete(l.EntryFields[key], field)
				delete(l.EntryFieldAliasToTarget[key], field)
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
	entryType := l.EntryTypes[key]
	crossrefetyRAW := l.EntryFieldValueity(key, "crossref")

	crossrefety := l.DeAliasEntryKey(crossrefetyRAW)
	if crossrefety == "" {
		crossrefety = crossrefetyRAW
	}

	if allowedCrossrefType, hasAllowedCrossrefType := BibTeXCrossrefType[entryType]; hasAllowedCrossrefType {
		if crossrefety != "" {
			if CrossrefType, CrossrefExists := l.EntryTypes[crossrefety]; CrossrefExists {
				if allowedCrossrefType == CrossrefType {
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
	} else if crossrefety != "" {
		l.Warning("Entry %s of type %s is not allowed to have a crossref", key, entryType)
	}
}

func (l *TBibTeXLibrary) CheckFileReferences(key, otherKey string) {
	l.EntryFields[key]["file"] = l.ResolveFileReferences(key, otherKey)
}

func (l *TBibTeXLibrary) CheckFileReference(key string) {
	l.CheckFileReferences(key, key)
}

func (l *TBibTeXLibrary) CheckLanguageID(key string) {
	if l.EntryFieldValueity(key, "langid") == "english" {
		l.EntryFields[key]["langid"] = ""
	}
}

func (l *TBibTeXLibrary) CheckNeedToSplitBookishEntry(keyRAW string) string {
	key := l.DeAliasEntryKey(keyRAW) // Dealias, while we are likely to do this immediately after a merge (for now)
	// After merging all doubles, we can do this as part of the consistency check and CheckCrossref in particular, and then don't need to dealias.

	if BibTeXCrossreffer.Contains(l.EntryTypes[key]) {
		crossrefKey := l.EntryFieldValueity(l.DeAliasEntryKey(key), "crossref")
		if crossrefKey == "" {
			entryType := Library.EntryTypes[key]

			bookTitle := l.EntryFieldValueity(l.DeAliasEntryKey(key), "booktitle")
			if bookTitle == "" {
				l.Warning("Empty booktitle for a bookish entry %s of type %s", key, entryType)
			} else {
				crossrefType := BibTeXCrossrefType[entryType]
				crossrefKey = l.NewKey()
				l.KeyIsTemporary.Add(crossrefKey)

				// refactor this with func (l *TBibTeXLibrary) AssignField(field, value string) and StartRecordingEntry
				l.EntryFields[crossrefKey] = TStringMap{}
				l.EntryFields[crossrefKey]["title"] = bookTitle
				l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(bookTitle), crossrefKey)
				l.EntryTypes[crossrefKey] = crossrefType

				l.EntryFields[key]["crossref"] = crossrefKey
			}
		}
	}

	return ""
}

func (l *TBibTeXLibrary) CheckNeedToMergeForEqualTitles(key string) {
	// Why not do l.DeAliasEntryKey(key) always as part of l.EntryFieldValueity ???
	title := l.EntryFieldValueity(l.DeAliasEntryKey(key), "title")
	if title != "" {
		// Should be via a function!
		Keys := Library.TitleIndex[TeXStringIndexer(title)]
		if Keys.Size() > 1 {
			sortedKeys := Keys.ElementsSorted()
			for _, a := range sortedKeys {
				if a == Library.DeAliasEntryKey(a) {
					for _, b := range sortedKeys {
						if b == Library.DeAliasEntryKey(b) {
							Library.MaybeMergeEntries(Library.DeAliasEntryKey(a), Library.DeAliasEntryKey(b))
						}
					}
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckDBLP(keyRAW string) {
	key := l.DeAliasEntryKey(keyRAW) // Needed??

	l.MaybeSyncDBLPEntry(key)

	entryType := l.EntryTypes[key] /// function?
	entryDBLP := l.EntryFieldValueity(key, "dblp")

	if BibTeXCrossreffer.Contains(entryType) {
		crossrefKey := l.EntryFieldValueity(key, "crossref")
		crossrefDBLP := l.EntryFieldValueity(crossrefKey, "dblp")

		parentDBLP := l.MaybeGetDBLPCrossref(entryDBLP)
		parentKey := l.LookupDBLPKey(parentDBLP)

		if parentKey != "" && crossrefKey != parentKey {
			l.EntryFields[key]["crossref"] = parentKey
			crossrefKey = parentKey
			crossrefDBLP = parentDBLP
		}

		if crossrefDBLP == "" && parentDBLP != "" {
			l.EntryFields[crossrefKey]["dblp"] = parentDBLP
			crossrefDBLP = parentDBLP
		}

		if crossrefKey == "" {
			l.Warning("Crossref entry type without a crossref %s", key)
		}

		if entryDBLP != "" && crossrefDBLP == "" {
			l.Warning("Parent entry %s does not have a dblp key, while the child %s does have dblp key %s", crossrefKey, key, entryDBLP)
		}

		if entryDBLP == "" && crossrefDBLP != "" {
			l.Warning("Child entry %s does not have a dblp key, while the parent %s does have dblp key %s", key, crossrefKey, crossrefDBLP)
		}
	}

	// Add parent to child check for bookish
	if BibTeXBookish.Contains(entryType) {
		l.ForEachChildOfDBLPKey(entryDBLP, func(childDBLP string) {
			childKey := l.LookupDBLPKey(childDBLP)

			if childKey != "" {
				childCrossref := l.DeAliasEntryKey(l.EntryFieldValueity(childKey, "crossref"))
				if childCrossref == "" || childCrossref != key {
					l.EntryFields[childKey]["crossref"] = key
				}
			} else {
				l.MaybeAddDBLPChildEntry(childDBLP, key)
			}
		})
	}
}

func (l *TBibTeXLibrary) CheckEntry(key string) {
	if l.EntryExists(key) {
		l.CheckPreferredKeyAliasesConsistency(key)
		l.CheckDOIPresence(key)
		l.CheckEPrint(key)
		l.CheckCrossref(key)
		l.CheckBookishTitles(key)

		// CheckCrossref can lead to a merger of entries for now ...
		if l.EntryExists(key) {
			l.CheckISBNFromDOI(key)
			l.CheckLanguageID(key)
			l.CheckTitlePresence(key)
			l.CheckURLPresence(key)
			l.CheckURLDateNeed(key)
			l.CheckFileReference(key)
		}
	}
}

func (l *TBibTeXLibrary) CheckEntries() {
	l.Progress(ProgressCheckingConsistencyOfEntries)

	for key := range l.EntryTypes {
		l.CheckEntry(key)
	}
}

func (l *TBibTeXLibrary) CheckFiles() {
	// CONSTANT!!!!
	l.Progress("Checking for superfluous and duplicate files.")
	filePath := Library.FilesRoot + FilesFolder

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

	for _, Keys := range Library.FileMD5Index {
		if Keys.Size() > 1 {
			l.Warning("File, with same content, is used by multiple different files: %s", Keys.String())
			l.MaybeMergeEntrySet(Keys)
		}
	}
}
