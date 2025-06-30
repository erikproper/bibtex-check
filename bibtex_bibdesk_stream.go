/*
 *
 *  Module: bibtex_bibdesk_stream
 *
 * This module extends the TBibTeXStream type with the ability to parse BibDesk's static group definitions
 *
 * Creator: Henderik A. Proper (e.proper@acm.org)
 *
 * Version of: 27.06.2025
 *
 */

package main

import (
	"strings"
)

func XMLCleanLayout(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, string(NewlineCharacter), ""), string(TabCharacter), ""))
}

func XMLCleanGroupList(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
}
func XMLIsolate(s, tag string) string {
	_, right, _ := strings.Cut(s, "<"+tag+">")
	middle, _, _ := strings.Cut(right, "</"+tag+">")

	return middle
}

func XMLSplit(s, tag string) []string {
	sequence := strings.Split(s, "</"+tag+">")

	for i := range sequence {
		sequence[i] = XMLIsolate(sequence[i], tag)
	}

	return sequence
}

func (b *TBibTeXStream) BibDeskStaticGroupDefinition(comment string) bool {
	if strings.HasPrefix(comment, "BibDesk Static Groups{") {
		groups := XMLIsolate(XMLCleanLayout(comment), "array")

		for _, group := range XMLSplit(groups, "dict") {
			propertyKeys := XMLSplit(group, "string")

			if len(propertyKeys) == 3 {
				group := propertyKeys[0]
				entries := propertyKeys[1]

				for _, key := range strings.Split(XMLCleanGroupList(entries), ",") {
					b.library.GroupEntries.AddValueToStringSetMap(strings.TrimSpace(group), key)
				}
			}
		}
	}

	return true
}
