/*
 *
 * Module: bibtex_library_dblp
 *
 * This module is concerned with dblp specific functions
 *
 * Creator: Henderik A. Proper (erikproper@fastmail.com)
 *
 * Version of: 28.05.2024
 *
 */

package main

func KeyForDBLP(key string) string {
	return "DBLP:" + key
}

func (l *TBibTeXLibrary) MaybeMergeDBLPEntry(DBLPKey, key string) bool {
	if key != "" && DBLPKey != "" {
		// Via string constant with %s
		DBLPBibFile := l.FilesRoot + "DBLPScraper/bib/" + DBLPKey + "/bib"
		
		if FileExists(DBLPBibFile) {
			l.Progress("Syncing entry %s with the DBLP version %s", key, DBLPKey)

			if l.ParseRawBibFile(DBLPBibFile) {
				l.MergeEntries(KeyForDBLP(DBLPKey), key)
				l.EntryFields[key][DBLPField] = DBLPKey
				l.CheckEntry(key)

				return true
			}
		}
	}

	return false
}

// / Really need both!?
func (l *TBibTeXLibrary) MaybeAddDBLPEntry(DBLPKey string) string {
	if key := l.NewKey(); l.MaybeMergeDBLPEntry(DBLPKey, key) {
		return key
	}

	return ""
}

func (l *TBibTeXLibrary) MaybeSyncDBLPEntry(key string) {
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

		l.EntryFields[key]["crossref"] = crossref

		l.CheckNeedToMergeForEqualTitles(key)

		return key
	}

	return ""
}
