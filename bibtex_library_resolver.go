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
func (l *TBibTeXLibrary) ResolveFieldValue(key, field, challenger, current string) string {
	// OK. The key, field, and challenger are needed here. But, current is likely to be derivable from l with key and field.
	// But ... needs to be checked once done with the legacy migration.

	// If the the challenger equals the current one, we can just return the current one.
	if current == challenger {
		return current
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

			//			fmt.Println("KCU", current, "KK")
			//			fmt.Println("KCH", challenger, "KK")
			//			fmt.Println("KWI", Library.ChallengeWinners.GetValueityFromStringTripleMap(key, field, challenger), "KK")
			//			fmt.Println("KNO", NormaliseTitleString(&Library, Library.ChallengeWinners.GetValueityFromStringTripleMap(key, field, challenger)), "KK")

			warning := "For entry %s and field %s:\n- Challenger: %s\n- Current   : %s\nneeds to be resolved"
			question := "Current entry:\n" + l.EntryString(key, "  ") + "Keep the value as is?"

			if l.WarningBoolQuestion(question, warning, key, field, challenger, current) {
				l.UpdateChallengeWinner(key, field, challenger, current)
				l.WriteChallenges()
				l.WriteBibTeXFile()

				return current
			} else {
				l.UpdateChallengeWinner(key, field, current, challenger)
				l.WriteChallenges()
				l.WriteBibTeXFile()

				return challenger
			}
		}
	}

	return current
}

// If the current value is empty, then we can assign the challenger.
func (l *TBibTeXLibrary) MaybeResolveFieldValue(key, field, challenger, current string) string {
	if current == "" {
		return challenger
	} else {
		return l.ResolveFieldValue(key, field, challenger, current)
	}
}
