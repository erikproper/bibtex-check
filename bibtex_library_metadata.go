/*
 *
 * Module: bibtex_library_metadata
 *
 * Per-entry metadata stored as entry_metadata.json.  Replaces the legacy
 * pdf_confirmed_ok.csv, dblp_key_missing.csv, and entry_lineage.csv files.
 *
 * Format: map[entryKey]map[propertyName]value — all values are strings.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 30.05.2026
 *
 */

package main

import (
	"encoding/json"
	"os"
)

// TEntryMetadata maps entry keys to per-entry property maps.
// Property naming conventions:
//   - Simple date-stamped flags: MetaProp* constants below.
//   - Lineage: "lineage:<field>:source" and "lineage:<field>:edited".
type TEntryMetadata map[string]map[string]string

// Metadata property name constants.
const (
	MetaPropPdfConfirmedOk    = "pdf_confirmed_ok"
	MetaPropDblpKeyMissing    = "dblp_key_missing"
	MetaPropAlignVolumeWaived  = "align_volume_waived"
	MetaPropAlignEditionWaived = "align_edition_waived"
	MetaPropAlignCountryWaived = "align_country_waived"
	MetaPropUrlCheckDate      = "url_check_date"   // ISO date of last URL plausibility check
	MetaPropUrlCheckStatus    = "url_check_status" // "ok" or "dead"
	MetaPropWaivedDoublePdf   = "waived_double_pdf" // MD5 of shared PDF content — waives duplicate-PDF warning
)

// lineageSourceKey returns the metadata property key for a lineage source record.
func lineageSourceKey(field string) string { return "lineage:" + field + ":source" }

// lineageEditedKey returns the metadata property key for a lineage edited flag.
func lineageEditedKey(field string) string { return "lineage:" + field + ":edited" }

// GetMetadata returns the value of property prop for entry key, or "" if absent.
func (l *TBibTeXLibrary) GetMetadata(key, prop string) string {
	if m, ok := l.Metadata[key]; ok {
		return m[prop]
	}
	return ""
}

// HasMetadata reports whether entry key has a non-empty value for property prop.
func (l *TBibTeXLibrary) HasMetadata(key, prop string) bool {
	return l.GetMetadata(key, prop) != ""
}

// SetMetadata sets property prop to value for entry key, writes through to the DB,
// and marks metadata modified (for the end-of-run JSON backup).
func (l *TBibTeXLibrary) SetMetadata(key, prop, value string) {
	if _, ok := l.Metadata[key]; !ok {
		l.Metadata[key] = map[string]string{}
	}
	l.Metadata[key][prop] = value
	l.metadataModified = true
	db.Exec(`INSERT INTO entry_metadata (entry_key, property, value) VALUES (?, ?, ?)
	          ON CONFLICT(entry_key, property) DO UPDATE SET value = excluded.value`,
		key, prop, value)
}

// DeleteMetadata removes property prop from entry key's metadata, writes through to
// the DB, and marks metadata modified (for the end-of-run JSON backup).
func (l *TBibTeXLibrary) DeleteMetadata(key, prop string) {
	if m, ok := l.Metadata[key]; ok {
		if _, exists := m[prop]; exists {
			delete(m, prop)
			if len(m) == 0 {
				delete(l.Metadata, key)
			}
			l.metadataModified = true
			db.Exec(`DELETE FROM entry_metadata WHERE entry_key = ? AND property = ?`, key, prop)
		}
	}
}

// AllEntriesWithProp returns a snapshot map of entry key → value for all entries
// that have property prop set.
func (l *TBibTeXLibrary) AllEntriesWithProp(prop string) map[string]string {
	result := map[string]string{}
	for key, props := range l.Metadata {
		if val, ok := props[prop]; ok {
			result[key] = val
		}
	}
	return result
}

// ReadMetadataFile loads entry metadata from the DB.
func (l *TBibTeXLibrary) ReadMetadataFile() {
	loadEntryMetadataFromDb(l)
	l.metadataModified = false
}

// WriteMetadataFile writes entry metadata to the DB and keeps entry_metadata.json
// as a human-readable backup.
func (l *TBibTeXLibrary) WriteMetadataFile() {
	if l.NoMetadataFileWriting || !l.metadataModified {
		return
	}
	saveEntryMetadataToDb(l)
	path := bibTeXFolder + bibTeXBaseName + EntryMetadataFilePath
	data, err := json.MarshalIndent(l.Metadata, "", "  ")
	if err != nil {
		l.Warning("Could not marshal metadata: %s", err)
		return
	}
	if writeErr := os.WriteFile(path, data, 0644); writeErr != nil {
		l.Warning("Could not write %s: %s", path, writeErr)
	}
	l.metadataModified = false
}
