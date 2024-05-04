package main

import (
	"fmt"
	"regexp"
	"strings"
)

//
//
// Value conformity checks
//
//

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

//
//
// General checks
//
//

func (l *TBibTeXLibrary) CheckAliasEntryPair(alias, entry string) {
	// Each "DBLP:" pre-fixed alias should be consistent with the dblp field of the referenced entry.
	if l.EntryExists(entry) {
		if strings.Index(alias, "DBLP:") == 0 {
			dblpAlias := alias[5:]
			dblpValue := l.GetEntryFieldValue(entry, "dblp")
			if dblpAlias != dblpValue {
				if dblpValue == "" {
					l.SetEntryFieldValue(entry, "dblp", dblpAlias)
				} else {
					fmt.Println("Found:", dblpAlias, "for", entry, "while", dblpValue)
				}
			}
		}

		//// HERE! !l.PreferredAliasExists(entry)
		if l.preferredAliases[entry] == "" {
			if CheckPreferredAliasValidity(alias) {
				l.AddPreferredAlias(alias)
			} else {
				loweredAlias := strings.ToLower(alias)
				//////
				if l.deAlias[loweredAlias] == "" && loweredAlias != entry && CheckPreferredAliasValidity(loweredAlias) {
					l.AddKeyAlias(loweredAlias, entry, false)
					l.AddPreferredAlias(loweredAlias)
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckAliases() {
	l.ForEachAliasEntryPair(l.CheckAliasEntryPair)
}

func (l *TBibTeXLibrary) CheckEntries() {
	//ForEachStringPair(l.entryType, func(a, b string) { fmt.Println(a, b) })
	//for key, entryType := range l.GetEntryTypeMap() {
	//fmt.Println(key, entryType)
	//}
}
