package main

import "fmt"

// This is a dummy function for now.
// In the future, this will be crucial when dealing with the integration of double entries and legacy files in particular.
// Needs the library as parameter as we need to access interacton from there ...
func (l *TBibTeXLibrary) ResolveFieldValue(field, challenger, current string) string {
	if current != challenger {
		fmt.Println("Need to resolve for entry", l.currentKey, "for the field", field, "the challenger", challenger, "to present value", current)
	}

	return current
}
