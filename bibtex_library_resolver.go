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

// Still a major construction site.
//
// Needs the library as parameter as we need to access interacton from there .. and lookup additional things.
func (l *TBibTeXLibrary) MaybeResolveFieldValue(key, field, challenger, current string) string {
	// OK. The key, field, and challenger are needed here. But, current is likely to be derivable from l with key and field.
	// But ... needs to be checked once done with the legacy migration.

	// If the current one is empty, then the challenger should succeed.
	if current == "" || current == challenger {
		return challenger
	}

	if !l.legacyMode {
		// So we have a difference between a non-empty current value and the challenger.
		// So, who is the winner ...

		if l.CheckChallengeWinner(key, field, current, challenger) {
			// It is recorded that the challenger is the winner over the current value.
			// So, we can return the challenger as the winner
			return challenger

		} else if l.CheckChallengeWinner(key, field, challenger, current) {
			// It is recorded that the current value is the winner over the challenger
			// So, we can return the current value as the winner

			return current
		} else {
			// If no winner is recorded, we need to ask the user ...
			// And update the recorded challenges
			// Note: this is an *update* as we may need to update this as a new winner for other challenges as well.

			warning := "Need to resolve for entry %s and field %s:\n- Challenger: %s\n- Current   : %s"
			question := "XXXCurrent entry:\n" + l.EntryString(key, "  ") + "Keep the value as is?"

			if l.WarningBoolQuestion(question, warning, key, field, challenger, current) {
				l.UpdateChallengeWinner(key, field, challenger, current)
				l.WriteChallenges()

				return current
			} else {
				l.UpdateChallengeWinner(key, field, current, challenger)
				l.WriteChallenges()

				return challenger
			}
		}
	}

	return current
}
