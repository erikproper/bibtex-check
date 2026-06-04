/*
 *
 * Module: bibtex_library_resolver
 *
 * This module is concerned with the resolution of conflicting field values
 * Presently, this is mainly needed to deal with the legacy migration.
 * In the future, this will also be crucial when dealing with the integration of double entries and legacy files in particular.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 06.05.2024
 *
 */

package main

func (l *TBibTeXLibrary) ResolveFileReferences(key, otherKey string) string {
	regularFileReference := l.FilesFolder + key + ".pdf"
	otherFileReference := l.FileReferencety(otherKey)

	fqOtherFileReference := l.FilesRoot + otherFileReference
	fqRegularFileReference := l.FilesRoot + regularFileReference

	if regularFileReference != otherFileReference {
		if FileExists(fqOtherFileReference) {
			if FileExists(fqRegularFileReference) &&
				l.WarningYesNoQuestion("Keep current", "Non-equal file reference; choice needed\nFor %s\nChallenge: %s\nCurrent:   %s", key, fqOtherFileReference, fqRegularFileReference) {
				FileDelete(fqOtherFileReference)
			} else {
				FileRename(fqOtherFileReference, fqRegularFileReference)
			}
		}
	}

	if FileExists(fqRegularFileReference) {
		return regularFileReference
	} else {
		return ""
	}
}

func (l *TBibTeXLibrary) ResolveFieldValue(key, challengeKey, field, challengeRaw, currentRaw string) string {
	current := l.MapFieldValue(field, currentRaw)
	challenge := l.MapFieldValue(field, l.NormaliseFieldValue(field, challengeRaw))

	if current == challenge {
		return current
	}

	if field == "crossref" {
		sourceEntry := l.EntryString(current, "", "  ")
		targetEntry := l.EntryString(challenge, "", "  ")
		if l.WarningYesNoQuestion("Shall I merge the crossreferenced entries as well?",
			"Different crossrefs (%s, %s) for entries (%s, %s) that you want to merge.\nFirst entry:\n%s\nSecond entry:\n%s",
			current, challenge, key, challengeKey, sourceEntry, targetEntry) {
			return l.MergeEntries(current, challenge)
		}
		return current
	}

	if field == "modificationdate" || field == "creationdate" {
		if current < challenge {
			return current
		}
		return challenge
	}

	if field == "url" && l.IsRedundantURL(challenge, key) {
		return ""
	}

	if field == "groups" {
		return current + ", " + challenge
	}

	if field == LocalURLField {
		return l.ResolveFileReferences(key, challengeKey)
	}

	if l.EntryFieldAliasHasTarget(key, field, current, challenge) {
		return challenge
	} else if l.EntryFieldAliasHasTarget(key, field, challenge, current) {
		return current
	}

	// If the normalised forms are equal, there is no genuine content difference —
	// only a representation difference (e.g. "China" vs "{China}" after country
	// normalisation).  Silently adopt the normalised form without prompting.
	// This check comes after the stored-mapping lookups so explicit user decisions
	// (recorded via UpdateEntryFieldAlias) still take precedence.
	if normCurrent := l.MapFieldValue(field, l.NormaliseFieldValue(field, currentRaw)); normCurrent == challenge {
		return normCurrent
	}

	// Full title beats a braced series abbreviation (e.g. "{ICDE}") in both directions.
	if field == "booktitle" || field == "title" {
		currentIsAbbrev := len(current) > 0 && current[0] == '{' && len(current)*3 < len(challenge)
		challengeIsAbbrev := len(challenge) > 0 && challenge[0] == '{' && len(challenge)*3 < len(current)
		if currentIsAbbrev {
			l.UpdateEntryFieldAlias(key, field, current, challenge)
			return challenge
		}
		if challengeIsAbbrev {
			l.UpdateEntryFieldAlias(key, field, challenge, current)
			return current
		}
	}

	// Priority-based resolution with lineage tracking.
	challengeSource := sourceFromChallengeKey(challengeKey)
	currentRec := l.getLineage(key, field)
	challengePriority := lineagePriorityOf(challengeSource)
	currentPriority := lineagePriorityOf(currentRec.Source)

	if challengePriority < currentPriority {
		return current
	}

	// Equal or higher priority challenger: compare semantic content.
	if TeXStringIndexer(current) == TeXStringIndexer(challenge) {
		// For title/booktitle in library-to-library merges (no external source), brace structure
		// is semantically significant — fall through to the user challenge rather than silently
		// accepting either form.
		if (field == TitleField || field == "booktitle") && challengeSource == "" {
			// fall through
		} else {
			// Semantically equal but textually different: keep our text, mark as intentionally diverged.
			if challengeSource != "" {
				l.setLineage(key, field, challengeSource, true)
			}
			return current
		}
	}

	// Semantically different: auto-accept known-authoritative fields, otherwise ask.
	if challengeSource != "" && dblpAutoAcceptFields.Contains(field) {
		l.setLineage(key, field, challengeSource, false)
		return challengeRaw
	}

	// -fix mode: auto-keep current for title/booktitle/volume/edition and record the
	// entry-specific mapping so DBLP cannot re-challenge with the same old form.
	if cmdAutoFixAlignTitles && (field == TitleField || field == "booktitle" || field == "volume" || field == "edition") {
		l.UpdateEntryFieldAlias(key, field, challenge, current)
		l.setLineage(key, field, challengeSource, true)
		return current
	}

	options := TStringSetNew()
	if field == EntryTypeField || field == "year" || field == "pages" || field == "author" || field == "editor" ||
		field == "month" || field == "dblp" || field == "title" || field == "number" || field == "booktitle" {
		options.Add("y", "n")
	} else {
		options.Add("Y", "y", "n", "N")
	}
	warning := "For entry %s and field %s:\n- Challenger: %s\n- Current   : %s\nneeds to be resolved"
	question := "Challenging entry:\n" + l.EntryString(challengeKey, "", "  ")
	question += "Current entry:\n" + l.EntryString(key, "", "  ")
	question += "Keep the value as is?"
	answer := l.WarningQuestion(question, options, warning, key, field, challenge, current)

	switch answer {
	case "y":
		if field != PreferredAliasField {
			l.UpdateEntryFieldAlias(key, field, challenge, current)
		} else {
			delete(l.KeyToKey, challenge)
		}
		l.setLineage(key, field, challengeSource, true)
		return current
	case "n":
		if field != PreferredAliasField {
			l.UpdateEntryFieldAlias(key, field, current, challenge)
		} else {
			delete(l.KeyToKey, current)
		}
		l.setLineage(key, field, challengeSource, false)
		return challenge
	case "Y":
		if field != PreferredAliasField {
			l.UpdateGenericFieldAlias(field, challenge, current)
		} else {
			delete(l.KeyToKey, challenge)
		}
		l.setLineage(key, field, challengeSource, true)
		return current
	case "N":
		if field != PreferredAliasField {
			l.UpdateGenericFieldAlias(field, current, challenge)
		} else {
			delete(l.KeyToKey, current)
		}
		l.setLineage(key, field, challengeSource, false)
		return challenge
	}

	return current
}

func (l *TBibTeXLibrary) MaybeResolveFieldValue(key, challengeKey, field, challenge, current string) string {
	if field == "url" && l.IsRedundantURL(challenge, key) {
		return ""
	}

	if current == "" {
		if challenge != "" {
			challengeSource := sourceFromChallengeKey(challengeKey)
			if challengeSource != "" {
				l.setLineage(key, field, challengeSource, false)
			}
		}
		return challenge
	}

	if challenge == "" {
		// A known-authoritative source asserting empty is allowed to clear the field.
		challengeSource := sourceFromChallengeKey(challengeKey)
		if challengeSource != "" && dblpKnownFields.Contains(field) {
			l.setLineage(key, field, challengeSource, false)
			return ""
		}
		return current
	}

	return l.ResolveFieldValue(key, challengeKey, field, challenge, current)
}
