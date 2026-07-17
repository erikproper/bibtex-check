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

// TSourceFieldData captures a pre-computed snapshot of one source's field delivery.
// Changed[field] is true when the source is delivering a different value than its
// last recorded signature, meaning the source has evolved since the prior run.
// Values holds the normalised field values as delivered; Changed is keyed by field name.
type TSourceFieldData struct {
	Values  map[string]string
	Changed map[string]bool
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
		EntryTypeField,
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
	if fm, ok := l.LineageMap[key]; ok {
		if rec, ok := fm[field]; ok {
			return rec
		}
	}
	return TLineageRecord{}
}

// setLineage updates the lineage record for (key, field). The zero value
// (source="", edited=false) is deleted rather than stored to keep the table sparse.
func (l *TBibTeXLibrary) setLineage(key, field, source string, edited bool) {
	current := l.getLineage(key, field)
	if current.Source == source && current.Edited == edited {
		return
	}
	if source == "" && !edited {
		if fm, ok := l.LineageMap[key]; ok {
			delete(fm, field)
			if len(fm) == 0 {
				delete(l.LineageMap, key)
			}
		}
		db.Exec(`DELETE FROM entry_lineage WHERE entry_key = ? AND field = ?`, key, field) //nolint:errcheck
		return
	}
	if _, ok := l.LineageMap[key]; !ok {
		l.LineageMap[key] = map[string]TLineageRecord{}
	}
	l.LineageMap[key][field] = TLineageRecord{Source: source, Edited: edited}
	editedInt := 0
	if edited {
		editedInt = 1
	}
	bibExec( //nolint:errcheck
		`INSERT INTO entry_lineage (entry_key, field, source, edited) VALUES (?, ?, ?, ?)
		 ON CONFLICT(entry_key, field) DO UPDATE SET source = excluded.source, edited = excluded.edited`,
		key, field, source, editedInt)
}

// getSourceFieldSignature returns the signature stored for (key, field, source),
// or "" when no signature has been recorded yet.
func (l *TBibTeXLibrary) getSourceFieldSignature(key, field, source string) string {
	if fm, ok := l.SourceSignatures[key]; ok {
		if sm, ok := fm[field]; ok {
			return sm[source]
		}
	}
	return ""
}

// setSourceContributorSignature records what source most recently delivered for one
// contributor position. role is "author" or "editor"; position is 1-based.
func (l *TBibTeXLibrary) setSourceContributorSignature(key, role string, position int, source, sig string) {
	bibExec( //nolint:errcheck
		`INSERT INTO source_contributor_signatures (entry_key, role, position, source, signature) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(entry_key, role, position, source) DO UPDATE SET signature = excluded.signature`,
		key, role, position, source, sig)
}

// setSourceFieldSignature records what source most recently delivered for (key, field).
// sig is the fully-normalized LaTeX form of the delivered value.
func (l *TBibTeXLibrary) setSourceFieldSignature(key, field, source, sig string) {
	if _, ok := l.SourceSignatures[key]; !ok {
		l.SourceSignatures[key] = map[string]map[string]string{}
	}
	if _, ok := l.SourceSignatures[key][field]; !ok {
		l.SourceSignatures[key][field] = map[string]string{}
	}
	if l.SourceSignatures[key][field][source] == sig {
		return
	}
	l.SourceSignatures[key][field][source] = sig
	bibExec( //nolint:errcheck
		`INSERT INTO source_field_signatures (entry_key, field, source, signature) VALUES (?, ?, ?, ?)
		 ON CONFLICT(entry_key, field, source) DO UPDATE SET signature = excluded.signature`,
		key, field, source, sig)
}
