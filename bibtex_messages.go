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
	ProgressLibrarySize       = "Size of %s is: %d"

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
	WarningDBLPMismatch       = "Found %s as dblp alias, while we already have %s for entry %s"
	WarningEntryAlreadyExists = "Entry '%s' already exists"
	WarningUnknownFields      = "Unknown field(s) used: %s"
	WarningAmbiguousAlias     = "Ambiguous alias; for %s we have %s and %s"
	WarningAliasIsKey         = "Alias %s is already known to be a key"
	WarningPreferredNotExist  = "Can't select a non existing alias %s as preferred alias"
	WarningAliasTargetIsAlias = "Alias %s has a target $s, which is actually an alias for $s"
	WarningBadAlias           = "Alias %s for %s does not comply to the rules"
	WarningIllegalField       = "Field \"%s\", with value \"%s\", is not allowed for entry %s of type %s"
	QuestionIgnore            = "Ignore this field?"
	ProgressCheckingAliases   = "Checking consistency of aliases"

	// Warnings when reading files
	WarningAliasLineBadEntries   = "Line in alias file must contain precisely two entries: %s"
	WarningChallengeLineTooShort = "Line in challenges file is too short: %s"

	// Progress reports for reading/writing files
	ProgressWritingBibFile        = "Writing bib file %s"
	ProgressWritingAliasesFile    = "Writing aliases file %s"
	ProgressWritingChallengesFile = "Writing challenges file %s"
	ProgressReadingBibFile        = "Reading bib file %s"
	ProgressReadingAliasesFile    = "Reading aliases file %s"
	ProgressReadingChallengesFile = "Reading challenges file %s"
	WarningMissingFile            = "File %s for key %s seems not to exist"
	WarningBadISBN                = "Found wrong ISBN \"%s\" for key %s"
	WarningBadISSN                = "Found wrong ISSN \"%s\" for key %s"
	WarningBadYear                = "Found wrong year \"%s\" for key %s"
)
