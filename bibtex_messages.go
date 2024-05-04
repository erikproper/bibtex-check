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
	ProgressOpeningBibFile    = "Opening BibTeX file: %s"

	// Names of syntactic classes as used in error messages when parsing BibTeX files
	CharacterClass  = "Character"
	EntryBodyClass  = "EntryBody"
	EntryTypeClass  = "EntryType"
	FieldValueClass = "FieldValue"

	// Error messages & warnings when parsing BibTeX files
	ErrorMissing               = "Missing"
	ErrorMissingCharacter      = ErrorMissing + " " + CharacterClass + " '%s', found '%s'"
	ErrorMissingEntryBody      = ErrorMissing + " " + EntryBodyClass
	ErrorMissingEntryType      = ErrorMissing + " " + EntryTypeClass
	ErrorMissingFieldValue     = ErrorMissing + " " + FieldValueClass
	ErrorOpeningFile           = "Could not open file '%s'"
	ErrorUnknownString         = "Unknown string '%s' referenced"
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
)
