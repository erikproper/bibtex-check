/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: HtmlCommandsMap
 *
 * Loads the per-folder html_commands_map.csv and writes a default when absent.
 * The CSV format is: element_name,open_latex,close_latex
 * Each row maps an HTML inline element (e.g. <i>, <sup>) to a LaTeX open/close pair.
 * Lines starting with # are comments and are ignored on load.
 *
 * At read time, xmlCollectText stores HTML tags verbatim (<i>text</i>).
 * dblpRawToLaTeX then replaces <tag> with open_latex and </tag> with close_latex.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 18.05.2026
 *
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// defaultHtmlCommandsSections defines the built-in HTML element → LaTeX mappings.
// Each entry is [element_name, open_latex, close_latex].
var defaultHtmlCommandsSections = []struct {
	header  string
	entries [][3]string
}{
	{
		"Inline formatting",
		[][3]string{
			{"i", `{\emph{`, `}}`},
			{"tt", `{\tt `, `}`},
		},
	},
	{
		"Superscript and subscript",
		[][3]string{
			{"sup", `\({}^{\mbox{`, `}}\)`},
			{"sub", `\({}_{\mbox{`, `}}\)`},
		},
	},
}

// htmlCmdOpen and htmlCmdClose are loaded from html_commands_map.csv.
var htmlCmdOpen = map[string]string{}
var htmlCmdClose = map[string]string{}

// reHtmlOpenTag matches <tagname> where tagname is letters only.
var reHtmlOpenTag = regexp.MustCompile(`<([a-zA-Z]+)>`)

// reHtmlCloseTag matches </tagname>.
var reHtmlCloseTag = regexp.MustCompile(`</([a-zA-Z]+)>`)

// nextCsvField reads one CSV field from the start of s, returning (value, rest, ok).
// The field may be double-quote-delimited with "" escaping; otherwise it runs to the
// next comma or end of string.
func nextCsvField(s string) (value, rest string, ok bool) {
	if len(s) == 0 {
		return "", "", true
	}
	if s[0] != '"' {
		i := strings.IndexByte(s, ',')
		if i < 0 {
			return s, "", true
		}
		return s[:i], s[i:], true
	}
	var b strings.Builder
	j := 1
	for j < len(s) {
		if s[j] == '"' {
			if j+1 < len(s) && s[j+1] == '"' {
				b.WriteByte('"')
				j += 2
			} else {
				j++
				break
			}
		} else {
			b.WriteByte(s[j])
			j++
		}
	}
	return b.String(), s[j:], true
}

// splitCsvLine3 splits a CSV line "tag,open_latex,close_latex" into three fields.
// open_latex and close_latex may be double-quote-delimited.
func splitCsvLine3(line string) (tag, openLatex, closeLatex string, ok bool) {
	i := strings.IndexByte(line, ',')
	if i < 0 {
		return
	}
	tag = strings.TrimSpace(line[:i])
	rest := line[i+1:]

	openLatex, rest, ok = nextCsvField(rest)
	if !ok || len(rest) == 0 || rest[0] != ',' {
		ok = false
		return
	}
	rest = rest[1:]

	closeLatex, _, ok = nextCsvField(rest)
	return
}

func writeHtmlCommandsMap(path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write HTML commands map %s: %s\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# html_commands_map.csv — HTML inline element to LaTeX open/close pairs")
	fmt.Fprintln(w, "# Format: element_name,open_latex,close_latex")
	fmt.Fprintln(w, "# Lines starting with # are ignored on load")
	for _, sec := range defaultHtmlCommandsSections {
		fmt.Fprintf(w, "#\n# %s\n", sec.header)
		for _, e := range sec.entries {
			fmt.Fprintf(w, "%s,%s,%s\n", e[0], quoteMapField(e[1]), quoteMapField(e[2]))
		}
	}
	w.Flush()
}

func loadHtmlCommandsMap(path string) {
	htmlCmdOpen = make(map[string]string)
	htmlCmdClose = make(map[string]string)

	f, err := os.Open(path)
	if err != nil {
		for _, sec := range defaultHtmlCommandsSections {
			for _, e := range sec.entries {
				htmlCmdOpen[e[0]] = e[1]
				htmlCmdClose[e[0]] = e[2]
			}
		}
		writeHtmlCommandsMap(path)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tag, open, close, ok := splitCsvLine3(line)
		if !ok || tag == "" {
			continue
		}
		htmlCmdOpen[tag] = open
		htmlCmdClose[tag] = close
	}
}
