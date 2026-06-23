/*
 *
 * Module: bibtex_library_repair
 *
 * Repairs garbled author/editor fields in bib_entries.  Garbling (e.g.
 * "Ae, Chun, Soon" instead of "Chun, Soon Ae") is repaired by re-deriving
 * the correct author/editor value from DBLP (for entries with a dblp field)
 * or from a reference .bib file (for entries without one).
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

// incompleteTeXAccentAtEnd matches a brace group containing only a single
// lowercase TeX command letter at the end of a string (e.g. "{\c}", "{\u}").
// This pattern indicates a name that was truncated at the space inside a
// TeX accent group like {\c c} — the {\c} fragment is left with no argument.
var incompleteTeXAccentAtEnd = regexp.MustCompile(`\{\\[a-z]\}$`)

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
// is garbled.  Three patterns are detected:
//   - ≥3 commas (e.g. "A., Bubenko, Jr, Janis")
//   - exactly 2 commas where the middle part is not a known suffix
//     (ruling out the valid "Bubenko, Jr, Janis A." form)
//   - "ss, ff" form where ff ends with an incomplete TeX accent group like {\c}
//     indicating truncation at the space inside {\c c} (Preguiça-type garbling)
//
// Single-letter surname cases (e.g. "A, Prajith C.") are intentionally NOT
// detected here: single-letter name parts occur in legitimate naming conventions
// and those entries require manual review rather than auto-correction.
func hasGarbledName(names string) bool {
	for _, name := range strings.Split(names, " and ") {
		name = strings.TrimSpace(name)
		if hasUnbalancedBraces(name) {
			return true
		}
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
		if commas == 1 {
			idx := strings.Index(name, ", ")
			if idx >= 0 && incompleteTeXAccentAtEnd.MatchString(name[idx+2:]) {
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

// hasUnbalancedBraces returns true when the number of opening braces does not
// equal the number of closing braces in s.  Used to detect name fragments that
// resulted from a naive " and " split inside a brace group.
func hasUnbalancedBraces(s string) bool {
	depth := 0
	for _, ch := range s {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	return depth != 0
}

// isGarbledContributorName returns true when name is not a valid single-person
// BibTeX name.  Catches:
//   - any occurrence of " and " (person names never contain " and ", even inside
//     brace groups — the brace may have absorbed an entire author list)
//   - names with ≥3 commas or an ambiguous 2-comma form
//   - names starting with "{" or containing "} {" (stray braces)
//   - "ss, ff" names where ff ends with an incomplete TeX accent group like {\c}
//     indicating truncation at the space inside a group like {\c c}
func isGarbledContributorName(name string) bool {
	if strings.Contains(name, " and ") {
		return true
	}
	if hasGarbledName(name) {
		return true
	}
	if hasStrayBrace(name) {
		return true
	}
	if idx := strings.Index(name, ", "); idx >= 0 {
		ff := name[idx+2:]
		if incompleteTeXAccentAtEnd.MatchString(ff) {
			return true
		}
	}
	return false
}

// CleanGarbledContributorNames removes garbled entries from contributor_names
// and contributors, then purges the corresponding entries from the library's
// in-memory maps so that the subsequent repair pass uses clean alias state.
// Proceeds in three passes:
//  1. Delete contributors whose canonical name is garbled (ON DELETE CASCADE
//     removes all their contributor_names rows automatically).
//  2. Delete individual garbled contributor_names entries for contributors
//     whose canonical name is valid.
//  3. Delete contributors that have no remaining contributor_names entries.
//
// Returns the number of rows deleted across all passes.
func (l *TBibTeXLibrary) CleanGarbledContributorNames() int {
	deleted := 0

	// Pass 1: remove contributors with garbled canonical names.
	crows, cerr := db.Query(`SELECT id, name FROM contributors`)
	if cerr != nil {
		dbInteraction.Warning("CleanGarbledContributorNames: query contributors: %s", cerr)
		return 0
	}
	type idNamePair struct{ id, name string }
	var garbledContribs []idNamePair
	for crows.Next() {
		var id, name string
		if crows.Scan(&id, &name) == nil && isGarbledContributorName(name) {
			garbledContribs = append(garbledContribs, idNamePair{id, name})
		}
	}
	crows.Close()
	for _, p := range garbledContribs {
		if err := bibExec(`DELETE FROM contributors WHERE id = ?`, p.id); err != nil {
			dbInteraction.Warning("CleanGarbledContributorNames: delete contributor %s: %s", p.id, err)
		} else {
			l.purgeContributorFromMemory(p.id)
			deleted++
		}
	}

	// Pass 2: remove individual garbled contributor_names rows.
	nrows, nerr := db.Query(`SELECT id, name FROM contributor_names`)
	if nerr != nil {
		dbInteraction.Warning("CleanGarbledContributorNames: query contributor_names: %s", nerr)
		return deleted
	}
	type idName struct{ id, name string }
	var garbledNames []idName
	for nrows.Next() {
		var id, name string
		if nrows.Scan(&id, &name) == nil && isGarbledContributorName(name) {
			garbledNames = append(garbledNames, idName{id, name})
		}
	}
	nrows.Close()
	for _, p := range garbledNames {
		deleteContributorNameFromDB(p.id, p.name)
		delete(l.NameToContributorID, p.name)
		delete(l.NameAliasToName, p.name)
		deleted++
	}

	// Pass 3: remove contributors with no remaining contributor_names entries.
	orphanRows, oerr := db.Query(`
		SELECT c.id FROM contributors c
		WHERE NOT EXISTS (SELECT 1 FROM contributor_names n WHERE n.id = c.id)`)
	if oerr != nil {
		dbInteraction.Warning("CleanGarbledContributorNames: query orphans: %s", oerr)
		return deleted
	}
	var orphanIDs []string
	for orphanRows.Next() {
		var id string
		if orphanRows.Scan(&id) == nil {
			orphanIDs = append(orphanIDs, id)
		}
	}
	orphanRows.Close()
	for _, id := range orphanIDs {
		if err := bibExec(`DELETE FROM contributors WHERE id = ?`, id); err != nil {
			dbInteraction.Warning("CleanGarbledContributorNames: delete orphan contributor %s: %s", id, err)
		} else {
			l.purgeContributorFromMemory(id)
			deleted++
		}
	}

	return deleted
}

// purgeContributorFromMemory removes all traces of a contributor from the
// library's in-memory maps.  Called after the contributor and its names have
// been deleted from the DB.
func (l *TBibTeXLibrary) purgeContributorFromMemory(id string) {
	contrib, ok := l.ContributorByID[id]
	if !ok {
		return
	}
	// Remove all name forms that point to this contributor.
	for name, cid := range l.NameToContributorID {
		if cid == id {
			delete(l.NameToContributorID, name)
			delete(l.NameAliasToName, name)
		}
	}
	// Remove reverse alias entries that resolve to the canonical.
	canonical := contrib.Name
	for alias, target := range l.NameAliasToName {
		if target == canonical {
			delete(l.NameAliasToName, alias)
		}
	}
	delete(l.ContributorByID, id)
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
