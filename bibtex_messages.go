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

	// Warnings regarding the correctness of libraries
	WarningDBLPMismatch                     = "Found %s as dblp alias, while we already have %s for entry %s."
	WarningEntryAlreadyExists               = "Entry '%s' already exists."
	WarningUnknownFields                    = "Unknown field(s) used: %s."
	WarningEmptyTitle                       = "Empty title field for %s."
	WarningAmbiguousAlias                   = "Ambiguous alias: for %s we have %s and %s."
	WarningPreferredAliasNotExist           = "Can't select a non existing alias %s as preferred alias."
	WarningAliasIsKey                       = "Alias %s is already known to be a key."
	WarningAliasTargetKeyIsAlias            = "Alias %s has a target key %s, which is actually an alias for %s."
	WarningAliasIsName                      = "Alias %s is already known to be a name."
	WarningAliasTargetNameIsAlias           = "Alias %s has a target name %s, which is actually an alias for %s."
	WarningAliasIsJournal                   = "Alias %s is already known to be a journal."
	WarningAliasTargetJournalIsAlias        = "Alias %s has a target journal %s, which is actually an alias for %s."
	WarningAliasIsSchool                    = "Alias %s is already known to be a school."
	WarningAliasTargetSchoolIsAlias         = "Alias %s has a target school %s, which is actually an alias for %s."
	WarningAliasIsInstitution               = "Alias %s is already known to be an institution."
	WarningAliasTargetInstitutionIsAlias    = "Alias %s has a target institution %s, which is actually an alias for %s."
	WarningAliasIsOrganisation              = "Alias %s is already known to be an organisation."
	WarningAliasTargetOrganisationIsAlias   = "Alias %s has a target organisation %s, which is actually an alias for %s."
	WarningAliasIsSeries                    = "Alias %s is already known to be a series."
	WarningAliasTargetSeriesIsAlias         = "Alias %s has a target series %s, which is actually an alias for %s."
	WarningAliasIsPublisher                 = "Alias %s is already known to be a publisher."
	WarningAliasTargetPublisherIsAlias      = "Alias %s has a target publisher %s, which is actually an alias for %s."
	WarningBadAlias                         = "Alias %s for %s does not comply to the rules."
	WarningIllegalField                     = "Field \"%s\", with value \"%s\", is not allowed for entry %s of type %s."
	QuestionIgnore                          = "Ignore this field?"
	ProgressCheckingConsistencyOfEntries    = "Checking consistency and completeness of the library entries."
	ProgressCheckingConsistencyOfKeyAliases = "Checking consistency and completeness of key aliases."

	// Warnings when reading files
	WarningUnknownEntryType                = "Entry %s has an unknown entry type %s."
	WarningKeyAliasesLineBadEntries        = "Line in key aliases file must contain precisely two entries: %s"
	WarningFieldMappingsTooShort           = "Line in field mappings file is too short: %s"
	WarningEntryFieldAliasesLineTooShort   = "Line in entry field aliases file is too short: %s"
	WarningGenericFieldAliasesLineTooShort = "Line in generic field aliases file is too short: %s"
	WarningNameAliasesLineTooShort         = "Line in name aliases file is too short: %s"
	WarningAliasesLineTooShort             = "Line in aliases file is too short: %s"

	// Progress reports for reading/writing files"Line in aliases file is too short: %s"
	ProgressWritingBibFile                 = "Writing bib file %s"
	ProgressWritingGroupsFile              = "Writing groups file %s"
	ProgressWritingNonDoublesFile          = "Writing non_doubles file %s"
	ProgressWritingGenericFieldAliasesFile = "Writing generic field aliases file %s"
	ProgressWritingEntryFieldAliasesFile   = "Writing entry field aliases file %s"
	ProgressWritingPreferredKeyAliasesFile = "Writing preferred key aliases file %s"
	ProgressWritingAddressesFile           = "Writing addresses file %s"
	ProgressWritingKeyAliasesFile          = "Writing key aliases file %s"
	ProgressWritingNameAliasesFile         = "Writing name aliases file %s"
	ProgressWritingFieldMappingsFile       = "Writing field mappings file %s"
	ProgressReadingBibFile                 = "Reading bib file %s"
	ProgressReadingNonDoublesFile          = "Reading non_doubles file %s"
	ProgressReadingGenericFieldAliasesFile = "Reading generic field aliases file %s" // Really all these variations?
	ProgressReadingEntryFieldAliasesFile   = "Reading entry field aliases file %s"
	ProgressReadingAddressesFile           = "Reading organisational addresses file %s"
	ProgressReadingPreferredKeyAliasesFile = "Reading preferred key aliases file %s"
	ProgressReadingKeyAliasesFile          = "Reading key aliases file %s"
	ProgressReadingNameAliasesFile         = "Reading name aliases file %s"
	ProgressReadingFieldMappingsFile       = "Reading field mappings file %s"
	ProgressCheckingKeyAliasesMapping      = "Checking key aliases mapping."
	ProgressCheckingFieldAliasesMapping    = "Checking field aliases mapping."
	WarningMissingFile                     = "File %s for key %s seems not to exist."
	WarningInvalidPreferredKeyAlias        = "Alias %s for %s does not comply to the rules for preferred aliases."
	WarningBadISBN                         = "Found wrong ISBN \"%s\" for key %s."
	WarningBadISSN                         = "Found wrong ISSN \"%s\" for key %s."
	WarningBadYear                         = "Found wrong year \"%s\" for key %s."
	WarningBadDate                         = "Found wrong date \"%s\" for key %s."
)
