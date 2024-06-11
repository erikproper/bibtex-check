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

func (l *TBibTeXLibrary) checkAliasesMapping(aliasMap TStringMap, inverseMap TStringSetMap, warningUsedAsAlias, warningTargetIsAlias string) {
	// Note: when we find an issue, we will not immediately stop as we want to list all issues.
	for alias, target := range aliasMap {
		if aliasedTarget, targetIsUsedAsAlias := aliasMap[target]; targetIsUsedAsAlias {
			// We cannot alias aliases
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
		// Once we're not in legacy mode anymore, then we need to enforce l.EntryExists(key)
		if AllowLegacy && l.EntryExists(key) {
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
		if l.tryGetDOIFromURL(key, "url", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-1", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-2", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-3", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-4", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-5", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-6", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-7", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-8", &foundDOI) ||
			l.tryGetDOIFromURL(key, "bdsk-url-9", &foundDOI) {

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
		l.EntryFields[key]["booktitle"] = l.MaybeResolveFieldValue(key, "", "booktitle", l.EntryFieldValueity(key, "title"), l.EntryFieldValueity(key, "booktitle"))
		l.UpdateEntryFieldAlias(key, "title", l.EntryFields[key]["title"], l.EntryFields[key]["booktitle"])
		l.EntryFields[key]["title"] = l.EntryFields[key]["booktitle"]
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

func (l *TBibTeXLibrary) CheckCrossrefMustInheritField(crossrefKey, key, field string) {
	if challenge, hasChallenge := l.EntryFields[key][field]; hasChallenge {
		target := l.MaybeResolveFieldValue(crossrefKey, key, field, challenge, l.EntryFieldValueity(crossrefKey, field))
		
		if field == "booktitle" {
			if l.EntryFields[crossrefKey]["title"] == l.EntryFields[crossrefKey]["booktitle"] {
				l.EntryFields[crossrefKey]["title"] = target
			}
		}

		l.EntryFields[crossrefKey][field] = target

		for otherChallenger := range l.EntryFieldAliasToTarget[key][field] {
			l.AddEntryFieldAlias(crossrefKey, field, otherChallenger, target, false)
		}

		// This check should not be necessary, but ...
		if target != "" {
			l.EntryFields[key][field] = ""
			
			delete(l.EntryFieldAliasToTarget[key], field)
		}
	}
}

func (l *TBibTeXLibrary) CheckCrossrefMayInheritField(crossrefKey, key, field string) {
	if crossrefValue, hasCrossrefValue := l.EntryFields[crossrefKey][field]; hasCrossrefValue {
		if crossrefValue == l.EntryFields[key][field] {
			l.EntryFields[key][field] = ""
		}
	}
}

func (l *TBibTeXLibrary) CheckCrossref(key string) {
	Crossrefity := l.EntryFieldValueity(key, "crossref")
	EntryType := l.EntryTypes[key]

	if Crossrefity != "" {
		if CrossrefType, CrossrefExists := l.EntryTypes[Crossrefity]; CrossrefExists {
			if BibTeXCrossrefType[EntryType] == CrossrefType {
				for field := range BibTeXMustInheritFields.Elements() {
					l.CheckCrossrefMustInheritField(Crossrefity, key, field)
				}

				for field := range BibTeXMayInheritFields.Elements() {
					l.CheckCrossrefMayInheritField(Crossrefity, key, field)
				}

				l.CheckBookishTitles(CrossrefType)
			} else {
				l.Warning("Crossref from %s %s to %s %s does not comply to the typing rules.", EntryType, key, CrossrefType, Crossrefity)
			}
		} else {
			l.Warning("Target %s of crossref from %s does not exist.", Crossrefity, key)
		}
	}
}

func (l *TBibTeXLibrary) BDSKFileValueMatches(key, field, localURL string) bool {
	FilePath := BDSKFile(l.EntryFieldValueity(key, field))
	if FilePath == "" {
		return false
	} else {
		return strings.HasSuffix(localURL, FilePath)
	}
}

func (l *TBibTeXLibrary) CheckLanguageID(key string) {
	if l.EntryFieldValueity(key, "langid") == "english" {
		l.EntryFields[key]["langid"] = ""
	}
}

func (l *TBibTeXLibrary) CheckNeedForLocalURL(key string) {
	LocalURL := l.EntryFieldValueity(key, "local-url")

	if LocalURL == "" {
		// We also seem to use this in main.go ... so maybe a function?
		LocalURL = Library.FilesRoot + FilesFolder + key + ".pdf"
		if !FileExists(LocalURL) {
			LocalURL = ""
		}
	}

	if LocalURL != "" {
		if l.BDSKFileValueMatches(key, "bdsk-file-1", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-2", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-3", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-4", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-5", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-6", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-7", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-8", LocalURL) ||
			l.BDSKFileValueMatches(key, "bdsk-file-9", LocalURL) {

			LocalURL = ""
		} else {
			l.Warning(WarningKeyHasLocalURL, key)
		}
	}

	l.EntryFields[key]["local-url"] = LocalURL
}

func (l *TBibTeXLibrary) CheckEntries() {
	l.Progress(ProgressCheckingConsistencyOfEntries)

	for key := range l.EntryTypes {
		l.CheckPreferredKeyAliasesConsistency(key)
		l.CheckDOIPresence(key)
		l.CheckNeedForLocalURL(key)
		l.CheckBookishTitles(key)
		l.CheckEPrint(key)
		l.CheckCrossref(key)
		l.CheckISBNFromDOI(key)
		l.CheckLanguageID(key)
		l.CheckURLPresence(key)
		l.CheckTitlePresence(key)
		l.CheckURLDateNeed(key)
	}
}
