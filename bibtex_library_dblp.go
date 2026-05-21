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
	"regexp"
	"strings"
)

func KeyForDBLP(key string) string {
	return "DBLP:" + key
}


// resolveDBLPCrossref converts a raw DBLP crossref key (as stored in dblp_entries or
// scraped bib files) to the corresponding library key. If the parent entry is not yet
// in the library it is created on the spot. Returns "" if the parent cannot be found
// or created.
func (l *TBibTeXLibrary) resolveDBLPCrossref(crossrefDblpKey string) string {
	dblpAlias := KeyForDBLP(crossrefDblpKey)
	libraryKey := l.MapEntryKey(dblpAlias)
	if libraryKey == dblpAlias {
		// Parent not yet in library — create it first.
		libraryKey = l.MaybeAddDBLPEntry(crossrefDblpKey)
	}
	return libraryKey
}

// MaybeMergeDBLPEntry builds a source entry from the DBLP database (primary) or the
// scraped bib file (fallback), then merges it into the existing entry for key inside
// a single transaction. Returns true if the merge was performed.
func (l *TBibTeXLibrary) MaybeMergeDBLPEntry(DBLPKey, key string) bool {
	if key == "" || DBLPKey == "" {
		return false
	}

	var dblpEntry *TBibTeXEntry
	dblpEntry = dblpEntryFromFile(DBLPKey)
	if dblpEntry != nil {
		l.MaybeApplyFieldMappings(dblpEntry)
	}
	if dblpEntry == nil {
		DBLPBibFile := l.FilesRoot + "DBLPScraper/bib/" + DBLPKey + "/bib"
		if !FileExists(DBLPBibFile) {
			return false
		}
		l.capturedDBLPEntry = &TBibTeXEntry{Key: KeyForDBLP(DBLPKey), Fields: map[string]string{}}
		l.ParseRawBibFile(DBLPBibFile)
		dblpEntry = l.capturedDBLPEntry
		l.capturedDBLPEntry = nil
		if !dblpEntry.Exists() {
			return false
		}
	}

	// Both DB and scraped bib store the crossref as the raw DBLP key. Resolve it to
	// the library key before merging; create the parent entry if it does not exist yet.
	if crossrefDblpKey := dblpEntry.Fields["crossref"]; crossrefDblpKey != "" {
		if libraryKey := l.resolveDBLPCrossref(crossrefDblpKey); libraryKey != "" {
			dblpEntry.Fields["crossref"] = libraryKey
			// DBLP stores short/incomplete inherited fields (booktitle, year, editor…)
			// on child entries. Strip them so the library's full values are not
			// overwritten. Full crossref inheritance resolution is deferred to 10.3.
			for field := range BibTeXMustInheritFields.Elements() {
				delete(dblpEntry.Fields, field)
			}
		} else {
			delete(dblpEntry.Fields, "crossref")
		}
	}


	beginBibTransaction()
	changed := l.MergeInMemoryDBLPEntry(dblpEntry, key)
	if l.EntryFieldValueity(key, DBLPField) != DBLPKey {
		changed = true
		l.SetEntryFieldValue(key, DBLPField, DBLPKey)
	}
	// If the DBLP store marks this entry as withdrawn, override author and record date.
	if isWithdrawn, mdate := dblpWithdrawnInfoFromFile(DBLPKey); isWithdrawn {
		if l.EntryFieldValueity(key, "author") != "{Withdrawn publication}" {
			l.SetEntryFieldValue(key, "author", "{Withdrawn publication}")
			changed = true
		}
		if mdate != "" && l.EntryFieldValueity(key, "withdrawn") != mdate {
			l.SetEntryFieldValue(key, "withdrawn", mdate)
			changed = true
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

func (l *TBibTeXLibrary) MaybeAddDBLPEntry(DBLPKey string) string {
	// Short-circuit: if this DBLP key was already added (possibly earlier in the same
	// run), return the existing library key rather than creating a duplicate.
	if existing := l.LookupDBLPKey(DBLPKey); existing != "" {
		return existing
	}

	key := l.NewKey()
	if !l.MaybeMergeDBLPEntry(DBLPKey, key) {
		return ""
	}

	// Register the DBLP alias in memory immediately so that any subsequent
	// LookupDBLPKey call in the same run finds this entry rather than creating
	// another duplicate. Direct assignment avoids setting keyOldiesModified —
	// buildKeyAliasesFromDb already rebuilds these from the DB on every startup.
	l.KeyToKey[KeyForDBLP(DBLPKey)] = key
	l.HintToKey[KeyForDBLP(DBLPKey)] = key

	return key
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


// reCedillaSpace matches the space-form cedilla {\c x} for a single letter x.
var reCedillaSpace = regexp.MustCompile(`\{\\c ([a-zA-Z])\}`)

// reMathDollar matches inline math $...$ for normalisation to \(...\).
var reMathDollar = regexp.MustCompile(`\$([^$]+)\$`)


// reAccentLong matches the long-form accent {\ACCENT{letter}} used by the DBLP website
// BibTeX export (e.g. {\'{e}}, {\"{o}}, {\~{a}}) and converts to short-form {\'e}.
var reAccentLong = regexp.MustCompile(`\{(\\['\"` + "`" + `^~.=])\{([a-zA-Z])\}\}`)

// reOuterBracedCmd matches {\cmd{X}} where X is exactly one letter — the single-character
// brace-protection form used in DBLP XML (e.g. {\emph{W}}). The outer braces are stripped
// to match the scraped form \emph{W}. Multi-letter content is NOT stripped because the outer
// braces are semantically meaningful grouping there (e.g. {\emph{ALCQI}}\mathcal{ALCQI}).
var reOuterBracedCmd = regexp.MustCompile(`\{(\\[a-zA-Z]+)\{([a-zA-Z])\}\}`)

// rePunctAfterBrace matches a closing brace followed by one or more punctuation characters
// from the set :.;,  Used as part of the symmetric comparison fallback to normalise e.g.
// {SOFL}: → {SOFL:}. Applied last so that ({X}): can first be rewritten to {(X)}: by the
// earlier transforms, after which rePunctAfterBrace moves the : inside.
var rePunctAfterBrace = regexp.MustCompile(`\}([:.;,]+)`)

// applyCompareFallbacks applies a sequence of normalising transforms to a title comparison
// string. Applied symmetrically to both the scraped and dblp sides so that parts which were
// already equal remain equal after each transform. rePunctAfterBrace runs last so that the
// ({X}): → {(X)}: rewrite (via the first two transforms) exposes the outer }: for it to act on.
func applyCompareFallbacks(s string) string {
	s = strings.ReplaceAll(s, "({", "{(")
	s = strings.ReplaceAll(s, "})", ")}")
	s = strings.ReplaceAll(s, " I ", " {I} ")
	s = strings.ReplaceAll(s, "}.{", ".")
	s = rePunctAfterBrace.ReplaceAllString(s, `$1}`)
	return s
}

// with no argument. Applied after NormaliseFieldValue to equalise {\emphword} (from the
// NormaliseTitleString output of {\emph{word}}) with \emphword (from \emph{word}).

// normalizeTeXEncoding unifies encoding-level differences that appear between the
// DBLP website BibTeX export (scraped) and the DBLP XML import. Applied before
// NormaliseFieldValue so both sources reach the normalizer in the same form. Handles:
//   - Long-form accents {\'{e}} → {\'e}
//   - Single-char outer-brace commands {\emph{W}} → \emph{W}
//   - Brace-protected hyphens {-} → -
//   - Dotless-i variants {\'\i} → {\'i}
//   - Space-form cedilla {\c x} → {\c{x}}
//   - Bare & → \& (DB stores raw XML text; BibTeX requires \&)
//   - Bare _ → {\_} (DB stores raw XML text; BibTeX requires {\_}; _{/^{ protected)
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
	s = strings.ReplaceAll(s, `_{`, "\x00sub")
	s = strings.ReplaceAll(s, `^{`, "\x00sup")
	s = strings.ReplaceAll(s, `{\_}`, "\x00und")
	s = strings.ReplaceAll(s, `\_`, "\x00und")
	s = strings.ReplaceAll(s, `_`, `{\_}`)
	s = strings.ReplaceAll(s, "\x00und", `{\_}`)
	s = strings.ReplaceAll(s, "\x00sub", `_{`)
	s = strings.ReplaceAll(s, "\x00sup", `^{`)
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
	s = strings.ReplaceAll(s, `\textasciiacute`, `\'{}`)
	s = strings.ReplaceAll(s, `\texttimes`, `\(\times\)`)
	s = strings.ReplaceAll(s, `\textdollar`, `\$`)
	s = strings.ReplaceAll(s, `\textdegree`, `{^\circ}`)
	s = strings.ReplaceAll(s, `\textsection`, `\S`)
	s = strings.ReplaceAll(s, `\textsterling`, `\pounds`)
	s = strings.ReplaceAll(s, `\texttheta`, `\theta`)
	s = strings.ReplaceAll(s, `\textquestiondown`, `?`)
	// \not= and \not = are both encodings of ≠; normalise to \neq.
	s = strings.ReplaceAll(s, `\not =`, `\neq`)
	s = strings.ReplaceAll(s, `\not=`, `\neq`)
	// Greek variant: \varepsilon and \epsilon are visually identical; treat as equal.
	s = strings.ReplaceAll(s, `\varepsilon`, `\epsilon`)
	// Remove thin-space commands (\, and \:) used for punctuation spacing in scraped titles.
	s = strings.ReplaceAll(s, `\,`, ``)
	s = strings.ReplaceAll(s, `\:`, ``)
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
// Unmapped non-ASCII runes are converted to \unicode{N} via normaliseUnicodeRune so they
// survive the byte-oriented character stream without high-bit truncation.
func applyUnicodeMap(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(normaliseUnicodeRune(r))
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
	s = applyHtmlCharMap(s)
	s = applyUnicodeMap(s)
	s = applyHtmlCmds(s)
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
