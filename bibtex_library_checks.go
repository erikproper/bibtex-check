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
func CheckPreferredKeyAliasValidity(alias string) bool {
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

/*
 *
 * General library/entry level checks
 *
 */

// Check all alias/entry pairs of the library
func (l *TBibTeXLibrary) CheckKeyAliasesConsistency() {
	l.Progress(ProgressCheckingAliases)

	for alias, entry := range l.KeyAliasToKey {
		// Once we're not in legacy mode anymore, then we need to enforce l.EntryExists(entry)
		if AllowLegacy && l.EntryExists(entry) {
			// Each "DBLP:" pre-fixed alias should be consistent with the dblp field of the referenced entry.
			if strings.Index(alias, "DBLP:") == 0 {
				dblpAlias := alias[5:]
				dblpValue := l.EntryFieldValueity(entry, "dblp")
				if dblpAlias != dblpValue {
					if dblpValue == "" {
						// If we have a dblp alias, and we have no dblp entry, we can safely add this as the dblp value for this entry.
						l.SetEntryFieldValue(entry, "dblp", dblpAlias)
					} else {
						l.Warning(WarningDBLPMismatch, dblpAlias, dblpValue, entry)
					}
				}
			}

			// If we have no defined preferred alias, then we can try to define one.
			// Note: when reading the aliases from file, the first alias that fits the preferred alias requirements is selected as the preferred one.
			// So, if an entry has no preferred alias after reading the alias file, we can only try to create one.
			// To do so, we will actually try to test if the current alias could be coerced into a
			if !l.PreferredKeyAliasExists(entry) {
				loweredAlias := strings.ToLower(alias)

				if !(loweredAlias == alias || loweredAlias == entry || l.AliasExists(loweredAlias)) {
					if CheckPreferredKeyAliasValidity(loweredAlias) {
						l.AddKeyAlias(loweredAlias, entry)
						l.AddPreferredKeyAlias(loweredAlias)
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
	fmt.Println("KKKK")
	//ForEachStringPair(l.entryType, func(a, b string) { fmt.Println(a, b) })
	//for key, entryType := range l.GetEntryTypeMap() {
	//fmt.Println(key, entryType)
	//}
}
