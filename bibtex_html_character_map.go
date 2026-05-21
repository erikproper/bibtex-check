/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: HtmlCharacterMap
 *
 * Loads the per-folder html_character_map.csv and writes a default when absent.
 * The CSV format is: entity_name,latex_replacement
 * Maps HTML character entity names (e.g. "iacute", "agrave") directly to their
 * BibTeX/LaTeX equivalents without going through a Unicode intermediate.
 * Lines starting with # are comments and are ignored on load.
 *
 * Also builds xmlEntityPassthrough, an xml.Decoder Entity map that preserves
 * entity references as "&name;" in the decoded text so they can be stored
 * verbatim in the SQLite DB and converted at read time by dblpRawToLaTeX.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 18.05.2026
 *
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// htmlCharSection groups related entity→LaTeX entries for the default CSV.
type htmlCharSection struct {
	header  string
	entries [][2]string // [entityName, latexReplacement]
}

// Grave-accent helper used in raw-string-hostile array literals.
// g(l) returns the LaTeX short-form grave accent on letter l, e.g. g("a") = "{\`a}".
func g(l string) string { return "{\\" + "`" + l + "}" }

// gi(l) returns the dotless-i grave: {\`\i} for lowercase; for uppercase just {\`l}.
func gi() string { return "{\\" + "`" + `\i}` }

var defaultHtmlCharSections = []htmlCharSection{
	{
		"Latin-1 lowercase accented",
		[][2]string{
			{"agrave", g("a")}, {"aacute", `{\'a}`}, {"acirc", `{\^a}`}, {"atilde", `{\~a}`},
			{"auml", `{\"a}`}, {"aring", `{\aa}`}, {"aelig", `{\ae}`}, {"ccedil", `{\c{c}}`},
			{"egrave", g("e")}, {"eacute", `{\'e}`}, {"ecirc", `{\^e}`}, {"euml", `{\"e}`},
			{"igrave", gi()}, {"iacute", `{\'\i}`}, {"icirc", `{\^\i}`}, {"iuml", `{\"\i}`},
			{"eth", `{\dh}`}, {"ntilde", `{\~n}`},
			{"ograve", g("o")}, {"oacute", `{\'o}`}, {"ocirc", `{\^o}`}, {"otilde", `{\~o}`},
			{"ouml", `{\"o}`}, {"oslash", `{\o}`},
			{"ugrave", g("u")}, {"uacute", `{\'u}`}, {"ucirc", `{\^u}`}, {"uuml", `{\"u}`},
			{"yacute", `{\'y}`}, {"thorn", `{\th}`}, {"yuml", `{\"y}`}, {"szlig", `{\ss}`},
		},
	},
	{
		"Latin-1 uppercase accented",
		[][2]string{
			{"Agrave", g("A")}, {"Aacute", `{\'A}`}, {"Acirc", `{\^A}`}, {"Atilde", `{\~A}`},
			{"Auml", `{\"A}`}, {"Aring", `{\AA}`}, {"AElig", `{\AE}`}, {"Ccedil", `{\c{C}}`},
			{"Egrave", g("E")}, {"Eacute", `{\'E}`}, {"Ecirc", `{\^E}`}, {"Euml", `{\"E}`},
			{"Igrave", g("I")}, {"Iacute", `{\'I}`}, {"Icirc", `{\^I}`}, {"Iuml", `{\"I}`},
			{"ETH", `{\DH}`}, {"Ntilde", `{\~N}`},
			{"Ograve", g("O")}, {"Oacute", `{\'O}`}, {"Ocirc", `{\^O}`}, {"Otilde", `{\~O}`},
			{"Ouml", `{\"O}`}, {"Oslash", `{\O}`},
			{"Ugrave", g("U")}, {"Uacute", `{\'U}`}, {"Ucirc", `{\^U}`}, {"Uuml", `{\"U}`},
			{"Yacute", `{\'Y}`}, {"THORN", `{\TH}`},
		},
	},
	{
		"Latin extended",
		[][2]string{
			{"OElig", `{\OE}`}, {"oelig", `{\oe}`},
			{"Scaron", `{\v{S}}`}, {"scaron", `{\v{s}}`},
			{"Yuml", `{\"Y}`},
		},
	},
	{
		"Spacing and punctuation",
		[][2]string{
			{"nbsp", " "}, {"ndash", "--"}, {"mdash", "---"},
			{"lsquo", "'"}, {"rsquo", "'"}, {"ldquo", "``"}, {"rdquo", "''"},
			{"hellip", "..."}, {"bull", `\textbullet{}`},
			{"dagger", `\dag{}`}, {"Dagger", `\ddag{}`},
			{"trade", `{\texttrademark}`}, {"euro", `{\euro}`},
		},
	},
	{
		"Symbols (Latin-1 Supplement)",
		[][2]string{
			{"iexcl", "!"}, {"cent", `\textcent{}`}, {"pound", `{\pounds}`},
			{"curren", `\textcurrency{}`}, {"yen", `\textyen{}`},
			{"sect", `\S{}`}, {"copy", `{\copyright}`}, {"reg", `{\textregistered}`},
			{"laquo", `\guillemotleft{}`}, {"raquo", `\guillemotright{}`},
			{"ordf", `\textordfeminine{}`}, {"ordm", `\textordmasculine{}`},
			{"deg", `\(^\circ\)`}, {"micro", `\(\mu\)`}, {"para", `\P{}`},
			{"middot", `{\cdot}`}, {"cedil", `{\c{}}`}, {"iquest", "?"},
			{"frac14", `{\textonequarter}`}, {"frac12", `{\textonehalf}`},
			{"frac34", `{\textthreequarters}`},
		},
	},
	{
		"Mathematical and typographic",
		[][2]string{
			{"minus", "-"}, {"times", `\(\times\)`}, {"divide", `\(\div\)`},
			{"plusmn", `\(\pm\)`}, {"sup2", `\({}^{\mbox{2}}\)`}, {"sup3", `\({}^{\mbox{3}}\)`},
			{"prime", `\({}'\)`}, {"Prime", `\({}''\)`},
		},
	},
	{
		"Greek uppercase",
		[][2]string{
			{"Alpha", `\(A\)`}, {"Beta", `\(B\)`}, {"Gamma", `\(\Gamma\)`},
			{"Delta", `\(\Delta\)`}, {"Epsilon", `\(E\)`}, {"Zeta", `\(Z\)`},
			{"Eta", `\(H\)`}, {"Theta", `\(\Theta\)`}, {"Iota", `\(I\)`},
			{"Kappa", `\(K\)`}, {"Lambda", `\(\Lambda\)`}, {"Mu", `\(M\)`},
			{"Nu", `\(N\)`}, {"Xi", `\(\Xi\)`}, {"Omicron", `\(O\)`},
			{"Pi", `\(\Pi\)`}, {"Rho", `\(P\)`}, {"Sigma", `\(\Sigma\)`},
			{"Tau", `\(T\)`}, {"Upsilon", `\(\Upsilon\)`}, {"Phi", `\(\Phi\)`},
			{"Chi", `\(X\)`}, {"Psi", `\(\Psi\)`}, {"Omega", `\(\Omega\)`},
		},
	},
	{
		"Greek lowercase",
		[][2]string{
			{"alpha", `\(\alpha\)`}, {"beta", `\(\beta\)`}, {"gamma", `\(\gamma\)`},
			{"delta", `\(\delta\)`}, {"epsilon", `\(\varepsilon\)`}, {"zeta", `\(\zeta\)`},
			{"eta", `\(\eta\)`}, {"theta", `\(\theta\)`}, {"iota", `\(\iota\)`},
			{"kappa", `\(\kappa\)`}, {"lambda", `\(\lambda\)`}, {"mu", `\(\mu\)`},
			{"nu", `\(\nu\)`}, {"xi", `\(\xi\)`}, {"omicron", `\(o\)`},
			{"pi", `\(\pi\)`}, {"rho", `\(\rho\)`}, {"sigma", `\(\sigma\)`},
			{"tau", `\(\tau\)`}, {"upsilon", `\(\upsilon\)`}, {"phi", `\(\phi\)`},
			{"chi", `\(\chi\)`}, {"psi", `\(\psi\)`}, {"omega", `\(\omega\)`},
		},
	},
}

// htmlCharMap maps HTML entity names to LaTeX replacements.
// Loaded from html_character_map.csv; built from defaultHtmlCharSections when absent.
var htmlCharMap = map[string]string{}

// xmlEntityPassthrough maps HTML entity names to their "&name;" form so that the
// xml.Decoder inserts them verbatim into CharData rather than decoding to Unicode.
// Built from htmlCharMap keys during loadHtmlCharacterMap.
var xmlEntityPassthrough = map[string]string{}

func writeHtmlCharacterMap(path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write HTML character map %s: %s\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# html_character_map.csv — HTML entity names to BibTeX/LaTeX replacements")
	fmt.Fprintln(w, "# Format: entity_name,replacement   (maps &name; directly to LaTeX)")
	fmt.Fprintln(w, "# Lines starting with # are ignored on load")
	for _, sec := range defaultHtmlCharSections {
		fmt.Fprintf(w, "#\n# %s\n", sec.header)
		for _, e := range sec.entries {
			fmt.Fprintf(w, "%s,%s\n", e[0], quoteMapField(e[1]))
		}
	}
	w.Flush()
}

func loadHtmlCharacterMap(path string) {
	htmlCharMap = make(map[string]string)

	f, err := os.Open(path)
	if err != nil {
		for _, sec := range defaultHtmlCharSections {
			for _, e := range sec.entries {
				htmlCharMap[e[0]] = e[1]
			}
		}
		writeHtmlCharacterMap(path)
	} else {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			idx := strings.IndexByte(line, ',')
			if idx < 0 {
				continue
			}
			name := strings.TrimSpace(line[:idx])
			latex := unquoteMapField(line[idx+1:])
			if name != "" {
				htmlCharMap[name] = latex
			}
		}
	}

	// Build the XML decoder passthrough map: each entity name → "&name;"
	// so the decoder stores entity refs verbatim instead of decoding them.
	xmlEntityPassthrough = make(map[string]string, len(htmlCharMap))
	for name := range htmlCharMap {
		xmlEntityPassthrough[name] = "&" + name + ";"
	}
}
