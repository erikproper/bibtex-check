/*
 *
 * Module: bibtex_library_render
 *
 * Renders library entries as self-contained BibTeX, HTML, or plain text.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 11.05.2026
 *
 */

package main

import (
	"fmt"
	stdlib_html "html"
	"strings"
)

// renderNonExportFields is the set of fields omitted from -render_as_bibtex output.
// Unlike bibGetNonExportFields it retains preferredalias so consumers get a citable key.
var renderNonExportFields = func() TStringSet {
	s := TStringSetNew()
	s.Add(
		GroupsField, DBLPField, EntryTypeField,
		LocalURLField, "date-added", "date-modified",
		"researchgate", "abstract", "ketwords", "repositum",
		"owner", "creationdate", "modificationdate", JabrefFileField,
		"bdsk-url-1", "bdsk-url-2", "bdsk-url-3", "bdsk-url-4", "bdsk-url-5",
		"bdsk-url-6", "bdsk-url-7", "bdsk-url-8", "bdsk-url-9",
		"bdsk-file-1", "bdsk-file-2", "bdsk-file-3", "bdsk-file-4", "bdsk-file-5",
		"bdsk-file-6", "bdsk-file-7", "bdsk-file-8", "bdsk-file-9",
	)
	return s
}()

// --- TeX → HTML conversion ---

// applyTeXCommand replaces \cmd{...} with openTag content closeTag.
// Handles one level of nested braces within the command argument.
func applyTeXCommand(s, cmd, openTag, closeTag string) string {
	prefix := cmd + "{"
	for {
		start := strings.Index(s, prefix)
		if start < 0 {
			break
		}
		after := start + len(prefix)
		depth := 1
		pos := after
		for pos < len(s) && depth > 0 {
			switch s[pos] {
			case '{':
				depth++
			case '}':
				depth--
			}
			if depth > 0 {
				pos++
			}
		}
		if depth != 0 {
			break
		}
		s = s[:start] + openTag + s[after:pos] + closeTag + s[pos+1:]
	}
	return s
}

// texAccentPairs maps TeX accent sequences to Unicode characters.
// Two-brace forms (e.g. {\'{e}}) are listed before one-brace forms ({\'e})
// so the longer match is replaced first.
var texAccentPairs [][2]string

func init() {
	// grave accent (\`) cannot appear in raw string literals; build via concatenation.
	gr := func(l string) string { return "{\\" + "`" + l + "}" }
	gr2 := func(l string) string { return "{\\" + "`{" + l + "}}" }

	texAccentPairs = [][2]string{
		// Acute (')
		{`{\'{a}}`, "á"}, {`{\'a}`, "á"},
		{`{\'{e}}`, "é"}, {`{\'e}`, "é"},
		{`{\'{\i}}`, "í"}, {`{\'\i}`, "í"},
		{`{\'{o}}`, "ó"}, {`{\'o}`, "ó"},
		{`{\'{u}}`, "ú"}, {`{\'u}`, "ú"},
		{`{\'{y}}`, "ý"}, {`{\'y}`, "ý"},
		{`{\'{A}}`, "Á"}, {`{\'A}`, "Á"},
		{`{\'{E}}`, "É"}, {`{\'E}`, "É"},
		{`{\'{I}}`, "Í"}, {`{\'I}`, "Í"},
		{`{\'{O}}`, "Ó"}, {`{\'O}`, "Ó"},
		{`{\'{U}}`, "Ú"}, {`{\'U}`, "Ú"},
		{`{\'{Y}}`, "Ý"}, {`{\'Y}`, "Ý"},
		{`{\'{c}}`, "ć"}, {`{\'c}`, "ć"},
		{`{\'{C}}`, "Ć"}, {`{\'C}`, "Ć"},
		{`{\'{n}}`, "ń"}, {`{\'n}`, "ń"},
		{`{\'{N}}`, "Ń"}, {`{\'N}`, "Ń"},
		{`{\'{s}}`, "ś"}, {`{\'s}`, "ś"},
		{`{\'{S}}`, "Ś"}, {`{\'S}`, "Ś"},
		{`{\'{z}}`, "ź"}, {`{\'z}`, "ź"},
		{`{\'{Z}}`, "Ź"}, {`{\'Z}`, "Ź"},
		// Grave (`)
		{gr2("a"), "à"}, {gr("a"), "à"},
		{gr2("e"), "è"}, {gr("e"), "è"},
		{gr2("\\i"), "ì"}, {gr("\\i"), "ì"},
		{gr2("o"), "ò"}, {gr("o"), "ò"},
		{gr2("u"), "ù"}, {gr("u"), "ù"},
		{gr2("A"), "À"}, {gr("A"), "À"},
		{gr2("E"), "È"}, {gr("E"), "È"},
		{gr2("I"), "Ì"}, {gr("I"), "Ì"},
		{gr2("O"), "Ò"}, {gr("O"), "Ò"},
		{gr2("U"), "Ù"}, {gr("U"), "Ù"},
		// Circumflex (^)
		{`{\^{a}}`, "â"}, {`{\^a}`, "â"},
		{`{\^{e}}`, "ê"}, {`{\^e}`, "ê"},
		{`{\^{\i}}`, "î"}, {`{\^\i}`, "î"},
		{`{\^{o}}`, "ô"}, {`{\^o}`, "ô"},
		{`{\^{u}}`, "û"}, {`{\^u}`, "û"},
		{`{\^{A}}`, "Â"}, {`{\^A}`, "Â"},
		{`{\^{E}}`, "Ê"}, {`{\^E}`, "Ê"},
		{`{\^{I}}`, "Î"}, {`{\^I}`, "Î"},
		{`{\^{O}}`, "Ô"}, {`{\^O}`, "Ô"},
		{`{\^{U}}`, "Û"}, {`{\^U}`, "Û"},
		// Umlaut (")
		{`{\"{a}}`, "ä"}, {`{\"a}`, "ä"},
		{`{\"{e}}`, "ë"}, {`{\"e}`, "ë"},
		{`{\"{\i}}`, "ï"}, {`{\"\i}`, "ï"},
		{`{\"{o}}`, "ö"}, {`{\"o}`, "ö"},
		{`{\"{u}}`, "ü"}, {`{\"u}`, "ü"},
		{`{\"{y}}`, "ÿ"}, {`{\"y}`, "ÿ"},
		{`{\"{A}}`, "Ä"}, {`{\"A}`, "Ä"},
		{`{\"{E}}`, "Ë"}, {`{\"E}`, "Ë"},
		{`{\"{I}}`, "Ï"}, {`{\"I}`, "Ï"},
		{`{\"{O}}`, "Ö"}, {`{\"O}`, "Ö"},
		{`{\"{U}}`, "Ü"}, {`{\"U}`, "Ü"},
		{`{\"{Y}}`, "Ÿ"}, {`{\"Y}`, "Ÿ"},
		// Tilde (~)
		{`{\~{a}}`, "ã"}, {`{\~a}`, "ã"},
		{`{\~{n}}`, "ñ"}, {`{\~n}`, "ñ"},
		{`{\~{o}}`, "õ"}, {`{\~o}`, "õ"},
		{`{\~{A}}`, "Ã"}, {`{\~A}`, "Ã"},
		{`{\~{N}}`, "Ñ"}, {`{\~N}`, "Ñ"},
		{`{\~{O}}`, "Õ"}, {`{\~O}`, "Õ"},
		// Cedilla (\c)
		{`{\c{c}}`, "ç"}, {`{\c c}`, "ç"},
		{`{\c{C}}`, "Ç"}, {`{\c C}`, "Ç"},
		{`{\c{s}}`, "ş"}, {`{\c s}`, "ş"},
		{`{\c{S}}`, "Ş"}, {`{\c S}`, "Ş"},
		// Ring (\r)
		{`{\r{a}}`, "å"}, {`{\r a}`, "å"},
		{`{\r{A}}`, "Å"}, {`{\r A}`, "Å"},
		// Caron (\v)
		{`{\v{c}}`, "č"}, {`{\v c}`, "č"},
		{`{\v{C}}`, "Č"}, {`{\v C}`, "Č"},
		{`{\v{s}}`, "š"}, {`{\v s}`, "š"},
		{`{\v{S}}`, "Š"}, {`{\v S}`, "Š"},
		{`{\v{z}}`, "ž"}, {`{\v z}`, "ž"},
		{`{\v{Z}}`, "Ž"}, {`{\v Z}`, "Ž"},
		{`{\v{n}}`, "ň"}, {`{\v n}`, "ň"},
		{`{\v{N}}`, "Ň"}, {`{\v N}`, "Ň"},
	}
}

// htmlEncodeNonASCII replaces every non-ASCII rune with a decimal numeric
// character reference (&#N;) so the output is safe regardless of the page
// charset declaration.
func htmlEncodeNonASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 127 {
			fmt.Fprintf(&b, "&#%d;", int(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// texToHTML converts BibTeX field value TeX markup to clean HTML.
func texToHTML(s string) string {
	// Dashes (before brace removal to avoid false matches on brace-only strings)
	s = strings.ReplaceAll(s, "---", "—")
	s = strings.ReplaceAll(s, "--", "–")

	// Special characters
	s = strings.ReplaceAll(s, "~", "&nbsp;")
	s = strings.ReplaceAll(s, `\&`, "&amp;")

	// TeX emphasis commands
	s = applyTeXCommand(s, `\emph`, "<em>", "</em>")
	s = applyTeXCommand(s, `\textit`, "<em>", "</em>")
	s = applyTeXCommand(s, `\textbf`, "<strong>", "</strong>")
	s = applyTeXCommand(s, `\texttt`, "<code>", "</code>")

	// Accented characters (two-brace forms first)
	for _, p := range texAccentPairs {
		s = strings.ReplaceAll(s, p[0], p[1])
	}

	// Special letter sequences (handle before brace removal)
	s = strings.ReplaceAll(s, `{\ss}`, "ß")
	s = strings.ReplaceAll(s, `{\ae}`, "æ")
	s = strings.ReplaceAll(s, `{\AE}`, "Æ")
	s = strings.ReplaceAll(s, `{\oe}`, "œ")
	s = strings.ReplaceAll(s, `{\OE}`, "Œ")
	s = strings.ReplaceAll(s, `{\aa}`, "å")
	s = strings.ReplaceAll(s, `{\AA}`, "Å")
	s = strings.ReplaceAll(s, `{\o}`, "ø")
	s = strings.ReplaceAll(s, `{\O}`, "Ø")
	s = strings.ReplaceAll(s, `{\i}`, "ı")
	s = strings.ReplaceAll(s, `{\l}`, "ł")
	s = strings.ReplaceAll(s, `{\L}`, "Ł")
	s = strings.ReplaceAll(s, `\ss`, "ß")
	s = strings.ReplaceAll(s, `\ae`, "æ")
	s = strings.ReplaceAll(s, `\AE`, "Æ")
	s = strings.ReplaceAll(s, `\oe`, "œ")
	s = strings.ReplaceAll(s, `\OE`, "Œ")
	s = strings.ReplaceAll(s, `\aa`, "å")
	s = strings.ReplaceAll(s, `\AA`, "Å")
	s = strings.ReplaceAll(s, `\o`, "ø")
	s = strings.ReplaceAll(s, `\O`, "Ø")

	// Remove remaining TeX protection braces
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")

	return s
}

// texToText converts BibTeX field value markup to plain text.
func texToText(s string) string {
	html := texToHTML(s)
	for strings.Contains(html, "<") {
		start := strings.Index(html, "<")
		end := strings.Index(html[start:], ">")
		if end < 0 {
			break
		}
		html = html[:start] + html[start+end+1:]
	}
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	return stdlib_html.UnescapeString(html)
}

// joinDots joins citation segments with ". ", but uses " " when the preceding
// segment already ends with "." to avoid doubled periods (e.g. "A. B." not "A.. B.").
// A final "." is appended unless the last segment already ends with one.
func joinDots(parts []string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			if strings.HasSuffix(parts[i-1], ".") {
				b.WriteString(" ")
			} else {
				b.WriteString(". ")
			}
		}
		b.WriteString(p)
	}
	if b.Len() > 0 && !strings.HasSuffix(b.String(), ".") {
		b.WriteString(".")
	}
	return b.String()
}

// normalNameOrder converts a BibTeX "Last, First" name to "First Last".
// Names without a comma are returned unchanged.
func normalNameOrder(name string) string {
	depth := 0
	for i := 0; i < len(name)-1; i++ {
		switch name[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 && name[i+1] == ' ' {
				return name[i+2:] + " " + name[:i]
			}
		}
	}
	return name
}

// formatNameList formats a BibTeX "A and B and C" name list as "First Last" order,
// joining with commas and "and": "A, B, and C".
func formatNameList(names string) string {
	parts := strings.Split(names, " and ")
	for i, p := range parts {
		parts[i] = normalNameOrder(strings.TrimSpace(p))
	}
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
	}
}

// editorSuffix returns "(ed.)" or "(eds.)" based on whether there are multiple editors.
func editorSuffix(editors string) string {
	if strings.Contains(editors, " and ") {
		return "(eds.)"
	}
	return "(ed.)"
}

// mergedField returns the field value for entry, falling back to parent for inheritable fields.
// Values are returned with field-value normalisation applied.
func (l *TBibTeXLibrary) mergedField(entry, parent *TBibTeXEntry, field string) string {
	if v := entry.FieldValue(field); v != "" {
		return l.MapEntryFieldValue(entry.Key, field, v)
	}
	if parent != nil && BibTeXInheritableFields.Contains(field) {
		if v := parent.FieldValue(field); v != "" {
			return l.MapFieldValue(field, v)
		}
	}
	return ""
}

// resolveParent returns the parent entry for a crossref, or nil.
func (l *TBibTeXLibrary) resolveParent(entry *TBibTeXEntry) (*TBibTeXEntry, string) {
	crossref := entry.FieldValue("crossref")
	if crossref == "" {
		return nil, ""
	}
	rc := l.MapEntryKey(crossref)
	if rc == "" {
		rc = crossref
	}
	p := loadEntryFromDb(rc)
	if !p.Exists() {
		return nil, ""
	}
	return p, rc
}

// renderAsBibTeX produces a self-contained BibTeX string for the entry.
// Inherited crossref fields are merged into the child; the crossref field itself is
// omitted. The crossref parent entry is appended if present.
// renderAsBibTeX renders entry key as BibTeX. When outputKey is non-empty and
// different from key it is used as the @type{KEY} identifier and preferredalias
// is suppressed (the alias is already the key).
func (l *TBibTeXLibrary) renderAsBibTeX(key string, outputKey ...string) string {
	entry := loadEntryFromDb(key)
	if !entry.Exists() {
		return ""
	}

	bibKey := key
	suppressPreferredAlias := false
	if len(outputKey) > 0 && outputKey[0] != "" && outputKey[0] != key {
		bibKey = outputKey[0]
		suppressPreferredAlias = true
	}

	parent, resolvedCrossref := l.resolveParent(entry)

	result := "@" + entry.EntryType() + "{" + bibKey + ",\n"
	if parent != nil {
		result += FormatBibTeXFieldAssignment("", "crossref", resolvedCrossref)
	}
	for _, field := range BibTeXAllowedEntryFields[entry.EntryType()].Set().ElementsSorted() {
		if field == EntryTypeField || field == "crossref" || renderNonExportFields.Contains(field) {
			continue
		}
		if suppressPreferredAlias && field == PreferredAliasField {
			continue
		}
		if v := l.mergedField(entry, parent, field); v != "" {
			result += FormatBibTeXFieldAssignment("", field, v)
		}
	}
	result += "}\n"

	if parent != nil {
		result += "\n" + l.EntryString(resolvedCrossref, "")
	}
	return result
}

// renderAsHTML formats the entry as an HTML bibliography reference.
func (l *TBibTeXLibrary) renderAsHTML(key string) string {
	entry := loadEntryFromDb(key)
	if !entry.Exists() {
		return ""
	}

	parent, _ := l.resolveParent(entry)

	get := func(field string) string {
		return htmlEncodeNonASCII(texToHTML(l.mergedField(entry, parent, field)))
	}

	entryType := entry.EntryType()
	author := get("author")
	editor := get("editor")
	title := get("title")
	year := get("year")
	pages := get("pages")

	person := ""
	if author != "" {
		person = formatNameList(author)
	} else if editor != "" {
		person = formatNameList(editor) + " " + editorSuffix(editor)
	}

	var dots []string
	if person != "" {
		dots = append(dots, person)
	}

	switch entryType {
	case "inproceedings", "incollection", "inbook":
		booktitle := get("booktitle")
		publisher := get("publisher")
		address := get("address")
		organization := get("organization")
		volume := get("volume")
		number := get("number")
		series := get("series")

		if title != "" {
			dots = append(dots, "\""+title+"\"")
		}

		venue := ""
		if booktitle != "" {
			venue = "In: <em>" + booktitle + "</em>"
		}
		if volume != "" {
			venue += ", vol.&nbsp;" + volume
		} else if number != "" {
			venue += ", no.&nbsp;" + number
		}
		if series != "" {
			venue += ", " + series
		}
		if pages != "" {
			venue += ", pp.&nbsp;" + pages
		}
		if venue != "" {
			dots = append(dots, venue)
		}

		pub := publisher
		if pub == "" {
			pub = organization
		}
		if address != "" {
			if pub != "" {
				pub += ", " + address
			} else {
				pub = address
			}
		}
		if year != "" {
			if pub != "" {
				pub += ", " + year
			} else {
				pub = year
			}
		}
		if pub != "" {
			dots = append(dots, pub)
		}

	case "article":
		journal := get("journal")
		volume := get("volume")
		number := get("number")

		if title != "" {
			dots = append(dots, "\""+title+"\"")
		}
		venue := ""
		if journal != "" {
			venue = "<em>" + journal + "</em>"
			if volume != "" {
				venue += ",&nbsp;" + volume
				if number != "" {
					venue += "(" + number + ")"
				}
			}
			if pages != "" {
				venue += ":" + pages
			}
		}
		if year != "" {
			if venue != "" {
				venue += ",&nbsp;" + year
			} else {
				venue = year
			}
		}
		if venue != "" {
			dots = append(dots, venue)
		}

	case "book":
		publisher := get("publisher")
		address := get("address")
		edition := get("edition")

		if title != "" {
			dots = append(dots, "<em>"+title+"</em>")
		}
		if edition != "" {
			dots = append(dots, edition+" edition")
		}
		tail := publisher
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	case "proceedings":
		publisher := get("publisher")
		address := get("address")
		organization := get("organization")
		volume := get("volume")
		series := get("series")

		if title != "" {
			dots = append(dots, "<em>"+title+"</em>")
		}
		if volume != "" && series != "" {
			dots = append(dots, series+", vol.&nbsp;"+volume)
		} else if volume != "" {
			dots = append(dots, "Vol.&nbsp;"+volume)
		}
		pub := publisher
		if pub == "" {
			pub = organization
		}
		if address != "" {
			if pub != "" {
				pub += ", " + address
			} else {
				pub = address
			}
		}
		if year != "" {
			if pub != "" {
				pub += ", " + year
			} else {
				pub = year
			}
		}
		if pub != "" {
			dots = append(dots, pub)
		}

	case "phdthesis":
		school := get("school")
		address := get("address")

		if title != "" {
			dots = append(dots, "\""+title+"\"")
		}
		dots = append(dots, "PhD Thesis")
		tail := school
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	case "mastersthesis":
		school := get("school")
		address := get("address")

		if title != "" {
			dots = append(dots, "\""+title+"\"")
		}
		dots = append(dots, "Master's Thesis")
		tail := school
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	case "techreport":
		institution := get("institution")
		number := get("number")
		address := get("address")

		if title != "" {
			dots = append(dots, "\""+title+"\"")
		}
		label := "Technical Report"
		if number != "" {
			label += "&nbsp;" + number
		}
		dots = append(dots, label)
		tail := institution
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	default:
		if title != "" {
			dots = append(dots, "\""+title+"\"")
		}
		if how := get("howpublished"); how != "" {
			dots = append(dots, how)
		}
		if year != "" {
			dots = append(dots, year)
		}
	}

	if url := l.mergedField(entry, parent, "url"); url != "" {
		dots = append(dots, `<a href="`+url+`">`+url+`</a>`)
	}
	if doi := l.mergedField(entry, parent, "doi"); doi != "" {
		dots = append(dots, `<a href="https://dx.doi.org/`+doi+`">doi:`+doi+`</a>`)
	}

	return joinDots(dots)
}

// renderAsText formats the entry as a plain-text bibliography reference.
func (l *TBibTeXLibrary) renderAsText(key string) string {
	return texToText(l.renderAsHTML(key))
}

// renderAsTeX formats the entry as a TeX bibliography reference.
// Field values are used as-is (already in TeX); structural markup uses \emph{} and ~.
func (l *TBibTeXLibrary) renderAsTeX(key string) string {
	entry := loadEntryFromDb(key)
	if !entry.Exists() {
		return ""
	}

	parent, _ := l.resolveParent(entry)

	get := func(field string) string {
		return l.mergedField(entry, parent, field)
	}

	entryType := entry.EntryType()
	author := get("author")
	editor := get("editor")
	title := get("title")
	year := get("year")
	pages := get("pages")

	person := ""
	if author != "" {
		person = formatNameList(author)
	} else if editor != "" {
		person = formatNameList(editor) + " " + editorSuffix(editor)
	}

	var dots []string
	if person != "" {
		dots = append(dots, person)
	}

	switch entryType {
	case "inproceedings", "incollection", "inbook":
		booktitle := get("booktitle")
		publisher := get("publisher")
		address := get("address")
		organization := get("organization")
		volume := get("volume")
		number := get("number")
		series := get("series")

		if title != "" {
			dots = append(dots, "``"+title+"''")
		}

		venue := ""
		if booktitle != "" {
			venue = "In: \\emph{" + booktitle + "}"
		}
		if volume != "" {
			venue += ", vol.~" + volume
		} else if number != "" {
			venue += ", no.~" + number
		}
		if series != "" {
			venue += ", " + series
		}
		if pages != "" {
			venue += ", pp.~" + pages
		}
		if venue != "" {
			dots = append(dots, venue)
		}

		pub := publisher
		if pub == "" {
			pub = organization
		}
		if address != "" {
			if pub != "" {
				pub += ", " + address
			} else {
				pub = address
			}
		}
		if year != "" {
			if pub != "" {
				pub += ", " + year
			} else {
				pub = year
			}
		}
		if pub != "" {
			dots = append(dots, pub)
		}

	case "article":
		journal := get("journal")
		volume := get("volume")
		number := get("number")

		if title != "" {
			dots = append(dots, "``"+title+"''")
		}
		venue := ""
		if journal != "" {
			venue = "\\emph{" + journal + "}"
			if volume != "" {
				venue += ",~" + volume
				if number != "" {
					venue += "(" + number + ")"
				}
			}
			if pages != "" {
				venue += ":" + pages
			}
		}
		if year != "" {
			if venue != "" {
				venue += ",~" + year
			} else {
				venue = year
			}
		}
		if venue != "" {
			dots = append(dots, venue)
		}

	case "book":
		publisher := get("publisher")
		address := get("address")
		edition := get("edition")

		if title != "" {
			dots = append(dots, "\\emph{"+title+"}")
		}
		if edition != "" {
			dots = append(dots, edition+" edition")
		}
		tail := publisher
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	case "proceedings":
		publisher := get("publisher")
		address := get("address")
		organization := get("organization")
		volume := get("volume")
		series := get("series")

		if title != "" {
			dots = append(dots, "\\emph{"+title+"}")
		}
		if volume != "" && series != "" {
			dots = append(dots, series+", vol.~"+volume)
		} else if volume != "" {
			dots = append(dots, "Vol.~"+volume)
		}
		pub := publisher
		if pub == "" {
			pub = organization
		}
		if address != "" {
			if pub != "" {
				pub += ", " + address
			} else {
				pub = address
			}
		}
		if year != "" {
			if pub != "" {
				pub += ", " + year
			} else {
				pub = year
			}
		}
		if pub != "" {
			dots = append(dots, pub)
		}

	case "phdthesis":
		school := get("school")
		address := get("address")

		if title != "" {
			dots = append(dots, "``"+title+"''")
		}
		dots = append(dots, "PhD Thesis")
		tail := school
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	case "mastersthesis":
		school := get("school")
		address := get("address")

		if title != "" {
			dots = append(dots, "``"+title+"''")
		}
		dots = append(dots, "Master's Thesis")
		tail := school
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	case "techreport":
		institution := get("institution")
		number := get("number")
		address := get("address")

		if title != "" {
			dots = append(dots, "``"+title+"''")
		}
		label := "Technical Report"
		if number != "" {
			label += "~" + number
		}
		dots = append(dots, label)
		tail := institution
		if address != "" {
			if tail != "" {
				tail += ", " + address
			} else {
				tail = address
			}
		}
		if year != "" {
			if tail != "" {
				tail += ", " + year
			} else {
				tail = year
			}
		}
		if tail != "" {
			dots = append(dots, tail)
		}

	default:
		if title != "" {
			dots = append(dots, "``"+title+"''")
		}
		if how := get("howpublished"); how != "" {
			dots = append(dots, how)
		}
		if year != "" {
			dots = append(dots, year)
		}
	}

	if url := get("url"); url != "" {
		dots = append(dots, `\\ \url{`+url+`}`)
	}
	if doi := get("doi"); doi != "" {
		dots = append(dots, `\\ \doi{`+doi+`}`)
	}

	return joinDots(dots)
}
