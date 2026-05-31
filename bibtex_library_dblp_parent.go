/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: DBLPParent
 *
 * DBLP crossref disambiguation and dblp_parent.csv management.
 *
 * When multiple proceedings/book entries exist in the same conference directory
 * for the same year (e.g. SCITEPRESS multi-volume events), this module resolves
 * which parent a given child entry belongs to, storing the result in
 * dblp_parent.csv so DBLP merges use the correct crossref.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 26.05.2026
 *
 */

package main

import (
	"math"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// reVolumeInTitle matches "Volume N" (case-insensitive) in a proceedings title.
var reVolumeInTitle = regexp.MustCompile(`(?i)\bVolume\s+(\d+)\b`)

// reKeyNumericSuffix matches a trailing "-N" in a DBLP key (e.g. conf/iceis/2021-1).
var reKeyNumericSuffix = regexp.MustCompile(`-(\d+)$`)

// findCandidateDblpParents returns all DBLP keys for proceedings/book entries
// in the same conference directory as childDBLPKey and with the same year.
// Returns nil when there is no ambiguity (zero or one candidate).
// cache maps "parentDir\x00year" to the crossrefs/-scan result; pass nil to skip caching.
// Rule 0 (child's own crossref) is checked first and bypasses the cache entirely,
// since different children in the same venue/year may point to different parents.
func findCandidateDblpParents(childDBLPKey string, cache map[string][]string) []string {
	parentDir := path.Dir(childDBLPKey)
	if parentDir == "." || parentDir == "" {
		return nil
	}

	childJe := readDblpJSONEntry(childDBLPKey)

	// No DBLP data or no crossref claim → nothing to disambiguate.
	if childJe == nil {
		return nil
	}
	ownParent := childJe.Fields["crossref"]
	if ownParent == "" {
		return nil
	}

	// Rule 0: if the child's data.json claims a specific parent and that parent
	// exists as a valid parent-type entry, DBLP's own assignment is authoritative.
	// Do not cache: siblings in the same venue/year may point to different parents.
	if parentJe := readDblpJSONEntry(ownParent); parentJe != nil {
		if parentJe.EntryType == "proceedings" || parentJe.EntryType == "book" || parentJe.EntryType == "data" {
			return []string{ownParent}
		}
	}

	// Child claims a parent that isn't in the store yet or has an unexpected type.
	// Fall back to crossrefs/ scan to find candidates in the same venue/year.
	childYear := childJe.Fields["year"]
	cacheKey := parentDir + "\x00" + childYear
	if cache != nil {
		if cached, ok := cache[cacheKey]; ok {
			return cached
		}
	}

	crossrefsDir := dblpFolder() + "crossrefs/" + parentDir
	dirEntries, err := os.ReadDir(crossrefsDir)
	if err != nil {
		if cache != nil {
			cache[cacheKey] = nil
		}
		return nil
	}

	var candidates []string
	for _, e := range dirEntries {
		if !e.IsDir() {
			continue
		}
		key := parentDir + "/" + e.Name()
		je := readDblpJSONEntry(key)
		if je == nil {
			continue
		}
		if je.EntryType != "proceedings" && je.EntryType != "book" && je.EntryType != "data" {
			continue
		}
		if childYear != "" && je.Fields["year"] != childYear {
			continue
		}
		candidates = append(candidates, key)
	}

	if cache != nil {
		cache[cacheKey] = candidates
	}
	return candidates
}

// checkSiblingConsistency returns true when every library entry that shares
// crossrefLibKey as its crossref also has a DBLP data.json that points to
// candidateDblpKey as its parent. Siblings without a DBLP key are skipped.
func (l *TBibTeXLibrary) checkSiblingConsistency(crossrefLibKey, candidateDblpKey string) bool {
	consistent := true
	forEachBibEntryType(func(key, _ string) {
		if !consistent {
			return
		}
		if l.EntryFieldValueity(key, "crossref") != crossrefLibKey {
			return
		}
		siblingDblpKey := l.EntryFieldValueity(key, DBLPField)
		if siblingDblpKey == "" {
			return
		}
		je := readDblpJSONEntry(siblingDblpKey)
		if je == nil {
			return
		}
		if siblingParent := je.Fields["crossref"]; siblingParent != "" && siblingParent != candidateDblpKey {
			consistent = false
		}
	})
	return consistent
}

// askUserForDblpParent shows the candidates and asks the user to choose one.
// Returns "" when the user cannot or does not choose.
func (l *TBibTeXLibrary) askUserForDblpParent(childDBLPKey string, candidates []string) string {
	l.Warning("Multiple parent candidates for DBLP entry %s:", childDBLPKey)
	for i, c := range candidates {
		title := ""
		if je := readDblpJSONEntry(c); je != nil {
			title = je.Fields["title"]
		}
		l.Warning("  %d: %s  (%s)", i+1, c, title)
	}

	options := TStringSetNew()
	for i := range candidates {
		options.Add(strconv.Itoa(i + 1))
	}
	choice := l.WarningQuestion("Which parent should be used?", options, "Choose parent for %s", childDBLPKey)
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(candidates) {
		return ""
	}
	return candidates[idx-1]
}

// ResolveDblpParentAmbiguity applies rules 1–5 to select one parent from
// candidates for childDBLPKey. existingCrossrefLibKey is the library key of
// the crossref already stored for this entry (may be "").
// Returns "" when no resolution is possible.
func (l *TBibTeXLibrary) ResolveDblpParentAmbiguity(childDBLPKey string, candidates []string, existingCrossrefLibKey string) string {
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	// Rule 1: all candidates have a numeric volume field → pick the lowest.
	lowestVol := math.MaxInt32
	bestByVol := ""
	allHaveVol := true
	for _, c := range candidates {
		je := readDblpJSONEntry(c)
		if je == nil {
			allHaveVol = false
			break
		}
		v, err := strconv.Atoi(strings.TrimSpace(je.Fields["volume"]))
		if err != nil {
			allHaveVol = false
			break
		}
		if v < lowestVol {
			lowestVol = v
			bestByVol = c
		}
	}
	if allHaveVol && bestByVol != "" {
		return bestByVol
	}

	// Rule 2: all candidates have "Volume N" in their title → pick the lowest N.
	lowestTitleVol := math.MaxInt32
	bestByTitleVol := ""
	allHaveTitleVol := true
	for _, c := range candidates {
		je := readDblpJSONEntry(c)
		if je == nil {
			allHaveTitleVol = false
			break
		}
		m := reVolumeInTitle.FindStringSubmatch(je.Fields["title"])
		if m == nil {
			allHaveTitleVol = false
			break
		}
		n, _ := strconv.Atoi(m[1])
		if n < lowestTitleVol {
			lowestTitleVol = n
			bestByTitleVol = c
		}
	}
	if allHaveTitleVol && bestByTitleVol != "" {
		return bestByTitleVol
	}

	// Rule 3: all keys end with "-N" → pick the lowest N.
	lowestSuffix := math.MaxInt32
	bestBySuffix := ""
	allHaveSuffix := true
	for _, c := range candidates {
		m := reKeyNumericSuffix.FindStringSubmatch(c)
		if m == nil {
			allHaveSuffix = false
			break
		}
		n, _ := strconv.Atoi(m[1])
		if n < lowestSuffix {
			lowestSuffix = n
			bestBySuffix = c
		}
	}
	if allHaveSuffix && bestBySuffix != "" {
		return bestBySuffix
	}

	// Rule 4: library already has a crossref to one of the candidates.
	// Keep it if all siblings also agree with DBLP on that parent.
	if existingCrossrefLibKey != "" {
		existingDblpKey := l.EntryFieldValueity(existingCrossrefLibKey, DBLPField)
		for _, c := range candidates {
			if c == existingDblpKey {
				if l.checkSiblingConsistency(existingCrossrefLibKey, c) {
					return c
				}
				break
			}
		}
	}

	// Rule 5: ask the user.
	return l.askUserForDblpParent(childDBLPKey, candidates)
}

// FixDblpHierarchy resolves DBLP parent ambiguities for all library entries and
// checks the crossref graph for cycles. Called as a prerequisite before any fix pass.
// Skipped when neither the bib data nor the DBLP store has changed since the last run.
func (l *TBibTeXLibrary) FixDblpHierarchy() {
	hierarchyTime := tableModTime("dblp_hierarchy")
	if hierarchyTime > 0 && !isTableDirty("dblp_hierarchy") {
		var dblpTime int64
		if meta := readDblpMeta(); meta != nil {
			if t, err := time.Parse(time.RFC3339, meta.LoadedAt); err == nil {
				dblpTime = t.UnixMicro()
			}
		}
		if hierarchyTime >= dblpTime {
			return
		}
	}

	total := countBibEntries()
	n := 0
	spinner := l.NewSpinner(ProgressFixingDblpHierarchy)
	cache := map[string][]string{}
	forEachBibEntryType(func(key, entryType string) {
		n++
		spinner.Update(n, total)
		if BibTeXBookish.Contains(entryType) {
			return
		}
		dblpKey := l.EntryFieldValueity(key, DBLPField)
		if dblpKey == "" {
			return
		}
		candidates := findCandidateDblpParents(dblpKey, cache)
		if len(candidates) <= 1 {
			return
		}
		existingCrossref := l.EntryFieldValueity(key, "crossref")
		if resolved := l.ResolveDblpParentAmbiguity(dblpKey, candidates, existingCrossref); resolved != "" {
			l.SetDblpParentOverride(dblpKey, resolved)
		}
	})
	spinner.Stop()
	l.CheckCrossrefAcyclicity()

	setTableDate("dblp_hierarchy", time.Now().UnixMicro())
	clearTableDirty("dblp_hierarchy")
}

// SetDblpParentOverride records that childDBLPKey's parent is parentDBLPKey,
// overriding whatever DBLP's data.json says.
func (l *TBibTeXLibrary) SetDblpParentOverride(childDBLPKey, parentDBLPKey string) {
	l.DblpParentOverrides[childDBLPKey] = parentDBLPKey
	l.dblpParentModified = true
}

// CheckCrossrefAcyclicity verifies the library's crossref graph is a forest
// (no cycles). Reports a warning for each cycle found, showing the full cycle path.
func (l *TBibTeXLibrary) CheckCrossrefAcyclicity() {
	next := map[string]string{}
	forEachBibEntryType(func(key, _ string) {
		if cr := l.EntryFieldValueity(key, "crossref"); cr != "" {
			next[key] = cr
		}
	})

	const (white, grey, black = 0, 1, 2)
	color := map[string]int{}
	var path []string

	var dfs func(v string)
	dfs = func(v string) {
		if v == "" || color[v] == black {
			return
		}
		if color[v] == grey {
			for i, u := range path {
				if u == v {
					cycle := append(path[i:], v)
					l.Warning(WarningCrossrefCycle, strings.Join(cycle, " → "))
					return
				}
			}
			return
		}
		color[v] = grey
		path = append(path, v)
		dfs(next[v])
		path = path[:len(path)-1]
		color[v] = black
	}

	for v := range next {
		if color[v] == white {
			dfs(v)
		}
	}
}
