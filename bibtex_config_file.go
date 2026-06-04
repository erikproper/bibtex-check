/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: FoldersFile
 *
 * Loads the per-library bootstrap file (<basename>.folders), writes it when absent,
 * and migrates the legacy <basename>.config file on the first run after upgrading.
 *
 * The .folders file holds ONLY the two settings that must be known before the DB can
 * be opened: global_folder and cache_folder. All other settings (key_prefix,
 * csv_delimiter, backup_folder) live in the DB config table.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 03.06.2026
 *
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// TBibTeXFolders is the structure of the <basename>.folders bootstrap file.
// It holds the path settings required to locate the database and backup files.
type TBibTeXFolders struct {
	GlobalFolder     string `json:"global_folder"`
	DblpFolderLegacy string `json:"dblp_folder,omitempty"` // migrated to global_folder on next write
	CacheFolder      string `json:"cache_folder"`
	BackupFolder     string `json:"backup_folder,omitempty"` // optional; overrides DB config value
	// key_prefix was here in v22.x; v23.0 moves it to the DB config table only.
	// Read and migrate on first run, but never write back.
	KeyPrefixLegacy string `json:"key_prefix,omitempty"`
}

var (
	csvDelimiter = ";"
	keyPrefix    = ""
	globalFolder = "" // set by loadBibTeXFolders; defaults to bibTeXFolder when empty
	backupFolder = "" // set by loadBibTeXSettings (from DB); defaults to bibTeXFolder+bibTeXBaseName+".backups/" when empty
	cacheFolder  = "" // set by loadBibTeXFolders; defaults to bibTeXFolder when empty
)

func writeBibTeXFolders(foldersPath string, f TBibTeXFolders) {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not marshal folders file: %s\n", err)
		return
	}
	if err := os.WriteFile(foldersPath, append(data, '\n'), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write folders file %s: %s\n", foldersPath, err)
	}
}

// promptKeyPrefix asks the user for a two-uppercase-letter key prefix and
// loops until a valid answer is given. Exits if stdin is not interactive.
func promptKeyPrefix() string {
	for {
		raw, err := Reporting.AskForInput("Enter a two-uppercase-letter key prefix for this library (e.g. EP)")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: stdin is not interactive; set key_prefix in the folders file.")
			os.Exit(1)
		}
		s := strings.ToUpper(raw)
		if len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' {
			return s
		}
		fmt.Fprintln(os.Stderr, "Key prefix must be exactly two uppercase letters.")
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}

// loadBibTeXFolders reads <basename>.folders (bootstrap + key_prefix).
// On first run after upgrading from a pre-13.3b binary, it reads the legacy
// <basename>.config file instead, copies all non-bootstrap fields into memory
// for maybeBootstrapConfigFromFile() to persist to the DB, then writes the new
// .folders file with bootstrap fields only.
func loadBibTeXFolders(foldersPath string) {
	var f TBibTeXFolders
	needsWrite := false

	data, readErr := os.ReadFile(foldersPath)
	if readErr != nil {
		// .folders absent — try legacy .config for one-time migration.
		configPath := strings.TrimSuffix(foldersPath, FoldersFileExtension) + ConfigFileExtension
		legacyData, legacyErr := os.ReadFile(configPath)
		if legacyErr == nil {
			var legacy struct {
				CSVDelimiter     string `json:"csv_delimiter"`
				KeyPrefix        string `json:"key_prefix"`
				GlobalFolder     string `json:"global_folder"`
				DblpFolderLegacy string `json:"dblp_folder,omitempty"`
				BackupFolder     string `json:"backup_folder"`
				CacheFolder      string `json:"cache_folder"`
			}
			if json.Unmarshal(legacyData, &legacy) == nil {
				// Carry non-bootstrap fields into memory so maybeBootstrapConfigFromFile
				// can write them to the DB config table on the first open.
				if legacy.CSVDelimiter != "" {
					csvDelimiter = legacy.CSVDelimiter
				}
				if legacy.BackupFolder != "" {
					backupFolder = expandHome(legacy.BackupFolder)
					if !strings.HasSuffix(backupFolder, "/") {
						backupFolder += "/"
					}
				}
				f.GlobalFolder = legacy.GlobalFolder
				f.DblpFolderLegacy = legacy.DblpFolderLegacy
				f.CacheFolder = legacy.CacheFolder
				f.KeyPrefixLegacy = legacy.KeyPrefix
				fmt.Fprintf(os.Stderr, "Migrating %s.config → %s.folders\n",
					strings.TrimSuffix(foldersPath, FoldersFileExtension),
					strings.TrimSuffix(foldersPath, FoldersFileExtension))
			}
		}
		needsWrite = true
	} else if err := json.Unmarshal(data, &f); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not parse folders file %s: %s\n", foldersPath, err)
		needsWrite = true
	}

	// Migrate legacy dblp_folder → global_folder on next write.
	if f.GlobalFolder == "" && f.DblpFolderLegacy != "" {
		f.GlobalFolder = f.DblpFolderLegacy
		needsWrite = true
	}
	f.DblpFolderLegacy = ""

	// Migrate key_prefix from folders to DB (v23.0): carry value into memory so
	// maybeBootstrapConfigFromFile can persist it to the config table, then strip
	// it from the struct so it is no longer written to the .folders file.
	if f.KeyPrefixLegacy != "" {
		keyPrefix = f.KeyPrefixLegacy
		f.KeyPrefixLegacy = ""
		needsWrite = true
	}
	if f.GlobalFolder != "" {
		globalFolder = expandHome(f.GlobalFolder)
		if !strings.HasSuffix(globalFolder, "/") {
			globalFolder += "/"
		}
	}
	if f.CacheFolder != "" {
		cacheFolder = expandHome(f.CacheFolder)
		if !strings.HasSuffix(cacheFolder, "/") {
			cacheFolder += "/"
		}
	}
	if f.BackupFolder != "" {
		backupFolder = expandHome(f.BackupFolder)
		if !strings.HasSuffix(backupFolder, "/") {
			backupFolder += "/"
		}
	}

	// key_prefix is no longer stored in .folders. If it is still absent after
	// reading the legacy field, it will be prompted and written to the DB config
	// table by loadBibTeXSettings() once the DB is open.

	if needsWrite {
		writeBibTeXFolders(foldersPath, f)
	}
}
