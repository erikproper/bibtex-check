/*
 *
 * Module: bibtex_library_checks
 *
 * This module is concerned with checks of fields and entries.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 03.05.2026
 *
 */

///// Are these really all "checks"??? The actual checks might even be done while reading the entries.

package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

/*
 *
 * BibTeX field value conformity checks
 *
 */

func (l *TBibTeXLibrary) IsRedundantURL(url, key string) bool {
	foundURL := strings.ToLower(url)

	return foundURL == strings.ToLower("https://doi.org/"+l.EntryFieldValueity(key, "doi"))
}

func IsValidKey(key string) bool {
	var validKey = regexp.MustCompile(`^` + keyPrefix + `-[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-[0-9][0-9]$`)

	return validKey.MatchString(key)
}

// Checks if a given ISSN fits the desired format
func IsValidISSN(ISSN string) bool {
	var validISSN = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9,X]$`)

	return validISSN.MatchString(ISSN)
}

// Checks if a given ISBN fits the desired format
func IsValidISBN(ISBN string) bool {
	var validISBN = regexp.MustCompile(`^([0-9][-]?[0-9][-]?[0-9][-]?|)[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9][-]?[0-9,X]$`)

	return validISBN.MatchString(ISBN)
}

// Checks if a given year is indeed a year
func IsValidYear(year string) bool {
	var validYear = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]$`)

	return validYear.MatchString(year)
}

// Checks if a given date is indeed a date
func IsValidDate(date string) bool {
	var validDate = regexp.MustCompile(`^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$`)

	return validDate.MatchString(date)
}

var (
	validChapterArabic = regexp.MustCompile(`^[0-9]+$`)
	// Standard Roman numerals I–MMMCMXCIX (case-insensitive); empty string excluded by
	// the outer chapter == "" guard in IsValidChapter.
	validChapterRoman = regexp.MustCompile(`(?i)^M{0,4}(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})$`)
)

// IsValidChapter reports whether chapter is an Arabic or Roman numeral.
func IsValidChapter(chapter string) bool {
	if chapter == "" {
		return false
	}
	return validChapterArabic.MatchString(chapter) || validChapterRoman.MatchString(chapter)
}

/*
 *
 * Check if a field is allowed for a given entry.
 *
 */
func (l *TBibTeXLibrary) EntryAllowsForField(entry, field string) bool {
	return BibTeXAllowedEntryFields[l.EntryType(entry)].Set().Contains(field)
}

/*
 *
 * Basic correctness checks of mappings
 *
 */

func (l *TBibTeXLibrary) checkValueMapping(valueMap TStringMap, inverseMap TStringSetMap, keyety string) {
	for source, target := range valueMap {
		if source == target {
			continue
		}
		if _, targetAlreadyUsedAsSource := valueMap[target]; targetAlreadyUsedAsSource {
			l.Warning(WarningTargetAlreadyUsedAsSource+keyety, target)
		}

		if _, sourceAlreadyUsedAsTarget := inverseMap[source]; sourceAlreadyUsedAsTarget {
			l.Warning(WarningSourceAlreadyUsedAsTarget+keyety, source)
		}
	}
}

func (l *TBibTeXLibrary) CheckFieldMappings() {
	total := len(l.GenericFieldSourceToTarget)
	for _, fieldValueMapping := range l.EntryFieldSourceToTarget {
		total += len(fieldValueMapping)
	}

	ticker := l.NewProgressTicker(ProgressCheckingFieldMappings, total)

	for field, valueMapping := range l.GenericFieldSourceToTarget {
		ticker.Step()
		l.checkValueMapping(valueMapping, l.GenericFieldTargetToSource[field], ".")
	}

	for key, fieldValueMapping := range l.EntryFieldSourceToTarget {
		for field, valueMapping := range fieldValueMapping {
			ticker.Step()
			l.checkValueMapping(valueMapping, l.EntryFieldTargetToSource[key][field], WarningMappingForKey+key+".")
		}
	}

	ticker.Done()
}

// CheckEntryFieldMappingWinners verifies that every winner recorded in the
// entry-field alias mappings still matches the entry's actual current field
// value.  A mismatch means the bib file was edited after the mapping was
// created without updating the mapping, so the old winner is now stale.
//
// Mismatches are fixed automatically: the actual value becomes the new winner,
// the old winner becomes a challenger (via UpdateEntryFieldAlias, which also
// cascade-updates every other challenger that pointed to the old winner).
// Fixes are collected before any map mutation to avoid iteration side-effects.
// Entries with an empty actual value (field deleted) are skipped.
func (l *TBibTeXLibrary) CheckEntryFieldMappingWinners() {
	l.Progress(ProgressCheckingEntryFieldMappingWinners)

	type mismatch struct {
		entry, field, winner, actual string
	}
	type redundant struct {
		entry, field, challenger string
	}
	var fixes []mismatch
	var redundants []redundant
	deletedMappings := 0

	for entry, fieldMap := range l.EntryFieldSourceToTarget {
		if !l.EntryExists(entry) {
			for field, challengerMap := range fieldMap {
				for challenger, winner := range challengerMap {
					l.Warning(WarningEntryFieldMappingDeletedEntry, entry, field, challenger, winner)
					deletedMappings++
				}
			}
			continue
		}

		for field, challengerMap := range fieldMap {
			actual := l.MapFieldValue(field, l.EntryFieldValueity(entry, field))
			if actual == "" {
				continue
			}
			seen := map[string]bool{}
			for challenger, winner := range challengerMap {
				if seen[winner] || winner == actual {
					seen[winner] = true
				} else {
					seen[winner] = true
					fixes = append(fixes, mismatch{entry, field, winner, actual})
				}
				// A challenger that now normalises to the actual value is
				// redundant: the name_mapping makes it equivalent to the
				// winner, so the losing_field_values entry can be deleted.
				if l.NormaliseFieldValue(field, challenger) == actual {
					redundants = append(redundants, redundant{entry, field, challenger})
				}
			}
		}
	}

	for _, m := range fixes {
		l.Warning(WarningEntryFieldMappingWinnerMismatch, m.entry, m.field, m.winner, m.actual)
		l.UpdateEntryFieldAlias(m.entry, m.field, m.winner, m.actual)
	}

	if len(redundants) > 0 {
		tx, err := db.Begin()
		if err == nil {
			for _, r := range redundants {
				tx.Exec(`DELETE FROM losing_field_values WHERE entry_key = ? AND field = ? AND value = ?`,
					r.entry, r.field, r.challenger)
				delete(l.EntryFieldSourceToTarget[r.entry][r.field], r.challenger)
			}
			tx.Commit()
		}
	}

	l.Progress(ProgressEntryFieldMappingWinnersResult, len(fixes), deletedMappings)
}

/*
 *
 * General library/entry level checks & updates
 *
 */

// Check all oldies
func (l *TBibTeXLibrary) CheckKeyOldiesConsistency() {
	l.Progress(ProgressCheckingConsistencyOfKeyOldies)

	var ghosts []struct{ oldie, key string }
	l.KeyOldies.ForEach(func(oldie, key string) {
		if !l.EntryExists(key) {
			l.Warning(WarningTargetOfOldieNotExists, key, oldie)
		}
		if bibEntryExists(oldie) {
			ghosts = append(ghosts, struct{ oldie, key string }{oldie, key})
		}
	})
	for _, g := range ghosts {
		// Ghost: the oldie key still has rows in bib_entries even though it is
		// recorded as an alias for g.key. Merge its fields into the canonical first
		// (in case the ghost holds data that was never reconciled), then the merge
		// will delete the ghost row as part of normal cleanup.
		l.Progress("Auto-repairing ghost entry %s (alias for %s)", g.oldie, g.key)
		l.MergeEntries(g.oldie, g.key)
	}
}

// CheckKeyHintsConsistency moves misplaced canonical-key hints to key_oldies.
// When both the hint and its target match the canonical key pattern (EP-...), the
// row belongs in key_oldies (old alias → canonical), not key_hints (shorthand → canonical).
// This can happen after restoring key hints from a backup that mixed the two tables.
func (l *TBibTeXLibrary) CheckKeyHintsConsistency() {
	var toMigrate []struct{ hint, key string }
	l.HintToKey.ForEach(func(hint, key string) {
		if IsValidKey(hint) {
			toMigrate = append(toMigrate, struct{ hint, key string }{hint, key})
		}
	})
	if len(toMigrate) == 0 {
		return
	}
	for _, m := range toMigrate {
		l.AddKeyAlias(m.hint, m.key)
		l.HintToKey.Delete(m.hint)
	}
	l.Progress("Migrated %d canonical-key hint(s) from key_hints to key_oldies", len(toMigrate))
}

// CheckDblpDuplicates finds pairs of live entries that share the same DBLP key and
// merges them. These arise when dblp_canonical writes fail (SQLITE_BUSY) and a later
// run creates a second entry for a key already present in the library.
// Same DBLP key = same paper, so duplicates are merged without prompting.
func (l *TBibTeXLibrary) CheckDblpDuplicates() {
	rows, err := db.Query(`
		SELECT a.entry_key, b.entry_key
		FROM bib_entries a
		JOIN bib_entries b ON a.value = b.value AND a.entry_key < b.entry_key
		WHERE a.field = ? AND b.field = ?
		  AND EXISTS (SELECT 1 FROM bib_entries WHERE entry_key = a.entry_key AND field = ?)
		  AND EXISTS (SELECT 1 FROM bib_entries WHERE entry_key = b.entry_key AND field = ?)`,
		DBLPField, DBLPField, EntryTypeField, EntryTypeField)
	if err != nil {
		dbInteraction.Warning("CheckDblpDuplicates: %s", err)
		return
	}
	defer rows.Close()

	type pair struct{ a, b string }
	var pairs []pair
	for rows.Next() {
		var a, b string
		if err := rows.Scan(&a, &b); err != nil {
			continue
		}
		pairs = append(pairs, pair{a, b})
	}

	for _, p := range pairs {
		a := l.MapEntryKey(p.a)
		b := l.MapEntryKey(p.b)
		if a != b && l.EntryExists(a) && l.EntryExists(b) {
			l.Progress("Merging duplicate DBLP entries: %s ← %s", a, b)
			beginBibTransaction()
			l.MergeEntries(b, a)
			commitBibTransaction()
		}
	}
}

// CheckDblpWaivedConsistency removes stale keys from DblpWaived: any key that is
// now an alias (MapEntryKey(k) != k) or no longer exists as a library entry.
func (l *TBibTeXLibrary) CheckDblpWaivedConsistency() {
	var stale []string
	l.DblpWaived.ForEach(func(key string, _ bool) {
		if l.MapEntryKey(key) != key || !l.EntryExists(key) {
			stale = append(stale, key)
		}
	})
	for _, key := range stale {
		l.DblpWaived.Delete(key)
	}
}

func (l *TBibTeXLibrary) CheckURLRedundance(entry *TBibTeXEntry) {
	url := entry.FieldValue("url")

	if l.IsRedundantURL(url, entry.Key) {
		l.Progress(ProgressRemovedRedundantURL, entry.Key, url)
		l.setEntryField(entry, "url", "")
	}
}

func (l *TBibTeXLibrary) tryGetDOIFromURL(key, field string, foundDOI *string) bool {
	if *foundDOI == "" {
		if URL := l.EntryFieldValueity(key, field); URL != "" {
			var DOIURL = regexp.MustCompile(`^(doi:|http(s|)://(doi.org|dx.doi.org|hdl.handle.net|doi.acm.org|doi.ieeecomputersociety.org|dl.acm.org/doi|onlinelibrary.wiley.com/doi|publications.amsus.org/doi/abs|press.endocrine.org/doi/abs|doi.apa.org/index.cfm?doi=|www.crcnetbase.com/doi/abs|publications.amsus.org/doi/abs|econtent.hogrefe.com/doi/abs|www.mitpressjournals.org/doi/abs|www.atsjournals.org/doi/abs)/)`)

			DOICandidate := DOIURL.ReplaceAllString(URL, "")

			if DOICandidate != URL {
				*foundDOI = DOICandidate

				return true
			}
		}
	}

	return false
}

var validPreferredKeyAlias = regexp.MustCompile(`^[a-z]+[0-9][0-9][0-9][0-9][a-z]([a-z0-9-]*[a-z0-9])?$`)
var reYearInAlias = regexp.MustCompile(`[0-9][0-9][0-9][0-9]`)

var stripNonAlpha = regexp.MustCompile(`[^a-z]`)
var stripNonAlphaNum = regexp.MustCompile(`[^a-z0-9]`)

var titleKeywordStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "in": true, "on": true,
	"at": true, "to": true, "for": true, "by": true, "and": true, "or": true,
	"with": true, "from": true, "is": true, "are": true, "was": true, "were": true,
	"proceedings": true, "workshop": true, "conference": true, "symposium": true,
	"international": true, "annual": true,
}

// titleKeywords returns all meaningful words from a title, in order, suitable
// for use as the keyword component of a preferred alias.
func titleKeywords(title string) []string {
	words := strings.FieldsFunc(title, func(r rune) bool {
		return r == ' ' || r == ':' || r == ',' || r == '.' || r == '/' || r == '~' || r == '('
	})
	var result []string
	seen := map[string]bool{}
	for _, w := range words {
		if strings.Contains(w, `\unicode{`) {
			continue
		}
		clean := stripNonAlphaNum.ReplaceAllString(TeXStringIndexer(w), "")
		if clean == "" || titleKeywordStopWords[clean] || seen[clean] {
			continue
		}
		if clean[0] >= '0' && clean[0] <= '9' {
			continue
		}
		seen[clean] = true
		result = append(result, clean)
	}
	return result
}

// splitOnUnbracedSpaces splits s on spaces that are outside brace groups,
// so "{Smith Kline}" is kept as a single token.
func splitOnUnbracedSpaces(s string) []string {
	var tokens []string
	var cur strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '{':
			depth++
			cur.WriteRune(r)
		case '}':
			depth--
			cur.WriteRune(r)
		case ' ':
			if depth == 0 {
				if cur.Len() > 0 {
					tokens = append(tokens, cur.String())
					cur.Reset()
				}
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// deriveAliasBase derives the <surname><year> prefix for a preferred alias.
// For "Last, First" names the surname is everything before the comma.
// For "First … Last" names the surname is the last brace-aware token, so
// "Osvaldo Cair{\'o} Battistutti" → "battistutti" and
// "John {Smith Kline}" → "smithkline".
// Name fallback chain: author → editor → crossref parent author/editor →
// publisher → crossref parent publisher → DBLP venue code (second-to-last segment).
// Year falls back to the crossref parent's year if the entry has none.
// Warns and returns "" if surname or year cannot be determined after all fallbacks.
func (l *TBibTeXLibrary) deriveAliasBase(entry *TBibTeXEntry) string {
	nameField := entry.FieldValue("author")
	if nameField == "" {
		nameField = entry.FieldValue("editor")
	}

	// Load crossref parent once; used as fallback for both name and year.
	var parent *TBibTeXEntry
	if crossrefKey := entry.FieldValue("crossref"); crossrefKey != "" {
		if p := l.buildEntry(l.MapEntryKey(crossrefKey)); p.Exists() {
			parent = p
		}
	}

	if nameField == "" && parent != nil {
		nameField = parent.FieldValue("author")
		if nameField == "" {
			nameField = parent.FieldValue("editor")
		}
	}
	if nameField == "" {
		nameField = entry.FieldValue("publisher")
	}
	if nameField == "" && parent != nil {
		nameField = parent.FieldValue("publisher")
	}
	// Last resort: venue code from the DBLP key (second-to-last segment,
	// e.g. "bled" from "conf/bled/2006").
	if nameField == "" {
		if dblpKey := entry.FieldValue(DBLPField); dblpKey != "" {
			parts := strings.Split(dblpKey, "/")
			if len(parts) >= 2 {
				nameField = parts[len(parts)-2]
			}
		}
	}
	// Final last resort: use the first title keyword as the name component.
	// derivePreferredAlias will then produce <keyword><year><keyword> (e.g. hello2002hello).
	if nameField == "" {
		if kws := titleKeywords(entry.FieldValue(TitleField)); len(kws) > 0 {
			nameField = kws[0]
		}
	}
	if nameField == "" {
		l.ReportEntryWarning(entry.Key, WarningCannotDeriveAliasNoName)
		return ""
	}

	first := strings.TrimSpace(strings.Split(nameField, " and ")[0])

	var surnameRaw string
	if idx := strings.Index(first, ", "); idx >= 0 {
		surnameRaw = first[:idx]
	} else {
		tokens := splitOnUnbracedSpaces(first)
		if len(tokens) == 0 {
			l.Warning(WarningCannotDeriveAliasNoName, entry.Key)
			return ""
		}
		surnameRaw = tokens[len(tokens)-1]
	}

	if strings.Contains(surnameRaw, `\unicode{`) {
		l.ReportEntryWarning(entry.Key, WarningCannotDeriveAliasEmptySurname, surnameRaw)
		return ""
	}
	surname := stripNonAlpha.ReplaceAllString(TeXStringIndexer(surnameRaw), "")
	if surname == "" {
		l.ReportEntryWarning(entry.Key, WarningCannotDeriveAliasEmptySurname, surnameRaw)
		return ""
	}

	year := entry.FieldValue("year")
	if !IsValidYear(year) && parent != nil {
		year = parent.FieldValue("year")
	}
	if !IsValidYear(year) {
		l.ReportEntryWarning(entry.Key, WarningCannotDeriveAliasNoYear)
		return ""
	}

	return surname + year
}

// derivePreferredAlias returns the first non-colliding <surname><year><keyword>
// alias candidate. It first tries each single title keyword, then concatenations
// of 2 adjacent keywords, then 3, etc. — so "knowledge" and "graphs" are tried
// individually before "knowledgegraphs" is tried as a compound.
// If all keyword combinations are exhausted (or the title has no usable keywords),
// the last segment of the DBLP key is tried as a final fallback.
// Returns "" if base data is missing (silent) or all approaches are exhausted (warns).
func (l *TBibTeXLibrary) derivePreferredAlias(entry *TBibTeXEntry) string {
	base := l.deriveAliasBase(entry)
	if base == "" {
		return ""
	}

	tryCandidate := func(keyword string) (string, bool) {
		candidate := base + keyword
		if !validPreferredKeyAlias.MatchString(candidate) {
			return "", false
		}
		if target := l.HintToKey.GetValue(candidate); target != "" && l.MapEntryKey(target) != entry.Key {
			return "", false
		}
		return candidate, true
	}

	// For bookish entries with a DBLP key, try the venue code (second-to-last
	// DBLP segment, e.g. "bled" from "conf/bled/2006") as the first keyword
	// candidate before falling back to title keywords.
	if BibTeXBookish.Contains(entry.FieldValue(EntryTypeField)) {
		if dblpKey := entry.FieldValue(DBLPField); dblpKey != "" {
			parts := strings.Split(dblpKey, "/")
			if len(parts) >= 2 && (parts[0] == "conf" || parts[0] == "journals") {
				keyword := stripNonAlphaNum.ReplaceAllString(TeXStringIndexer(parts[len(parts)-2]), "")
				if candidate, ok := tryCandidate(keyword); ok {
					return candidate
				}
			}
		}
	}

	keywords := titleKeywords(entry.FieldValue(TitleField))
	for length := 1; length <= len(keywords); length++ {
		for start := 0; start+length <= len(keywords); start++ {
			if candidate, ok := tryCandidate(strings.Join(keywords[start:start+length], "")); ok {
				return candidate
			}
		}
	}

	// Last resort: last two segments of the DBLP key joined
	// (e.g. "icac/X05a" → "icacx05a" from "conf/icac/X05a").
	if dblpKey := entry.FieldValue(DBLPField); dblpKey != "" {
		parts := strings.Split(dblpKey, "/")
		suffix := parts[len(parts)-1]
		if len(parts) >= 2 {
			suffix = parts[len(parts)-2] + suffix
		}
		keyword := stripNonAlphaNum.ReplaceAllString(TeXStringIndexer(suffix), "")
		if candidate, ok := tryCandidate(keyword); ok {
			return candidate
		}
	}

	if len(keywords) == 0 {
		l.ReportEntryWarning(entry.Key, WarningNoTitleKeywordsForPreferredAlias, base)
	} else {
		l.ReportEntryWarning(entry.Key, WarningCannotDeriveUniquePreferredAlias, base)
	}
	return ""
}

// setPreferredAlias sets alias as the preferred alias for entry, registering it
// in both KeyToKey and HintToKey.
func (l *TBibTeXLibrary) setPreferredAlias(entry *TBibTeXEntry, alias string) {
	l.setEntryField(entry, PreferredAliasField, alias)
	l.AddKeyAlias(alias, entry.Key)
	l.AddKeyHint(alias, entry.Key)
}

// CheckAndEnforcePreferredAlias validates and, when possible, corrects the preferred alias.
// Format rule: ^[a-z]+[0-9][0-9][0-9][0-9][a-z]([a-z0-9-]*[a-z0-9])?$  e.g. gordijn2002e3value or balau2026human-ai-balance
func (l *TBibTeXLibrary) CheckAndEnforcePreferredAlias(entry *TBibTeXEntry) {
	alias := entry.FieldValue(PreferredAliasField)

	if alias != "" {
		// Cross-check: alias must be registered as a hint.
		if !l.HintToKey.Contains(alias) {
			l.AddKeyHint(alias, entry.Key)
		}

		if validPreferredKeyAlias.MatchString(alias) {
			return
		}

		// Non-compliant alias: warn, try to derive a valid replacement.
		l.ReportEntryWarning(entry.Key, WarningInvalidPreferredKeyAlias, alias)
		if derived := l.derivePreferredAlias(entry); derived != "" {
			// Keep old non-compliant alias as a hint; set the derived one.
			l.setPreferredAlias(entry, derived)
			l.Progress(ProgressGeneratedPreferredAlias, derived, entry.Key)
		}
		return
	}

	if derived := l.derivePreferredAlias(entry); derived != "" {
		l.setPreferredAlias(entry, derived)
		l.Progress(ProgressGeneratedPreferredAlias, derived, entry.Key)
	}
}

func (l *TBibTeXLibrary) CheckTitlePresence(entry *TBibTeXEntry) {
	if entry.FieldValue(TitleField) == "" {
		l.ReportEntryWarning(entry.Key, WarningEmptyTitle)
	}
}


func (l *TBibTeXLibrary) CheckDOIPresence(entry *TBibTeXEntry) {
	foundDOI := entry.FieldValue("doi")

	if foundDOI == "" {
		if l.tryGetDOIFromURL(entry.Key, "url", &foundDOI) {
			l.Progress("Entry %s: extracted DOI from URL: %s", entry.Key, foundDOI)
			l.setEntryField(entry, "doi", foundDOI)
		}
	}
}

// noteURLPattern matches a \url{...} command embedded in a note field,
// optionally followed by whitespace-only brackets "[ ]" or "[]" (empty placeholder
// left by authors who confused \url{} with \href{}{} syntax). Brackets that
// contain actual text (e.g. "[Accessed: 2023-01-15]") are NOT consumed so that
// CheckNoteAccessed can still extract the date from what remains.
// Capturing group 1 is the raw URL content (may have leading/trailing whitespace).
var noteURLPattern = regexp.MustCompile(`\\url\{([^}]*)\}(?:\s*\[\s*\])?`)

// findNoteURL finds the first \url{...} in s and returns its position plus the trimmed URL.
// Returns (matchStart, matchEnd, url) where matchStart==-1 means no match, url=="" means
// the braces were empty (no action needed).
func findNoteURL(s string) (start, end int, url string) {
	m := noteURLPattern.FindStringSubmatchIndex(s)
	if m == nil {
		return -1, -1, ""
	}
	return m[0], m[1], strings.TrimSpace(s[m[2]:m[3]])
}

// applyNoteURLFix extracts the first \url{...} from the note field of a raw fields map,
// removes it from the note in-place (reusing stripAccessedSpan for separator cleanup),
// and returns the trimmed URL. Returns "" when no \url{} is present or the braces are empty.
func applyNoteURLFix(fields map[string]string) string {
	note := fields["note"]
	if note == "" {
		return ""
	}
	start, end, url := findNoteURL(note)
	if start == -1 || url == "" {
		return ""
	}
	fields["note"] = stripAccessedSpan(note, start, end)
	return url
}

// CheckNoteURL detects \url{...} embedded in the note field and handles it:
//   - No url field set → promote the embedded URL to the url field and clean the note.
//   - url field matches embedded URL → remove from note (redundant, silent).
//   - extracted URL is a DOI URL matching the doi field → remove from note (redundant, silent).
//   - url field differs and not DOI-redundant → remove from note, report warning.
//
// Called before CheckNoteAccessed (so the url field is set when urldate promotion is
// evaluated), before CheckURLRedundance (so the promoted URL feeds into the redundancy
// check), and before CheckYearFromURLDate (so the url field is established before the
// year is derived).
func (l *TBibTeXLibrary) CheckNoteURL(entry *TBibTeXEntry) {
	existingURL := entry.FieldValue("url")
	extractedURL := applyNoteURLFix(entry.Fields)
	if extractedURL == "" {
		return
	}
	l.setEntryField(entry, "note", entry.Fields["note"])

	switch {
	case existingURL == "":
		l.setEntryField(entry, "url", extractedURL)
	case strings.EqualFold(existingURL, extractedURL):
		// Redundant: url field already has this URL; note cleaned above is enough.
	case l.IsRedundantURL(extractedURL, entry.Key):
		// The note URL is just the doi field as a URL (https://doi.org/<doi>);
		// the doi field already captures this information — remove from note silently.
	default:
		l.ReportEntryWarning(entry.Key,
			`note contains \url{%s} but url field already has %s; removed from note`,
			extractedURL, existingURL)
	}
}

// CheckHowPublishedURL detects \url{...} embedded in the howpublished field and
// handles it the same way CheckNoteURL handles the note field:
//   - No url field set → promote the embedded URL to the url field and clean howpublished.
//   - url field matches embedded URL → remove from howpublished (redundant, silent).
//   - extracted URL is a DOI URL matching the doi field → remove silently.
//   - url field differs and not DOI-redundant → remove from howpublished, report warning.
//
// Called before CheckNoteAccessed and CheckURLPlausibility so that the extracted
// URL is established before urldate derivation occurs.
func (l *TBibTeXLibrary) CheckHowPublishedURL(entry *TBibTeXEntry) {
	how := entry.Fields["howpublished"]
	if how == "" {
		return
	}
	existingURL := entry.FieldValue("url")
	start, end, extractedURL := findNoteURL(how)
	if start == -1 || extractedURL == "" {
		return
	}
	cleaned := stripAccessedSpan(how, start, end)

	// If an access date remains in howpublished after the URL is stripped, promote it
	// to urldate (when not already set) and remove it from howpublished.
	if aStart, aEnd, isoDate := findAccessedDate(cleaned); aStart != -1 {
		cleaned = stripAccessedSpan(cleaned, aStart, aEnd)
		if entry.Fields["urldate"] == "" {
			entry.Fields["urldate"] = isoDate
			l.setEntryField(entry, "urldate", isoDate)
		}
	}

	entry.Fields["howpublished"] = cleaned
	l.setEntryField(entry, "howpublished", cleaned)

	switch {
	case existingURL == "":
		l.setEntryField(entry, "url", extractedURL)
	case strings.EqualFold(existingURL, extractedURL):
		// Redundant: url field already has this URL; howpublished cleaned above is enough.
	case l.IsRedundantURL(extractedURL, entry.Key):
		// The howpublished URL is just the doi field as a URL; remove silently.
	default:
		l.ReportEntryWarning(entry.Key,
			`howpublished contains \url{%s} but url field already has %s; removed from howpublished`,
			extractedURL, existingURL)
	}
}

// Accessed-date patterns, tried in order.
// accessedPatternYMD: YYYY-MM-DD / YYYY.MM.DD / YYYY/MM/DD
// accessedPatternDMY: DD.MM.YYYY / DD-MM-YYYY / DD/MM/YYYY
// accessedPatternWords: DD MonthName YYYY  (e.g. "14 June 2013")
// accessedPatternWordsAmerican: MonthName DD, YYYY  (e.g. "February 24, 2025")
// All accept an optional "Last " prefix before "Accessed", or the German "abgerufen am".
// An optional surrounding [...] is consumed so the bracket residue is removed.
var (
	accessedPrefix = `(?i)\[?\s*(?:(?:last\s+)?accessed\s*:?|abgerufen\s+am)\s*`

	accessedPatternYMD = regexp.MustCompile(
		accessedPrefix + `(\d{4})[./-](\d{2})[./-](\d{2})\s*\]?`,
	)
	accessedPatternDMY = regexp.MustCompile(
		accessedPrefix + `(\d{1,2})[./-](\d{1,2})[./-](\d{4})\s*\]?`,
	)
	accessedPatternWords = regexp.MustCompile(
		accessedPrefix + `(\d{1,2})\s+(january|february|march|april|may|june|july|august|september|october|november|december)\.?\s+(\d{4})\s*\]?`,
	)
	// accessedPatternWordsAmerican: MonthName DD, YYYY  (e.g. "February 24, 2025")
	accessedPatternWordsAmerican = regexp.MustCompile(
		accessedPrefix + `(january|february|march|april|may|june|july|august|september|october|november|december)\.?\s+(\d{1,2}),?\s+(\d{4})\s*\]?`,
	)
)

var monthNameToNumber = map[string]string{
	"january": "01", "february": "02", "march": "03", "april": "04",
	"may": "05", "june": "06", "july": "07", "august": "08",
	"september": "09", "october": "10", "november": "11", "december": "12",
}

// findAccessedDate tries all recognised Accessed-date patterns against s.
// Returns (matchStart, matchEnd, isoDate) where matchStart==-1 means no match.
func findAccessedDate(s string) (start, end int, isoDate string) {
	// YYYY-MM-DD (or YYYY.MM.DD etc.)
	if m := accessedPatternYMD.FindStringSubmatchIndex(s); m != nil {
		y, mo, d := s[m[2]:m[3]], s[m[4]:m[5]], s[m[6]:m[7]]
		return m[0], m[1], y + "-" + mo + "-" + d
	}
	// DD MonthName YYYY  — try before DMY so "14 June 2013" doesn't partially match DMY
	if m := accessedPatternWords.FindStringSubmatchIndex(s); m != nil {
		d, monName, y := s[m[2]:m[3]], strings.ToLower(s[m[4]:m[5]]), s[m[6]:m[7]]
		if mon, ok := monthNameToNumber[monName]; ok {
			if len(d) == 1 {
				d = "0" + d
			}
			return m[0], m[1], y + "-" + mon + "-" + d
		}
	}
	// MonthName DD, YYYY  (American order — e.g. "February 24, 2025")
	if m := accessedPatternWordsAmerican.FindStringSubmatchIndex(s); m != nil {
		monName, d, y := strings.ToLower(s[m[2]:m[3]]), s[m[4]:m[5]], s[m[6]:m[7]]
		if mon, ok := monthNameToNumber[monName]; ok {
			if len(d) == 1 {
				d = "0" + d
			}
			return m[0], m[1], y + "-" + mon + "-" + d
		}
	}
	// DD.MM.YYYY
	if m := accessedPatternDMY.FindStringSubmatchIndex(s); m != nil {
		d, mo, y := s[m[2]:m[3]], s[m[4]:m[5]], s[m[6]:m[7]]
		if len(d) == 1 {
			d = "0" + d
		}
		if len(mo) == 1 {
			mo = "0" + mo
		}
		return m[0], m[1], y + "-" + mo + "-" + d
	}
	return -1, -1, ""
}

// stripAccessedSpan removes the matched span [start:end] from s, along with any
// adjacent separators, and returns the cleaned string. If the result is only empty
// braces (e.g. "{}" or "{{}}"), returns "".
func stripAccessedSpan(s string, start, end int) string {
	cleaned := s[:start]
	if end < len(s) {
		rest := strings.TrimLeft(s[end:], " ,;.")
		if cleaned != "" && rest != "" {
			cleaned = strings.TrimRight(cleaned, " ,;.") + " " + rest
		} else {
			cleaned += rest
		}
	}
	cleaned = strings.TrimSpace(cleaned)
	// Collapse empty-brace residue: "{}", "{{}}", "{ }", "{{  }}", etc.
	// If stripping all braces and whitespace leaves nothing, the field is empty.
	if strings.Trim(cleaned, "{} \t") == "" {
		return ""
	}
	return cleaned
}

// applyNoteAccessedFix extracts an "Accessed: date" span from a raw fields map.
// Cleans the note in-place and sets urldate when appropriate (URL present, no stable
// identifier, urldate not already set). Returns the extracted ISO date, or "".
// Used for pre-normalising harvest candidates before display, and by CheckNoteAccessed.
func applyNoteAccessedFix(fields map[string]string) string {
	note := fields["note"]
	if note == "" {
		return ""
	}
	start, end, isoDate := findAccessedDate(note)
	if start == -1 {
		return ""
	}
	fields["note"] = stripAccessedSpan(note, start, end)

	if fields["url"] != "" &&
		fields[DBLPField] == "" &&
		fields["doi"] == "" &&
		fields["isbn"] == "" &&
		fields["issn"] == "" &&
		fields["urldate"] == "" {
		fields["urldate"] = isoDate
	}
	return isoDate
}

// CheckNoteAccessed detects "Accessed: YYYY-MM-DD" (and variants) in the note field,
// promotes the date to urldate when appropriate, and removes the accessed span from
// the note. Called before CheckURLDateNeed so the derived urldate feeds into that
// check, and before CheckYearFromURLDate so the promoted urldate is available for
// year derivation.
func (l *TBibTeXLibrary) CheckNoteAccessed(entry *TBibTeXEntry) {
	existing := entry.FieldValue("urldate")
	isoDate := applyNoteAccessedFix(entry.Fields)
	if isoDate == "" {
		return
	}

	// Always write the cleaned note back to the DB, regardless of urldate outcome.
	l.setEntryField(entry, "note", entry.Fields["note"])

	if existing != "" && existing != isoDate {
		// Auto-accept the later date: YYYY-MM-DD strings order lexicographically.
		if isoDate > existing {
			l.setEntryField(entry, "urldate", isoDate)
		}
		// existing >= isoDate: keep existing (already in DB; in-memory map correct).
		return
	}

	// No conflict: sync urldate to DB if applyNoteAccessedFix set it.
	if entry.Fields["urldate"] != existing {
		l.setEntryField(entry, "urldate", entry.Fields["urldate"])
	}
}

func (l *TBibTeXLibrary) CheckURLDateNeed(entry *TBibTeXEntry) {
	if entry.FieldValue("urldate") != "" {
		if entry.FieldValue("url") == "" ||
			entry.FieldValue(DBLPField) != "" ||
			entry.FieldValue("doi") != "" ||
			entry.FieldValue("isbn") != "" ||
			entry.FieldValue("issn") != "" {

			// In these cases, we do not need an urldate
			l.setEntryField(entry, "urldate", "")
		}
	}
}

func (l *TBibTeXLibrary) CheckBookishTitles(entry *TBibTeXEntry) {
	if !BibTeXBookish.Contains(entry.EntryType()) {
		return
	}

	title := entry.FieldValue(TitleField)
	booktitle := entry.FieldValue("booktitle")

	if title == booktitle {
		return
	}

	if booktitle == "" {
		l.setEntryField(entry, "booktitle", title)
		return
	}

	if title == "" {
		l.setEntryField(entry, TitleField, booktitle)
		return
	}

	// Both non-empty and different: resolve and assign the winner to both fields.
	winner := l.MaybeResolveFieldValue(entry.Key, entry.Key, "booktitle", title, booktitle)
	if winner != booktitle {
		l.setEntryField(entry, "booktitle", winner)
	}
	if winner != title {
		l.setEntryField(entry, TitleField, winner)
	}
	// if strings.Contains(l.EntryFields[key]["booktitle"], "proc.") || strings.Contains(l.EntryFields[key]["booktitle"], "Proc.") ||
	//
	//		strings.Contains(l.EntryFields[key]["booktitle"], "proceedings") || strings.Contains(l.EntryFields[key]["booktitle"], "Proceedings") ||
	//		strings.Contains(l.EntryFields[key]["booktitle"], "workshop") || strings.Contains(l.EntryFields[key]["booktitle"], "Workshop") ||
	//		strings.Contains(l.EntryFields[key]["booktitle"], "conference") || strings.Contains(l.EntryFields[key]["booktitle"], "Conference") {
	//		if l.EntryFields[key][TitleField] == l.EntryFields[key]["booktitle"] {
	//			if entryType != "proceedings" {
	//				fmt.Println("Expected a proceedings", key)
	//				l.SetEntryType(key, "proceedings")
	//			}
	//		} else {
	//			if entryType != "inproceedings" {
	//				fmt.Println("Expected an inproceedings", key)
	//				l.SetEntryType(key, "inproceedings")
	//			}
	//		}
	//	}
}

// Harmonize with tryGetDOIFromURL ???
// Config based ... needs a bit of work I guess ....
func (l *TBibTeXLibrary) CheckEPrint(entry *TBibTeXEntry) {
	EPrintTypeValueity := strings.ToLower(entry.FieldValue("eprinttype"))
	EPrintValueity := entry.FieldValue("eprint")

	DOIValueity := entry.FieldValue("doi")
	URLValueity := entry.FieldValue("url")

	DOIValueLower := strings.ToLower(DOIValueity)
	URLValueLower := strings.ToLower(URLValueity)

	OnArXive := EPrintTypeValueity == "arxiv" ||
		/*   */ strings.HasPrefix(DOIValueLower, "10.48550/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://arxiv.org/abs/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://doi.org/10.48550/")

	OnJstor := EPrintTypeValueity == "jstor" ||
		/*   */ strings.HasPrefix(DOIValueLower, "10.2307/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://doi.org/10.2307/") ||
		/*   */ strings.HasPrefix(URLValueLower, "http://www.jstor.org/stable/") ||
		/*   */ strings.HasPrefix(URLValueLower, "https://www.jstor.org/stable/")

	switch {
	case OnArXive:
		EPrintTypeValue := "arXiv"
		EPrintValue := EPrintValueity

		if EPrintValue != "" {
			EPrintValue = strings.ReplaceAll(strings.ToLower(EPrintValue), "arxiv:", "")
		}

		if EPrintValue == "" && DOIValueLower != "" {
			EPrintValue = strings.ReplaceAll(DOIValueLower, "10.48550/arxiv.", "")

			if EPrintValue == DOIValueLower {
				EPrintValue = ""
			}
		}

		if EPrintValue == "" && URLValueLower != "" {
			EPrintValue = strings.ReplaceAll(URLValueLower, "https://arxiv.org/abs/", "")

			if EPrintValue == URLValueLower {
				EPrintValue = ""
			}
		}

		if EPrintValue == "" {
			l.ReportEntryWarning(entry.Key, "Not able to find eprint data.")
		} else {
			if DOIValueity == "" {
				DOIValueity = "10.48550/arXiv." + EPrintValue
			}
		}

		l.setEntryField(entry, "eprinttype", EPrintTypeValue)
		l.setEntryField(entry, "eprint", EPrintValue)
		l.setEntryField(entry, "doi", DOIValueity)

	case OnJstor:
		EPrintTypeValue := "jstor"
		EPrintValue := EPrintValueity

		if EPrintValue == "" {
			EPrintValue = strings.ReplaceAll(DOIValueLower, "10.2307/", "")

			if EPrintValue == "" {
				EPrintValue = strings.ReplaceAll(URLValueLower, "https://doi.org/10.2307/", "")

				if EPrintValue == "" {
					EPrintValue = strings.ReplaceAll(EPrintValue, "http://www.jstor.org/stable/", "")

					if EPrintValue == "" {
						EPrintValue = strings.ReplaceAll(EPrintValue, "https://www.jstor.org/stable/", "")

						if EPrintValue == "" {
							l.ReportEntryWarning(entry.Key, "Not able to find eprint data.")
						}
					}
				}
			}
		}

		l.setEntryField(entry, "eprinttype", EPrintTypeValue)
		l.setEntryField(entry, "eprint", EPrintValue)

	default:
		if (EPrintTypeValueity != "" && EPrintValueity == "") || (EPrintTypeValueity == "" && EPrintValueity != "") {
			l.setEntryField(entry, "eprinttype", "")
			l.setEntryField(entry, "eprint", "")
		}
	}
}

func (l *TBibTeXLibrary) CheckISBNFromDOI(entry *TBibTeXEntry) {
	DOIValueity := entry.FieldValue("doi")
	if !strings.HasPrefix(DOIValueity, "10.1007/978-") {
		return
	}

	ISBNCandidate := strings.ReplaceAll(DOIValueity, "10.1007/", "")
	if !IsValidISBN(ISBNCandidate) {
		return
	}

	crossrefRAW := entry.FieldValue("crossref")
	if crossrefRAW == "" {
		l.UpdateEntryFieldAlias(entry.Key, "isbn", entry.FieldValue("isbn"), ISBNCandidate)
		l.setEntryField(entry, "isbn", ISBNCandidate)
		return
	}

	// The doi is a book-level Springer doi; isbn belongs on the parent, not this child.
	crossrefKey := l.MapEntryKey(crossrefRAW)
	if crossrefKey == "" {
		crossrefKey = crossrefRAW
	}
	crossrefEntry := l.buildEntry(crossrefKey)
	if !crossrefEntry.Exists() {
		return
	}
	parentISBN := crossrefEntry.FieldValue("isbn")
	switch {
	case parentISBN == "":
		l.UpdateEntryFieldAlias(crossrefKey, "isbn", "", ISBNCandidate)
		l.setEntryField(crossrefEntry, "isbn", ISBNCandidate)
		l.deleteEntryField(entry, "doi")
	case parentISBN == ISBNCandidate:
		// doi already accounted for on parent; child doi will be cleaned by CheckCrossrefDOI
	default:
		l.ReportEntryWarning(entry.Key, WarningISBNMismatchFromCrossrefDOI, crossrefKey, ISBNCandidate, parentISBN)
		l.EntryInvolvedInWarning(crossrefKey)
	}
}

func (l *TBibTeXLibrary) CheckCrossrefInheritableField(crossrefEntry, entry *TBibTeXEntry, field string) {
	if BibTeXMustInheritFields.Contains(field) {
		if challenge, hasChallenge := entry.Fields[field]; hasChallenge {
			parentValue := crossrefEntry.FieldValue(field)
			var target string
			if parentValue == "" {
				// Parent has no value: silently move child's value up — no question needed.
				target = challenge
			} else {
				target = l.MaybeResolveFieldValue(crossrefEntry.Key, entry.Key, field, challenge, parentValue)
			}

			if target != "" {
				l.setEntryField(crossrefEntry, field, target)
			}

			if field == "booktitle" {
				currentTitle := crossrefEntry.FieldValue(TitleField)
				var newTitle string
				if currentTitle == "" {
					newTitle = target
				} else {
					newTitle = l.MaybeResolveFieldValue(crossrefEntry.Key, entry.Key, field, target, currentTitle)
				}

				if currentTitle != newTitle {
					l.TitleIndex.DeleteValueFromStringSetMap(TeXStringIndexer(currentTitle), crossrefEntry.Key)

					l.setEntryField(crossrefEntry, TitleField, newTitle)
					l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(newTitle), crossrefEntry.Key)
				}
			}

			for otherChallenger := range l.EntryFieldSourceToTarget[entry.Key][field] {
				l.AddEntryFieldAlias(crossrefEntry.Key, field, otherChallenger, target, false)
			}

			// Always clear the child's copy of a MustInheritField — the child is not
			// permitted to hold it regardless of how the parent/child resolution went.
			// The pre-DB-migration code never hit the empty-parent case (both sides were
			// always populated from the bib file), so `target` was always non-empty and
			// the guard was never needed. In DB-primary mode entries can be added
			// individually, so the parent may lack the field → target can be "" →
			// the guard would wrongly preserve the child's copy.
			l.deleteEntryField(entry, field)
			delete(l.EntryFieldSourceToTarget[entry.Key], field)
		}
	} else if BibTeXMayInheritFields.Contains(field) {
		if crossrefValue, hasCrossrefValue := crossrefEntry.Fields[field]; hasCrossrefValue {
			// No need to override the child value, when it is the same as the parent's value
			if crossrefValue == entry.Fields[field] {
				l.setEntryField(entry, field, "")
			}
		}
	}
}

// CheckCrossrefDOI drops the child's DOI when it is the parent's ISBN-based Springer DOI.
// The may-inherit logic handles the case where the parent explicitly stores the same DOI;
// this covers the case where the parent has an isbn but no doi field.
func (l *TBibTeXLibrary) CheckCrossrefDOI(crossrefEntry, entry *TBibTeXEntry) {
	childDOI := entry.FieldValue("doi")
	if childDOI == "" {
		return
	}
	parentISBN := crossrefEntry.FieldValue("isbn")
	if parentISBN != "" && childDOI == "10.1007/"+parentISBN {
		l.deleteEntryField(entry, "doi")
	}
}

func (l *TBibTeXLibrary) CheckCrossref(entry *TBibTeXEntry) {
	entryType := entry.EntryType()
	crossrefetyRAW := entry.FieldValue("crossref")

	crossrefety := l.MapEntryKey(crossrefetyRAW)
	if crossrefety == "" {
		crossrefety = crossrefetyRAW
	}

	if crossrefety == entry.Key {
		l.ReportEntryWarning(entry.Key, "Found self-referencing crossref; cleaned up.")
		l.setEntryField(entry, "crossref", "")
		crossrefety = ""
	}

	if allowedCrossrefType, hasAllowedCrossrefType := BibTeXCrossrefType[entryType]; hasAllowedCrossrefType {
		if crossrefety != "" {
			if CrossrefType := l.EntryType(crossrefety); CrossrefType != "" {
				if allowedCrossrefType == CrossrefType {
					crossrefEntry := l.buildEntry(crossrefety)
					for field := range BibTeXInheritableFields.Elements() {
						l.CheckCrossrefInheritableField(crossrefEntry, entry, field)
					}

					l.CheckCrossrefDOI(crossrefEntry, entry)
					l.CheckBookishTitles(crossrefEntry)
				} else {
					l.ReportEntryWarning(entry.Key, "Crossref to %s (%s) does not comply to typing rules.", crossrefety, CrossrefType)
				l.EntryInvolvedInWarning(crossrefety)
				}
			} else {
				l.ReportEntryWarning(entry.Key, "Crossref target %s does not exist.", crossrefety)
			}
		}
	} else if crossrefety != "" {
		l.ReportEntryWarning(entry.Key, "Entry type %s does not support crossref (pointing to %s).", entryType, crossrefety)
		l.EntryInvolvedInWarning(crossrefety)
	}
}

//// How does this relate???
//func (l *TBibTeXLibrary) CheckFileReferences(key, otherKey string) {
//	upsertBibEntryField(key, LocalURLField, l.ResolveFileReferences(key, otherKey))
//}
func (l *TBibTeXLibrary) CheckFileReference(entry *TBibTeXEntry) {
	l.setEntryField(entry, LocalURLField, l.ResolveFileReferences(entry.Key, entry.Key))
}

func (l *TBibTeXLibrary) CheckISSN(entry *TBibTeXEntry) {
	issn := entry.FieldValue("issn")

	if issn == "" || IsValidISSN(issn) {
		return
	}

	l.ReportEntryWarning(entry.Key, WarningBadISSN, issn)
}

func (l *TBibTeXLibrary) CheckISBN(entry *TBibTeXEntry) {
	isbn := entry.FieldValue("isbn")

	if isbn == "" || IsValidISBN(isbn) {
		return
	}

	if IsValidISSN(isbn) {
		l.Progress("Moving ISSN-shaped isbn %q to issn field for %s", isbn, entry.Key)
		l.setEntryField(entry, "issn", isbn)
		l.deleteEntryField(entry, "isbn")
		return
	}

	l.ReportEntryWarning(entry.Key, WarningBadISBN, isbn)
}

func (l *TBibTeXLibrary) CheckChapter(entry *TBibTeXEntry) {
	chapter := entry.FieldValue("chapter")
	if chapter == "" || IsValidChapter(chapter) {
		return
	}
	l.ReportEntryWarning(entry.Key, "Non-numeric chapter %q (must be Arabic or Roman numeral)", chapter)
}

// CheckYearFromURLDate derives the year from the urldate when the year field is
// absent or invalid, but only for standalone entries (no crossref). For crossref
// children, urldate is an access date — publication year must come from the parent.
// Must run after CheckNoteAccessed so that a urldate promoted from the note field
// is available here.
func (l *TBibTeXLibrary) CheckYearFromURLDate(entry *TBibTeXEntry) {
	if entry.FieldValue("crossref") != "" {
		return
	}

	year := entry.FieldValue("year")
	if year != "" && IsValidYear(year) {
		return
	}

	urldate := entry.FieldValue("urldate")
	if len(urldate) >= 4 && IsValidYear(urldate[:4]) {
		l.setEntryField(entry, "year", urldate[:4])
	}
}

func (l *TBibTeXLibrary) CheckYear(entry *TBibTeXEntry) {
	year := entry.FieldValue("year")

	if year == "" || IsValidYear(year) {
		return
	}

	l.ReportEntryWarning(entry.Key, WarningBadYear, year)
}


var (
	urlDateDMY     = regexp.MustCompile(`^(\d{2})[./](\d{2})[./](\d{4})$`)
	urlDateYMDSlash = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2})$`)
	urlDatePrefix  = regexp.MustCompile(`(?i)^(accessed|last accessed|last visited|zugegriffen|abgerufen|aufgerufen)[:\s]+`)
)

// normaliseURLDate attempts to convert common non-ISO urldate formats to YYYY-MM-DD.
// It strips access-verb prefixes (English and German) then tries DD.MM.YYYY,
// DD/MM/YYYY, and YYYY/MM/DD. Returns the normalised date and true on success.
func normaliseURLDate(date string) (string, bool) {
	trimmed := urlDatePrefix.ReplaceAllString(strings.TrimSpace(date), "")
	trimmed = strings.TrimSpace(trimmed)
	if m := urlDateDMY.FindStringSubmatch(trimmed); m != nil {
		return m[3] + "-" + m[2] + "-" + m[1], true
	}
	if m := urlDateYMDSlash.FindStringSubmatch(trimmed); m != nil {
		return m[1] + "-" + m[2] + "-" + m[3], true
	}
	return "", false
}

func (l *TBibTeXLibrary) CheckURLDate(entry *TBibTeXEntry) {
	date := entry.FieldValue("urldate")

	if date == "" || IsValidDate(date) {
		return
	}

	if normalised, ok := normaliseURLDate(date); ok && IsValidDate(normalised) {
		l.setEntryField(entry, "urldate", normalised)
		return
	}

	l.ReportEntryWarning(entry.Key, WarningBadDate, date)
}

func (l *TBibTeXLibrary) CheckWithdrawn(entry *TBibTeXEntry) {
	date := entry.FieldValue("withdrawn")
	if date == "" {
		return
	}

	if !IsValidDate(date) {
		l.ReportEntryWarning(entry.Key, "Invalid date %q in withdrawn field.", date)
		return
	}

	if entry.FieldValue("author") != "{Withdrawn publication}" {
		if l.WarningYesNoQuestion("Set author to {Withdrawn publication}", "Entry %s has withdrawn date but author is not {Withdrawn publication}", entry.Key) {
			l.setEntryField(entry, "author", "{Withdrawn publication}")
			if entry.FieldValue("note") == "Withdrawn." {
				l.setEntryField(entry, "note", "")
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckNeedToSplitBookishEntry(keyRAW string) string {
	key := l.MapEntryKey(keyRAW) // Dealias, while we are likely to do this immediately after a merge (for now)
	// After merging all doubles, we can do this as part of the consistency check and CheckCrossref in particular, and then don't need to dealias.

	entryTypeForSplit := l.EntryType(key)
	if BibTeXCrossreffer.Contains(entryTypeForSplit) && entryTypeForSplit != "misc" {
		crossrefKey := l.EntryFieldValueity(l.MapEntryKey(key), "crossref")
		if crossrefKey == "" {
			entryType := entryTypeForSplit

			bookTitle := l.EntryFieldValueity(l.MapEntryKey(key), "booktitle")
			if bookTitle == "" {
				l.Warning("Empty booktitle for a bookish entry %s of type %s", key, entryType)
			} else {
				crossrefType := BibTeXCrossrefType[entryType]
				crossrefKey = l.NewKey()
				l.KeyIsTemporary.Add(crossrefKey)

				upsertBibEntryField(crossrefKey, TitleField, bookTitle)
				upsertBibEntryField(crossrefKey, "booktitle", bookTitle)
				l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(bookTitle), crossrefKey)
				l.SetEntryType(crossrefKey, crossrefType)

				l.SetEntryFieldValue(key, "crossref", crossrefKey)

				// Check for an existing library entry with the same title so the
				// split-off parent does not silently duplicate a known publication.
				// Re-resolve after a potential merge: the surviving entry becomes
				// the effective crossref target even if key still stores the temp key.
				l.CheckNeedToMergeForEqualTitles(crossrefKey)
				crossrefKey = l.MapEntryKey(crossrefKey)

				return crossrefKey
			}
		}
	}

	return ""
}

func (l *TBibTeXLibrary) CheckNeedToMergeForEqualTitles(key string) {
	// Why not do l.MapEntryKey(key) always as part of l.EntryFieldValueity ???
	title := l.EntryFieldValueity(l.MapEntryKey(key), TitleField)
	if title != "" {
		if l.IgnoredTitleIndexes.Contains(TeXStringIndexer(title)) {
			return
		}
		// Should be via a function!
		Keys := l.TitleIndex[TeXStringIndexer(title)]
		if Keys.Size() > 1 {
			sortedKeys := Keys.ElementsSorted()
			for _, a := range sortedKeys {
				if a == l.MapEntryKey(a) {
					for _, b := range sortedKeys {
						if b == l.MapEntryKey(b) {
							l.MaybeMergeEntries(l.MapEntryKey(a), l.MapEntryKey(b))
						}
					}
				}
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckKeyValidity(entry *TBibTeXEntry) {
	if !IsValidKey(entry.Key) {
		l.ReportEntryWarning(entry.Key, WarningInvalidKey)
	}
	if entryType := entry.EntryType(); entryType != "" {
		if _, known := BibTeXAllowedEntryFields[entryType]; !known {
			if mapped, hasMapped := BibTeXEntryMap[entryType]; hasMapped {
				l.Progress(ProgressFixedEntryType, entry.Key, entryType, mapped)
				l.SetEntryType(entry.Key, mapped)
				entry.Fields[EntryTypeField] = mapped
			} else {
				l.Warning(WarningUnknownEntryType, entry.Key, entryType)
			}
		}
	}
}

func (l *TBibTeXLibrary) CheckDBLP(keyRAW string) {
	key := l.MapEntryKey(keyRAW) // Needed??

	l.MaybeFixDBLPEntry(key)

	entryType := l.EntryType(key)
	entryDBLP := l.EntryFieldValueity(key, DBLPField)

	if BibTeXCrossreffer.Contains(entryType) {
		crossrefKey := l.EntryFieldValueity(key, "crossref")
		crossrefDBLP := l.EntryFieldValueity(crossrefKey, DBLPField)

		parentDBLP := l.MaybeGetDBLPCrossref(entryDBLP)
		parentKey := l.LookupDBLPKey(parentDBLP)

		if parentKey != "" && crossrefKey != parentKey {
			// Only redirect to parentKey when the current crossref has a DBLP key
			// (safe to update within the DBLP hierarchy) or no longer exists.
			// When crossrefKey is a live non-DBLP proceedings, skip the redirect —
			// the crossrefDBLP block below will assign the DBLP parent key to it
			// directly, keeping it as the canonical parent without orphaning it.
			if crossrefKey == "" || !l.EntryExists(crossrefKey) ||
				l.EntryFieldValueity(crossrefKey, DBLPField) != "" {
				l.SetEntryFieldValue(key, "crossref", parentKey)
				crossrefKey = parentKey
				crossrefDBLP = parentDBLP
			}
		}

		if crossrefDBLP == "" && parentDBLP != "" {
			l.SetEntryFieldValue(crossrefKey, DBLPField, parentDBLP)
			crossrefDBLP = parentDBLP
		}

		if crossrefKey == "" && entryType != "misc" && !l.HasMetadata(key, MetaPropDblpKeyMissing) {
			l.Warning("Crossref entry type without a crossref %s", key)
		}

		if entryDBLP != "" && crossrefDBLP == "" && !strings.HasPrefix(entryDBLP, "homepages/") {
			l.Warning("Parent entry %s does not have a dblp key, while the child %s does have dblp key %s", crossrefKey, key, entryDBLP)
		}

	}

	// Add parent to child check for bookish (suppressed by no_dblp_children flag).
	if BibTeXBookish.Contains(entryType) && !l.EntryHasFlag(key, EntryFlagNoDBLPChildren) && entryDBLP != "" {
		children := readDblpCrossrefChildren(entryDBLP)
		if len(children) > 0 {
			ticker := l.NewProgressTicker(fmt.Sprintf("Checking %d children of %s", len(children), entryDBLP), len(children))
			for _, childDBLP := range children {
				childKey := l.LookupDBLPKey(childDBLP)
				if childKey != "" {
					// Same guard as MaybeAddDBLPEntry: don't redirect away from a live
					// non-DBLP proceedings — that would orphan it as a lone proceedings.
					oldCrossref := l.EntryFieldValueity(childKey, "crossref")
					if oldCrossref == "" || !l.EntryExists(oldCrossref) ||
						l.EntryFieldValueity(oldCrossref, DBLPField) != "" {
						l.SetEntryFieldValue(childKey, "crossref", key)
					}
				} else {
					l.MaybeAddDBLPChildEntry(childDBLP, key)
				}
				ticker.Step()
			}
			ticker.Done()
		}

		// Check that every library child of this DBLP-keyed parent also has a DBLP key.
		if entryDBLP != "" {
			forEachLibraryChildOf(key, func(childKey string) {
				// Re-resolve: the child may have been merged during the child-import loop above.
				childKey = l.MapEntryKey(childKey)
				if !l.EntryExists(childKey) {
					return
				}
				if l.EntryFieldValueity(childKey, DBLPField) == "" && !l.DblpWaived.Contains(childKey) {
					msg := fmt.Sprintf(WarningNoDblpKeyForChild, key, entryDBLP)
					l.ReportEntryWarning(childKey, "%s", msg)
					l.EntryInvolvedInWarning(key)
					fmt.Fprintf(os.Stderr, "\nChild entry:\n%s\nParent entry:\n%s\n",
						l.entryDisplayString(childKey), l.entryDisplayString(key))
					// First offer unassigned DBLP children of the parent as candidates.
					var candidates []string
					for _, c := range readDblpCrossrefChildren(entryDBLP) {
						if l.LookupDBLPKey(c) == "" {
							candidates = append(candidates, c)
						}
					}
					if len(candidates) > 9 {
						candidates = candidates[:9]
					}
					resolved := false
					if len(candidates) > 0 {
						if chosen := l.AskCandidateDblpKey(childKey, candidates); chosen != "" {
							l.AssociateDblpKey(childKey, chosen)
							resolved = true
						}
					}
					// No candidate selected: offer manual key entry, waive, or skip.
					if !resolved {
						childOptions := TStringSetNew()
						childOptions.Add("k", "y", "n")
						switch l.WarningQuestion(QuestionNoDblpKeyForChildAction, childOptions, "") {
						case "k":
							if dblpKey, err := Reporting.AskForInput("DBLP key"); err == nil && dblpKey != "" {
								if dblpEntryFromFile(dblpKey) == nil {
									l.Warning("DBLP key %q not found in file store", dblpKey)
								} else {
									l.AssociateDblpKey(childKey, dblpKey)
									sessionManualDblpAssignments++
									deleteEntryWarning(childKey, msg)
									deleteEntryWarning(key, "")
								}
							}
						case "y":
							l.DblpWaived.Set(childKey, true)
							// Waived: remove from entry_warnings so it doesn't appear in repair.bib.
							deleteEntryWarning(childKey, msg)
							deleteEntryWarning(key, "")
						}
					}
				}
			})
		}
	}
}

func (l *TBibTeXLibrary) NormaliseEntryFields(entry *TBibTeXEntry) {
	for field, current := range entry.Fields {
		if field == EntryTypeField || field == LocalURLField || field == PreferredAliasField || current == "" {
			continue
		}
		if normalised := l.NormaliseFieldValue(field, current); normalised != current {
			l.setEntryField(entry, field, normalised)
		}
	}
}

func (l *TBibTeXLibrary) CheckEntry(entry *TBibTeXEntry) {
	if entry.Exists() {
		originalType := entry.EntryType()
		l.CheckKeyValidity(entry)

		// CheckCrossref can lead to a merger of entries for now ...
		if entry.Exists() && l.EntryExists(entry.Key) {
			if !l.InteractionIsOff() {
				l.CheckIfFieldsAreAllowed(entry, func(key, field, value string) {
					if l.ignoreIllegalFields || l.WarningYesNoQuestion(QuestionIgnore, WarningIllegalField, field, value, key, entry.EntryType()) {
						l.deleteEntryField(entry, field)
					} else if !l.QuitWasRequested() {
						l.Warning("Stopping programme. Please fix this manually.")
						os.Exit(0)
					}
				})
				if l.QuitWasRequested() {
					if currentType := entry.EntryType(); currentType != originalType {
						l.SetEntryType(entry.Key, originalType)
						entry.Fields[EntryTypeField] = originalType
					}
					return
				}
			}
			l.NormaliseEntryFields(entry)
			l.MaybeApplyFieldMappings(entry, true)
			l.CheckDOIPresence(entry)
			l.CheckEPrint(entry)
			l.CheckNoteURL(entry)
			l.CheckHowPublishedURL(entry)
			l.CheckNoteAccessed(entry)
			l.CheckURLPlausibility(entry)
			l.CheckYearFromURLDate(entry)
			l.CheckTitleFromURL(entry)
			l.CheckCrossref(entry)
			l.CheckAndEnforcePreferredAlias(entry)
			l.CheckBookishTitles(entry)
			l.CheckISBNFromDOI(entry)
			l.CheckTitlePresence(entry)
			l.CheckURLRedundance(entry)
			l.CheckURLDateNeed(entry)
			l.CheckISSN(entry)
			l.CheckISBN(entry)
			l.CheckChapter(entry)
			l.CheckYear(entry)
			l.CheckURLDate(entry)
			l.CheckWithdrawn(entry)
			l.CheckGarbledContributors(entry)
		}
	}
}

// CheckGarbledContributors warns when an entry has author or editor contributors
// that were stored as garbled (the raw name could not be parsed as a valid BibTeX
// person name and was brace-wrapped during migration or harvest). The warning
// appears as homework so the user can fix the underlying data.
func (l *TBibTeXLibrary) CheckGarbledContributors(entry *TBibTeXEntry) {
	if !contributorRolesActive {
		return
	}
	rows, err := bibQuery(
		`SELECT cr.role, c.name
		 FROM contributor_roles cr
		 JOIN contributors c ON c.id = cr.contributor_id
		 WHERE cr.entry_key = ? AND c.garbled = 1
		 ORDER BY cr.role, cr.position`,
		entry.Key)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var role, name string
		if rows.Scan(&role, &name) != nil {
			continue
		}
		l.ReportEntryWarning(entry.Key, "Garbled %s name (needs fixing): %s", role, name)
	}
}

func (l *TBibTeXLibrary) CheckEntries() {
	total := countBibEntries()
	ticker := l.NewProgressTicker(ProgressCheckingConsistencyOfEntries, total)

	forEachBibEntryKey(func(key string) bool {
		ticker.Step()
		l.ResetQuestionFlag()
		l.CheckEntry(l.buildEntry(key))
		return !Reporting.QuitWasRequested()
	})

	ticker.Done()
}

// RenormaliseNameFields reloads name_mappings from the DB (picking up any mappings
// added or changed since the library was opened) then sweeps all author and editor
// values in bib_entries through the updated normaliser. Called after
// -import_name_mappings / -add_name_mappings to apply new mappings immediately
// without requiring a separate bib.check run (§11.3 cascade).
func (l *TBibTeXLibrary) RenormaliseNameFields() {
	loadContributorsFromDb(l)
	l.CheckNameMappingConsistency()

	total := countBibEntries()
	ticker := l.NewProgressTicker("Re-normalising author/editor fields", total)
	forEachBibEntryKey(func(key string) bool {
		ticker.Step()
		entry := l.buildEntry(key)
		for _, field := range []string{"author", "editor"} {
			current := entry.FieldValue(field)
			if current == "" {
				continue
			}
			if normalised := l.NormaliseFieldValue(field, current); normalised != current {
				l.setEntryField(entry, field, normalised)
			}
		}
		return true
	})
	ticker.Done()
}

// CheckDuplicateDBLPKeys detects library entries that share the same DBLP key and
// offers to merge each duplicate set. Both entries are recorded in entry_warnings so
// they appear in warnings; select results.
func (l *TBibTeXLibrary) CheckDuplicateDBLPKeys() {
	forEachDuplicateDBLPKey(func(dblpKey string, keys []string) {
		keySet := TStringSetNew()
		for _, k := range keys {
			resolved := l.MapEntryKey(k)
			if resolved == "" {
				resolved = k
			}
			keySet.Add(resolved)
		}
		if keySet.Size() < 2 {
			return // already merged or aliased
		}
		for key := range keySet.Elements() {
			l.ReportEntryWarning(key, "Duplicate DBLP key %s (also on: %s).", dblpKey,
				strings.Join(func() []string {
					var others []string
					for k := range keySet.Elements() {
						if k != key {
							others = append(others, k)
						}
					}
					return others
				}(), ", "))
		}
		l.MaybeMergeEntrySet(keySet)
	})
}

// CheckLoneProceedings finds @proceedings entries that have no children (no entry
// has a crossref pointing to them) and are not waived. For each, it offers the user:
//   w — waive: record FlagLoneProceedingsWaived in entry_flags, skip in future runs
//   d — delete: remove the entry plus all hints and oldies pointing to it
//   s/enter — skip for now
func (l *TBibTeXLibrary) CheckLoneProceedings() {
	// Build the set of canonical crossref targets used by any library entry.
	crossrefTargets := TStringSetNew()
	forEachBibEntryKey(func(key string) bool {
		if cr := l.EntryFieldValueity(key, "crossref"); cr != "" {
			crossrefTargets.Add(l.MapEntryKey(cr))
		}
		return true
	})

	// addDblpChildrenForKey imports all DBLP children of libraryKey whose proceedings
	// has the given dblpKey. Only reports progress when something actually changes.
	// Returns true when at least one child was found in the DBLP file store.
	addDblpChildrenForKey := func(libraryKey, dblpKey string) bool {
		children := readDblpCrossrefChildren(dblpKey)
		if len(children) == 0 {
			return false
		}
		added := 0
		crossrefSet := 0
		for _, childDBLP := range children {
			if childKey := l.LookupDBLPKey(childDBLP); childKey != "" {
				if l.EntryFieldValueity(childKey, "crossref") != libraryKey {
					l.SetEntryFieldValue(childKey, "crossref", libraryKey)
					crossrefSet++
				}
			} else {
				if newKey := l.MaybeAddDBLPChildEntry(childDBLP, libraryKey); newKey != "" {
					doAllChecks(newKey)
					added++
				}
			}
		}
		if added > 0 || crossrefSet > 0 {
			l.Progress("Children of %s: %d added, %d crossref(s) updated", dblpKey, added, crossrefSet)
		}
		return true
	}

	validAnswers := TStringSetNew()
	validAnswers.Add("w", "d", "k", "s")
	forEachBibEntryKey(func(key string) bool {
		if l.EntryFieldValueity(key, EntryTypeField) != "proceedings" {
			return true
		}
		if crossrefTargets.Contains(key) {
			return true
		}
		if l.EntryHasFlag(key, FlagLoneProceedingsWaived) {
			return true
		}

		// A proceedings with a PDF file is not lone — it has real content.
		if FileExists(l.FilesRoot + l.FilesFolder + key + ".pdf") {
			return true
		}

		// If the entry is anchored to a DBLP key, it is not spurious.
		// Try to add any known children, then skip the prompt regardless —
		// we must never offer to delete a DBLP-backed proceedings entry.
		if dblpKey := l.EntryFieldValueity(key, DBLPField); dblpKey != "" {
			addDblpChildrenForKey(key, dblpKey)
			return true
		}

		// No DBLP key yet — try the title-hash search before offering the prompt.
		if l.EntryFieldValueity(key, DBLPField) == "" {
			if maybeFindDBLPCandidates(key) {
				if dblpKey := l.EntryFieldValueity(key, DBLPField); dblpKey != "" {
					addDblpChildrenForKey(key, dblpKey)
				}
				return true
			}
		}

		l.Warning(WarningLoneProceedings, key)
		fmt.Fprint(os.Stderr, l.entryDisplayString(key))

		switch l.WarningQuestion(QuestionLoneProceedings, validAnswers, "") {
		case "k":
			if dblpKey, err := Reporting.AskForInput("DBLP key"); err == nil && dblpKey != "" {
				if dblpEntryFromFile(dblpKey) == nil {
					l.Warning("DBLP key %q not found in file store", dblpKey)
				} else {
					Library.AssociateDblpKey(key, dblpKey)
					sessionManualDblpAssignments++
					if newDblpKey := l.EntryFieldValueity(key, DBLPField); newDblpKey != "" {
						addDblpChildrenForKey(key, newDblpKey)
					}
				}
			}
		case "w":
			l.SetEntryFlag(key, FlagLoneProceedingsWaived)
		case "d":
			l.DeleteEntry(key)
		}

		return !Reporting.QuitWasRequested()
	})
}

