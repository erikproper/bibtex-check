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

package main

import (
	// "fmt"
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
func PreferredKeyAliasIsValid(alias string) bool {
	var validPreferredKeyAlias = regexp.MustCompile(`^[a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*$`)

	return validPreferredKeyAlias.MatchString(alias)
}

// Checks if a given ISSN fits the desired format
func CheckISSNValidity(ISSN string) bool {
	var validISSN = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9,X]$`)

	return validISSN.MatchString(ISSN)
}

// Checks if a given ISBN fits the desired format
func CheckISBNValidity(ISBN string) bool {
	var validISBN = regexp.MustCompile(`^([0-9][-]?[0-9][-]?[0-9][-]?|)[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9,X]$`)

	return validISBN.MatchString(ISBN)
}

// Checks if a given year is indeed a year
func CheckYearValidity(year string) bool {
	var validYear = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]$`)

	return validYear.MatchString(year)
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

func (l *TBibTeXLibrary) CheckAliasesMapping(aliasMap TStringMap, inverseMap TStringSetMap, progress, warningUsedAsAlias, warningTargetIsAlias string) {
	l.Progress(progress)

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

func (l *TBibTeXLibrary) CheckKeyAliasesMapping() {
	l.CheckAliasesMapping(l.KeyAliasToKey, l.KeyToAliases, ProgressCheckingKeyAliasesMapping, WarningAliasIsKey, WarningAliasTargetKeyIsAlias)
}

func (l *TBibTeXLibrary) CheckNameAliasesMapping() {
	l.CheckAliasesMapping(l.NameAliasToName, l.NameToAliases, ProgressCheckingNameAliasesMapping, WarningAliasIsName, WarningAliasTargetNameIsAlias)
}

func (l *TBibTeXLibrary) CheckJournalAliasesMapping() {
	l.CheckAliasesMapping(l.JournalAliasToJournal, l.JournalToAliases, ProgressCheckingJournalAliasesMapping, WarningAliasIsJournal, WarningAliasTargetJournalIsAlias)
}

func (l *TBibTeXLibrary) CheckSeriesAliasesMapping() {
	l.CheckAliasesMapping(l.SeriesAliasToSeries, l.SeriesToAliases, ProgressCheckingSeriesAliasesMapping, WarningAliasIsSeries, WarningAliasTargetSeriesIsAlias)
}

// School, Institution, Organization, Publisher, 

/*
 *
 * General library/entry level checks & updates
 *
 */

func (l *TBibTeXLibrary) CheckPreferredKeyAliasesConsistency() {
	l.Progress(ProgressCheckingConsistencyOfPreferredKeyAliases)

	for key := range l.EntryTypes {
		if !l.PreferredKeyAliasExists(key) {
			///// CLEANER
			for alias := range l.KeyToAliases[key].Set().Elements() {
				if PreferredKeyAliasIsValid(alias) {
					// If we have no defined preferred alias, then we can use this one if it would be a valid preferred alias
					l.AddPreferredKeyAlias(alias)
				} else {
					// If we have no defined preferred alias, and the current alias is not valid, we can still try to lower the case and see if this works.
					loweredAlias := strings.ToLower(alias)

					// We do have to make sure the new alias is not already in use, and if it is then a valid alias.
					if !l.AliasExists(loweredAlias) && PreferredKeyAliasIsValid(loweredAlias) {
						l.AddPreferredKeyAlias(alias)
					}
				}
			}
		}
	}
}

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
	}
}

//	if _, aliasIsActuallyKeyToEntry := l.EntryFields[alias]; aliasIsActuallyKeyToEntry {
//		// Aliases cannot be keys themselves.
//		l.Warning(WarningAliasIsKey, alias)
//
//		return
//	}

func (l *TBibTeXLibrary) CheckEntries() {
	//	fmt.Println("KKKK")
	//
	// ForEachStringPair(l.entryType, func(a, b string) { fmt.Println(a, b) })
	// for key, entryType := range l.GetEntryTypeMap() {
	// fmt.Println(key, entryType)
	// }
}
