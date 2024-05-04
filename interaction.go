//
// Module: interaction
//
// This module is concerned with the interaction with the user.
// For the moment, it only involves the reporting of errors and warnings.
// In the future, it will also involve input from the user concerning e.g. resolving ambiguities when importing bibtex files.
//
// Creator: Henderik A. Proper (erikproper@gmail.com)
//
// Version of: 23.04.2024
//

package main

import (
	"bufio"
	"fmt"
	"os"
)

type TInteraction struct {
	silenced bool
}

// Surpress any output to standard out.
func (r *TInteraction) Silenced() {
	r.silenced = true
}

// Reporting errors.
// The error message should provide the formatting.
func (r *TInteraction) Error(errorMessage string, context ...any) bool {
	if !r.silenced {
		fmt.Printf("ERROR:    "+errorMessage+".\n", context...)
	}

	return true
}

// Reporting warnings.
// The warning message should provide the formatting.
func (r *TInteraction) Warning(warning string, context ...any) bool {
	if !r.silenced {
		fmt.Printf("WARNING:  "+warning+".\n", context...)
	}

	return true
}

// Reporting warnings that involve a choice on how to proceed.
// The warning message should provide the formatting.
func (r *TInteraction) WarningBoolQuestion(question, warning string, context ...any) bool {
	r.Warning(warning, context...)
	fmt.Printf("QUESTION: " + question + " (y/n): ")

	reader := bufio.NewReader(os.Stdin)
	char, _, _ := reader.ReadRune()

	return char == 'y'
}

// Reporting progres.
// The progress message should provide the formatting.
func (r *TInteraction) Progress(progress string, context ...any) bool {
	if !r.silenced {
		fmt.Printf("PROGRESS: "+progress+".\n", context...)
	}
	return true
}
