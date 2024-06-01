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

// / Generic binary mapping reader??
func (l *TBibTeXLibrary) readAddressMapping(fileExtension, progress string, addMapping func(alias, target string)) {
	l.readLibraryFile(fileExtension, progress, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningAddressesLineTooShort, line)
			return
		}

		addMapping(elements[0], elements[1])
	})
}

func (l *TBibTeXLibrary) readISSNMapping(fileExtension, progress string, addMapping func(alias, target string)) {
	l.readLibraryFile(fileExtension, progress, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 2 {
			l.Warning(WarningISSNLineTooShort, line)
			return
		}

		addMapping(elements[0], elements[1])
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

// Read aliases files
func (l *TBibTeXLibrary) ReadAliasesFiles() {
	l.readAliasesMapping(KeyAliasesFileExtension, ProgressReadingKeyAliasesFile, l.AddAliasForKey, &l.KeyAliasToKey, &l.KeyToAliases)
	l.readAliasesMapping(NameAliasesFileExtension, ProgressReadingNameAliasesFile, l.AddNameAlias, &l.NameAliasToName, &l.NameToAliases)
	l.readAliasesMapping(JournalAliasesFileExtension, ProgressReadingJournalAliasesFile, l.AddAliasForTextString, &l.JournalAliasToJournal, &l.JournalToAliases)
	l.readAliasesMapping(SeriesAliasesFileExtension, ProgressReadingSeriesAliasesFile, l.AddAliasForTextString, &l.SeriesAliasToSeries, &l.SeriesToAliases)
	l.readAliasesMapping(SchoolAliasesFileExtension, ProgressReadingSchoolAliasesFile, l.AddAliasForTextString, &l.SchoolAliasToSchool, &l.SchoolToAliases)
	l.readAliasesMapping(InstitutionAliasesFileExtension, ProgressReadingInstitutionAliasesFile, l.AddAliasForTextString, &l.InstitutionAliasToInstitution, &l.InstitutionToAliases)
	l.readAliasesMapping(OrganisationAliasesFileExtension, ProgressReadingOrganisationAliasesFile, l.AddAliasForTextString, &l.OrganisationAliasToOrganisation, &l.OrganisationToAliases)
	l.readAliasesMapping(PublisherAliasesFileExtension, ProgressReadingPublisherAliasesFile, l.AddAliasForTextString, &l.PublisherAliasToPublisher, &l.PublisherToAliases)

	l.readLibraryFile(PreferredKeyAliasesFileExtension, ProgressReadingPreferredKeyAliasesFile, l.AddPreferredKeyAlias)

	l.readAddressMapping(AddressesFileExtension, ProgressReadingAddressesFile, l.AddOrganisationalAddress)
	l.readISSNMapping(ISSNFileExtension, ProgressReadingISSNFile, l.AddSeriesISSN)
}

// Read key field challenge file
func (l *TBibTeXLibrary) ReadKeyFieldChallengesFile() {
	l.readLibraryFile(KeyFieldChallengesFileExtension, ProgressReadingKeyFieldChallengesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningKeyFieldChallengeLineTooShort, line)
			return
		}

		key := elements[0]
		field := elements[1]
		challenger := elements[2]
		winner := elements[3]

		if winner != "" {
			winner = l.NormaliseFieldValue(field, winner)
		}

		// We do normalise the challengers, but want to ignore error messages.
		// The challenged values may actually have errors ...
		if challenger != "" {
			silenced := l.InteractionIsOff()
			l.SetInteractionOff()
			challenger = l.NormaliseFieldValue(field, challenger)
			l.SetInteraction(silenced)
		}

		l.AddKeyFieldChallengeWinner(key, field, challenger, winner)
	})
}

// Read field challenge file
func (l *TBibTeXLibrary) ReadFieldChallengesFile() {
	l.readLibraryFile(FieldChallengesFileExtension, ProgressReadingFieldChallengesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 3 {
			l.Warning(WarningFieldChallengeLineTooShort, line)
			return
		}

		field := elements[0]
		challenger := elements[1]
		winner := elements[2]

		if winner != "" {
			winner = l.NormaliseFieldValue(field, winner)
		}

		// We do normalise the challengers, but want to ignore error messages, since the challenged values may actually have errors ...
		if challenger != "" {
			silenced := l.InteractionIsOff()
			l.SetInteractionOff()
			challenger = l.NormaliseFieldValue(field, challenger)
			l.SetInteraction(silenced)
		}

		l.AddFieldChallengeWinner(field, challenger, winner)
	})
}

func (l *TBibTeXLibrary) ReadChallengesFiles() {
	l.ReadFieldChallengesFile()
	l.ReadKeyFieldChallengesFile()
}
