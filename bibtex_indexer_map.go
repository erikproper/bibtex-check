/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: IndexerMap
 *
 * Loads the per-folder latex_indexer.csv file and writes a default when absent.
 * The CSV format is: macroname;replacement
 * An empty replacement means the macro is silently removed.
 * Lines starting with # are comments and are ignored on load.
 *
 * Entries are applied by TeXStringIndexer after Phase 1 (structural chars stripped)
 * so each entry matches \macroname without surrounding braces.
 * Longer macro names are applied before shorter ones to prevent partial matches.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 19.05.2026
 *
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// latexIndexerEntry holds one macro→replacement mapping for TeXStringIndexer.
type latexIndexerEntry struct {
	macro       string
	replacement string
}

// latexIndexerEntries is sorted longest-first so longer macros shadow shorter prefixes.
var latexIndexerEntries []latexIndexerEntry

// defaultLatexIndexerSections defines the built-in entries written when no CSV exists.
var defaultLatexIndexerSections = []struct {
	header  string
	entries [][2]string
}{
	{
		"Cyrillic — \\ cyrchar prefix and letter approximations",
		[][2]string{
			{"cyrchar", ""},
			{"CYRS", "c"},
		},
	},
}

func writeLatexIndexerMap(path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write latex indexer map %s: %s\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# latex_indexer.csv — LaTeX macro approximations for title indexing")
	fmt.Fprintln(w, "# Format: macroname;replacement   (empty replacement = silently remove)")
	fmt.Fprintln(w, "# Applied after Phase 1 (braces stripped) so entries match \\macroname without braces.")
	fmt.Fprintln(w, "# Longer macro names shadow shorter prefixes — put longer names first within each group.")
	fmt.Fprintln(w, "# Lines starting with # are ignored on load.")
	for _, sec := range defaultLatexIndexerSections {
		fmt.Fprintf(w, "#\n# %s\n", sec.header)
		for _, e := range sec.entries {
			fmt.Fprintf(w, "%s;%s\n", e[0], e[1])
		}
	}
	w.Flush()
}

func loadLatexIndexerMap(path string) {
	raw := make(map[string]string)

	f, err := os.Open(path)
	if err != nil {
		for _, sec := range defaultLatexIndexerSections {
			for _, e := range sec.entries {
				raw[e[0]] = e[1]
			}
		}
		writeLatexIndexerMap(path)
	} else {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			idx := strings.IndexByte(line, ';')
			if idx < 0 {
				continue
			}
			macro := strings.TrimSpace(line[:idx])
			if macro == "" {
				continue
			}
			raw[macro] = line[idx+1:]
		}
	}

	latexIndexerEntries = make([]latexIndexerEntry, 0, len(raw))
	for macro, repl := range raw {
		latexIndexerEntries = append(latexIndexerEntries, latexIndexerEntry{macro, repl})
	}
	sort.Slice(latexIndexerEntries, func(i, j int) bool {
		return len(latexIndexerEntries[i].macro) > len(latexIndexerEntries[j].macro)
	})
}
