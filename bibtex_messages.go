/*
 *
 * Module: bibtex_messages
 *
 * This module contains the definition of several warnings, error, and progress reports
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 24.04.2024
 *
 */

package main

const (
	// Progress reports
	ProgressInitialiseLibrary = "Initialising %s library"
	ProgressLibrarySize       = "Size of %s library is: %d"

	// Names of syntactic classes as used in error messages when parsing BibTeX files
	CharacterClass  = "Character"
	EntryBodyClass  = "EntryBody"
	EntryTypeClass  = "EntryType"
	FieldValueClass = "FieldValue"

	// Error messages & warnings when parsing BibTeX files
	ErrorMissingCharacter      = "Missing " + CharacterClass + " '%s', found '%s'"
	ErrorMissingEntryBody      = "Missing " + EntryBodyClass
	ErrorMissingEntryType      = "Missing " + EntryTypeClass
	ErrorMissingFieldValue     = "Missing " + FieldValueClass
	ErrorOpeningFile           = "Could not open file '%s'"
	ErrorUnknownString         = "Unknown string '%s' referenced"
	ErrorCharacterNotIn        = "Expected a character from %s"
	WarningSkippingToNextEntry = "Skipping to next entry"

	// Warnings regarding the consistency of the libraries
	WarningDBLPMismatch           = "Found %s as dblp alias, while we already have %s for entry %s"
	WarningEntryAlreadyExists     = "Entry '%s' already exists"
	WarningUnknownFields          = "Unknown field(s) used: %s"
	WarningAmbiguousKeyAlias      = "Ambiguous key alias: for %s we have %s and %s"
	WarningAmbiguousNameAlias     = "Ambiguous name alias: for %s we have %s and %s"
	WarningPreferredNotExist      = "Can't select a non existing alias %s as preferred alias"
	WarningAliasIsKey             = "Alias %s is already known to be a key"
	WarningAliasTargetKeyIsAlias  = "Alias %s has a target key %s, which is actually an alias for %s"
	WarningAliasIsName            = "Alias %s is already known to be a name"
	WarningAliasTargetNameIsAlias = "Alias %s has a target name %s, which is actually an alias for %s"
	WarningBadAlias               = "Alias %s for %s does not comply to the rules"
	WarningIllegalField           = "Field \"%s\", with value \"%s\", is not allowed for entry %s of type %s"
	QuestionIgnore                = "Ignore this field?"
	ProgressCheckingAliases       = "Checking consistency of aliases"

	// Warnings when reading files
	WarningKeyAliasesLineBadEntries = "Line in key aliases file must contain precisely two entries: %s"
	WarningChallengeLineTooShort    = "Line in challenges file is too short: %s"
	WarningNameAliasesLineTooShort  = "Line in name aliases file is too short: %s"
	WarningAliasesLineTooShort      = "Line in aliases file is too short: %s"

	// Progress reports for reading/writing files
	ProgressWritingBibFile             = "Writing bib file %s"
	ProgressWritingKeyAliasesFile      = "Writing key aliases file %s"
	ProgressWritingNameAliasesFile     = "Writing name aliases file %s"
	ProgressWritingChallengesFile      = "Writing challenges file %s"
	ProgressReadingBibFile             = "Reading bib file %s"
	ProgressReadingKeyAliasesFile      = "Reading key aliases file %s"
	ProgressCheckingKeyAliasesMapping  = "Checking key aliases mapping"
	ProgressReadingChallengesFile      = "Reading challenges file %s"
	ProgressReadingNameAliasesFile     = "Reading name aliases file %s"
	ProgressCheckingNameAliasesMapping = "Checking name aliases mapping"
	WarningMissingFile                 = "File %s for key %s seems not to exist"
	WarningBadISBN                     = "Found wrong ISBN \"%s\" for key %s"
	WarningBadISSN                     = "Found wrong ISSN \"%s\" for key %s"
	WarningBadYear                     = "Found wrong year \"%s\" for key %s"
)
