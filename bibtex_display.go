/*
 *
 * Module:    bibtex_check
 * Component:
 * - bibtex_display
 *
 * Live external preview channel: displayContent writes text to display_file
 * (see the .settings file) and, the first time it is called during a run,
 * launches display_command to open that file in an external, auto-refreshing
 * viewer. Subsequent calls just overwrite display_file — the viewer is
 * expected to pick up the change on its own (e.g. an editor that reloads
 * files changed on disk).
 *
 * Both settings are optional; when display_file is unset, displayContent is a
 * silent no-op so callers never need to guard the call themselves.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 04.08.2026
 *
 */

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var displayCommandLaunched = false

// displayContent writes content to display_file, launching display_command
// once per run (on the first call) to open it. No-op when display_file is
// not configured.
func displayContent(content string) {
	if displayFile == "" {
		return
	}
	if !displayCommandLaunched {
		launchDisplayCommand()
		displayCommandLaunched = true
	}
	if dir := filepath.Dir(displayFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not create display_file directory %s: %s\n", dir, err)
			return
		}
	}
	if err := os.WriteFile(displayFile, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write display_file %s: %s\n", displayFile, err)
	}
}

// launchDisplayCommand starts display_command (a shell command line) detached from
// this process, without waiting for it to finish. Failures are non-fatal — the
// display_file is still written, just without a viewer open on it.
func launchDisplayCommand() {
	if displayCommand == "" {
		return
	}
	cmd := exec.Command("sh", "-c", displayCommand)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not run display_command %q: %s\n", displayCommand, err)
		return
	}
	go cmd.Wait() //nolint:errcheck // detached viewer process; nothing to do with its exit status
}
