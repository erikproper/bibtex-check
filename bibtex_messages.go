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
	WarningEntryAlreadyExists              = "Entry '%s' already exists."
	WarningUnknownFields                   = "Unknown field(s) used: %s."
	WarningEmptyTitle                      = "Empty title field for %s."
	WarningAmbiguousKeyOldie               = "Ambiguous key oldie: for %s we already have %s which differs from %s."
	WarningAmbiguousKeyHint                = "Ambiguous key hint: for %s we already have %s which differs from %s."
	WarningAmbiguousAlias                  = "Ambiguous alias: for %s we have %s and %s."
	WarningPreferredAliasNotExist          = "Can't select a non existing alias %s as preferred alias."
	WarningInvalidKey                      = "Invalid key: %s."
	WarningIllegalField                    = "Field \"%s\", with value \"%s\", is not allowed for entry %s of type %s."
	WarningSourceAlreadyUsedAsTarget       = "Mapping source %s is already used as a target%s."
	WarningTargetAlreadyUsedAsSource       = "Mapping target %s is already used as a source%s."
	WarningMappingForKey                   = "for " // FIXXXX the latter one with a sprintf of some form
	WarningTargetOfOldieNotExists          = "Target %s of oldie %s does not exist."
	WarningOldieIsKey                      = "Oldie %s is currently still a key."
	ProgressCheckingConsistencyOfEntries   = "Checking consistency and completeness of the library entries."
	ProgressCheckingConsistencyOfKeyOldies = "Checking consistency and completeness of key oldies."

	// Ignore an (illegal) field
	QuestionIgnore = "Ignore this field?"

	// Warnings when reading files
	WarningUnknownEntryType                 = "Entry %s has an unknown entry type %s."
	WarningFieldMappingsTooShort            = "Line in field mappings file is too short: %s"
	WarningEntryFieldMappingsLineTooShort   = "Line in entry field mappings file is too short: %s"
	WarningGenericFieldMappingsLineTooShort = "Line in generic field mappings file is too short: %s"
	WarningNameMappingsLineTooShort         = "Line in name mappings file is too short: %s"
	WarningKeyAliasesLineTooShort           = "Line in key aliases file is too short: %s"
	WarningKeyHintsLineTooShort             = "Line in key hints file is too short: %s"
	WarningNonDoublesLineTooShort           = "Line in non doubles file is too short: %s"

	// Progress reports for reading/writing files"Line in aliases file is too short: %s"
	ProgressWritingBibFile                 = "Writing bib file %s"
	ProgressWritingFieldsCache             = "Writing fields cache %s"
	ProgressWritingTypesCache              = "Writing types cache %s"
	ProgressWritingCommentsCache           = "Writing comments cache %s"
	ProgressWritingGroupsFile              = "Writing groups file %s"
	ProgressWritingNonDoublesFile          = "Writing non_doubles file %s"
	ProgressWritingGenericFieldAliasesFile = "Writing generic field aliases file %s"
	ProgressWritingEntryFieldAliasesFile   = "Writing entry field aliases file %s"
	ProgressWritingNameMappingsFile        = "Writing name aliases file %s"
	ProgressWritingFieldMappingsFile       = "Writing field mappings file %s"
	ProgressWritingKeyOldiesFile           = "Writing key aliases file %s"
	ProgressWritingKeyHintsFile            = "Writing key hints file %s"

	ProgressReadingBibFile = "Reading bib file %s"

	ProgressReadingFieldsCache     = "Reading fields cache %s"
	ProgressReadingTypesCache      = "Reading types cache %s"
	ProgressReadingCommentsCache   = "Reading comments cache %s"
	ProgressReadingKeyAliasesCache = "Reading key aliases cache %s"

	ProgressReadingNonDoublesFile          = "Reading non_doubles file %s"
	ProgressReadingGenericFieldAliasesFile = "Reading generic field aliases file %s" // Really all these variations?
	ProgressReadingEntryFieldAliasesFile   = "Reading entry field aliases file %s"
	ProgressReadingKeyOldiesFile           = "Reading key oldies file %s"
	ProgressReadingKeyHintsFile            = "Reading key hints file %s"
	ProgressReadingNameMappingsFile        = "Reading name aliases file %s"
	ProgressReadingFieldMappingsFile       = "Reading field mappings file %s"

	ProgressCheckingFieldMappings   = "Checking field mappings."
	WarningMissingFile              = "File %s for key %s seems not to exist."
	WarningInvalidPreferredKeyAlias = "Alias %s for %s does not comply to the rules for preferred aliases."
	WarningBadISBN                  = "Found wrong ISBN \"%s\" for key %s."
	WarningBadISSN                  = "Found wrong ISSN \"%s\" for key %s."
	WarningBadYear                  = "Found wrong year \"%s\" for key %s."
	WarningBadDate                  = "Found wrong date \"%s\" for key %s."
)
