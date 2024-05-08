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
 * General library/entry level checks
 *
 */

// The driver function to check all alias/entry pairs of the library
func (l *TBibTeXLibrary) CheckAliases() {
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
						l.AddKeyAlias(loweredAlias, entry, false)
						l.AddPreferredKeyAlias(loweredAlias)
					}
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) EntryAllowsForField(entry, field string) bool {
	return BibTeXAllowedEntryFields[l.EntryTypes[entry]].Set().Contains(field)
}

func (l *TBibTeXLibrary) CheckEntries() {
	//ForEachStringPair(l.entryType, func(a, b string) { fmt.Println(a, b) })
	//for key, entryType := range l.GetEntryTypeMap() {
	//fmt.Println(key, entryType)
	//}
}
