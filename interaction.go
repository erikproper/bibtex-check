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
	"strings"
)

// TTermSpinner renders an in-place spinner + progress counter on stderr.
// A nil receiver is safe on all methods, so callers can do:
//
//	spinner := r.NewSpinner(label)
//	defer spinner.Stop()
//
// and the whole thing is a no-op when interaction is silenced.
type TTermSpinner struct {
	label string
	frame int
}

var spinnerChars = []string{"|", "/", "-", "\\"}
var activeSpinner *TTermSpinner

// SpinnerInterrupt clears the current spinner line so that a warning/error/progress
// message can print cleanly on its own line. The spinner is not stopped; the next
// Update call will redraw it.
func SpinnerInterrupt() {
	if activeSpinner != nil {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

// NewSpinner creates and activates a spinner with the given label.
// Returns nil (no-op) when the interaction is silenced.
func (r *TInteraction) NewSpinner(label string) *TTermSpinner {
	if r.silenced {
		return nil
	}
	s := &TTermSpinner{label: label}
	activeSpinner = s
	return s
}

// Update redraws the spinner in place showing done/total progress.
func (s *TTermSpinner) Update(done, total int) {
	if s == nil {
		return
	}
	pct := 0.0
	if total > 0 {
		pct = float64(done) * 100.0 / float64(total)
	}
	s.frame = (s.frame + 1) % len(spinnerChars)
	fmt.Fprintf(os.Stderr, "\r%s %s %d/%d (%.0f%%)", spinnerChars[s.frame], s.label, done, total, pct)
}

// Tick advances the spinner one frame without any progress counts. Use this
// when the total work is unknown — e.g. while waiting for a background task.
func (s *TTermSpinner) Tick() {
	if s == nil {
		return
	}
	s.frame = (s.frame + 1) % len(spinnerChars)
	fmt.Fprintf(os.Stderr, "\r%s %s...", spinnerChars[s.frame], s.label)
}

// TickCount advances the spinner one frame and shows a running entry count.
func (s *TTermSpinner) TickCount(n int) {
	if s == nil {
		return
	}
	s.frame = (s.frame + 1) % len(spinnerChars)
	fmt.Fprintf(os.Stderr, "\r%s %s... (%d entries)", spinnerChars[s.frame], s.label, n)
}

// Stop prints a "done" completion line and deactivates the global spinner.
func (s *TTermSpinner) Stop() {
	if s == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "\r\033[KPROGRESS: %s - done\n", s.label)
	activeSpinner = nil
}

type TInteraction struct {
	silenced          bool
	questionWasAsked  bool
	outputWasProduced bool
	stepMode          bool
	stepSize          int
	quitRequested     bool // set when user answers "q" to AskContinueOrQuit
	questionsAnswered int  // total questions answered; snapshotted before/after runHarvestEntry
}

// QuitWasRequested reports whether the user asked to stop in any AskContinueOrQuit prompt.
func (r *TInteraction) QuitWasRequested() bool {
	return r.quitRequested
}

// QuestionsAnswered returns the running total of user answers (WarningQuestion + ConfirmAction).
func (r *TInteraction) QuestionsAnswered() int {
	return r.questionsAnswered
}

// ResetQuestionFlag clears the per-entry trackers before processing each entry
// in a "for all" loop.
func (r *TInteraction) ResetQuestionFlag() {
	r.questionWasAsked = false
	r.outputWasProduced = false
}

// QuestionWasAsked reports whether any WarningQuestion was issued since the
// last ResetQuestionFlag call.
func (r *TInteraction) QuestionWasAsked() bool {
	return r.questionWasAsked
}

// OutputWasProduced reports whether any Progress/Warning/Error was emitted
// since the last ResetQuestionFlag call.
func (r *TInteraction) OutputWasProduced() bool {
	return r.outputWasProduced
}

// AskForInput prints prompt and returns the trimmed line the user types.
// The second return value is non-nil when stdin is closed (EOF or read error).
// Does not set questionWasAsked (setup prompts, not entry-processing questions).
func (r *TInteraction) AskForInput(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "INPUT:    %s: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

// SetStepMode enables or disables per-entry stepping (size 1).
func (r *TInteraction) SetStepMode(on bool) {
	if on {
		r.stepSize = 1
	} else {
		r.stepSize = 0
	}
	r.stepMode = r.stepSize > 0
}

// SetStepSize sets the batch size for step mode (0 = off, 1 = per-entry, N = every N entries).
func (r *TInteraction) SetStepSize(n int) {
	r.stepSize = n
	r.stepMode = n > 0
}

// StepMode reports whether step mode is currently enabled.
func (r *TInteraction) StepMode() bool {
	return r.stepMode
}

// StepSize returns the current step batch size (0 = off).
func (r *TInteraction) StepSize() int {
	return r.stepSize
}

// PressEnterToContinue pauses and waits for the user to press Enter,
// but only when step mode is on and something was actually printed for this entry.
func (r *TInteraction) PressEnterToContinue() {
	if r.silenced || !r.stepMode || !r.outputWasProduced {
		return
	}
	fmt.Fprint(os.Stderr, "--- Press Enter to continue ---")
	reader := bufio.NewReader(os.Stdin)
	reader.ReadString('\n')
}

// PressBatchEnterToContinue unconditionally pauses for Enter; used after
// every N entries in batch step mode (stepSize > 1).
func (r *TInteraction) PressBatchEnterToContinue() {
	if r.silenced {
		return
	}
	fmt.Fprint(os.Stderr, "--- Press Enter to continue ---")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// AskContinueOrQuit asks the user whether to continue or quit after an entry
// that required interaction. Returns true if the user chose to quit.
// When interaction is silenced, always continues.
func (r *TInteraction) AskContinueOrQuit() bool {
	if r.silenced {
		return false
	}
	fmt.Fprint(os.Stderr, "QUESTION: Continue with next entry, or quit? (c/q): ")
	reader := bufio.NewReader(os.Stdin)
	for {
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer == "c" || answer == "q" {
			if answer == "q" {
				r.quitRequested = true
			}
			return answer == "q"
		}
		fmt.Fprint(os.Stderr, "(c/q): ")
	}
}

// ConfirmAction always prompts the user for y/n confirmation, even when the
// interaction is silenced. Use for safety gates that must not be skipped
// by batch-mode callers.
func (r *TInteraction) ConfirmAction(prompt string) bool {
	r.questionsAnswered++
	fmt.Fprintf(os.Stderr, "CONFIRM:  %s (y/n): ", prompt)
	reader := bufio.NewReader(os.Stdin)
	for {
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(answer)
		if answer == "y" || answer == "n" {
			return answer == "y"
		}
		fmt.Fprint(os.Stderr, "(y/n): ")
	}
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
		SpinnerInterrupt()
		r.outputWasProduced = true
		fmt.Fprintf(os.Stderr, "ERROR:    "+errorMessage+"\n", context...)
	}

	return true
}

// Reporting warnings.
// The warning message should provide the formatting.
func (r *TInteraction) Warning(warning string, context ...any) bool {
	if !r.silenced {
		SpinnerInterrupt()
		r.outputWasProduced = true
		fmt.Fprintf(os.Stderr, "WARNING: "+warning+"\n", context...)
	}

	return true
}

// Reporting warnings that involve a choice on how to proceed.
// The warning message should provide the formatting.
func (r *TInteraction) WarningQuestion(question string, options TStringSet, warning string, context ...any) string {
	r.questionWasAsked = true
	r.questionsAnswered++
	if warning != "" {
		r.Warning(warning, context...)
	}

	optionSet := "("
	separator := ""
	for _, option := range options.ElementsSorted() {
		optionSet += separator + option
		separator = "/"
	}
	optionSet += "): "

	fmt.Fprint(os.Stderr, "QUESTION: "+question+" "+optionSet)

	reader := bufio.NewReader(os.Stdin)
	validOption := false
	for {
		option, _ := reader.ReadString('\n')
		option = option[:len(option)-1]

		validOption = options.Contains(option)

		if validOption {
			return option
		}

		fmt.Fprint(os.Stderr, optionSet)
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
		SpinnerInterrupt()
		fmt.Fprintf(os.Stderr, "PROGRESS: "+progress+"\n", context...)
	}
	return true
}
