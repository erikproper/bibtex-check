package main

import "fmt"

// This is a dummy function for now.
// In the future, this will be crucial when dealing with the integration of double entries and legacy files in particular.
// Needs the library as parameter as we need to access interacton from there ...
func (l *TBibTeXLibrary) ResolveFieldValue(key, field, challenger, current string) string {
	// OK. The key, field, and challenger are needed here. But, current is likely to be derivable from l with key and field.
	// But ... needs to be checked once done with the legacy migration.
	if current != challenger && !l.legacyMode {
		if l.checkChallengeWinner(key, field, current, challenger) {
			return challenger
		} else if !l.checkChallengeWinner(key, field, challenger, current) {
			fmt.Println("WORK:", key)

			warning := "Need to resolve for entry %s and field %s:\n- Challenger: %s\n- Current   : %s"
			question := "Current entry:\n" + l.entryString(key) + "Keep the value as is?"

			if l.WarningBoolQuestion(question, warning, key, field, challenger, current) {
				l.updateChallengeWinner(key, field, challenger, current)
				l.WriteChallenges()

				return current
			} else {
				l.updateChallengeWinner(key, field, current, challenger)
				l.WriteChallenges()

				return challenger
			}
		}
	}

	return current
}
