/*
 *
 * Module: interaction
 *
 * This module is concerned with the interaction with the user.
 * For the moment, it only involves the reporting of errors and warnings.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 23.04.2024
 *
 */

package main

import (
	"bufio"
	"fmt"
	"os"
)

type TInteraction struct {
	silenced bool
}

// Disable any output to standard out.
func (r *TInteraction) SetInteractionOff() {
	r.silenced = true
}

// Enable any output to standard out.
func (r *TInteraction) SetInteractionOn() {
	r.silenced = false
}

// Status of interaction.
func (r *TInteraction) InteractionIsOff() bool {
	return r.silenced
}

// Set the interaction status to the specified state
func (r *TInteraction) SetInteraction(status bool) {
	r.silenced = status
}

// Reporting errors.
// The error message should provide the formatting.
func (r *TInteraction) Error(errorMessage string, context ...any) bool {
	if !r.silenced {
		fmt.Printf("ERROR:    "+errorMessage+"\n", context...)
	}

	return true
}

// Reporting warnings.
// The warning message should provide the formatting.
func (r *TInteraction) Warning(warning string, context ...any) bool {
	if !r.silenced {
		fmt.Printf("WARNING:  "+warning+"\n", context...)
	}

	return true
}

// Reporting warnings that involve a choice on how to proceed.
// The warning message should provide the formatting.
func (r *TInteraction) WarningQuestion(question string, options TStringSet, warning string, context ...any) string {
	r.Warning(warning, context...)
	optionSet := "("
	separator := ""
	for _, option := range options.ElementsSorted() {
		optionSet += separator + option
		separator = "/"
	}
	optionSet += "): "

	fmt.Printf("QUESTION: " + question + " " + optionSet)

	reader := bufio.NewReader(os.Stdin)
	validOption := false
	for {
		option, _ := reader.ReadString('\n')
		option = option[:len(option)-1]

		validOption = options.Contains(option)

		if validOption {
			return option
		}

		fmt.Printf(optionSet)
	}
}

// Reporting warnings that involve a choice on how to proceed.
// The warning message should provide the formatting.
func (r *TInteraction) WarningYesNoQuestion(question, warning string, context ...any) bool {
	options := TStringSetNew()
	options.Add("y", "n")

	answer := r.WarningQuestion(question, options, warning, context...)

	return answer == "y"
}

// Reporting progres.
// The progress message should provide the formatting.
func (r *TInteraction) Progress(progress string, context ...any) bool {
	if !r.silenced {
		fmt.Printf("PROGRESS: "+progress+"\n", context...)
	}
	return true
}
