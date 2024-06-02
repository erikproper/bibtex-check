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

// Still a major construction site.
//
// Needs the library as parameter as we need to access interacton from there .. and lookup additional things.
func (l *TBibTeXLibrary) ResolveFieldValue(key, field, alias, current string) string {
	// OK. The key, field, and alias are needed here. But, current is likely to be derivable from l with key and field.
	// But ... needs to be checked once done with the legacy migration.

	// If the the alias equals the current one, we can just return the current one.
	if current == alias {
		return current
	}

	if !l.legacyMode {
		// So we have a difference between a non-empty current value and the alias.
		// So, who is the target ...

		if l.EntryFieldAliasHasTarget(key, field, current, alias) {
			// It is recorded that the alias is the target over the current value.
			// So, we can return the alias as the target
			return alias

		} else if l.EntryFieldAliasHasTarget(key, field, alias, current) {
			// It is recorded that the current value is the target over the alias
			// So, we can return the current value as the target

			return current
		} else {
			// If no target is recorded, we need to ask the user ...
			// And update the recorded challenges
			// Note: this is an *update* as we may need to update this as a new target for other challenges as well.

			options := TStringSetNew()
			options.Add("Y", "y", "n", "N")
			warning := "For entry %s and field %s:\n- Challenger: %s\n- Current   : %s\nneeds to be resolved"
			question := "Current entry:\n" + l.EntryString(key, "  ") + "Keep the value as is?"
			// Don't like this via "Warning" ... should be a separate class
			answer := l.WarningQuestion(question, options, warning, key, field, alias, current)

			if answer == "y" {
				l.UpdateEntryFieldAlias(key, field, alias, current)
				l.WriteAliasesFiles()
				l.WriteBibTeXFile()

				return current

			} else if answer == "n" {
				l.UpdateEntryFieldAlias(key, field, current, alias)
				l.WriteAliasesFiles()
				l.WriteBibTeXFile()

				return alias

			} else if answer == "Y" {
				l.UpdateGenericFieldAlias(field, alias, current)
				l.WriteAliasesFiles()
				l.WriteBibTeXFile()

				return current

			} else if answer == "N" {
				l.UpdateGenericFieldAlias(field, current, alias)
				l.WriteAliasesFiles()
				l.WriteBibTeXFile()

				return alias

			}
		}
	}

	return current
}

// If the current value is empty, then we can assign the alias.
func (l *TBibTeXLibrary) MaybeResolveFieldValue(key, field, alias, current string) string {
	if current == "" {
		return alias
	} else {
		return l.ResolveFieldValue(key, field, alias, current)
	}
}
