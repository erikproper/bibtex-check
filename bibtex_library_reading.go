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
	l.BibFilePath = filePath

	return l.ParseBibFile(l.FilesRoot + l.BibFilePath)
}

// Generic function to read library related files
func (l *TBibTeXLibrary) readLibraryFile(fileExtension, message string, reading func(string)) bool {
	FullFilePath := l.FilesRoot + l.BaseName + fileExtension

	l.Progress(message, FullFilePath)

	file, err := os.Open(FullFilePath)
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
	l.JournalAliasesFilePath = l.BaseName + JournalAliasesFileExtension
	l.PreferredKeyAliasesFilePath = l.BaseName + PreferredKeyAliasesFileExtension
	l.NameAliasesFilePath = l.BaseName + NameAliasesFileExtension
	l.KeyAliasesFilePath = l.BaseName + KeyAliasesFileExtension

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
}

// Read challenge file
func (l *TBibTeXLibrary) ReadChallenges() {
	l.ChallengesFilePath = l.BaseName + ChallengesFileExtension

	l.readLibraryFile(ChallengesFileExtension, ProgressReadingChallengesFile, func(line string) {
		elements := strings.Split(line, "\t")
		if len(elements) < 4 {
			l.Warning(WarningChallengeLineTooShort, line)
			return
		}

		key := elements[0]
		field := elements[1]
		winner := l.NormaliseFieldValue(field, elements[3])

		// We do normalise the challengers, but want to ignore error messages.
		// The challenged values may actually have errors ...
		silenced := l.InteractionIsOff()
		l.SetInteractionOff()
	    challenger := l.NormaliseFieldValue(field, elements[2])
		l.SetInteraction(silenced)

		l.AddChallengeWinner(key, field, challenger, winner)
	})
}
