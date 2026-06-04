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

	spinner := l.NewSpinner(ProgressCheckingFieldMappings)
	done := 0

	for field, valueMapping := range l.GenericFieldSourceToTarget {
		done++
		spinner.Update(done, total)
		l.checkValueMapping(valueMapping, l.GenericFieldTargetToSource[field], ".")
	}

	for key, fieldValueMapping := range l.EntryFieldSourceToTarget {
		for field, valueMapping := range fieldValueMapping {
			done++
			spinner.Update(done, total)
			l.checkValueMapping(valueMapping, l.EntryFieldTargetToSource[key][field], WarningMappingForKey+key+".")
		}
	}

	spinner.Stop()
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
	var fixes []mismatch
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
			for _, winner := range challengerMap {
				if seen[winner] || winner == actual {
					continue
				}
				seen[winner] = true
				fixes = append(fixes, mismatch{entry, field, winner, actual})
			}
		}
	}

	for _, m := range fixes {
		l.Warning(WarningEntryFieldMappingWinnerMismatch, m.entry, m.field, m.winner, m.actual)
		l.UpdateEntryFieldAlias(m.entry, m.field, m.winner, m.actual)
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

	for oldie, key := range l.KeyToKey {
		if !l.EntryExists(key) {
			l.Warning(WarningTargetOfOldieNotExists, key, oldie)
		}

		if bibEntryExists(oldie) {
			l.Warning(WarningOldieIsKey, oldie)
		}
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
	if nameField == "" {
		l.Warning(WarningCannotDeriveAliasNoName, entry.Key)
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
		l.Warning(WarningCannotDeriveAliasEmptySurname, entry.Key, surnameRaw)
		return ""
	}
	surname := stripNonAlpha.ReplaceAllString(TeXStringIndexer(surnameRaw), "")
	if surname == "" {
		l.Warning(WarningCannotDeriveAliasEmptySurname, entry.Key, surnameRaw)
		return ""
	}

	year := entry.FieldValue("year")
	if !IsValidYear(year) && parent != nil {
		year = parent.FieldValue("year")
	}
	if !IsValidYear(year) {
		l.Warning(WarningCannotDeriveAliasNoYear, entry.Key)
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
		if target, inUse := l.HintToKey[candidate]; inUse && target != entry.Key {
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
		l.Warning(WarningNoTitleKeywordsForPreferredAlias, entry.Key, base)
	} else {
		l.Warning(WarningCannotDeriveUniquePreferredAlias, entry.Key, base)
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
		if _, known := l.HintToKey[alias]; !known {
			l.AddKeyHint(alias, entry.Key)
		}

		if validPreferredKeyAlias.MatchString(alias) {
			return
		}

		// Non-compliant alias: warn, try to derive a valid replacement.
		l.Warning(WarningInvalidPreferredKeyAlias, alias, entry.Key)
		if derived := l.derivePreferredAlias(entry); derived != "" {
			// Keep old non-compliant alias as a hint; set the derived one.
			l.setPreferredAlias(entry, derived)
			l.Progress(ProgressGeneratedPreferredAlias, derived, entry.Key)
		}
		return
	}

	// No alias yet. Only generate one for DBLP-linked entries (temporary until doubles are cleaned up).
	if entry.FieldValue(DBLPField) == "" {
		return
	}

	if derived := l.derivePreferredAlias(entry); derived != "" {
		l.setPreferredAlias(entry, derived)
		l.Progress(ProgressGeneratedPreferredAlias, derived, entry.Key)
	}
}

func (l *TBibTeXLibrary) CheckTitlePresence(entry *TBibTeXEntry) {
	if entry.FieldValue(TitleField) == "" {
		l.Warning(WarningEmptyTitle, entry.Key)
	}
}


func (l *TBibTeXLibrary) CheckDOIPresence(entry *TBibTeXEntry) {
	foundDOI := entry.FieldValue("doi")

	if foundDOI == "" {
		if l.tryGetDOIFromURL(entry.Key, "url", &foundDOI) {
			l.Warning("Found DOI in URL %s for %s", foundDOI, entry.Key)
			l.setEntryField(entry, "doi", foundDOI)
		}
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
	// SAFE??
	entryType := entry.EntryType()

	if BibTeXBookish.Contains(entryType) {
		newBookTitle := l.MaybeResolveFieldValue(entry.Key, entry.Key, "booktitle", entry.FieldValue(TitleField), entry.FieldValue("booktitle"))
		l.setEntryField(entry, "booktitle", newBookTitle)
		l.UpdateEntryFieldAlias(entry.Key, TitleField, entry.FieldValue(TitleField), entry.FieldValue("booktitle"))
		l.setEntryField(entry, TitleField, entry.FieldValue("booktitle"))
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
			l.Warning("Not able to find eprint data for %s", entry.Key)
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
							l.Warning("Not able to find eprint data for %s", entry.Key)
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
		l.Warning(WarningISBNMismatchFromCrossrefDOI, entry.Key, crossrefKey, ISBNCandidate, parentISBN)
	}
}

func (l *TBibTeXLibrary) CheckCrossrefInheritableField(crossrefEntry, entry *TBibTeXEntry, field string) {
	if BibTeXMustInheritFields.Contains(field) {
		if challenge, hasChallenge := entry.Fields[field]; hasChallenge {
			target := l.MaybeResolveFieldValue(crossrefEntry.Key, entry.Key, field, challenge, crossrefEntry.FieldValue(field))

			l.setEntryField(crossrefEntry, field, target)

			if field == "booktitle" {
				currentTitle := crossrefEntry.FieldValue(TitleField)
				newTitle := l.MaybeResolveFieldValue(crossrefEntry.Key, entry.Key, field, target, currentTitle)

				if currentTitle != newTitle {
					l.TitleIndex.DeleteValueFromStringSetMap(TeXStringIndexer(currentTitle), crossrefEntry.Key)

					/// Refactor this into a function. We need this more often.
					l.setEntryField(crossrefEntry, TitleField, newTitle)
					l.TitleIndex.AddValueToStringSetMap(TeXStringIndexer(newTitle), crossrefEntry.Key)
				}
			}

			for otherChallenger := range l.EntryFieldSourceToTarget[entry.Key][field] {
				l.AddEntryFieldAlias(crossrefEntry.Key, field, otherChallenger, target, false)
			}

			if target != "" {
				l.deleteEntryField(entry, field)
				delete(l.EntryFieldSourceToTarget[entry.Key], field)
			}
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
		l.Warning("Found self referencing crossref for %s. Cleaning this up.", entry.Key)
		l.setEntryField(entry, "crossref", "")
	}

	if allowedCrossrefType, hasAllowedCrossrefType := BibTeXCrossrefType[entryType]; hasAllowedCrossrefType {
		if crossrefety != "" {
			if CrossrefType := l.EntryType(crossrefety); CrossrefType != "" {
				if allowedCrossrefType == CrossrefType || CrossrefType == "incollection" { // MAKE THIS CLEANER
					crossrefEntry := l.buildEntry(crossrefety)
					for field := range BibTeXInheritableFields.Elements() {
						l.CheckCrossrefInheritableField(crossrefEntry, entry, field)
					}

					l.CheckCrossrefDOI(crossrefEntry, entry)
					l.CheckBookishTitles(crossrefEntry)
				} else {
					l.Warning("Crossref from %s %s to %s %s does not comply to the typing rules.", entryType, entry.Key, CrossrefType, crossrefety)
				}
			} else {
				l.Warning("Target %s of crossref from %s does not exist.", crossrefety, entry.Key)
			}
		}
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

	l.Warning(WarningBadISSN, issn, entry.Key)
}

func (l *TBibTeXLibrary) CheckISBN(entry *TBibTeXEntry) {
	isbn := entry.FieldValue("isbn")

	if isbn == "" || IsValidISBN(isbn) {
		return
	}

	l.Warning(WarningBadISBN, isbn, entry.Key)
}

func (l *TBibTeXLibrary) CheckYear(entry *TBibTeXEntry) {
	year := entry.FieldValue("year")

	if year == "" || IsValidYear(year) {
		return
	}

	l.Warning(WarningBadYear, year, entry.Key)
}


func (l *TBibTeXLibrary) CheckURLDate(entry *TBibTeXEntry) {
	date := entry.FieldValue("urldate")

	if date == "" || IsValidDate(date) {
		return
	}

	l.Warning(WarningBadDate, date, entry.Key)
}

func (l *TBibTeXLibrary) CheckWithdrawn(entry *TBibTeXEntry) {
	date := entry.FieldValue("withdrawn")
	if date == "" {
		return
	}

	if !IsValidDate(date) {
		l.Warning("Invalid date %q in withdrawn field for entry %s", date, entry.Key)
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
		l.Warning(WarningInvalidKey, entry.Key)
	}
	if entryType := entry.EntryType(); entryType != "" {
		if _, known := BibTeXAllowedEntryFields[entryType]; !known {
			l.Error("Entry %s has unknown entry type %q — bib file writing disabled", entry.Key, entryType)
			l.NoDBUpdating = true
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
			l.SetEntryFieldValue(key, "crossref", parentKey)
			crossrefKey = parentKey
			crossrefDBLP = parentDBLP
		}

		if crossrefDBLP == "" && parentDBLP != "" {
			l.SetEntryFieldValue(crossrefKey, DBLPField, parentDBLP)
			crossrefDBLP = parentDBLP
		}

		if crossrefKey == "" && !l.HasMetadata(key, MetaPropDblpKeyMissing) {
			l.Warning("Crossref entry type without a crossref %s", key)
		}

		if entryDBLP != "" && crossrefDBLP == "" {
			l.Warning("Parent entry %s does not have a dblp key, while the child %s does have dblp key %s", crossrefKey, key, entryDBLP)
		}

	}

	// Add parent to child check for bookish (suppressed by no_dblp_children flag).
	if BibTeXBookish.Contains(entryType) && !l.EntryHasFlag(key, EntryFlagNoDBLPChildren) && entryDBLP != "" {
		l.Progress("Checking children of %s", entryDBLP)
		l.ForEachChildOfDBLPKey(entryDBLP, func(childDBLP string) {
			childKey := l.LookupDBLPKey(childDBLP)

			if childKey != "" {
				l.SetEntryFieldValue(childKey, "crossref", key)
			} else {
				l.MaybeAddDBLPChildEntry(childDBLP, key)
			}
		})

		// Check that every library child of this DBLP-keyed parent also has a DBLP key.
		if entryDBLP != "" {
			forEachLibraryChildOf(key, func(childKey string) {
				if l.EntryFieldValueity(childKey, DBLPField) == "" && !l.DblpWaived.Contains(childKey) {
					if l.WarningYesNoQuestion(QuestionAddToDblpWaived, WarningNoDblpKeyForChild, childKey, key, entryDBLP) {
						l.DblpWaived.Add(childKey)
						l.dblpWaivedModified = true
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
		l.CheckKeyValidity(entry)

		// CheckCrossref can lead to a merger of entries for now ...
		if entry.Exists() && l.EntryExists(entry.Key) {
			l.NormaliseEntryFields(entry)
			l.CheckDOIPresence(entry)
			l.CheckEPrint(entry)
			l.CheckCrossref(entry)
			l.CheckAndEnforcePreferredAlias(entry)
			l.CheckBookishTitles(entry)
			l.CheckISBNFromDOI(entry)
			l.CheckTitlePresence(entry)
			l.CheckURLRedundance(entry)
			l.CheckURLDateNeed(entry)
			l.CheckFileReference(entry)
			l.CheckISSN(entry)
			l.CheckISBN(entry)
			l.CheckYear(entry)
			l.CheckURLDate(entry)
			l.CheckWithdrawn(entry)
		}
	}
}

func (l *TBibTeXLibrary) CheckEntries() {
	total := countBibEntries()
	spinner := l.NewSpinner(ProgressCheckingConsistencyOfEntries)
	done := 0
	stepN := Reporting.StepSize()
	questionCounter := 0

	forEachBibEntryKey(func(key string) bool {
		done++
		spinner.Update(done, total)
		l.ResetQuestionFlag()
		l.CheckEntry(l.buildEntry(key))
		if stepN > 0 && l.QuestionWasAsked() {
			questionCounter++
			if questionCounter >= stepN {
				if l.AskContinueOrQuit() {
					return false
				}
				questionCounter = 0
			}
		}
		return true
	})

	spinner.Stop()
}

// RenormaliseNameFields reloads name_mappings from the DB (picking up any mappings
// added or changed since the library was opened) then sweeps all author and editor
// values in bib_entries through the updated normaliser. Called after
// -import_name_mappings / -add_name_mappings to apply new mappings immediately
// without requiring a separate bib.check run (§11.3 cascade).
func (l *TBibTeXLibrary) RenormaliseNameFields() {
	loadNameMappingsFromDb(l)
	l.CheckNameMappingConsistency()

	total := countBibEntries()
	spinner := l.NewSpinner("Re-normalising author/editor fields")
	done := 0
	forEachBibEntryKey(func(key string) bool {
		done++
		spinner.Update(done, total)
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
	spinner.Stop()
}

