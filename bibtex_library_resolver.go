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

//import "fmt"

func (l *TBibTeXLibrary) ResolveFileReferences(key, otherKey string) string {
	regularFileReference := FilesFolder + key + ".pdf"
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

// Still a major construction site.
//
// Needs the library as parameter as we need to access interacton from there .. and lookup additional things.
//
// SPLIT like the normalisers ...
// Rethink this name ... "Field", since it may also work on the type ...
func (l *TBibTeXLibrary) ResolveFieldValue(key, challengeKey, field, challengeRaw, currentRaw string) string {
	current := l.MapFieldValue(field, currentRaw)
	challenge := l.MapFieldValue(field, challengeRaw)

	// OK. The key, field, and challenge are needed here. But, current is likely to be derivable from l with key and field.
	// But ... needs to be checked once done with the legacy migration.

	// If the the challenge equals the current one, we can just return the current one.
	if current == challenge {
		return current
	}

	if field == "crossref" {
		if current == challenge {
			return current

		} else {
			if l.WarningYesNoQuestion("Shall I merge the crossreferenced entries as well?", "Different crossrefs (%s, %s) for entries (%s, %s) that you want to merge.", current, challenge, key, challengeKey) {
				return l.MergeEntries(current, challenge)

			} else {
				return current

			}
		}
	}

	if field == "modificationdate" || field == "creationdate" {
		if current < challenge {
			return current

		} else {
			return challenge

		}
	}

	// CHECK the Maybe variation. Need to do this smarter.
	if field == "url" && l.IsRedundantURL(challenge, key) {
		return ""
	}

	// DELETE later
	if field == "groups" {
		return current + ", " + challenge
	}

	if field == LocalURLField {
		return l.ResolveFileReferences(key, challengeKey)
	}

	// So we have a difference between the current value and the challenge.
	// So, who is the target ...

	if l.EntryFieldAliasHasTarget(key, field, current, challenge) {
		// It is recorded that the challenge is the target over the current value.
		// So, we can return the challenge as the target

		return challenge

	} else if l.EntryFieldAliasHasTarget(key, field, challenge, current) {
		// It is recorded that the current value is the target over the challenge
		// So, we can return the current value as the target

		return current

	} else {
		// If no target for an alias is recorded, we need to ask the user ...
		// And update the recorded challenges
		// Note: this is an *update* as we may need to update this as a new target for other challenges as well.

		options := TStringSetNew()
		options.Add("Y", "y", "n", "N")
		warning := "For entry %s and field %s:\n- Challenger: %s\n- Current   : %s\nneeds to be resolved"
		question := "Current entry:\n" + l.EntryString(key, "", "  ") + "Keep the value as is?"
		// Don't like this via "Warning" ... should be a separate class
		answer := l.WarningQuestion(question, options, warning, key, field, challenge, current)

		if answer == "y" {
			if field != PreferredAliasField {
				l.UpdateEntryFieldAlias(key, field, challenge, current)
				l.WriteAliasesFiles()
			} else {
				delete(l.KeyToKey, challenge)
			}

			return current

		} else if answer == "n" {
			if field != PreferredAliasField {
				l.UpdateEntryFieldAlias(key, field, current, challenge)
				l.WriteAliasesFiles()
			} else {
				delete(l.KeyToKey, current)
			}

			return challenge

		} else if answer == "Y" {
			if field != PreferredAliasField {
				l.UpdateGenericFieldAlias(field, challenge, current)
				l.WriteAliasesFiles()
			} else {
				delete(l.KeyToKey, challenge)
			}

			return current

		} else if answer == "N" {
			if field != PreferredAliasField {
				l.UpdateGenericFieldAlias(field, current, challenge)
				l.WriteAliasesFiles()
			} else {
				delete(l.KeyToKey, current)
			}

			return challenge

		}
	}

	return current
}

// If the current value is empty, then we can assign the alias.
func (l *TBibTeXLibrary) MaybeResolveFieldValue(key, challengeKey, field, challenge, current string) string {
	if field == "url" && l.IsRedundantURL(challenge, key) {
		return ""
	}

	if current == "" {
		return challenge
	} else if challenge == "" {
		return current
	} else {
		return l.ResolveFieldValue(key, challengeKey, field, challenge, current)
	}
}
