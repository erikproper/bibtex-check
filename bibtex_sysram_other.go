//go:build !darwin

/*
 *
 * Module:    bibtex_check
 * Component:
 * - bibtex_sysram
 *   - bibtex_sysram_other
 *
 * Stub systemTotalRAM() for non-macOS platforms; always returns 0 so that
 * initEntryCache unconditionally attempts to load the entry cache.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 04.05.2026
 *
 */

package main

func systemTotalRAM() uint64 {
	return 0
}
