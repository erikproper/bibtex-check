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
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func KeyForDBLP(key string) string {
	return "DBLP:" + key
}

// dblpEntryFromURL fetches the BibTeX record for DBLPKey from dblp.org, writes it to
// a temp file, parses it via the capturedDBLPEntry mechanism, and returns the result.
// Returns nil on any network, I/O, or parse error.
func (l *TBibTeXLibrary) dblpEntryFromURL(DBLPKey string) *TBibTeXEntry {
	l.Progress(ProgressFetchingDBLPEntry, DBLPKey)

	client := newBibCheckHTTPClient()
	resp, err := client.Get("https://dblp.org/rec/" + DBLPKey + ".bib")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	tmpFile, err := os.CreateTemp("", "dblp_*.bib")
	if err != nil {
		return nil
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return nil
	}
	tmpFile.Close()

	l.capturedDBLPEntry = &TBibTeXEntry{Key: KeyForDBLP(DBLPKey), Fields: map[string]string{}}
	l.ParseRawBibFile(tmpPath)
	dblpEntry := l.capturedDBLPEntry
	l.capturedDBLPEntry = nil

	if !dblpEntry.Exists() {
		return nil
	}
	return dblpEntry
}

// resolveDBLPCrossref converts a raw DBLP crossref key (as stored in dblp_entries or
// scraped bib files) to the corresponding library key. When allowURLFetch is true and
// the parent entry is not yet in the library it is created on the spot (possibly via a
// live fetch). When allowURLFetch is false and the parent is absent, returns "" so the
// caller drops the crossref — the parent will be merged separately in a bulk run.
func (l *TBibTeXLibrary) resolveDBLPCrossref(crossrefDblpKey string, allowURLFetch bool) string {
	dblpAlias := KeyForDBLP(crossrefDblpKey)
	libraryKey := l.MapEntryKey(dblpAlias)
	if libraryKey == dblpAlias {
		// Add from local file store regardless of allowURLFetch; URL fetch only
		// happens inside MaybeMergeDBLPEntry when dblpEntryFromFile returns nil.
		if dblpEntryFromFile(crossrefDblpKey) != nil || allowURLFetch {
			libraryKey = l.MaybeAddDBLPEntry(crossrefDblpKey)
		} else {
			l.Warning("DBLP crossref not in local DB (skipping): %s", crossrefDblpKey)
			return ""
		}
	}
	return libraryKey
}

// MaybeMergeDBLPEntry builds a source entry from the DBLP database (primary) or,
// when allowURLFetch is true, a live dblp.org fetch (fallback). Merges into the
// existing library entry for key inside a single transaction. Returns true if the
// merge was performed. Pass allowURLFetch=false for bulk fix loops; true when
// intentionally adding a new entry.
func (l *TBibTeXLibrary) MaybeMergeDBLPEntry(DBLPKey, key string, allowURLFetch bool) bool {
	if key == "" || DBLPKey == "" {
		return false
	}

	dblpEntry := dblpEntryFromFile(DBLPKey)
	if dblpEntry == nil && allowURLFetch {
		dblpEntry = l.dblpEntryFromURL(DBLPKey)
	}
	if dblpEntry == nil {
		return false
	}
	l.MaybeApplyFieldMappings(dblpEntry, false)

	// Normalise all DBLP field values before merging so that the stored entry is
	// already in normalised form. Without this, CheckEntry finds raw-vs-normalised
	// mismatches and raises spurious challenges even on brand-new entries.
	for field, value := range dblpEntry.Fields {
		if field != EntryTypeField {
			dblpEntry.Fields[field] = l.NormaliseFieldValue(field, value)
		}
	}

	// For proceedings entries DBLP's JSON booktitle is an internal series abbreviation
	// (e.g. "IWPSE") that does not appear in DBLP's own BibTeX output. Always replace it
	// with the full title so child inproceedings can inherit the correct value via crossref.
	if dblpEntry.Fields[EntryTypeField] == "proceedings" {
		if title := dblpEntry.Fields["title"]; title != "" {
			dblpEntry.Fields["booktitle"] = title
		}
	}

	// DBLP occasionally uses @article + crossref-to-proceedings to model journal articles
	// published within an LNCS sub-series (e.g. TAOSD). This is a DBLP data hack that does
	// not map to valid BibTeX: articles should not have crossrefs to proceedings. Correct it:
	// convert the child to @inproceedings, drop the journal field, and drop the child's volume
	// when the parent already carries the volume (so it is not inherited redundantly).
	if dblpEntry.Fields[EntryTypeField] == "article" {
		rawCrossref := l.DblpParentOverrides[DBLPKey]
		if rawCrossref == "" {
			rawCrossref = dblpEntry.Fields["crossref"]
		}
		if rawCrossref != "" {
			if parent := dblpEntryFromFile(rawCrossref); parent != nil && parent.Fields[EntryTypeField] == "proceedings" {
				dblpEntry.Fields[EntryTypeField] = "inproceedings"
				// Strip all inheritable fields from the in-memory DBLP entry — the child
				// must inherit these from the proceedings parent, not carry its own values.
				// Also drop journal, which is article-only and invalid for inproceedings.
				for field := range BibTeXMustInheritFields.Elements() {
					delete(dblpEntry.Fields, field)
				}
				delete(dblpEntry.Fields, "journal")
				// Pre-set the library entry so MergeInMemoryDBLPEntry sees no conflicts
				// and does not prompt — this is a rule-based structural correction, not a
				// content dispute requiring user input. Only runs once (on the repair pass).
				if l.EntryFieldValueity(key, EntryTypeField) == "article" {
					l.Progress("DBLP: corrected article→inproceedings for %s (parent: %s)", key, rawCrossref)
					l.SetEntryFieldValue(key, EntryTypeField, "inproceedings")
					for field := range BibTeXMustInheritFields.Elements() {
						deleteBibEntryField(key, field)
					}
					deleteBibEntryField(key, "journal")
				}
			}
		}
	}

	// Only entry types in BibTeXCrossreffer (inproceedings, incollection, inbook, misc)
	// participate in the crossref hierarchy. For all others (e.g. article), DBLP may
	// carry an internal crossref to its journal volume, but that has no meaning in the
	// library model — discard it and keep the entry's own field values intact.
	if BibTeXCrossreffer.Contains(dblpEntry.Fields[EntryTypeField]) {
		// Both DB and scraped bib store the crossref as the raw DBLP key. Resolve it to
		// the library key before merging; create the parent entry if it does not exist yet.
		// A dblp_parent override (from -fix_dblp_hierarchy) takes priority over the raw
		// DBLP crossref field to handle multi-volume ambiguity (e.g. SCITEPRESS events).
		crossrefDblpKey := l.DblpParentOverrides[DBLPKey]
		if crossrefDblpKey == "" {
			crossrefDblpKey = dblpEntry.Fields["crossref"]
		}
		if crossrefDblpKey != "" {
			if libraryKey := l.resolveDBLPCrossref(crossrefDblpKey, allowURLFetch); libraryKey != "" {
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
	} else {
		delete(dblpEntry.Fields, "crossref")
	}

	// After crossref resolution: enforce parent entry type and align child year.
	if resolvedCrossref := dblpEntry.Fields["crossref"]; resolvedCrossref != "" {
		childType := dblpEntry.Fields[EntryTypeField]
		if expectedParentType, ok := BibTeXCrossrefType[childType]; ok {
			currentParentType := l.EntryFieldValueity(resolvedCrossref, EntryTypeField)
			if currentParentType != "" && currentParentType != expectedParentType &&
				!l.EntryFieldAliasHasTarget(resolvedCrossref, EntryTypeField, expectedParentType, currentParentType) {
				l.Progress(ProgressFixedParentType, resolvedCrossref, currentParentType, expectedParentType, key)
				l.SetEntryFieldValue(resolvedCrossref, EntryTypeField, expectedParentType)
				l.UpdateEntryFieldAlias(resolvedCrossref, EntryTypeField, currentParentType, expectedParentType)
			}
		}
		parentYear := l.EntryFieldValueity(resolvedCrossref, "year")
		childYear := dblpEntry.Fields["year"]
		if parentYear != "" && childYear != "" && childYear != parentYear {
			l.Progress(ProgressFixedChildYear, key, childYear, parentYear)
			dblpEntry.Fields["year"] = parentYear
		}
	}

	// Check before the merge: if the library entry already has a withdrawn date, the
	// author must stay as {Withdrawn publication} even if DBLP's file store doesn't
	// know about the withdrawal.
	locallyWithdrawn := l.EntryFieldValueity(key, "withdrawn") != ""

	beginBibTransaction()
	changed := l.MergeInMemoryDBLPEntry(dblpEntry, key)
	if l.EntryFieldValueity(key, DBLPField) != DBLPKey {
		changed = true
		l.SetEntryFieldValue(key, DBLPField, DBLPKey)
	}
	// Override author when withdrawn — either DBLP file store marks it as such, or the
	// library itself already has a withdrawn date (locally set by the user).
	isWithdrawn, mdate := dblpWithdrawnInfoFromFile(DBLPKey)
	if isWithdrawn || locallyWithdrawn {
		if l.EntryFieldValueity(key, "author") != "{Withdrawn publication}" {
			l.SetEntryFieldValue(key, "author", "{Withdrawn publication}")
			changed = true
		}
		if l.EntryFieldValueity(key, "note") == "Withdrawn." {
			l.SetEntryFieldValue(key, "note", "")
			changed = true
		}
		if mdate != "" && l.EntryFieldValueity(key, "withdrawn") != mdate {
			l.SetEntryFieldValue(key, "withdrawn", mdate)
			changed = true
		}
	}
	if strings.HasPrefix(DBLPKey, "data/") {
		if l.normaliseDblpDataEntry(DBLPKey, key) {
			changed = true
		}
	}
	commitBibTransaction()

	if changed {
		bibEntriesModified = true
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
	if !l.MaybeMergeDBLPEntry(DBLPKey, key, true) {
		return ""
	}
	setTableDirty("dblp_hierarchy")

	// Register the DBLP alias in memory immediately so that any subsequent
	// LookupDBLPKey call in the same run finds this entry rather than creating
	// another duplicate. Direct assignment avoids setting keyOldiesModified —
	// buildKeyAliasesFromDb already rebuilds these from the DB on every startup.
	l.KeyToKey[KeyForDBLP(DBLPKey)] = key
	l.HintToKey[KeyForDBLP(DBLPKey)] = key

	// MaybeMergeDBLPEntry writes to the DB but does not update the in-memory
	// TitleIndex that buildTitleIndexFromDb built at startup. Add the title now
	// so that CheckNeedToMergeForEqualTitles can detect duplicates for this entry.
	if title := l.EntryFieldValueity(key, TitleField); title != "" {
		l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(title), key)
	}

	l.CheckAndEnforcePreferredAlias(l.buildEntry(key))

	// For bookish entries (proceedings/book) add all children from the DBLP file store,
	// unless the entry carries the no_dblp_children flag.
	// Alias registration above must complete first so that LookupDBLPKey on this parent
	// resolves correctly when children call back into MaybeAddDBLPEntry for their crossref.
	entryType := l.EntryFieldValueity(key, EntryTypeField)
	if BibTeXBookish.Contains(entryType) && !l.EntryHasFlag(key, EntryFlagNoDBLPChildren) {
		children := readDblpCrossrefChildren(DBLPKey)
		if len(children) > 0 {
			spinner := l.NewSpinner(fmt.Sprintf("Adding %d children of %s", len(children), DBLPKey))
			for i, childDBLP := range children {
				if childKey := l.LookupDBLPKey(childDBLP); childKey != "" {
					l.SetEntryFieldValue(childKey, "crossref", key)
				} else {
					l.MaybeAddDBLPChildEntry(childDBLP, key)
				}
				spinner.Update(i+1, len(children))
			}
			spinner.Stop()
		}
	}

	return key
}

// MarkDblpKeyMissing records that DBLPKey is not in the local file store for
// library entry key. Does nothing if DBLPKey is already present in the file store.
func (l *TBibTeXLibrary) MarkDblpKeyMissing(key, DBLPKey string) {
	if DBLPKey == "" || dblpEntryFromFile(DBLPKey) != nil {
		return
	}
	canon := l.MapEntryKey(key)
	if !l.HasMetadata(canon, MetaPropDblpKeyMissing) {
		l.SetMetadata(canon, MetaPropDblpKeyMissing, time.Now().Format("2006-01-02"))
	}
}

// AssociateDblpKey links dblpKey to an existing library entry key.
// When the DBLP key is already mapped to a different library entry, that entry
// is merged into key first. Then the entry is refreshed from the local file
// store (or dblp.org when not yet stored locally), and MarkDblpKeyMissing is
// called in case the file store does not have it yet.
func (l *TBibTeXLibrary) AssociateDblpKey(key, dblpKey string) {
	if existing := l.LookupDBLPKey(dblpKey); existing != "" && existing != key {
		l.MergeEntries(existing, key)
	}
	l.KeyToKey[KeyForDBLP(dblpKey)] = key
	l.HintToKey[KeyForDBLP(dblpKey)] = key
	if !l.MaybeMergeDBLPEntry(dblpKey, key, true) {
		l.SetEntryFieldValue(key, DBLPField, dblpKey)
		bibEntriesModified = true
	}
	l.MarkDblpKeyMissing(key, dblpKey)
	setTableDirty("dblp_hierarchy")
}

// AskCandidateDblpKey presents numbered DBLP candidate entries for a library
// entry and asks the user to choose one (0 = none of them match).
// Returns the chosen DBLP key, or "" when the user picks 0.
func (l *TBibTeXLibrary) AskCandidateDblpKey(key string, candidates []string) string {
	fmt.Fprintf(os.Stderr, "\nLibrary entry:\n%s\n", l.entryDisplayString(key))
	for i, dblpKey := range candidates {
		fmt.Fprintf(os.Stderr, "[%d] %s", i+1, dblpKey)
		if entry := dblpEntryFromFile(dblpKey); entry != nil {
			if entryType := entry.EntryType(); entryType != "" {
				fmt.Fprintf(os.Stderr, " (%s)", entryType)
			}
			priorityFields := []string{
				"title", "booktitle", "journal", "year",
				"author", "editor", "school", "publisher", "series",
			}
			shown := TStringSetNew()
			shown.Add(EntryTypeField)
			for _, field := range priorityFields {
				shown.Add(field)
				if v := entry.Fields[field]; v != "" {
					fmt.Fprintf(os.Stderr, "\n    %-12s: %s", field, v)
				}
			}
			remaining := []string{}
			for field := range entry.Fields {
				if !shown.Contains(field) && entry.Fields[field] != "" {
					remaining = append(remaining, field)
				}
			}
			sort.Strings(remaining)
			for _, field := range remaining {
				fmt.Fprintf(os.Stderr, "\n    %-12s: %s", field, entry.Fields[field])
			}
			// Show crossref parent for context (title, year, editor).
			if crossref := entry.Fields["crossref"]; crossref != "" {
				if parent := dblpEntryFromFile(crossref); parent != nil {
					fmt.Fprintf(os.Stderr, "\n  Parent: %s", crossref)
					for _, field := range []string{"title", "booktitle", "year", "editor", "publisher", "series"} {
						if v := parent.Fields[field]; v != "" {
							fmt.Fprintf(os.Stderr, "\n    %-12s: %s", field, v)
						}
					}
				}
			}
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
	options := TStringSetNew()
	options.Add("0", "k")
	for i := range candidates {
		options.Add(fmt.Sprintf("%d", i+1))
	}
	answer := l.WarningQuestion(QuestionExtendDblpCoverageChoose, options,
		WarningExtendDblpCandidatesFound, key, len(candidates))
	if answer == "k" {
		if dblpKey, err := Reporting.AskForInput("DBLP key"); err == nil && dblpKey != "" {
			if dblpEntryFromFile(dblpKey) != nil {
				return dblpKey
			}
			l.Warning("DBLP key %q not found in file store", dblpKey)
		}
		return ""
	}
	n, _ := strconv.Atoi(answer)
	if n <= 0 || n > len(candidates) {
		return ""
	}
	return candidates[n-1]
}

// CheckDblpKeyMissingWarnings warns about library entries whose DBLP key has
// been absent from the local file store for more than two months.
func (l *TBibTeXLibrary) CheckDblpKeyMissingWarnings() {
	threshold := time.Now().AddDate(0, -2, 0)
	for key, dateStr := range l.AllEntriesWithProp(MetaPropDblpKeyMissing) {
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		if t.Before(threshold) {
			DBLPKey := l.EntryFieldValueity(key, DBLPField)
			l.Warning(WarningDblpKeyNotInXML, DBLPKey, key, dateStr)
		}
	}
}

func (l *TBibTeXLibrary) MaybeFixDBLPEntry(key string) {
	if DBLPKey := l.EntryFieldValueity(key, DBLPField); DBLPKey != "" {
		if !l.MaybeMergeDBLPEntry(DBLPKey, key, false) {
			l.MarkDblpKeyMissing(key, DBLPKey)
		} else {
			l.DeleteMetadata(key, MetaPropDblpKeyMissing)
		}
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

		l.CheckAndEnforcePreferredAlias(l.buildEntry(key))

		return key
	}

	return ""
}

// reAccessedOn matches the "Accessed on DATE." note pattern used on DBLP data/ entries.
// Captures a real ISO date (all digits); the placeholder "YYYY-MM-DD" does not match.
var reAccessedOn = regexp.MustCompile(`^Accessed on (\d{4}-\d{2}-\d{2})\.$`)

// normaliseDblpDataEntry applies DBLP data/→misc field fixes to a library entry after a
// DBLP merge. Clears howpublished (url is already set from ee), converts the note
// "Accessed on DATE." to urldate (using the entry's mdate when the date is a placeholder),
// and forces the entry type to misc. Returns true when any field was changed.
func (l *TBibTeXLibrary) normaliseDblpDataEntry(DBLPKey, key string) bool {
	changed := false

	if l.EntryFieldValueity(key, EntryTypeField) != "misc" {
		l.SetEntryFieldValue(key, EntryTypeField, "misc")
		changed = true
	}

	if l.EntryFieldValueity(key, "howpublished") != "" {
		l.SetEntryFieldValue(key, "howpublished", "")
		changed = true
	}

	if note := l.EntryFieldValueity(key, "note"); note != "" {
		var urldate string
		if m := reAccessedOn.FindStringSubmatch(note); m != nil {
			urldate = m[1]
		} else if strings.HasPrefix(note, "Accessed on ") {
			if je := readDblpJSONEntry(DBLPKey); je != nil {
				urldate = je.Mdate
			}
		}
		if urldate != "" {
			if l.EntryFieldValueity(key, "urldate") == "" {
				l.SetEntryFieldValue(key, "urldate", urldate)
			}
			l.SetEntryFieldValue(key, "note", "")
			changed = true
		}
	}

	return changed
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
	s = strings.ReplaceAll(s, "&", `\&`)
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
