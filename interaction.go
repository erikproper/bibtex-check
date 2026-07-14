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
// DBLP scan, etc.) pulls from this channel so there is
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
	quitRequested      bool // set when user answers "q" to any question or ticker
	questionsAnswered  int  // total questions answered; snapshotted before/after runHarvestEntry
}

// QuitWasRequested reports whether the user asked to stop at any prompt or ticker.
func (r *TInteraction) QuitWasRequested() bool {
	return r.quitRequested
}

// QuestionsAnswered returns the running total of user answers (WarningQuestion + ConfirmAction).
func (r *TInteraction) QuestionsAnswered() int {
	return r.questionsAnswered
}

// ResetQuestionFlag is a no-op retained for call-site compatibility; to be removed in 17.2 cleanup.
func (r *TInteraction) ResetQuestionFlag() {}

// AskForInput prints prompt and returns the trimmed line the user types.
// Typing "q" sets quitRequested and returns "".
func (r *TInteraction) AskForInput(prompt string) (string, error) {
	fmt.Fprintf(os.Stderr, "INPUT:    %s (q=quit): ", prompt)
	line := readStdinLine()
	if line == "q" {
		r.quitRequested = true
		return "", nil
	}
	return line, nil
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
		fmt.Fprintf(os.Stderr, "ERROR:    "+errorMessage+"\n", context...)
	}

	return true
}

// Reporting warnings.
// The warning message should provide the formatting.
func (r *TInteraction) Warning(warning string, context ...any) bool {
	if !r.silenced {
		SpinnerInterrupt()
		fmt.Fprintf(os.Stderr, "WARNING: "+warning+"\n", context...)
	}

	return true
}

// WarningQuestion presents a question and waits for one of the listed options.
// "q" is always accepted (in addition to the named options) and triggers a
// graceful quit: quitRequested is set and "q" is returned to the caller.
func (r *TInteraction) WarningQuestion(question string, options TStringSet, warning string, context ...any) string {
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
	if !options.Contains("q") {
		optionSet += separator + "q"
	}
	optionSet += "): "

	fmt.Fprint(os.Stderr, "QUESTION: "+question+" "+optionSet)

	for {
		option := readStdinLine()
		if option == "q" {
			r.quitRequested = true
			return "q"
		}
		if options.Contains(option) {
			return option
		}
		fmt.Fprint(os.Stderr, optionSet)
	}
}

// WarningYesNoQuestion presents a yes/no question; returns true for "y".
// "q" is also accepted and triggers a graceful quit (returns false).
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

// printStatBlock prints header then rows with labels left-aligned and values
// right-aligned so all columns line up regardless of label/value widths.
// An optional comment is appended after two spaces.
func printStatBlock(header string, rows []statRow) {
	maxLabelLen := 0
	maxValLen := 0
	for _, r := range rows {
		if n := len(r.label) + 1; n > maxLabelLen { // +1 for the colon
			maxLabelLen = n
		}
		if n := len(r.value); n > maxValLen {
			maxValLen = n
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s\n", header)
	for _, r := range rows {
		if r.comment != "" {
			fmt.Fprintf(os.Stderr, "  %-*s  %*s  %s\n", maxLabelLen, r.label+":", maxValLen, r.value, r.comment)
		} else {
			fmt.Fprintf(os.Stderr, "  %-*s  %*s\n", maxLabelLen, r.label+":", maxValLen, r.value)
		}
	}
}

// ConfirmAction always prompts the user for y/n/q confirmation, even when the
// interaction is silenced. Use for safety gates that must not be skipped
// by batch-mode callers. "q" sets quitRequested and returns false.
func (r *TInteraction) ConfirmAction(prompt string) bool {
	r.questionsAnswered++
	fmt.Fprintf(os.Stderr, "CONFIRM:  %s (y/n/q): ", prompt)
	for {
		answer := readStdinLine()
		if answer == "y" || answer == "n" {
			return answer == "y"
		}
		if answer == "q" {
			r.quitRequested = true
			return false
		}
		fmt.Fprint(os.Stderr, "(y/n/q): ")
	}
}

// Reporting progres.
// The progress message should provide the formatting.
func (r *TInteraction) Progress(progress string, context ...any) bool {
	if !r.silenced && !r.progressSuppressed {
		SpinnerInterrupt()
		fmt.Fprintf(os.Stderr, progress+"\n", context...)
	}
	return true
}
