//
// Module: interaction
//
// This module is concerned with the interaction with the user.
// For the moment, it only involves the reporting of errors and warnings.
// In the future, it will also involve input from the user concerning e.g. resolving ambiguities when importing bibtex files.
//
// Creator: Henderik A. Proper (erikproper@fastmail.com)
//
// Version of: 23.04.2024
//

package main

import "fmt"

type TInteraction struct{}

// Reporting errors.
// The error message should provide the formatting.
func (r *TInteraction) Error(errorMessage string, context ...any) bool {
	fmt.Printf("ERROR:    "+errorMessage+".\n", context...)

	return true
}

// Reporting warnings.
// The warning message should provide the formatting.
func (r *TInteraction) Warning(warning string, context ...any) bool {
	fmt.Printf("WARNING:  "+warning+".\n", context...)

	return true
}

// Reporting progres.
// The progress message should provide the formatting.
func (r *TInteraction) Progress(progress string, context ...any) bool {
	fmt.Printf("PROGRESS: "+progress+".\n", context...)

	return true
}