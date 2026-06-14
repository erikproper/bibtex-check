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
	if !strings.HasPrefix(comment, "BibDesk Static Groups{") {
		return false
	}

	// Build a key→index map for captured entries so we can write the groups field
	// back onto each entry (making BibDesk and JabRef group handling uniform).
	var keyToIdx map[string]int
	if b.library.capturedHarvestEntries != nil {
		keyToIdx = make(map[string]int, len(*b.library.capturedHarvestEntries))
		for i, e := range *b.library.capturedHarvestEntries {
			keyToIdx[e.Key] = i
		}
	}

	groups := XMLIsolate(XMLCleanLayout(comment), "array")
	for _, group := range XMLSplit(groups, "dict") {
		propertyKeys := XMLSplit(group, "string")
		if len(propertyKeys) == 3 {
			groupName := strings.TrimSpace(propertyKeys[0])
			entries := propertyKeys[1]
			for _, key := range strings.Split(XMLCleanGroupList(entries), ",") {
				key = strings.TrimSpace(key)
				if keyToIdx != nil {
					// Harvest-capture mode: write group onto the entry only.
					// Do NOT update GroupEntries here — syncGroupMembershipsFromBib
					// must see the pre-parse DB state to detect new additions.
					if idx, ok := keyToIdx[key]; ok {
						e := &(*b.library.capturedHarvestEntries)[idx]
						if e.Fields["groups"] == "" {
							e.Fields["groups"] = groupName
						} else {
							e.Fields["groups"] += ", " + groupName
						}
					}
				} else {
					b.library.GroupEntries.AddValueToStringSetMap(groupName, key)
				}
			}
		}
	}
	return true
}
