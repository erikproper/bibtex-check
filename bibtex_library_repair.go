/*
 *
 * Module: bibtex_library_repair
 *
 * Repairs garbled author/editor fields in bib_entries and cleans polluted
 * name_mappings.  Garbling (e.g. "Ae, Chun, Soon" instead of "Chun, Soon Ae")
 * is caused by bad name_mapping canonicals derived from a corrupt CSV import.
 * This module:
 *   1. Removes name_mappings rows whose canonical or alias form is garbled
 *      (≥3 commas, 2 commas with non-suffix middle part, or contains
 *      parentheses/braces — see hasGarbledName and isBadNameMappingCanonical).
 *   2. Re-derives the correct author/editor value from DBLP (for entries with
 *      a dblp field) or from a reference .bib file (for entries without one).
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 */

package main

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// knownSuffixes is the set of BibTeX name suffixes recognised in the
// "Last, Jr, First" three-part format.  A 2-comma name whose middle part
// is in this set is a valid suffix form, NOT garbled.
var knownSuffixes = map[string]bool{
	"Jr": true, "Jr.": true,
	"Sr": true, "Sr.": true,
	"II": true, "III": true, "IV": true, "V": true,
}

// hasSingleLetterSurname returns true when any name in a BibTeX "and"-list
// has a single-letter surname (e.g. "A, Prajith C." from auto-harvesting
// "Prajith C. A").  Kept separate from hasGarbledName because single-letter
// name parts are legitimate in some conventions and should not be flagged
// during normal operation — only rolled back during an explicit repair run
// when a reference bib is available.
func hasSingleLetterSurname(names string) bool {
	for _, name := range strings.Split(names, " and ") {
		name = strings.TrimSpace(name)
		if idx := strings.Index(name, ","); idx >= 0 {
			if len(strings.TrimRight(strings.TrimSpace(name[:idx]), ".")) <= 1 {
				return true
			}
		}
	}
	return false
}

// hasGarbledName returns true when any individual name in a BibTeX "and"-list
// is garbled.  Two patterns are detected:
//   - ≥3 commas (e.g. "A., Bubenko, Jr, Janis")
//   - exactly 2 commas where the middle part is not a known suffix
//     (ruling out the valid "Bubenko, Jr, Janis A." form)
//
// Single-letter surname cases (e.g. "A, Prajith C.") are intentionally NOT
// detected here: single-letter name parts occur in legitimate naming conventions
// and those entries require manual review rather than auto-correction.
func hasGarbledName(names string) bool {
	for _, name := range strings.Split(names, " and ") {
		name = strings.TrimSpace(name)
		commas := strings.Count(name, ",")
		if commas >= 3 {
			return true
		}
		if commas == 2 {
			parts := strings.SplitN(name, ",", 3)
			if !knownSuffixes[strings.TrimSpace(parts[1])] {
				return true
			}
		}
	}
	return false
}

// hasStrayBrace returns true when s has brace characters that are not valid
// TeX encoding.  Two patterns are detected:
//   - s starts with "{" (the entire token is brace-wrapped, e.g. "{Bubenko, Jr. J. A.}")
//   - s contains "} {" (stray closing brace from invertedNameForm output,
//     e.g. "Jr. J. A.} {Bubenko")
//
// This deliberately does NOT flag valid TeX encoding such as B{\"o}hm or
// {\c c} where braces wrap a single command sequence.
func hasStrayBrace(s string) bool {
	if strings.HasPrefix(s, "{") {
		return true
	}
	return strings.Contains(s, "} {")
}

// isBadNameMappingCanonical returns true for canonical forms that were created
// by the auto-harvesting bugs and should not exist in name_mappings:
//   - garbled multi-comma forms (e.g. "Ae, Chun, Soon") — via hasGarbledName
//   - forms with parentheticals (e.g. "), Hilgartner, C.A.") — from (Andy)-style names
//   - brace-wrapped tokens or stray braces — via hasStrayBrace
//   - single-letter surname forms (e.g. "A, Prajith C.") — from wrongly inverting
//     a trailing initial; not in hasGarbledName to avoid auto-correcting bib entries
func isBadNameMappingCanonical(name string) bool {
	if hasGarbledName(name) {
		return true
	}
	if strings.ContainsAny(name, "()") {
		return true
	}
	if hasStrayBrace(name) {
		return true
	}
	if idx := strings.Index(name, ","); idx >= 0 {
		if len(strings.TrimRight(strings.TrimSpace(name[:idx]), ".")) == 1 {
			return true
		}
	}
	return false
}

// cleanBadNameMappings removes name_mappings entries whose canonical or alias
// form was created by auto-harvesting bugs.  Two patterns are cleaned:
//   1. Garbled canonicals (≥3 commas, or 2 commas with non-suffix middle part).
//   2. Canonicals/aliases containing parentheses or braces (from names like "C. A. (Andy)").
//
// Both the DB rows and the in-memory maps are purged.
// Returns the number of distinct bad canonicals removed.
func (l *TBibTeXLibrary) cleanBadNameMappings() int {
	// Pattern 1: bad canonicals — ≥2 commas (garbled), OR single-letter surname
	// (e.g. "A, Prajith C." from wrongly inverting "Prajith C. A").
	// Single-letter surnames are kept out of hasGarbledName to avoid auto-correcting
	// bib entries, but they should still be purged from name_mappings.
	rows, err := db.Query(
		`SELECT DISTINCT name FROM name_mappings
		 WHERE (LENGTH(name) - LENGTH(REPLACE(name, ',', ''))) >= 2
		    OR name LIKE '_, %' OR name LIKE '_., %'`)
	if err != nil {
		dbInteraction.Warning("Could not query name_mappings for cleanup: %s", err)
		return 0
	}
	var bad []string
	seen := map[string]bool{}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && isBadNameMappingCanonical(name) && !seen[name] {
			bad = append(bad, name)
			seen[name] = true
		}
	}
	rows.Close()

	// Pattern 2: parentheticals, brace-wrapped tokens ({...}), or stray "} {" brace pairs.
	// Deliberately avoids '%{%' which would match valid TeX encoding like B{\"o}hm.
	rows2, err := db.Query(`SELECT DISTINCT alias, name FROM name_mappings
		WHERE alias LIKE '%(%' OR alias LIKE '%)%'
		   OR name  LIKE '%(%' OR name  LIKE '%)%'
		   OR alias LIKE '{%'  OR name  LIKE '{%'
		   OR alias LIKE '%} {%'`)
	if err == nil {
		for rows2.Next() {
			var alias, name string
			if rows2.Scan(&alias, &name) != nil {
				continue
			}
			if !seen[name] {
				bad = append(bad, name)
				seen[name] = true
			}
		}
		rows2.Close()
	}
	if len(bad) == 0 {
		return 0
	}

	// Clean in-memory maps for bad canonicals.
	for _, canonical := range bad {
		if aliasSet, ok := l.NameToAliases[canonical]; ok {
			for alias := range aliasSet.Elements() {
				delete(l.NameAliasToName, alias)
			}
			delete(l.NameToAliases, canonical)
		}
		delete(l.NameAliasToName, canonical)
	}

	// Collect stray aliases (parentheticals / brace-wrapped) before the transaction.
	var strayAliases []string
	aliasRows, err := db.Query(`SELECT alias FROM name_mappings
		WHERE alias LIKE '%(%' OR alias LIKE '%)%'
		   OR alias LIKE '{%'  OR alias LIKE '%} {%'`)
	if err == nil {
		for aliasRows.Next() {
			var alias string
			if aliasRows.Scan(&alias) == nil {
				strayAliases = append(strayAliases, alias)
			}
		}
		aliasRows.Close()
	}

	// Clean in-memory maps for stray aliases.
	for _, alias := range strayAliases {
		if canonical, ok := l.NameAliasToName[alias]; ok {
			l.NameToAliases.DeleteValueFromStringSetMap(canonical, alias)
		}
		delete(l.NameAliasToName, alias)
	}

	// Batch all DB deletes into one transaction.
	tx, txErr := db.Begin()
	if txErr != nil {
		dbInteraction.Warning("Could not begin name_mappings cleanup transaction: %s", txErr)
	} else {
		for _, canonical := range bad {
			tx.Exec(`DELETE FROM name_mappings WHERE name = ?`, canonical)
		}
		for _, alias := range strayAliases {
			tx.Exec(`DELETE FROM name_mappings WHERE alias = ?`, alias)
		}
		if err := tx.Commit(); err != nil {
			dbInteraction.Warning("Could not commit name_mappings cleanup: %s", err)
			tx.Rollback()
		}
	}

	l.nameMappingsModified = true
	return len(bad)
}

// bibEntryRe matches the opening line of a BibTeX entry and captures the key.
var bibEntryRe = regexp.MustCompile(`(?i)^@(\w+)\{([^,]+),`)

// bibFieldRe matches the start of a brace-delimited BibTeX field assignment.
var bibFieldRe = regexp.MustCompile(`(?i)^\s*(\w+)\s*=\s*\{(.*)$`)

// parseBibForAuthorEditor scans bibPath and returns a map from entry key to a
// map of {"author": raw_value, "editor": raw_value}.  Only author and editor
// fields are extracted; values are raw (not normalised).
func parseBibForAuthorEditor(bibPath string) (map[string]map[string]string, error) {
	f, err := os.Open(bibPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := map[string]map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	var (
		currentKey   string
		currentField string
		braceDepth   int
		valueBuf     strings.Builder
	)

	flushField := func() {
		if currentKey != "" && currentField != "" {
			result[currentKey][currentField] = strings.TrimSpace(valueBuf.String())
		}
		currentField = ""
		braceDepth = 0
		valueBuf.Reset()
	}

	for sc.Scan() {
		line := sc.Text()

		if braceDepth > 0 {
			valueBuf.WriteByte('\n')
			for _, ch := range line {
				if ch == '{' {
					braceDepth++
					valueBuf.WriteRune(ch)
				} else if ch == '}' {
					braceDepth--
					if braceDepth == 0 {
						flushField()
						break
					}
					valueBuf.WriteRune(ch)
				} else {
					valueBuf.WriteRune(ch)
				}
			}
			continue
		}

		if currentKey == "" {
			m := bibEntryRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if t := strings.ToLower(m[1]); t == "comment" || t == "string" || t == "preamble" {
				continue
			}
			currentKey = strings.TrimSpace(m[2])
			result[currentKey] = map[string]string{}
			continue
		}

		if strings.TrimSpace(line) == "}" {
			currentKey = ""
			continue
		}

		m := bibFieldRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if field := strings.ToLower(m[1]); field == "author" || field == "editor" {
			currentField = field
			valueBuf.Reset()
			braceDepth = 1
			for _, ch := range m[2] {
				if ch == '{' {
					braceDepth++
					valueBuf.WriteRune(ch)
				} else if ch == '}' {
					braceDepth--
					if braceDepth == 0 {
						flushField()
						break
					}
					valueBuf.WriteRune(ch)
				} else {
					valueBuf.WriteRune(ch)
				}
			}
		}
	}
	return result, sc.Err()
}

// repairFieldFromValue normalises rawValue for field and writes it to key when
// the raw value is non-empty, clean (no garbling after normalisation), and
// differs from the current stored value.  Returns true when a change is made.
func (l *TBibTeXLibrary) repairFieldFromValue(key, field, rawValue string) bool {
	if rawValue == "" {
		return false
	}
	normalised := l.NormaliseFieldValue(field, rawValue)
	if normalised == "" || hasGarbledName(normalised) {
		return false
	}
	if l.EntryFieldValueity(key, field) == normalised {
		return false
	}
	l.SetEntryFieldValue(key, field, normalised)
	return true
}

// repairFieldFromDblp reads the DBLP file store for dblpKey and uses it to
// repair the author or editor field of key.  Returns true on any change.
func (l *TBibTeXLibrary) repairFieldFromDblp(key, dblpKey, field string) bool {
	if !hasGarbledName(l.EntryFieldValueity(key, field)) {
		return false
	}
	entry := dblpEntryFromFile(dblpKey)
	if entry == nil {
		return false
	}
	return l.repairFieldFromValue(key, field, entry.Fields[field])
}

// repairFieldFromBibMap looks up the author/editor value for key (and its
// key_oldies aliases) in bibMap.  Returns the raw value, or "" if not found.
func repairFieldFromBibMap(bibMap map[string]map[string]string, key, field string) string {
	if fields, ok := bibMap[key]; ok {
		if v := fields[field]; v != "" {
			return v
		}
	}
	rows, err := db.Query(`SELECT alias FROM key_oldies WHERE key = ?`, key)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var oldKey string
		if rows.Scan(&oldKey) != nil {
			continue
		}
		if fields, ok := bibMap[oldKey]; ok {
			if v := fields[field]; v != "" {
				return v
			}
		}
	}
	return ""
}

// RepairGarbledNames iterates every entry and repairs garbled author/editor
// fields.  DBLP data is tried first; bibMap (may be nil) is the fallback.
// Returns the number of fields repaired.
func (l *TBibTeXLibrary) RepairGarbledNames(bibMap map[string]map[string]string) int {
	total := countBibEntries()
	count := 0
	repaired := 0
	lastPct := -1

	forEachBibEntryKey(func(key string) bool {
		count++
		if pct := int(float64(count)*100/float64(total)) / 10 * 10; pct != lastPct {
			l.Progress(ProgressEntryProgress, count, total, float64(pct))
			lastPct = pct
		}

		for _, field := range []string{"author", "editor"} {
			current := l.EntryFieldValueity(key, field)
			if current == "" {
				continue
			}
			garbled := hasGarbledName(current)
			// Single-letter surname inversions (e.g. "A, Prajith C.") are
			// rolled back from the backup bib but not touched by DBLP, since
			// DBLP may carry the same ambiguous form.
			bibRollback := !garbled && bibMap != nil && hasSingleLetterSurname(current)
			if !garbled && !bibRollback {
				continue
			}
			fixed := false
			if garbled {
				if dblpKey := l.EntryFieldValueity(key, DBLPField); dblpKey != "" {
					fixed = l.repairFieldFromDblp(key, dblpKey, field)
				}
			}
			if !fixed && bibMap != nil {
				if raw := repairFieldFromBibMap(bibMap, key, field); raw != "" {
					fixed = l.repairFieldFromValue(key, field, raw)
				}
			}
			if fixed {
				repaired++
				bibEntriesModified = true
			}
		}
		return true
	})

	return repaired
}
