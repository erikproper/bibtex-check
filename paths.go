/*
 *
 * Module: paths
 *
 * This module provides definitions of file paths and names
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 18.01.2026
 *
 */

package main

import "strings"

var (
	bibTeXFolder   string
	bibTeXBaseName string
)

// stripKnownBaseExtension removes a trailing known library extension from path
// so accidental tab-completion (e.g. "-base foo.bib") still works.
func stripKnownBaseExtension(path string) string {
	for _, ext := range []string{BibFileExtension, cacheFileExtension, ".sqlite3", ConfigFileExtension, LockFileExtension} {
		if strings.HasSuffix(path, ext) {
			return path[:len(path)-len(ext)]
		}
	}
	return path
}
