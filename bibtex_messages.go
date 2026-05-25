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
	ProgressInitialiseLibrary = "Initialising library"
	ProgressLibrarySize       = "Library size: %d"

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
ProgressWritingNonDoublesFile          = "Writing non_doubles file %s"
	ProgressWritingGenericFieldMappingsFile = "Writing generic field mappings file %s"
	ProgressWritingEntryFieldMappingsFile   = "Writing entry field mappings file %s"
	ProgressWritingNameMappingsFile        = "Writing name aliases file %s"
	ProgressWritingFieldMappingsFile       = "Writing field mappings file %s"
	ProgressWritingKeyOldiesFile           = "Writing key aliases file %s"
	ProgressWritingKeyHintsFile            = "Writing key hints file %s"
	ProgressWritingGetBib                  = "Writing get bib file %s"
	WarningGetBibFileModified              = "Get-bib file %s has been manually edited since last generation."
	QuestionGetBibOverwrite                = "Overwrite anyway?"
	ProgressWritingPDFConfirmedOkFile      = "Writing PDF confirmed-OK file %s"
	ProgressCheckingPDFHealth             = "Checking PDF health in %s."
	WarningBrokenPDF                      = "PDF for %s is suspect: %s\n  Path: %s"
	WarningEmptyPDFFile                   = "Discarding empty file for %s: %s"
	WarningHTMLDisguisedAsPDF             = "Discarding HTML file disguised as PDF for %s: %s"
	WarningPSConversionFailed             = "PS→PDF conversion failed for %s: %v"
	ProgressConvertedPSToPDF              = "Converted PostScript to PDF for %s"
	ProgressFetchingDBLPEntry             = "Fetching DBLP entry for %s from dblp.org"
	ProgressDownloadingPDF                = "Downloading PDF for %s: %s"
	ProgressPDFDownloaded                 = "Downloaded PDF for %s → %s"
	WarningPDFDownloadFailed              = "Download failed for %s (%s): %v"

	ProgressReadingBibFile    = "Reading bib file %s"
	ProgressReparsingBibFile  = "Database out of date — re-parsing bib file into database"
	ProgressBackingUpDatabase     = "Backing up database before re-parse"
	ProgressCreatingLibraryBackup = "Creating library backup %s"

	ProgressReadingNonDoublesFile          = "Reading non_doubles file %s"
	ProgressReadingGenericFieldMappingsFile = "Reading generic field mappings file %s"
	ProgressReadingEntryFieldMappingsFile   = "Reading entry field mappings file %s"
	ProgressReadingKeyOldiesFile           = "Reading key oldies file %s"
	ProgressReadingKeyHintsFile            = "Reading key hints file %s"
	ProgressReadingNameMappingsFile        = "Reading name aliases file %s"
	ProgressReadingFieldMappingsFile       = "Reading field mappings file %s"

	ProgressEntryCacheLoaded  = "Entry access: in-memory cache (%d entries)"
	ProgressEntryPerQuery     = "Entry access: per-entry database reads"
	ProgressEntryProgress     = "Entry %d/%d (%.0f%%)"
	ProgressLoadingEntryCache = "Loading entry cache"
	ProgressBuildingTitleIndex = "Building title index"
	ProgressBuildingKeyAliases = "Building key aliases"

	ProgressFixingDblpEntries       = "Fixing DBLP entries"
	ProgressCheckingFieldMappings   = "Checking field mappings."
	ProgressCheckingNameMappings    = "Checking name mappings."
	WarningMissingFile              = "File %s for key %s seems not to exist."
	WarningFileNotAssociated        = "File %s is not associated to any library entry."
	WarningDuplicateFileContent     = "File with same content is used by multiple entries: %s."
	ProgressFileProgress            = "File %d/%d (%.0f%%)"
	WarningInvalidPreferredKeyAlias         = "Alias %s for %s does not comply to the rules for preferred aliases."
	ErrorSetPreferredAliasInvalidFormat     = "Alias %s does not comply to the rules for preferred aliases; not set."
	ErrorSetPreferredAliasAlreadyInUse      = "Alias %s is already in use for entry %s; not set."
	WarningCannotDeriveAliasNoName           = "Cannot derive preferred alias for %s: no author, editor, or publisher found."
	WarningCannotDeriveAliasEmptySurname     = "Cannot derive preferred alias for %s: surname reduces to empty string (raw: %s)."
	WarningCannotDeriveAliasNoYear           = "Cannot derive preferred alias for %s: no valid year found."
	WarningCannotDeriveUniquePreferredAlias  = "Cannot derive unique preferred alias for %s (base: %s): all title keywords already in use."
	WarningNoTitleKeywordsForPreferredAlias  = "Cannot derive preferred alias for %s (base: %s): no usable title keywords found."
	ProgressGeneratedPreferredAlias = "Generated preferred alias %s for %s"
	ProgressRemovedRedundantURL     = "Removed redundant URL for %s: %s"
	WarningBadISBN                       = "Found wrong ISBN \"%s\" for key %s."
	WarningISBNMismatchFromCrossrefDOI   = "Entry %s has crossref to %s with doi-derived isbn %s, but parent has isbn %s."
	WarningBadISSN                  = "Found wrong ISSN \"%s\" for key %s."
	WarningBadYear                  = "Found wrong year \"%s\" for key %s."
	WarningBadDate                  = "Found wrong date \"%s\" for key %s."
	WarningUnresolvedUnicode        = "Unresolved \\unicode escape in field '%s': %s"
)
