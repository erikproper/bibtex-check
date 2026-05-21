/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: ConfigFile
 *
 * Loads the per-library JSON config file (<basename>.config), writes it when absent
 * or when the binary version has advanced, and prompts for any required settings
 * (key prefix) that are missing.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 10.05.2026
 *
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type TBibTeXConfig struct {
	Version          string `json:"version"`
	CSVDelimiter     string `json:"csv_delimiter"`
	KeyPrefix        string `json:"key_prefix"`
	GlobalFolder     string `json:"global_folder"`
	DblpFolderLegacy string `json:"dblp_folder,omitempty"` // migrated to global_folder on next write
	BackupFolder     string `json:"backup_folder"`
}

var (
	csvDelimiter = ";"
	keyPrefix    = ""
	globalFolder = "" // set by loadBibTeXConfig; defaults to bibTeXFolder when empty
	backupFolder = "" // set by loadBibTeXConfig; defaults to bibTeXFolder+bibTeXBaseName+".backups/" when empty
)

func writeBibTeXConfig(configPath string, cfg TBibTeXConfig) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not marshal config: %s\n", err)
		return
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write config file %s: %s\n", configPath, err)
	}
}

// promptKeyPrefix asks the user for a two-uppercase-letter key prefix and
// loops until a valid answer is given. Exits if stdin is not interactive.
func promptKeyPrefix() string {
	for {
		raw, err := Reporting.AskForInput("Enter a two-uppercase-letter key prefix for this library (e.g. EP)")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error: stdin is not interactive; set key_prefix in the config file.")
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

func loadBibTeXConfig(configPath string) {
	var cfg TBibTeXConfig
	needsWrite := false

	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		needsWrite = true
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not parse config file %s: %s\n", configPath, err)
		needsWrite = true
	}

	if cfg.CSVDelimiter != "" {
		csvDelimiter = cfg.CSVDelimiter
	}
	if cfg.KeyPrefix != "" {
		keyPrefix = cfg.KeyPrefix
	}
	// Migrate legacy dblp_folder → global_folder on next config write.
	if cfg.GlobalFolder == "" && cfg.DblpFolderLegacy != "" {
		cfg.GlobalFolder = cfg.DblpFolderLegacy
		needsWrite = true
	}
	cfg.DblpFolderLegacy = "" // suppress old key on next write
	if cfg.GlobalFolder != "" {
		globalFolder = expandHome(cfg.GlobalFolder)
		if !strings.HasSuffix(globalFolder, "/") {
			globalFolder += "/"
		}
	}
	if cfg.BackupFolder != "" {
		backupFolder = expandHome(cfg.BackupFolder)
		if !strings.HasSuffix(backupFolder, "/") {
			backupFolder += "/"
		}
	}

	if keyPrefix == "" {
		keyPrefix = promptKeyPrefix()
		cfg.KeyPrefix = keyPrefix
		needsWrite = true
	}

	if cfg.Version != AppVersion {
		needsWrite = true
	}

	if needsWrite {
		cfg.Version = AppVersion
		if cfg.CSVDelimiter == "" {
			cfg.CSVDelimiter = csvDelimiter
		}
		writeBibTeXConfig(configPath, cfg)
	}
}
