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
	ProgressInitialiseLibrary = "  Initialising library"
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
	WarningEmptyTitle                      = "Empty title field."
	WarningAmbiguousKeyOldie               = "Ambiguous key oldie: for %s we already have %s which differs from %s."
	WarningAmbiguousKeyHint                = "Ambiguous key hint: for %s we already have %s which differs from %s."
	WarningAmbiguousAlias                  = "Ambiguous alias: for %s we have %s and %s."
	WarningPreferredAliasNotExist          = "Can't select a non existing alias %s as preferred alias."
	WarningInvalidKey                      = "Invalid key."
	WarningIllegalField                    = "Field \"%s\", with value \"%s\", is not allowed for entry %s of type %s."
	WarningSourceAlreadyUsedAsTarget       = "Mapping source %s is already used as a target"
	WarningTargetAlreadyUsedAsSource       = "Mapping target %s is already used as a source"
	WarningMappingForKey                   = " for "
	WarningTargetOfOldieNotExists          = "Target %s of oldie %s does not exist."
	WarningTargetOfHintNotExists           = "Key hint target %s (for hint %q) does not exist in library — entry may have been lost."
	ProgressCheckingConsistencyOfEntries   = "  Checking consistency and completeness of the library entries"
	ProgressCheckingConsistencyOfKeyOldies = "  Checking consistency and completeness of key oldies"

	// Ignore an (illegal) field
	QuestionIgnore         = "Ignore this field?"
	QuestionAddToDblpWaived        = "Add to dblp_waived (suppress further warnings)?"
	QuestionNoDblpKeyForChildAction = "Enter DBLP key, waive (suppress warnings), or skip? (k=dblp key, y=waive, n=skip)"

	// Warnings when reading files
	WarningUnknownEntryType                 = "Entry %s has an unknown entry type %s."
	WarningFieldMappingsTooShort            = "Line in field mappings file is too short: %s"
	WarningEntryFieldMappingsLineTooShort   = "Line in entry field mappings file is too short: %s"
	WarningGenericFieldMappingsLineTooShort = "Line in generic field mappings file is too short: %s"
	WarningKeyAliasesLineTooShort           = "Line in key aliases file is too short: %s"
	WarningKeyHintsLineTooShort             = "Line in key hints file is too short: %s"
	WarningNonDoublesLineTooShort           = "Line in non doubles file is too short: %s"
	WarningNoDblpKeyForChild                = "No DBLP key (parent %s has DBLP key %s)."
	WarningDblpKeyNotInXML                  = "DBLP key %s for entry %s not in local XML (missing since %s)."
	WarningExtendDblpCandidatesFound        = "Entry %s has no DBLP key — found %d candidate(s)"
	QuestionExtendDblpCoverageChoose        = "Which DBLP entry matches? (0 = none, k = enter key manually)"

	QuestionHarvestLibraryChoice       = "Which library entry matches? (0 = none)"
	QuestionHarvestAction              = "No match found — add to library or skip? (a=add, k=add+dblp key, m=merge into key, s=skip, i=ignore, q=quit)"
	QuestionHarvestDblpChoose          = "Which DBLP entry matches? (0 = none, k = enter key manually)"
	WarningHarvestDblpCandidatesFound  = "No DBLP key on source entry '%s' — found %d candidate(s)"
	ProgressHarvestParsed              = "harvest: %d entr%s parsed from %s"
	ProgressHarvestSkipped             = "harvest: no entries found in source bib"

	WarningURLDead              = "URL appears unreachable or lacks human content (%s): %s — setting urldate to %s"
	QuestionDoublePdfWaive      = "PDF shared by multiple entries — waive, merge, or skip? (w=waive all, m=merge, s=skip)"
	QuestionLocalPDFConflict    = "Local PDF is newer than global — keep local (copy→global), keep global (overwrite local), open both, or skip? (l=local, g=global, o=open-both, s=skip)"
	QuestionMergePDFConflict    = "Merge has two PDFs — keep target, keep source, open both, or skip? (t=target, s=source, o=open-both, k=skip)"

	WarningLoneProceedings    = "Lone proceedings (no children): %s"
	QuestionLoneProceedings   = "Waive, delete (+ hints/oldies), enter DBLP key, or skip? (w=waive, d=delete, k=dblp key, s=skip)"

	QuestionSubsetBibChanged  = "Bib entry changed — merge into library? (field challenges will follow)"
	QuestionSubsetDeleteEntry = "Entry removed from subset bib — delete from library?"
	QuestionSubsetBothChanged = "Both bib and DB changed — apply bib changes to library? (y=yes, n=keep DB version)"

	ProgressFixedEntryType                  = "Auto-fixed entry type for %s: %s → %s"
	ProgressFixedParentType                 = "Auto-fixed parent type for %s: %s → %s (required by child %s)"
	ProgressFixedChildYear                  = "Auto-fixed year for %s: %s → %s (aligned to parent)"
	ProgressFixedChildType                  = "Auto-fixed child type for %s: %s → %s (parent is proceedings)"

	// Progress reports for reading/writing files"Line in aliases file is too short: %s"
	ProgressWritingBibFile                 = "Writing bib file %s"
	ProgressWritingNonDoublesFile          = "Writing non_doubles file %s"
	ProgressWritingDblpParentFile          = "Writing DBLP parent file %s"
	ProgressWritingDblpWaivedFile          = "Writing DBLP waived file %s"
	ProgressWritingDblpKeyMissingFile      = "Writing DBLP key missing file %s"
	ProgressWritingEntryFlagsFile          = "Writing entry flags file %s"
	ProgressWritingEntryLineageFile        = "Writing entry lineage file %s"
	ProgressWritingGenericFieldMappingsFile = "Writing generic field mappings file %s"
	ProgressWritingEntryFieldMappingsFile   = "Writing entry field mappings file %s"
	ProgressWritingFieldMappingsFile       = "Writing field mappings file %s"
	ProgressWritingKeyOldiesFile           = "Writing key aliases file %s"
	ProgressWritingKeyHintsFile            = "Writing key hints file %s"
	ProgressWritingGetBib                  = "Writing get bib file %s"
	ProgressBuildingSyncBib               = "Building sync bib %s"
	WarningKeysFileUnknownKey             = "Keys file %s references unknown library key %s (local key: %s) — entry skipped"
	WarningKeysFileDuplicateCanonical     = "Keys file %s: canonical key %s is referenced by two local keys (%s and %s) — both kept; fix in LaTeX source"
	WarningGetBibFileModified              = "Get-bib file %s has been manually edited since last generation."
	QuestionGetBibOverwrite                = "Overwrite anyway?"
	ProgressWritingPDFConfirmedOkFile      = "Writing PDF confirmed-OK file %s"
	ProgressCheckingPDFHealth             = "Checking PDF health in %s"
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
	ProgressImportingBibFile  = "Importing bib file %s into database"
	ProgressClearingBibTables       = "Clearing bib tables"
	ProgressParsingBibFile          = "Parsing bib file"
	ProgressBackingUpDatabase       = "Backing up database before re-parse"
	ProgressCreatingLibraryBackup   = "Creating library backup %s"
	ProgressCopyingToWorkingDatabase = "Copying database to working location"
	ProgressSavingDatabaseToHome    = "Saving database to home location"

	WarningWorkingDbNewer = "Working database is newer than home (possible crash from previous run)"

	ProgressReadingNonDoublesFile          = "Reading non_doubles file %s"
	ProgressReadingGenericFieldMappingsFile = "Reading generic field mappings file %s"
	ProgressReadingEntryFieldMappingsFile   = "Reading entry field mappings file %s"
	ProgressReadingKeyOldiesFile           = "Reading key oldies file %s"
	ProgressReadingKeyHintsFile            = "Reading key hints file %s"
	ProgressReadingFieldMappingsFile       = "Reading field mappings file %s"

	ProgressEntryCacheLoaded  = "  Entry access: in-memory cache (%d entries)"
	ProgressEntryPerQuery     = "  Entry access: per-entry database reads"
	ProgressEntryProgress     = "Entry %d/%d (%.0f%%)"
	ProgressLoadingEntryCache = "  Loading entry cache"
	ProgressBuildingTitleIndex = "  Building title index"
	ProgressBuildingKeyAliases = "  Building key aliases"

	ProgressScanningOrcids           = "Scanning DBLP for ORCIDs"
	ProgressFixingDblpEntries        = "  Fixing DBLP entries"
	ProgressFixingDblpHierarchy      = "  Resolving DBLP parent ambiguities"
	WarningCrossrefCycle             = "Crossref cycle detected: %s"
	ProgressCheckingFieldMappings   = "  Checking field mappings"
	ProgressCheckingNameMappings    = "  Checking name mappings"
	WarningMissingFile              = "File %s for key %s seems not to exist."
	WarningFileNotAssociated        = "File %s is not associated to any library entry."
	WarningDuplicateFileContent     = "File with same content is used by multiple entries: %s."
	ProgressFileProgress            = "File %d/%d (%.0f%%)"
	WarningInvalidPreferredKeyAlias         = "Alias %q does not comply to preferred alias rules."
	ErrorSetPreferredAliasInvalidFormat     = "Alias %s does not comply to the rules for preferred aliases; not set."
	ErrorSetPreferredAliasAlreadyInUse      = "Alias %s is already in use for entry %s; not set."
	WarningCannotDeriveAliasNoName           = "Cannot derive preferred alias: no author, editor, or publisher found."
	WarningCannotDeriveAliasEmptySurname     = "Cannot derive preferred alias: surname reduces to empty string (raw: %q)."
	WarningCannotDeriveAliasNoYear           = "Cannot derive preferred alias: no valid year found."
	WarningCannotDeriveUniquePreferredAlias  = "Cannot derive unique preferred alias (base: %s): all title keywords already in use."
	WarningNoTitleKeywordsForPreferredAlias  = "Cannot derive preferred alias (base: %s): no usable title keywords found."
	ProgressGeneratedPreferredAlias = "Generated preferred alias %s for %s"
	ProgressRemovedRedundantURL     = "Removed redundant URL for %s: %s"
	WarningBadISBN                       = "Wrong ISBN: %q."
	WarningISBNMismatchFromCrossrefDOI   = "Crossref to %s: DOI-derived ISBN %s conflicts with parent ISBN %s."
	WarningBadISSN                  = "Wrong ISSN: %q."
	WarningBadYear                  = "Wrong year: %q."
	WarningBadDate                  = "Wrong URL date: %q."
	WarningUnresolvedUnicode        = "Unresolved \\unicode escape in field '%s': %s"

	WarningStateNamesLineTooShort              = "Line in state names file is too short: %s"
	WarningStateCountriesLineTooShort          = "Line in state countries file is too short: %s"
	WarningCountryNamesLineTooShort            = "Line in country names file is too short: %s"
	WarningBooktitleCountryNamesLineTooShort   = "Line in booktitle country names file is too short: %s"
	WarningStateOverlapsCountry       = "Address state '%s' also appears as a country name; will be treated as country."
	WarningStateHasNoCountry          = "Address state '%s' has no entry in the state-to-country mapping."

	WarningMergeConflictingField = "Entries %s and %s have conflicting %s values (%s vs %s)"
	QuestionMergeAnyway          = "Merge anyway?"

	ProgressCheckingEntryFieldMappingWinners  = "  Checking entry-field mapping winner consistency"
	WarningEntryFieldMappingWinnerMismatch    = "Entry-field mapping winner mismatch: entry=%s field=%s winner=%q actual=%q"
	WarningEntryFieldMappingDeletedEntry      = "Entry-field mapping references deleted entry %s — field=%s challenger=%q winner=%q"
	ProgressEntryFieldMappingWinnersResult    = "  Entry-field mapping winner check: %d fixed, %d mapping(s) for deleted entries"

	WarningGenericFieldMappingAuthorEditor = "field_mappings: field %q not allowed as same-field mapping (winner=%q, challenger=%q) — use losing_field_values instead"
	WarningFieldMappingCycle               = "Field mapping cycle rejected: (%s, %q) → (%s, %q) would close a cycle"

	// Stat row labels — used in Library statistics, Session changes, and Homework blocks.
	StatEntries                            = "Entries"
	StatPDFFiles                           = "PDF files"
	StatContributors                       = "Contributors"
	StatContributorForms                   = "Contributor name forms"
	StatNameSpellings                      = "Name spellings"
	StatEntryContributorAliases            = "Entry contributor aliases"
	StatFieldMappings                      = "Field mappings"
	StatFieldMappingsSameField             = "Same-field mappings"
	StatFieldMappingsCrossField            = "Cross-field mappings"
	StatLosingValues                       = "Superseded values"
	StatLosingValuesPending                = "Superseded values awaiting triage"
	StatDblpCoverage                       = "DBLP coverage"
	StatDblpCrossrefOverrides              = "DBLP crossref overrides"
	StatDblpWaivedChildren                 = "DBLP waived children"
	StatKeyOldies                          = "Key oldies"
	StatKeyHints                           = "Key hints"
	StatNonDoubleEntryPairs                = "Non-double entry pairs"
	StatNonDoubleContributorPairs          = "Non-double contributor pairs"
	StatNonDoubleNamePairs                 = "Non-double name pairs"
	StatEntryFlags                         = "Entry flags"
	StatDblpLinks                          = "DBLP links"
	StatDblpKeysManuallyEntered            = "DBLP keys manually entered"
	StatTitleGroupsWithUnresolvedDuplicates = "Title groups with unresolved duplicates"
	StatEntriesWithUnresolvedDblpCandidates = "Entries with unresolved DBLP candidates"
	StatContributorsWithOrcidNotYetEnriched = "Contributors with ORCID not yet enriched"
)
