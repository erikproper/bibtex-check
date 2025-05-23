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
		DBLPBibFile := l.FilesRoot + "DBLPScraper/bib/" + DBLPKey + "/bib"
		if FileExists(DBLPBibFile) {
			l.Progress("Syncing entry %s with the DBLP version %s", key, DBLPKey)

			l.IgnoreIllegalFields = true
			if l.ParseBibFile(DBLPBibFile) {
				l.IgnoreIllegalFields = false

				l.MergeEntries(KeyForDBLP(DBLPKey), key)
				l.EntryFields[key]["dblp"] = DBLPKey

				return true
			}
			l.IgnoreIllegalFields = false
		}
	}

	return false
}

// / Really need both!?
func (l *TBibTeXLibrary) MaybeAddDBLPEntry(DBLPKey string) string {
	key := l.NewKey()
	if l.MaybeMergeDBLPEntry(DBLPKey, key) {
		return key
	}

	return ""
}

func (l *TBibTeXLibrary) MaybeSyncDBLPEntry(key string) {
	DBLPKey := l.EntryFieldValueity(key, "dblp")

	if DBLPKey != "" {
		l.MaybeMergeDBLPEntry(DBLPKey, key)
	}
}

func (l *TBibTeXLibrary) MaybeAddDBLPChildEntry(DBLPKey, crossref string) string {
	key := l.MaybeAddDBLPEntry(DBLPKey)
	if key != "" && crossref != "" {
		l.CheckNeedToSplitBookishEntry(key)

		l.MergeEntries(l.EntryFieldValueity(key, "crossref"), crossref)

		l.EntryFields[key]["crossref"] = crossref

		return key
	}

	return ""
}
