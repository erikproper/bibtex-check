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

// inDblpUpdate is set while doUpsertDblpEntries is running so that per-name
// differences from DBLP are auto-accepted without prompting the user.
var inDblpUpdate bool

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
	current := l.MapFieldValue(field, l.NormaliseFieldValue(field, currentRaw))
	challenge := l.MapFieldValue(field, l.NormaliseFieldValue(field, challengeRaw))

	if current == challenge {
		return current
	}

	// In subset merge mode the user has explicitly edited this field; skip every
	// auto-resolution path that would silently keep the current value and always
	// present an interactive challenge when the raw texts differ.
	forceInteractive := subsetMergeActive && currentRaw != challengeRaw

	// With contributor_roles active, two author/editor strings are equal when they
	// resolve to the same contributor-ID sequence, even if the name strings differ.
	if !forceInteractive && contributorRolesActive && (field == "author" || field == "editor") {
		if idSeqEqual(
			resolveNamesToIDSeq(l, splitBibNameField(current)),
			resolveNamesToIDSeq(l, splitBibNameField(challenge)),
		) {
			return current
		}
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
	} else if !forceInteractive && l.EntryFieldAliasHasTarget(key, field, challenge, current) {
		return current
	}

	// If the normalised forms are equal, there is no genuine content difference —
	// only a representation difference (e.g. "China" vs "{China}" after country
	// normalisation).  Silently adopt the normalised form without prompting.
	// This check comes after the stored-mapping lookups so explicit user decisions
	// (recorded via UpdateEntryFieldAlias) still take precedence.
	if !forceInteractive {
		if normCurrent := l.MapFieldValue(field, l.NormaliseFieldValue(field, currentRaw)); normCurrent == challenge {
			return normCurrent
		}
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

	if challengePriority < currentPriority && !subsetMergeActive {
		return current
	}

	// If the user has already deliberately diverged from this source for this field
	// (Edited=true in lineage), honour that decision without re-asking.  DBLP's data
	// for this field has not changed from our perspective; showing the same challenge
	// again adds no new information.  The idSeqEqual check above already handles the
	// case where DBLP later corrects its data to match ours.
	if !subsetMergeActive && currentRec.Edited && currentRec.Source == challengeSource && challengeSource != "" {
		return current
	}

	// Equal or higher priority challenger: compare semantic content.
	if !forceInteractive && TeXStringIndexer(current) == TeXStringIndexer(challenge) {
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

	// With contributor_roles active, author/editor challenges go straight to
	// per-name breakdown: the comparison model already identified which positions
	// differ, so presenting the full-string y/n/b question is redundant.
	if contributorRolesActive && (field == "author" || field == "editor") {
		// If the challenge is a recorded loser for this entry+field, honour the prior
		// decision without re-asking. The strict EntryFieldAliasHasTarget check above
		// can miss this when the stored winner drifted due to name normalisation across
		// runs; checking the map key alone is sufficient.
		if !forceInteractive {
			if fieldMap, hasField := l.EntryFieldSourceToTarget[key][field]; hasField {
				if _, isLoser := fieldMap[challenge]; isLoser {
					return current
				}
			}
		}
		result, quit := l.resolveAuthorBreakdown(key, field, challenge, current)
		if quit {
			gracefulQuit()
		}
		if result != "" {
			l.UpdateEntryFieldAlias(key, field, challenge, result)
			l.setLineage(key, field, challengeSource, result != challenge)
			return result
		}
		// result == "": name counts differ and breakdown is impossible.
		// Fall through to the full-field y/n challenge so the user can explicitly
		// accept or reject the differently-sized challenger rather than silently
		// keeping current.
	}

	var currentAuthorNames, challengeAuthorNames []string
	canBreakDown := false
	if field == "author" || field == "editor" {
		currentAuthorNames = splitBibNameField(current)
		challengeAuthorNames = splitBibNameField(challenge)
		nC, nCh := len(currentAuthorNames), len(challengeAuthorNames)
		currentEndsWithOthers := nC > 0 && strings.ToLower(currentAuthorNames[nC-1]) == "others"
		challengeEndsWithOthers := nCh > 0 && strings.ToLower(challengeAuthorNames[nCh-1]) == "others"
		canBreakDown = (nC == nCh && nC > 1) || (currentEndsWithOthers && !challengeEndsWithOthers && nCh >= nC)
	}
	singleAuthor := len(currentAuthorNames) == 1 && len(challengeAuthorNames) == 1

	options := TStringSetNew()
	if field == "author" || field == "editor" {
		if canBreakDown {
			options.Add("y", "n", "b")
		} else if singleAuthor {
			options.Add("y", "n", "e")
		} else {
			options.Add("y", "n")
		}
	} else if field == EntryTypeField || field == "year" || field == "pages" ||
		field == "month" || field == "dblp" || field == "title" || field == "number" || field == "booktitle" {
		options.Add("y", "n")
	} else {
		options.Add("Y", "y", "n", "N")
	}
	warning := "For entry %s and field %s:\n- Challenger: %s\n- Current   : %s\nneeds to be resolved"
	question := "Challenging entry:\n" + l.EntryString(challengeKey, "", "  ")
	question += "Current entry:\n" + l.EntryString(key, "", "  ")
	if canBreakDown {
		question += "Keep the value as is? (b = break down by name)"
	} else if singleAuthor {
		question += "Keep the value as is? (e = edit)"
	} else {
		question += "Keep the value as is?"
	}
	answer := l.WarningQuestion(question, options, warning, key, field, challenge, current)

	switch answer {
	case "y":
		if singleAuthor {
			l.AddNameMapping(currentAuthorNames[0], challengeAuthorNames[0])
		}
		// Record challenge→current in losing_field_values. For PreferredAliasField, do NOT
		// call AddKeyAlias(challenge, key): challenge may already be an alias of another entry
		// and adding it here would produce a spurious "Ambiguous key oldie" warning.
		l.UpdateEntryFieldAlias(key, field, challenge, current)
		l.setLineage(key, field, challengeSource, true)
		return current
	case "n":
		if singleAuthor {
			l.AddNameMapping(challengeAuthorNames[0], currentAuthorNames[0])
		}
		l.UpdateEntryFieldAlias(key, field, current, challenge)
		if field == PreferredAliasField {
			// Demote the old alias to a key oldie only when it is not already claimed
			// by a different entry — otherwise AddKeyAlias would warn "Ambiguous key oldie".
			if ex := l.KeyOldies.Get(current); ex == "" || l.MapEntryKey(ex) == l.MapEntryKey(key) {
				l.AddKeyAlias(current, key)
			}
		}
		l.setLineage(key, field, challengeSource, false)
		return challenge
	case "e":
		edited, _ := l.AskForInput("Enter the resolved value for " + field)
		edited = strings.TrimSpace(edited)
		if edited == "" {
			edited = current
		}
		if singleAuthor {
			l.AddNameMapping(edited, currentAuthorNames[0])
			l.AddNameMapping(edited, challengeAuthorNames[0])
		}
		l.UpdateEntryFieldAlias(key, field, challenge, edited)
		l.UpdateEntryFieldAlias(key, field, current, edited)
		l.setLineage(key, field, challengeSource, true)
		return edited
	case "b":
		result, quit := l.resolveAuthorBreakdown(key, field, challenge, current)
		if quit {
			gracefulQuit()
		}
		if result != "" {
			l.UpdateEntryFieldAlias(key, field, challenge, result)
			l.setLineage(key, field, challengeSource, result != challenge)
			return result
		}
		// Breakdown was not possible — fall through to a plain y/n re-ask.
		ynOptions := TStringSetNew()
		ynOptions.Add("y", "n")
		answer = l.WarningQuestion(question, ynOptions, warning, key, field, challenge, current)
		if answer == "n" {
			l.UpdateEntryFieldAlias(key, field, current, challenge)
			l.setLineage(key, field, challengeSource, false)
			return challenge
		}
		l.UpdateEntryFieldAlias(key, field, challenge, current)
		l.setLineage(key, field, challengeSource, true)
		return current
	case "Y":
		l.UpdateGenericFieldAlias(field, challenge, current)
		l.setLineage(key, field, challengeSource, true)
		return current
	case "N":
		l.UpdateGenericFieldAlias(field, current, challenge)
		if field == PreferredAliasField {
			if ex := l.KeyOldies.Get(current); ex == "" || l.MapEntryKey(ex) == l.MapEntryKey(key) {
				l.AddKeyAlias(current, key)
			}
		}
		l.setLineage(key, field, challengeSource, false)
		return challenge
	}

	return current
}

// contributorORCID returns the ORCID for name. Checks the in-memory contributor table
// first (O(1)); on a file-store hit the result is written back to both in-memory and
// the DB so subsequent lookups for the same contributor are free.
func (l *TBibTeXLibrary) contributorORCID(name string) string {
	if id, ok := l.NameToContributorID[name]; ok {
		if c := l.ContributorByID[id]; c != nil {
			if c.ORCID != "" {
				return c.ORCID
			}
			orcid := resolveNameToORCID(name)
			if orcid != "" {
				c.ORCID = orcid
				upsertContributorORCIDToDB(id, orcid, true)
			}
			return orcid
		}
	}
	return resolveNameToORCID(name)
}

// resolveNamePair interactively resolves a single differing name position in an
// author/editor field. winnerName is the preferred/current form; loserName is the
// challenger/incoming form. namePos and nameTotal give position context for display.
//
// Returns:
//   resultName — canonical name to use at this position (winnerName when quit or skipped)
//   quit       — user pressed q; caller should abort its enclosing loop
//   mapped     — a name mapping was recorded (triage may retire the losing_field_values row;
//                breakdown uses the new resultName)
//
// The "n" (non-double) case records (winnerName, loserName) in non_double_contributor_names,
// and also in non_double_contributors when contributor IDs are available for both names.
// It returns (winnerName, false, false): no mapping, no quit, keep the winner name.
func (l *TBibTeXLibrary) resolveNamePair(key, field string, namePos, nameTotal, diffIdx, diffTotal int, winnerName, loserName string) (resultName string, quit bool, mapped bool) {
	winnerORCID := l.contributorORCID(winnerName)
	loserORCID := l.contributorORCID(loserName)

	// When both ORCIDs are known, auto-resolve without prompting.
	if winnerORCID != "" && loserORCID != "" {
		if winnerORCID == loserORCID {
			l.orcidAutoResolveSameCount++
			l.AddNameMapping(winnerName, loserName)
			return winnerName, false, true
		}
		l.orcidAutoResolveDiffCount++
		return loserName, false, false
	}

	// During DBLP update the loser is the DBLP-authoritative name form.
	// Accept it and record the canonical mapping so future runs skip the question.
	if inDblpUpdate {
		l.Progress("Auto-accepted DBLP name form %q for %s (mapping from %q)", loserName, key, winnerName)
		l.AddNameMapping(loserName, winnerName)
		return loserName, false, true
	}

	winnerDisplay := winnerName
	if winnerORCID != "" {
		winnerDisplay += " [" + winnerORCID + "]"
	}
	loserDisplay := loserName
	if loserORCID != "" {
		loserDisplay += " [" + loserORCID + "]"
	}

	options := TStringSetNew()
	options.Add("w", "l", "e", "c", "n", "q")
	answer := l.WarningQuestion(
		"Map to winner-canonical (w), loser-canonical (l), edit canonical (e), change to loser (c), non-double (n), quit (q)?",
		options,
		"Name %d of %d (difference %d of %d) for entry %s field %s:\n  Winner: %s\n  Loser:  %s",
		namePos, nameTotal, diffIdx, diffTotal, key, field, winnerDisplay, loserDisplay)
	switch answer {
	case "w":
		l.AddNameMapping(winnerName, loserName)
		return winnerName, false, true
	case "l":
		l.AddNameMapping(loserName, winnerName)
		return loserName, false, true
	case "e":
		canonical, err := l.AskForInput("Enter canonical name")
		if err == nil && canonical != "" {
			l.AddNameMapping(canonical, winnerName)
			l.AddNameMapping(canonical, loserName)
			return canonical, false, true
		}
		return winnerName, false, false
	case "c":
		// Different people: accept loser name for this entry at this position
		// without creating any name mapping between the two forms.
		return loserName, false, false
	case "n":
		addNonDoubleContributorNamePair(l, winnerName, loserName)
		if idA, okA := l.NameToContributorID[winnerName]; okA {
			if idB, okB := l.NameToContributorID[loserName]; okB {
				addNonDoubleContributorPair(idA, idB)
			}
		}
		return winnerName, false, false
	case "q":
		return winnerName, true, false
	}
	return winnerName, false, false
}

// resolveAuthorBreakdown interactively resolves per-name differences in an author/editor
// challenge using resolveNamePair for each differing position. Returns the resolved author
// string (or "" when breakdown is impossible due to different name counts), and a quit bool
// that is true when the user pressed q — the caller must not record any alias in that case.
func (l *TBibTeXLibrary) resolveAuthorBreakdown(key, field, challenge, current string) (string, bool) {
	challengeNames := splitBibNameField(challenge)
	currentNames := splitBibNameField(current)
	nCurrent := len(currentNames)
	nChallenge := len(challengeNames)

	currentEndsWithOthers := nCurrent > 0 && strings.ToLower(currentNames[nCurrent-1]) == "others"
	challengeEndsWithOthers := nChallenge > 0 && strings.ToLower(challengeNames[nChallenge-1]) == "others"
	othersExpansion := currentEndsWithOthers && !challengeEndsWithOthers && nChallenge >= nCurrent

	if nChallenge != nCurrent && !othersExpansion {
		l.Warning("Cannot break down %s %s by name: challenger has %d name(s), current has %d — presenting full-field challenge.",
			key, field, nChallenge, nCurrent)
		return "", false
	}

	// When expanding "others", only compare the concrete prefix (positions before "others").
	prefixLen := nCurrent
	if othersExpansion {
		prefixLen = nCurrent - 1
	}

	var diffPositions []int
	for i := 0; i < prefixLen; i++ {
		same := challengeNames[i] == currentNames[i]
		if !same && contributorRolesActive {
			cID, cOK := resolveNameToContributorID(l, currentNames[i])
			chID, chOK := resolveNameToContributorID(l, challengeNames[i])
			same = cOK && chOK && cID == chID
		}
		if !same {
			diffPositions = append(diffPositions, i)
		}
	}
	if len(diffPositions) == 0 && !othersExpansion {
		return current, false
	}

	resultNames := make([]string, prefixLen)
	copy(resultNames, currentNames[:prefixLen])

	for diffIdx, i := range diffPositions {
		resultName, quit, _ := l.resolveNamePair(key, field, i+1, nChallenge, diffIdx+1, len(diffPositions), currentNames[i], challengeNames[i])
		resultNames[i] = resultName
		if quit {
			return "", true
		}
	}

	if othersExpansion {
		tail := strings.Join(challengeNames[prefixLen:], " and ")
		if l.WarningYesNoQuestion(
			"Replace 'others' with: "+tail,
			"For entry %s field %s — challenger provides %d additional name(s): %s",
			key, field, nChallenge-prefixLen, tail) {
			resultNames = append(resultNames, challengeNames[prefixLen:]...)
		} else {
			resultNames = append(resultNames, "others")
		}
	}

	return strings.Join(resultNames, " and "), false
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
		// An absent field in the challenger means "unknown" — it carries no evidence
		// that the current value is wrong. Silently preserve the current value.
		return current
	}

	return l.ResolveFieldValue(key, challengeKey, field, challenge, current)
}
