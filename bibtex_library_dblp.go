/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: DBLPLibrary
 *
 * DBLP-specific operations for the BibTeX library.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 18.05.2026
 *
 */

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

func KeyForDBLP(key string) string {
	return "DBLP:" + key
}

// MaybeMergeDBLPEntry parses the DBLP bib file for DBLPKey into an in-memory entry
// (never touching the DB during parse), then merges it into the existing entry for key
// inside a single transaction. Returns true if the merge was performed.
func (l *TBibTeXLibrary) MaybeMergeDBLPEntry(DBLPKey, key string) bool {
	if key == "" || DBLPKey == "" {
		return false
	}

	DBLPBibFile := l.FilesRoot + "DBLPScraper/bib/" + DBLPKey + "/bib"
	if !FileExists(DBLPBibFile) {
		return false
	}

	l.Progress("Fixing entry %s against DBLP version %s", key, DBLPKey)

	l.capturedDBLPEntry = &TBibTeXEntry{Key: KeyForDBLP(DBLPKey), Fields: map[string]string{}}
	l.harvestNameAliases = true
	l.ParseRawBibFile(DBLPBibFile)
	l.harvestNameAliases = false
	dblpEntry := l.capturedDBLPEntry
	l.capturedDBLPEntry = nil

	if !dblpEntry.Exists() {
		return false
	}

	beginBibTransaction()
	changed := l.MergeInMemoryDBLPEntry(dblpEntry, key)
	if l.EntryFieldValueity(key, DBLPField) != DBLPKey {
		changed = true
		l.SetEntryFieldValue(key, DBLPField, DBLPKey)
	}
	// If the DBLP DB marks this entry as withdrawn, override author and record date.
	if dblpDB != nil {
		if isWithdrawn, mdate := dblpWithdrawnInfo(DBLPKey); isWithdrawn {
			if l.EntryFieldValueity(key, "author") != "{Withdrawn publication}" {
				l.SetEntryFieldValue(key, "author", "{Withdrawn publication}")
				changed = true
			}
			if mdate != "" && l.EntryFieldValueity(key, "withdrawn") != mdate {
				l.SetEntryFieldValue(key, "withdrawn", mdate)
				changed = true
			}
		}
	}
	commitBibTransaction()

	if changed {
		bibEntriesModified = true
		l.CheckEntry(l.buildEntry(key))
	} else {
		// Even when nothing else changed, a previously-set DOI may have made an
		// existing URL redundant. CheckEntry is too expensive to run unconditionally,
		// but URL redundancy is cheap and avoids a flood of autoremove messages on
		// the next plain bibtex_check run.
		l.CheckURLRedundance(l.buildEntry(key))
	}

	return true
}

// / Really need both!?
func (l *TBibTeXLibrary) MaybeAddDBLPEntry(DBLPKey string) string {
	if key := l.NewKey(); l.MaybeMergeDBLPEntry(DBLPKey, key) {
		return key
	}

	return ""
}

func (l *TBibTeXLibrary) MaybeFixDBLPEntry(key string) {
	if DBLPKey := l.EntryFieldValueity(key, DBLPField); DBLPKey != "" {
		l.MaybeMergeDBLPEntry(DBLPKey, key)
	}
}

func (l *TBibTeXLibrary) MaybeAddDBLPChildEntry(DBLPKey, crossref string) string {
	if key := l.MaybeAddDBLPEntry(DBLPKey); key != "" && crossref != "" {
		splitCrossref := l.CheckNeedToSplitBookishEntry(key)
		if splitCrossref != "" {
			l.MergeEntries(splitCrossref, crossref)
		}

		l.SetEntryFieldValue(key, "crossref", crossref)

		l.CheckNeedToMergeForEqualTitles(key)

		return key
	}

	return ""
}

// dblpWithdrawnInfo returns (true, mdate) when the DBLP entry carries
// publtype="withdrawn". dblpDB must be open before calling.
func dblpWithdrawnInfo(dblpKey string) (bool, string) {
	var publtype string
	dblpDB.QueryRow(`SELECT value FROM dblp_entries WHERE dblp_key = ? AND field = 'publtype' AND position = 0`, dblpKey).Scan(&publtype)
	if publtype != "withdrawn" {
		return false, ""
	}
	var mdate string
	dblpDB.QueryRow(`SELECT value FROM dblp_entries WHERE dblp_key = ? AND field = 'mdate' AND position = 0`, dblpKey).Scan(&mdate)
	return true, mdate
}

// reCedillaSpace matches the space-form cedilla {\c x} for a single letter x.
var reCedillaSpace = regexp.MustCompile(`\{\\c ([a-zA-Z])\}`)

// reMathDollar matches inline math $...$ for normalisation to \(...\).
var reMathDollar = regexp.MustCompile(`\$([^$]+)\$`)

// rePlainBraces matches {TOKEN} where TOKEN contains no backslash, spaces, or nested braces.
var rePlainBraces = regexp.MustCompile(`\{([^\\{}\s]+)\}`)

// reAccentLong matches the long-form accent {\ACCENT{letter}} used by the DBLP website
// BibTeX export (e.g. {\'{e}}, {\"{o}}, {\~{a}}) and converts to short-form {\'e}.
var reAccentLong = regexp.MustCompile(`\{(\\['\"` + "`" + `^~.=])\{([a-zA-Z])\}\}`)

// reOuterBracedCmd matches {\cmd{X}} where X is exactly one letter — the single-character
// brace-protection form used in DBLP XML (e.g. {\emph{W}}). The outer braces are stripped
// to match the scraped form \emph{W}. Multi-letter content is NOT stripped because the outer
// braces are semantically meaningful grouping there (e.g. {\emph{ALCQI}}\mathcal{ALCQI}).
var reOuterBracedCmd = regexp.MustCompile(`\{(\\[a-zA-Z]+)\{([a-zA-Z])\}\}`)

// reSimpleBracedCmd matches {\\cmd} where cmd is purely letters — a brace-protected command
// with no argument. Applied after NormaliseFieldValue to equalise {\emphword} (from the
// NormaliseTitleString output of {\emph{word}}) with \emphword (from \emph{word}).
var reSimpleBracedCmd = regexp.MustCompile(`\{(\\[a-zA-Z]+)\}`)

// normalizeTeXEncoding unifies encoding-level differences that appear between the
// DBLP website BibTeX export (scraped) and the DBLP XML import. Applied before
// NormaliseFieldValue so both sources reach the normalizer in the same form. Handles:
//   - Long-form accents {\'{e}} → {\'e}
//   - Single-char outer-brace commands {\emph{W}} → \emph{W}
//   - Brace-protected hyphens {-} → -
//   - Dotless-i variants {\'\i} → {\'i}
//   - Space-form cedilla {\c x} → {\c{x}}
//   - Bare & → \& (DB stores raw XML text; BibTeX requires \&)
//   - Bare _ → {\_} (DB stores raw XML text; BibTeX requires {\_})
//   - Bare # → {\#} (DB stores raw XML text; BibTeX requires {\#})
//   - Bare % → \%  (DB stores raw XML text; BibTeX requires \%)
func normalizeTeXEncoding(s string) string {
	s = reAccentLong.ReplaceAllString(s, `{$1$2}`)
	s = reOuterBracedCmd.ReplaceAllString(s, `$1{$2}`)
	s = strings.ReplaceAll(s, `{-}`, `-`)
//	s = strings.ReplaceAll(s, `{\i}`, `i`)
//	s = strings.ReplaceAll(s, `\i}`, `i}`)
	s = reCedillaSpace.ReplaceAllString(s, `{\c{$1}}`)
	s = strings.ReplaceAll(s, `\&`, "\x00amp")
	s = strings.ReplaceAll(s, `&`, `\&`)
	s = strings.ReplaceAll(s, "\x00amp", `\&`)
	s = strings.ReplaceAll(s, `{\_}`, "\x00und")
	s = strings.ReplaceAll(s, `\_`, "\x00und")
	s = strings.ReplaceAll(s, `_`, `{\_}`)
	s = strings.ReplaceAll(s, "\x00und", `{\_}`)
	s = strings.ReplaceAll(s, `{\#}`, "\x00hash")
	s = strings.ReplaceAll(s, `\#`, "\x00hash")
	s = strings.ReplaceAll(s, `#`, `{\#}`)
	s = strings.ReplaceAll(s, "\x00hash", `{\#}`)
	s = strings.ReplaceAll(s, `\%`, "\x00pct")
	s = strings.ReplaceAll(s, `%`, `{\%}`)
	s = strings.ReplaceAll(s, "\x00pct", `{\%}`)
	return s
}

// normalizeTeXTitleForCompare handles source-format differences between the DBLP
// website BibTeX export (scraped) and the DBLP XML import that survive
// NormaliseFieldValue: math $...$ vs \(...\) and plain protective braces.
// Applied on top of NormaliseFieldValue when comparing scraped vs DBLP titles.
func normalizeTeXTitleForCompare(s string) string {
	// $...$ → \(...\): scraped files use \( \) throughout; DBLP XML uses $
	//s = reMathDollar.ReplaceAllStringFunc(s, func(m string) string {
	//	return `\(` + m[1:len(m)-1] + `\)`
	//})
	// Collapse all text-mode caret/hat forms to bare ^ BEFORE stripping plain braces,
	// so {\^{}} → {^} → ^ and {\^} → {^} → ^ both work.
	s = strings.ReplaceAll(s, `\^{}`, `^`)
	s = strings.ReplaceAll(s, `\^`, `^`)
	// Non-breaking space ~ → regular space (scraped files use ~ as word-joiner in titles;
	// DBLP XML stores plain spaces).
	s = strings.ReplaceAll(s, `~`, ` `)
	// \textXXX command aliases: scraped bib uses \textXXX; DBLP XML uses shorter forms.
	s = strings.ReplaceAll(s, `\textcopyright`, `\copyright`)
	s = strings.ReplaceAll(s, `\textgreater`, `>`)
	s = strings.ReplaceAll(s, `\textless`, `<`)
	s = strings.ReplaceAll(s, `\texteuro`, `\euro`)
	s = strings.ReplaceAll(s, `\texttildelow`, `{\~{}}`)
	s = strings.ReplaceAll(s, `\textasciitilde`, `{\~{}}`)
	s = strings.ReplaceAll(s, `\texttimes`, `\(\times\)`)
	s = strings.ReplaceAll(s, `\textdollar`, `\$`)
	s = strings.ReplaceAll(s, `\textdegree`, `{^\circ}`)
	// Greek variant: \varepsilon and \epsilon are visually identical; treat as equal.
	s = strings.ReplaceAll(s, `\varepsilon`, `\epsilon`)
	// Remove thin-space commands (\, and \:) used for punctuation spacing in scraped titles.
	s = strings.ReplaceAll(s, `\,`, ``)
	s = strings.ReplaceAll(s, `\:`, ``)
	// Strip plain protective braces {TOKEN} → TOKEN (title-case / acronym protection).
	// Scraped entries (from DBLP website BibTeX) add {} around tokens with internal
	// capitals; the raw XML does not. Safe to strip: TOKEN contains no LaTeX commands.
	s = rePlainBraces.ReplaceAllString(s, `$1`)
	// Strip {\cmd} → \cmd when cmd is purely letters (NormaliseTitleString produces
	// {\emphword} from {\emph{word}} but \emphword from \emph{word}; this equalises them).
	// Safe: content has no argument braces, so {\emph{word}} (which has {) is unaffected.
	s = reSimpleBracedCmd.ReplaceAllString(s, `$1`)
	return s
}

// applyHtmlCmds replaces HTML inline tags stored verbatim in dblp_entries
// (e.g. <i>text</i>) with their LaTeX equivalents from html_commands_map.csv.
func applyHtmlCmds(s string) string {
	s = reHtmlOpenTag.ReplaceAllStringFunc(s, func(m string) string {
		tag := m[1 : len(m)-1]
		if open, ok := htmlCmdOpen[tag]; ok {
			return open
		}
		return m
	})
	s = reHtmlCloseTag.ReplaceAllStringFunc(s, func(m string) string {
		tag := m[2 : len(m)-1]
		if close, ok := htmlCmdClose[tag]; ok {
			return close
		}
		return m
	})
	return s
}

// reHtmlEntityRef matches "&name;" entity references stored verbatim in dblp_entries.
var reHtmlEntityRef = regexp.MustCompile(`&([a-zA-Z]+);`)

// applyHtmlCharMap replaces "&name;" entity references with their LaTeX equivalents
// from html_character_map.csv.
func applyHtmlCharMap(s string) string {
	return reHtmlEntityRef.ReplaceAllStringFunc(s, func(m string) string {
		name := m[1 : len(m)-1]
		if latex, ok := htmlCharMap[name]; ok {
			return latex
		}
		return m
	})
}

// applyUnicodeMap replaces Unicode runes with their LaTeX equivalents from unicode_map.csv.
// Used for Unicode characters stored verbatim in dblp_entries (e.g. from ISO-8859-1
// decoded text) and for any stray Unicode in bibtex= canonical name values.
func applyUnicodeMap(s string) string {
	var b strings.Builder
	for _, r := range s {
		if mapped, ok := normaliseUnicodeMap[r]; ok {
			b.WriteString(mapped)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dblpRawToLaTeX converts a verbatim value stored in dblp_entries or dblp_persons
// to LaTeX notation suitable for comparison with library entries. The three-step
// pipeline mirrors the structure of the three CSV maps:
//  1. html_commands_map: <i>text</i> → {\emph{text}}, <sup>n</sup> → \({}^{\mbox{n}}\), etc.
//  2. html_character_map: &iacute; → {\'\i}, &agrave; → {\`a}, etc.
//  3. unicode_map: Unicode runes → LaTeX (e.g. from ISO-8859-1 decoded text or
//     bibtex= values with stray Unicode).
func dblpRawToLaTeX(s string) string {
	s = applyHtmlCmds(s)
	s = applyHtmlCharMap(s)
	s = applyUnicodeMap(s)
	return s
}

// reDblpDisambig matches the trailing " 0001"-style disambiguation suffix that
// DBLP appends to person names when multiple people share the same name.
var reDblpDisambig = regexp.MustCompile(` \d{4}$`)

// dblpPersonNameToLaTeX converts a verbatim person name from dblp_persons to
// LaTeX and strips any trailing DBLP disambiguation suffix (e.g. " 0001").
func dblpPersonNameToLaTeX(raw string) string {
	return dblpRawToLaTeX(reDblpDisambig.ReplaceAllString(raw, ""))
}

// splitBibAndList splits a BibTeX "Name1 and Name2 and ..." string into individual names.
func splitBibAndList(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, p := range strings.Split(s, " and ") {
		if p = strings.TrimSpace(p); p != "" {
			result = append(result, p)
		}
	}
	return result
}

// fixBadDblpNameMappingCanonicals corrects both sides of l.NameAliasToName
// that were written with raw DBLP artefacts:
//   - HTML entities (e.g. &eacute;) → LaTeX via dblpRawToLaTeX
//   - Disambiguation suffixes (e.g. " 0001") → stripped via reDblpDisambig
//
// Both corrections are applied to both the alias (key) and the canonical
// (value) in a single pass. When cleaning produces a self-mapping the entry
// is removed entirely. Corrects both the forward and inverse maps and marks
// the mapping as modified so the file is rewritten.
func fixBadDblpNameMappingCanonicals(l *TBibTeXLibrary) int {
	type fix struct{ oldAlias, newAlias, oldCanonical, newCanonical string }
	var fixes []fix
	for alias, canonical := range l.NameAliasToName {
		newAlias := dblpRawToLaTeX(reDblpDisambig.ReplaceAllString(alias, ""))
		newCanonical := dblpRawToLaTeX(reDblpDisambig.ReplaceAllString(canonical, ""))
		if newAlias != alias || newCanonical != canonical {
			fixes = append(fixes, fix{alias, newAlias, canonical, newCanonical})
		}
	}
	for _, f := range fixes {
		delete(l.NameAliasToName, f.oldAlias)
		l.NameToAliases[f.oldCanonical].Set().Delete(f.oldAlias)
		if l.NameToAliases[f.oldCanonical].Set().Size() == 0 {
			delete(l.NameToAliases, f.oldCanonical)
		}
		if f.newAlias == f.newCanonical {
			continue
		}
		l.NameAliasToName[f.newAlias] = f.newCanonical
		l.NameToAliases.AddValueToStringSetMap(f.newCanonical, f.newAlias)
	}
	if len(fixes) > 0 {
		l.nameMappingsModified = true
	}
	return len(fixes)
}

// doVerifyDblpImport cross-checks dblp_entries/dblp_persons against scraped bib files
// for every ErikProper entry that has a dblp field and a corresponding scraped file.
// Title mismatches are compared via TeXStringIndexer; author/editor names are compared directly.
func doVerifyDblpImport() {
	Reporting.SetInteractionOff()
	if !openLibraryToReport() {
		return
	}
	Library.ReadNameMappingsFile()
	Library.CheckNameMappingConsistency()
	if n := fixBadDblpNameMappingCanonicals(&Library); n > 0 {
		fmt.Fprintf(os.Stderr, "Corrected %d name mappings with raw HTML entities.\n", n)
	}
	if !openDblpDB() {
		return
	}
	defer closeDblpDB()

	rows, err := db.Query(`SELECT entry_key, value FROM bib_entries WHERE field = 'dblp' ORDER BY entry_key`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Query error: %s\n", err)
		return
	}
	defer rows.Close()

	checked, mismatches, withdrawn, nameMappingsAdded := 0, 0, 0, 0

	for rows.Next() {
		var entryKey, dblpKey string
		rows.Scan(&entryKey, &dblpKey)

		scraperPath := Library.FilesRoot + "DBLPScraper/bib/" + dblpKey + "/bib"
		if !FileExists(scraperPath) {
			continue
		}

		Library.capturedDBLPEntry = &TBibTeXEntry{Key: KeyForDBLP(dblpKey), Fields: map[string]string{}}
		Library.ParseRawBibFile(scraperPath)
		scraped := Library.capturedDBLPEntry
		Library.capturedDBLPEntry = nil

		if !scraped.Exists() {
			continue
		}

		checked++
		label := fmt.Sprintf("%s (%s)", entryKey, dblpKey)

		// Withdrawn entries are not comparable: skip field checks.
		if isWithdrawn, mdate := dblpWithdrawnInfo(dblpKey); isWithdrawn {
			fmt.Printf("WITHDRAWN %s (mdate: %s)\n", label, mdate)
			withdrawn++
			continue
		}

		// Determine whether this entry is a child with a crossref. Inherited
		// fields (booktitle, editors) live only on the parent in the DBLP XML
		// and are not stored redundantly on child entries in dblp_entries /
		// dblp_persons. The scraped bib resolves the inheritance, so skip
		// those comparisons for crossref children.
		var crossref string
		dblpDB.QueryRow(`SELECT value FROM dblp_entries WHERE dblp_key = ? AND field = 'crossref' AND position = 0`,
			dblpKey).Scan(&crossref)
		hasCrossref := crossref != ""

		for _, field := range []string{"title", "booktitle"} {
			if hasCrossref && field == "booktitle" {
				continue
			}
			scrapedVal := scraped.Fields[field]
			if scrapedVal == "" {
				continue
			}
			var dblpVal string
			dblpDB.QueryRow(`SELECT value FROM dblp_entries WHERE dblp_key = ? AND field = ? AND position = 0`,
				dblpKey, field).Scan(&dblpVal)
			scrapedNorm := normalizeTeXTitleForCompare(Library.NormaliseFieldValue(field, normalizeTeXEncoding(scrapedVal)))
			dblpNorm := normalizeTeXTitleForCompare(Library.NormaliseFieldValue(field, normalizeTeXEncoding(dblpRawToLaTeX(dblpVal))))
			if scrapedNorm != dblpNorm {
				fmt.Printf("TITLE MISMATCH %s %s:\n  norm scraped: %s\n  norm dblp:    %s\n  scraped:      %s\n  dblp:         %s\n",
					label, field, scrapedNorm, dblpNorm, scrapedVal, dblpVal)
				mismatches++
			}
		}

		for _, role := range []string{"author", "editor"} {
			if hasCrossref && role == "editor" {
				continue
			}
			scrapedNames := splitBibAndList(scraped.Fields[role])
			if len(scrapedNames) == 0 {
				continue
			}
			pRows, _ := dblpDB.Query(
				`SELECT name FROM dblp_persons WHERE dblp_key = ? AND role = ? ORDER BY position`,
				dblpKey, role)
			var dblpNames []string
			for pRows.Next() {
				var name string
				pRows.Scan(&name)
				dblpNames = append(dblpNames, name)
			}
			pRows.Close()

			if len(scrapedNames) != len(dblpNames) {
				fmt.Printf("COUNT MISMATCH %s %s: scraped=%d dblp=%d\n",
					label, role, len(scrapedNames), len(dblpNames))
				mismatches++
				continue
			}
			for i := range scrapedNames {
				scrapedNorm := Library.NormaliseFieldValue(role, normalizeTeXEncoding(scrapedNames[i]))
				dblpNorm := Library.NormaliseFieldValue(role, normalizeTeXEncoding(dblpPersonNameToLaTeX(dblpNames[i])))
				if scrapedNorm != dblpNorm {
					fmt.Printf("NAME MISMATCH %s %s[%d]:\n  norm scraped: %s\n  norm dblp:    %s\n  scraped:      %s\n  dblp:         %s\n",
						label, role, i, scrapedNorm, dblpNorm, scrapedNames[i], dblpNames[i])
					mismatches++
					// Silently extend the name mapping: the DBLP preferred name
					// (from bibtex= attribute if available) is canonical; the
					// scraped name becomes an alias. Both paths apply
					// dblpPersonNameToLaTeX so the canonical is stored in LaTeX
					// form (not raw HTML-entity form from the DB).
					canonical := dblpPersonNameToLaTeX(dblpNames[i])
					var bibtexName string
					dblpDB.QueryRow(`SELECT bibtex_name FROM dblp_name_bibtex WHERE plain_name = ?`, dblpNames[i]).Scan(&bibtexName)
					if bibtexName != "" {
						canonical = dblpPersonNameToLaTeX(bibtexName)
					}
					// Use the normalised form as the alias key so that
					// NormalisePersonNameValue finds the mapping on the next run.
					Library.AddNameMapping(canonical, scrapedNorm)
					nameMappingsAdded++
				}
			}
		}
	}

	if Library.nameMappingsModified {
		Library.WriteNameMappingFile()
	}

	fmt.Fprintf(os.Stderr, "Verified %d entries against scraped bib files, %d mismatches (%d withdrawn, %d name mappings added)\n",
		checked, mismatches, withdrawn, nameMappingsAdded)
}
