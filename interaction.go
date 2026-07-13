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

// stdinCh is the single source of stdin lines for all interactive prompts.
// One pump goroutine reads os.Stdin here; every consumer (WarningQuestion,
// AskContinueOrQuit, DBLP scan, etc.) pulls from this channel so there is
// never more than one reader competing for the same input byte.
var stdinCh = make(chan string, 1)

func init() {
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			stdinCh <- scanner.Text()
		}
		close(stdinCh)
	}()
}

// readStdinLine blocks until the user types a line and returns it trimmed of
// whitespace (including any stray \r). Returns "" when stdin is closed.
func readStdinLine() string {
	line, ok := <-stdinCh
	if !ok {
		return ""
	}
	return strings.TrimSpace(line)
}

// TProgressTicker renders an in-place progress counter on stderr.
// A nil receiver is safe on all methods.
// Create with (r *TInteraction).NewProgressTicker.
type TProgressTicker struct {
	label       string
	total       int // 0 = indeterminate
	current     int
	totalWidth  int // digits in total; 0 when indeterminate
	rendered    bool
	interaction *TInteraction
}

var activeTicker *TProgressTicker

// SpinnerInterrupt clears the current ticker line so a warning/error/progress
// message can print cleanly on its own line. The ticker is not stopped; the next
// Step/Tick call redraws it.
func SpinnerInterrupt() {
	if activeTicker != nil {
		fmt.Fprint(os.Stderr, "\r\033[K")
		activeTicker.rendered = false
	}
}

// SpinnerCommit makes the current ticker line permanently visible by ending it
// with a newline. Use immediately before a multi-line interactive dialog so the
// user can see the progress count above the dialog.
func SpinnerCommit() {
	if activeTicker != nil && activeTicker.rendered {
		fmt.Fprint(os.Stderr, "\n")
		activeTicker.rendered = false
	}
}

// NewProgressTicker creates and activates a ticker with the given label and total.
// Pass total=0 for indeterminate progress (running count without a percentage).
// Returns nil (no-op) when interaction is silenced or progress is suppressed.
func (r *TInteraction) NewProgressTicker(label string, total int) *TProgressTicker {
	if r.silenced || r.progressSuppressed {
		return nil
	}
	w := 0
	if total > 0 {
		w = len(fmt.Sprintf("%d", total))
	}
	t := &TProgressTicker{
		label:       label,
		total:       total,
		totalWidth:  w,
		interaction: r,
	}
	activeTicker = t
	return t
}

// render redraws the progress line in place via \r.
// Determined mode:   label:  N/Total (XX%)
// Indeterminate:     label: N
// Indeterminate/idle: label: ...
func (t *TProgressTicker) render() {
	if t == nil {
		return
	}
	if t.total > 0 {
		pct := float64(t.current) * 100.0 / float64(t.total)
		fmt.Fprintf(os.Stderr, "\r%s:  %*d/%d (%.0f%%)",
			t.label, t.totalWidth, t.current, t.total, pct)
	} else if t.current > 0 {
		fmt.Fprintf(os.Stderr, "\r%s: %d", t.label, t.current)
	} else {
		fmt.Fprintf(os.Stderr, "\r%s: ...", t.label)
	}
	t.rendered = true
}

// Step increments the count by one, redraws the line, and non-blockingly checks
// whether the user typed 'q' to request a quit.
func (t *TProgressTicker) Step() {
	if t == nil {
		return
	}
	t.current++
	select {
	case line, ok := <-stdinCh:
		if ok && strings.TrimSpace(line) == "q" && t.interaction != nil {
			t.interaction.quitRequested = true
		}
	default:
	}
	t.render()
}

// SetCount sets the count to n (without incrementing) and redraws.
// Use when the count comes from an external source such as an atomic counter.
func (t *TProgressTicker) SetCount(n int) {
	if t == nil {
		return
	}
	t.current = n
	t.render()
}

// Tick redraws without incrementing. Use in time-based polling loops where no
// per-iteration count is available (e.g. waiting for an async background task).
func (t *TProgressTicker) Tick() {
	if t == nil {
		return
	}
	t.render()
}

// Done prints "label: done", advances to a new line, and deactivates the ticker.
func (t *TProgressTicker) Done() {
	if t == nil {
		return
	}
	if t.rendered {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
	fmt.Fprintf(os.Stderr, "%s: done\n", t.label)
	t.rendered = false
	activeTicker = nil
}

type TInteraction struct {
	silenced           bool
	progressSuppressed bool // suppress Progress() output without silencing warnings/questions
	questionWasAsked   bool
	outputWasProduced  bool
	stepMode           bool
	stepSize           int
	stepCounter        int  // running count of questions answered in the current step batch
	quitRequested      bool // set when user answers "q" to AskContinueOrQuit
	questionsAnswered  int  // total questions answered; snapshotted before/after runHarvestEntry
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
	return readStdinLine(), nil
}

// SetStepMode enables or disables per-entry stepping (size 1).
func (r *TInteraction) SetStepMode(on bool) {
	if on {
		r.stepSize = 1
	} else {
		r.stepSize = 0
	}
	r.stepMode = r.stepSize > 0
	r.stepCounter = 0
}

// SetStepSize sets the batch size for step mode (0 = off, 1 = per-entry, N = every N entries).
func (r *TInteraction) SetStepSize(n int) {
	r.stepSize = n
	r.stepMode = n > 0
	r.stepCounter = 0
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
	readStdinLine()
}

// PressBatchEnterToContinue unconditionally pauses for Enter; used after
// every N entries in batch step mode (stepSize > 1).
func (r *TInteraction) PressBatchEnterToContinue() {
	if r.silenced {
		return
	}
	fmt.Fprint(os.Stderr, "--- Press Enter to continue ---")
	readStdinLine()
}

// StepCheckReady returns true when a question was asked for this entry and the
// step batch counter has reached the configured size, then resets the counter.
// The caller is responsible for prompting the user. Returns false when step mode
// is off, no question was asked, or the batch is not yet full.
func (r *TInteraction) StepCheckReady() bool {
	if r.stepSize <= 0 || !r.questionWasAsked {
		return false
	}
	r.stepCounter++
	if r.stepCounter >= r.stepSize {
		r.stepCounter = 0
		return true
	}
	return false
}

// StepCheckReadyEvery is like StepCheckReady but fires on every call regardless
// of whether a question was asked. Use when the loop unconditionally presents a prompt.
func (r *TInteraction) StepCheckReadyEvery() bool {
	if r.stepSize <= 0 {
		return false
	}
	r.stepCounter++
	if r.stepCounter >= r.stepSize {
		r.stepCounter = 0
		return true
	}
	return false
}

// StepCheck combines StepCheckReady with AskContinueOrQuit. Call at the end of
// each entry in a loop where questions are asked conditionally.
// Returns true when the user asked to quit.
func (r *TInteraction) StepCheck() bool {
	if !r.StepCheckReady() {
		return r.quitRequested
	}
	return r.AskContinueOrQuit()
}

// StepCheckEvery combines StepCheckReadyEvery with AskContinueOrQuit.
// Returns true when the user asked to quit.
func (r *TInteraction) StepCheckEvery() bool {
	if !r.StepCheckReadyEvery() {
		return r.quitRequested
	}
	return r.AskContinueOrQuit()
}

// AskContinueOrQuit asks the user whether to continue or quit after an entry
// that required interaction. Returns true if the user chose to quit.
// When interaction is silenced, always continues.
func (r *TInteraction) AskContinueOrQuit() bool {
	if r.silenced {
		return false
	}
	SpinnerInterrupt()
	fmt.Fprint(os.Stderr, "QUESTION: Continue with next entry, or quit? (Enter/c/q): ")
	for {
		answer := readStdinLine()
		if answer == "" || answer == "c" || answer == "y" {
			return false
		}
		if answer == "q" {
			r.quitRequested = true
			return true
		}
		fmt.Fprint(os.Stderr, "(Enter/c/q): ")
	}
}

// ConfirmAction always prompts the user for y/n confirmation, even when the
// interaction is silenced. Use for safety gates that must not be skipped
// by batch-mode callers.
func (r *TInteraction) ConfirmAction(prompt string) bool {
	r.questionsAnswered++
	fmt.Fprintf(os.Stderr, "CONFIRM:  %s (y/n): ", prompt)
	for {
		answer := readStdinLine()
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

	for {
		option := readStdinLine()
		if options.Contains(option) {
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

// statRow is a label/value/comment triple for printStatBlock.
// comment is optional; when non-empty it is printed after the value.
type statRow struct {
	label   string
	value   string
	comment string
}

// printStatBlock prints header then rows with labels left-aligned to a column
// wide enough that the longest label+colon is followed by exactly one space
// before the value. An optional comment is appended after two spaces.
func printStatBlock(header string, rows []statRow) {
	maxLen := 0
	for _, r := range rows {
		if n := len(r.label) + 1; n > maxLen { // +1 for the colon
			maxLen = n
		}
	}
	fmt.Fprintf(os.Stderr, "%s\n", header)
	for _, r := range rows {
		if r.comment != "" {
			fmt.Fprintf(os.Stderr, "  %-*s %s  %s\n", maxLen, r.label+":", r.value, r.comment)
		} else {
			fmt.Fprintf(os.Stderr, "  %-*s %s\n", maxLen, r.label+":", r.value)
		}
	}
}

// Reporting progres.
// The progress message should provide the formatting.
func (r *TInteraction) Progress(progress string, context ...any) bool {
	if !r.silenced && !r.progressSuppressed {
		SpinnerInterrupt()
		fmt.Fprintf(os.Stderr, "PROGRESS: "+progress+"\n", context...)
	}
	return true
}
