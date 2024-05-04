/*
 *
 * Module: bibtex_library_checks
 *
 * This module is concerned with checks of fields and entries.
 *
 * Creator: Henderik A. Proper (erikproper@fastmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

import (
	//	"fmt"
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
func CheckPreferredAliasValidity(alias string) bool {
	var validPreferredAlias = regexp.MustCompile(`^[a-z]+[0-9][0-9][0-9][0-9][a-z][a-z,0-9]*$`)

	return validPreferredAlias.MatchString(alias)
}

func CheckISSNValidity(ISSN string) bool {
	var validISSN = regexp.MustCompile(`^[0-9][0-9][0-9][0-9][-]?[0-9][0-9][0-9][0-9,X]$`)

	return validISSN.MatchString(ISSN)
}

func CheckISBNValidity(ISBN string) bool {
	var validISBN = regexp.MustCompile(`^([0-9][-]?[0-9][-]?[0-9][-]?|)[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9,X]$`)

	return validISBN.MatchString(ISBN)
}

func CheckYearValidity(year string) bool {
	var validYear = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]$`)

	return validYear.MatchString(year)
}

/*
 *
 * General library/entry level checks
 *
 */

// Checking pairs of aliases/entries
func (l *TBibTeXLibrary) CheckAliasEntryPair(alias, entry string) {
	// Once we're not in legacy mode anymore, then we need to enforce l.EntryExists(entry)
	if AllowLegacy && l.EntryExists(entry) {
		// Each "DBLP:" pre-fixed alias should be consistent with the dblp field of the referenced entry.
		if strings.Index(alias, "DBLP:") == 0 {
			dblpAlias := alias[5:]
			dblpValue := l.GetEntryFieldValue(entry, "dblp")
			if dblpAlias != dblpValue {
				if dblpValue == "" {
					// If we have a dblp alias, and we have no dblp entry, we can safely add this.
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
		if !l.PreferredAliasExists(entry) {
			loweredAlias := strings.ToLower(alias)

			if !(loweredAlias == alias || loweredAlias == entry || l.AliasExists(loweredAlias)) {
				if CheckPreferredAliasValidity(loweredAlias) {
					l.AddKeyAlias(loweredAlias, entry, false)
					l.AddPreferredAlias(loweredAlias)
				}
			}
		}
	}
}

// The driver function to check all alias/entry pairs of the library
func (l *TBibTeXLibrary) CheckAliases() {
	l.ForEachAliasEntryPair(l.CheckAliasEntryPair)
}

func (l *TBibTeXLibrary) CheckEntries() {
	//ForEachStringPair(l.entryType, func(a, b string) { fmt.Println(a, b) })
	//for key, entryType := range l.GetEntryTypeMap() {
	//fmt.Println(key, entryType)
	//}
}
