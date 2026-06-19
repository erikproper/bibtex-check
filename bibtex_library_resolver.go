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

import "strings"

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
		if current == "" {
			return challenge
		}
		if challenge == "" {
			return current
		}
		sourceEntry := l.EntryString(current, "", "  ")
		targetEntry := l.EntryString(challenge, "", "  ")
		if l.WarningYesNoQuestion("Shall I merge the crossreferenced entries as well?",
			"Different crossrefs (%s, %s) for entries (%s, %s) that you want to merge.\nFirst entry:\n%s\nSecond entry:\n%s",
			current, challenge, key, challengeKey, sourceEntry, targetEntry) {
			return l.MergeEntries(challenge, current)
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

	// DOIs that differ only in letter case are the same DOI — resolve without prompting.
	// Prefer the DBLP-sourced form; if neither is from DBLP, keep whichever has uppercase
	// letters (the publisher's original form over a lowercased normalisation).
	if field == "doi" && strings.EqualFold(current, challenge) {
		challengeSource := sourceFromChallengeKey(challengeKey)
		currentRec := l.getLineage(key, field)
		if currentRec.Source == "dblp" {
			return current
		}
		if challengeSource == "dblp" {
			l.setLineage(key, field, "dblp", false)
			return challenge
		}
		if strings.ToLower(current) == current {
			return challenge // current is all-lowercase; challenge has uppercase
		}
		return current
	}

	// For fields that must not be empty for this entry type: a non-empty challenger
	// always wins against an empty current value — before stored-mapping lookups so
	// stale "challenge → empty" mappings cannot suppress the fix.
	if current == "" && FieldIsRequiredForEntry(l.EntryType(key), field) {
		return challenge
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

	// If the challenging preferred alias already resolves to this entry via key_oldies,
	// the resolution was already recorded in a prior run — silently keep the current value.
	if field == PreferredAliasField && l.MapEntryKey(challenge) == l.MapEntryKey(key) {
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
			l.AddKeyAlias(challenge, key)
		}
		l.setLineage(key, field, challengeSource, true)
		return current
	case "n":
		if field != PreferredAliasField {
			l.UpdateEntryFieldAlias(key, field, current, challenge)
		} else {
			l.AddKeyAlias(current, key)
		}
		l.setLineage(key, field, challengeSource, false)
		return challenge
	case "Y":
		if field != PreferredAliasField {
			l.UpdateGenericFieldAlias(field, challenge, current)
		} else {
			l.AddKeyAlias(challenge, key)
		}
		l.setLineage(key, field, challengeSource, true)
		return current
	case "N":
		if field != PreferredAliasField {
			l.UpdateGenericFieldAlias(field, current, challenge)
		} else {
			l.AddKeyAlias(current, key)
		}
		l.setLineage(key, field, challengeSource, false)
		return challenge
	}

	return current
}

// TODO (code cleanup): revisit lineage tracking across all MaybeResolveFieldValue / ResolveFieldValue paths —
// e.g. what lineage is set when a library-to-library merge fills an empty field and the user accepts.
func (l *TBibTeXLibrary) MaybeResolveFieldValue(key, challengeKey, field, challenge, current string) string {
	if field == "url" && l.IsRedundantURL(challenge, key) {
		return ""
	}

	if current == "" {
		if challenge == "" {
			return ""
		}
		challengeSource := sourceFromChallengeKey(challengeKey)
		if challengeSource != "" {
			// Known-authoritative external source filling an empty field: accept silently.
			l.setLineage(key, field, challengeSource, false)
			return challenge
		}
		// Library-to-library merge adding a value to an empty field: ask.
		return l.ResolveFieldValue(key, challengeKey, field, challenge, current)
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
