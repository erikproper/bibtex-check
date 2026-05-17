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
 * Version of: 05.05.2026
 *
 */

package main

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
	commitBibTransaction()

	if changed {
		bibEntriesModified = true
		l.CheckEntry(l.buildEntry(key))
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
