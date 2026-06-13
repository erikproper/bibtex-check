/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: EntryLineage
 *
 * Tracks the source and edit status of library field values. Used to implement
 * the priority-based field update interaction model: when a challenger value
 * arrives from a known source (e.g. DBLP), the lineage record tells us whether
 * the current value came from a higher-, equal-, or lower-priority source, and
 * whether the user has deliberately diverged from what that source provides.
 *
 * Lineage records are stored in entry_metadata.json under the keys
 * "lineage:<field>:source" and "lineage:<field>:edited".
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 30.05.2026
 *
 */

package main

import "strings"

// TLineageRecord tracks where a field value came from and whether the user
// has deliberately diverged from what that source would provide.
type TLineageRecord struct {
	Source string // "dblp", "orcid", "" (unknown / manually set)
	Edited bool   // true when current value diverges from source's latest provision
}

// lineagePriority maps source names to numeric priority values.
// Higher value = higher authority. No two entries share the same value.
// The zero value "" (unknown/manual) has the lowest priority.
var lineagePriority = map[string]int{
	"":     0,   // unknown or manually set without a confirmed source
	"dblp": 100, // DBLP XML import
	// "orcid": 150, (future)
}

// dblpAutoAcceptFields lists DBLP fields that are accepted automatically
// without a user prompt when DBLP re-syncs a value it previously provided
// (equal-priority re-sync with a different IndexTitle).
// These are "factual" fields where DBLP is authoritative and human review
// adds little value.
var dblpAutoAcceptFields TStringSet

// dblpKnownFields lists DBLP fields where an absent (empty) value is
// trustworthy: if DBLP has no value for this field, the field should be
// empty, and DBLP is allowed to clear our existing value.
var dblpKnownFields TStringSet

func init() {
	dblpAutoAcceptFields = TStringSetNew()
	dblpAutoAcceptFields.Add(
		// EntryTypeField is intentionally excluded: entry type is structural and user
		// edits (e.g. incollection vs inproceedings) must not be silently overridden.
		"year", "pages", "doi", "number",
		"publisher", "series", "isbn", "issn", "month",
	)
	dblpKnownFields = TStringSetNew()
	dblpKnownFields.Add("series", "number")
}

// sourceFromChallengeKey derives the source identifier from a challenge key.
// Keys prefixed "DBLP:" originate from the DBLP file store; all others are
// treated as unknown (library-internal merges, manual edits, etc.).
func sourceFromChallengeKey(challengeKey string) string {
	if strings.HasPrefix(challengeKey, "DBLP:") {
		return "dblp"
	}
	return ""
}

// lineagePriorityOf returns the numeric priority for a source name.
// Unrecognised sources default to 0 (same as unknown).
func lineagePriorityOf(source string) int {
	if p, ok := lineagePriority[source]; ok {
		return p
	}
	return 0
}

// getLineage returns the lineage record for (key, field), or the zero value
// {Source: "", Edited: false} when no record exists.
func (l *TBibTeXLibrary) getLineage(key, field string) TLineageRecord {
	return TLineageRecord{
		Source: l.GetMetadata(key, lineageSourceKey(field)),
		Edited: l.GetMetadata(key, lineageEditedKey(field)) == "true",
	}
}

// setLineage updates the lineage record for (key, field). The zero value
// (source="", edited=false) is deleted rather than stored to keep metadata sparse.
func (l *TBibTeXLibrary) setLineage(key, field, source string, edited bool) {
	if source == "" && !edited {
		l.DeleteMetadata(key, lineageSourceKey(field))
		l.DeleteMetadata(key, lineageEditedKey(field))
		return
	}
	current := l.getLineage(key, field)
	if current.Source == source && current.Edited == edited {
		return
	}
	if source != "" {
		l.SetMetadata(key, lineageSourceKey(field), source)
	} else {
		l.DeleteMetadata(key, lineageSourceKey(field))
	}
	editedStr := "false"
	if edited {
		editedStr = "true"
	}
	l.SetMetadata(key, lineageEditedKey(field), editedStr)
}
