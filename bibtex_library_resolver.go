package main

import "fmt"

// This is a dummy function for now.
// In the future, this will be crucial when dealing with the integration of double entries and legacy files in particular.
// We should turn this into a function with the library as parameter
func ResolveFieldValue(key, field, challenger, current string) string {
	if current != challenger {
		fmt.Println("Need to resolve for entry", key, "for the field", field, "the challenger", challenger, "to present value", current)
	}

	return current
}
