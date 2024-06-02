/*
 *
 * Module: bibtex_library_writing
 *
 * This module is adds the functionality (for TBibTeXLibrary) to write out BibTeX and associated files
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

import (
	"bufio"
	"os"
	"strings"
	// "fmt"
)

// Read bib files
func (l *TBibTeXLibrary) ReadBib(filePath string) bool {
	//	l.BibFilePath = filePath
	FullFilePath := l.FilesRoot + l.BaseName + BibFileExtension

	return l.ParseBibFile(FullFilePath)
	//l.FilesRoot + l.BibFilePath)
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readFile(fullFilePath, message string, reading func(string)) bool {
	l.Progress(message, fullFilePath)

	file, err := os.Open(fullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		reading(string(scanner.Text()))
	}

	return scanner.Err() == nil
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readLibraryFile(fileExtension, message string, reading func(string)) bool {
	return l.readFile(l.FilesRoot+l.BaseName+fileExtension, message, reading)
}

// Generic binary mapping reader!!
// But, then for <field1> <value1> <field2> <value2?
func (l *TBibTeXLibrary) readAddressMapping(fileExtension, progress string, addMapping func(alias, target string)) {
	l.readLibraryFile(fileExtension, progress, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningAddressesLineTooShort, line)
			return
		}

		// Move the dealiases/normalising to the Library addMapping funtion??
		addMapping(elements[0], l.UnAliasFieldValue("address", l.NormaliseFieldValue("address", elements[1])))
	})
}
func (l *TBibTeXLibrary) readISSNMapping(fileExtension, progress string, addMapping func(alias, target string)) {
	l.readLibraryFile(fileExtension, progress, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningISSNLineTooShort, line)
			return
		}

		addMapping(elements[0], l.UnAliasFieldValue("issn", l.NormaliseFieldValue("issn", elements[1])))
	})
}

// General function to read aliases mapping files
// These files contain two strings per line, separated by a tab, where the second string is an alias for the latter.
// The addMapping is needed as a function, as this involves different implementations per field
func (l *TBibTeXLibrary) readAliasesMapping(fileExtension, progress string, addMapping func(alias, target string, aliasMap *TStringMap, inverseMap *TStringSetMap), aliasMap *TStringMap, inverseMap *TStringSetMap) {
	l.readLibraryFile(fileExtension, progress, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningAliasesLineTooShort, line)
			return
		}

		addMapping(elements[1], elements[0], aliasMap, inverseMap)
	})
}

func (l *TBibTeXLibrary) ReadMappingFiles() {
	l.readAddressMapping(AddressesFileExtension, ProgressReadingAddressesFile, l.AddOrganisationalAddress)
	l.readISSNMapping(ISSNFileExtension, ProgressReadingISSNFile, l.AddSeriesISSN)
}

func (l *TBibTeXLibrary) normalisedWinnerChallengerPair(field, winner, challenger string) (string, string) {
	normalisedWinner := ""
	if winner != "" {
		normalisedWinner = l.NormaliseFieldValue(field, winner)
	}

	// We do normalise the challengers, but want to ignore error messages.
	// The challenged values may actually have errors ...
	normalisedChallenger := ""
	if challenger != "" {
		silenced := l.InteractionIsOff()
		l.SetInteractionOff()
		normalisedChallenger = l.NormaliseFieldValue(field, challenger)
		l.SetInteraction(silenced)
	}

	return normalisedWinner, normalisedChallenger
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadEntryAliasesFile() {
	l.readLibraryFile(EntryAliasesFileExtension, ProgressReadingEntryAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningEntryAliasesLineTooShort, line)
			l.NoEntryAliasesFileWriting = true
			return
		}

		key := elements[0]
		field := elements[1]
		winner, challenger := l.normalisedWinnerChallengerPair(field, elements[2], elements[3])
		l.AddEntryFieldAlias(key, field, l.UnAliasFieldValue(field, challenger), l.UnAliasFieldValue(field, winner), true)
	})
}

// Read field challenge file
func (l *TBibTeXLibrary) ReadGenericAliasesFile() {
	l.readLibraryFile(GenericAliasesFileExtension, ProgressReadingGenericAliasesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 3 {
			l.Warning(WarningGenericAliasesLineTooShort, line)
			l.NoGenericAliasesFileWriting = true
			return
		}

		field := elements[0]
		winner, challenger := l.normalisedWinnerChallengerPair(field, elements[1], elements[2])
		l.AddGenericFieldAlias(field, challenger, winner, true)
	})
}

// Read aliases files
func (l *TBibTeXLibrary) ReadAliasesFiles() {
	l.ReadGenericAliasesFile()
	l.ReadEntryAliasesFile()

	l.readAliasesMapping(KeyAliasesFileExtension, ProgressReadingKeyAliasesFile, l.AddAliasForKey, &l.KeyAliasToKey, &l.KeyToAliases)
	l.readAliasesMapping(NameAliasesFileExtension, ProgressReadingNameAliasesFile, l.AddAliasForName, &l.NameAliasToName, &l.NameToAliases)

	l.readLibraryFile(PreferredKeyAliasesFileExtension, ProgressReadingPreferredKeyAliasesFile, l.AddPreferredKeyAlias)
}
