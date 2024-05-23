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
)

// Generic function to write library related files
func (l *TBibTeXLibrary) writeLibraryFile(fileExtension, message string, writing func(*bufio.Writer)) bool {
	FullFilePath := l.FilesRoot + l.BaseName + fileExtension

	l.Progress(message, FullFilePath)

	BackupFile(FullFilePath)

	file, err := os.Create(FullFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	writing(writer)
	writer.Flush()

	return true
}

// Function to write the BibTeX content of the library to a bufio.bWriter buffer
// Notes:
// - As we ignore preambles, these are not written.
// - When we start managing the groups (of keys) the way Bibdesk does, we need to ensure that their embedded as an XML structure embedded in a comment, is updated.
func (l *TBibTeXLibrary) WriteBibTeXFile() {
	l.writeLibraryFile(BibFileExtension, ProgressWritingBibFile, func(bibWriter *bufio.Writer) {
		// Write out the entries and their fields
		for entry := range l.EntryTypes {
			bibWriter.WriteString(l.EntryString(entry))
			bibWriter.WriteString("\n")
		}

		// Write out the comments
		for _, comment := range l.Comments {
			bibWriter.WriteString("@" + CommentEntryType + "{" + comment + "}\n")
			bibWriter.WriteString("\n")
		}
	})
}

// Write the challenges and winners for field values, of this library, to a file
func (l *TBibTeXLibrary) WriteChallenges() {
	l.writeLibraryFile(ChallengesFileExtension, ProgressWritingChallengesFile, func(challengeWriter *bufio.Writer) {
		for key, fieldChallenges := range l.ChallengeWinners {
			if l.EntryExists(key) {
				for field, challenges := range fieldChallenges {
					for challenger, winner := range challenges {
						if challenger != winner {
							challengeWriter.WriteString(key + "\t" + field + "\t" + challenger + "\t" + winner + "\n")
						}
					}
				}
			}
		}
	})
}

// Write the preferred key aliases from this library, to a bufio.bWriter buffer
func (l *TBibTeXLibrary) writePreferredKeyAliases(aliasWriter *bufio.Writer) {
	for key, alias := range Library.PreferredKeyAliases {
		if key != alias && AllowLegacy {
			aliasWriter.WriteString(alias + "\n")
		}
	}
}

// Write alias/original pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) writeAliasesMapping(fileExtension, progress string, aliasMap TStringMap) {
	l.writeLibraryFile(fileExtension, progress, func(aliasWriter *bufio.Writer) {
		for alias, original := range aliasMap {
			if alias != original {
				aliasWriter.WriteString(original + "\t" + alias + "\n")
			}
		}
	})
}

// GENERIC binary writer
// Write address mappings to a bufio.bWriter file
func (l *TBibTeXLibrary) writeAddressMapping(fileExtension, progress string, aliasMap TStringMap) {
	l.writeLibraryFile(fileExtension, progress, func(aliasWriter *bufio.Writer) {
		for organisation, address := range aliasMap {
			aliasWriter.WriteString(organisation + "\t" + address + "\n")
		}
	})
}

// Write name/ISSN pairs to a bufio.bWriter buffer
func (l *TBibTeXLibrary) writeISSNMapping(fileExtension, progress string, ISSNMap TStringMap) {
	l.writeLibraryFile(fileExtension, progress, func(aliasWriter *bufio.Writer) {
		for name, ISSN := range ISSNMap {
			aliasWriter.WriteString(name + "\t" + ISSN + "\n")
		}
	})
}

func (l *TBibTeXLibrary) WriteAliasesFiles() {
	l.writeAliasesMapping(KeyAliasesFileExtension, ProgressWritingKeyAliasesFile, l.KeyAliasToKey)
	l.writeAliasesMapping(NameAliasesFileExtension, ProgressWritingNameAliasesFile, l.NameAliasToName)
	l.writeAliasesMapping(JournalAliasesFileExtension, ProgressWritingJournalAliasesFile, l.JournalAliasToJournal)
	l.writeAliasesMapping(SeriesAliasesFileExtension, ProgressWritingSeriesAliasesFile, l.SeriesAliasToSeries)
	l.writeAliasesMapping(SchoolAliasesFileExtension, ProgressWritingSchoolAliasesFile, l.SchoolAliasToSchool)
	l.writeAliasesMapping(InstitutionAliasesFileExtension, ProgressWritingInstitutionAliasesFile, l.InstitutionAliasToInstitution)
	l.writeAliasesMapping(OrganisationAliasesFileExtension, ProgressWritingOrganisationAliasesFile, l.OrganisationAliasToOrganisation)
	l.writeAliasesMapping(PublisherAliasesFileExtension, ProgressWritingPublisherAliasesFile, l.PublisherAliasToPublisher)

	l.writeLibraryFile(PreferredKeyAliasesFileExtension, ProgressWritingPreferredKeyAliasesFile, l.writePreferredKeyAliases)

	l.writeAddressMapping(AddressesFileExtension, ProgressWritingAddressesFile, l.OrganisationalAddresses)
	l.writeISSNMapping(ISSNFileExtension, ProgressWritingISSNFile, l.SeriesToISSN)
}
