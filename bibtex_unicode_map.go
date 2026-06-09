/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: UnicodeMap
 *
 * Loads the per-folder unicode_map.csv file and writes a default when absent.
 * The CSV format is: decimal_code_point,LaTeX_replacement
 * An empty replacement means the character is silently removed.
 * Lines starting with # are comments and are ignored on load.
 * Space-only replacements are written quoted (" ") to survive editor whitespace trimming.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 17.05.2026
 *
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// unicodeMapSection groups related entries for a readable default CSV.
type unicodeMapSection struct {
	header  string
	entries [][2]string
}

// defaultUnicodeMapSections defines the built-in mappings written when no CSV exists.
// First column: decimal Unicode code point. Second column: BibTeX/LaTeX replacement.
var defaultUnicodeMapSections = []unicodeMapSection{
	{
		"Spaces",
		[][2]string{
			{"160", " "}, {"8198", " "}, {"8201", " "}, {"8232", " "}, {"12288", " "},
		},
	},
	{
		"Invisible / formatting — silently removed",
		[][2]string{
			{"173", ""}, {"8203", ""}, {"8204", ""}, {"8205", ""}, {"8206", ""},
			{"8288", ""}, {"65039", ""},
		},
	},
	{
		"Hyphens and dashes",
		[][2]string{
			{"8208", "-"}, {"8209", "-"}, {"8722", "-"},
			{"8210", "--"}, {"8211", "--"}, {"9472", "--"},
			{"8212", "---"},
		},
	},
	{
		"Quotation marks",
		[][2]string{
			{"8216", "'"}, {"8217", "'"},
			{"8220", "``"}, {"8221", "''"},
		},
	},
	{
		"Fullwidth punctuation",
		[][2]string{
			{"12289", ","}, {"65292", ","},
			{"65294", "."}, {"65306", ":"}, {"65307", ";"},
			{"65281", "!"}, {"65311", "? "},
			{"65288", "("}, {"65289", ")"},
		},
	},
	{
		"Fullwidth / alternate brackets",
		[][2]string{
			{"65339", "{"}, {"12309", "{"},
			{"65341", "}"}, {"12310", "}"}, {"65373", "}"}, {"65371", "{"},
		},
	},
	{
		"Other symbols",
		[][2]string{
			{"10033", "*"}, {"10065", "*"},
			{"8230", "..."}, {"65345", "a"},
		},
	},
	{
		"Symbols (Latin-1 Supplement)",
		[][2]string{
			{"163", `{\pounds}`}, {"167", `\S{}`}, {"169", `{\copyright}`},
			{"171", `{\guillemotleft}`}, {"172", `\(\neg\)`}, {"174", `{\textregistered}`},
			{"176", `\(^\circ\)`}, {"177", `\(\pm\)`},
			{"178", `\({}^{\mbox{2}}\)`}, {"179", `\({}^{\mbox{3}}\)`},
			{"180", `\'{}` }, {"181", `\(\mu\)`}, {"183", `{\cdot}`},
			{"187", `{\guillemotright}`}, {"188", `{\textonequarter}`}, {"189", `{\textonehalf}`},
			{"190", `{\textthreequarters}`}, {"191", "?"}, {"215", `\(\times\)`},
		},
	},
	{
		"Latin uppercase accented (U+00C0-U+00DE)",
		[][2]string{
			{"192", `{\` + "`A}"}, {"193", `{\'A}`}, {"194", `{\^A}`}, {"195", `{\~A}`},
			{"196", `{\"A}`}, {"197", `{\AA}`}, {"198", `{\AE}`}, {"199", `{\c{C}}`},
			{"200", `{\` + "`E}"}, {"201", `{\'E}`}, {"202", `{\^E}`}, {"203", `{\"E}`},
			{"204", `{\` + "`I}"}, {"205", `{\'I}`}, {"206", `{\^I}`}, {"207", `{\"I}`},
			{"208", `{\DH}`}, {"209", `{\~N}`},
			{"210", `{\` + "`O}"}, {"211", `{\'O}`}, {"212", `{\^O}`}, {"213", `{\~O}`},
			{"214", `{\"O}`}, {"216", `{\O}`},
			{"217", `{\` + "`U}"}, {"218", `{\'U}`}, {"219", `{\^U}`}, {"220", `{\"U}`},
			{"221", `{\'Y}`}, {"222", `{\TH}`}, {"223", `{\ss}`},
		},
	},
	{
		"Latin lowercase accented (U+00E0-U+00FF)",
		[][2]string{
			{"224", `{\` + "`a}"}, {"225", `{\'a}`}, {"226", `{\^a}`}, {"227", `{\~a}`},
			{"228", `{\"a}`}, {"229", `{\aa}`}, {"230", `{\ae}`}, {"231", `{\c{c}}`},
			{"232", `{\` + "`e}"}, {"233", `{\'e}`}, {"234", `{\^e}`}, {"235", `{\"e}`},
			{"236", `{\` + "`i}"}, {"237", `{\'i}`}, {"238", `{\^i}`}, {"239", `{\"i}`},
			{"240", `{\dh}`}, {"241", `{\~n}`},
			{"242", `{\` + "`o}"}, {"243", `{\'o}`}, {"244", `{\^o}`}, {"245", `{\~o}`},
			{"246", `{\"o}`}, {"248", `{\o}`},
			{"249", `{\` + "`u}"}, {"250", `{\'u}`}, {"251", `{\^u}`}, {"252", `{\"u}`},
			{"253", `{\'y}`}, {"254", `{\th}`}, {"255", `{\"y}`},
		},
	},
	{
		"Latin Extended-A (selected)",
		[][2]string{
			{"256", `{\={A}}`}, {"257", `{\={a}}`},
			{"260", `{\k{A}}`}, {"261", `{\k{a}}`},
			{"262", `{\'C}`}, {"263", `{\'c}`}, {"268", `{\v{C}}`}, {"269", `{\v{c}}`},
			{"272", `{\DH}`}, {"273", `{\dh}`},
			{"274", `{\={E}}`}, {"275", `{\={e}}`}, {"282", `{\v{E}}`}, {"283", `{\v{e}}`},
			{"284", `{\^G}`}, {"285", `{\^g}`}, {"286", `{\u{G}}`}, {"287", `{\u{g}}`},
			{"304", `{\.I}`}, {"305", `{\i}`},
			{"313", `{\'L}`}, {"314", `{\'l}`}, {"317", `{\v{L}}`}, {"318", `{\v{l}}`},
			{"321", `{\L}`}, {"322", `{\l}`},
			{"323", `{\'N}`}, {"324", `{\'n}`}, {"327", `{\v{N}}`}, {"328", `{\v{n}}`},
			{"332", `{\={O}}`}, {"333", `{\={o}}`}, {"336", `{\H{O}}`}, {"337", `{\H{o}}`},
			{"338", `{\OE}`}, {"339", `{\oe}`},
			{"340", `{\'R}`}, {"341", `{\'r}`}, {"344", `{\v{R}}`}, {"345", `{\v{r}}`},
			{"346", `{\'S}`}, {"347", `{\'s}`}, {"350", `{\c{S}}`}, {"351", `{\c{s}}`},
			{"352", `{\v{S}}`}, {"353", `{\v{s}}`},
			{"354", `{\c{T}}`}, {"355", `{\c{t}}`}, {"356", `{\v{T}}`}, {"357", `{\v{t}}`},
			{"362", `{\={U}}`}, {"363", `{\={u}}`},
			{"366", `{\r{U}}`}, {"367", `{\r{u}}`}, {"368", `{\H{U}}`}, {"369", `{\H{u}}`},
			{"370", `{\k{U}}`}, {"371", `{\k{u}}`},
			{"376", `{\"Y}`},
			{"377", `{\'Z}`}, {"378", `{\'z}`}, {"379", `{\.Z}`}, {"380", `{\.z}`},
			{"381", `{\v{Z}}`}, {"382", `{\v{z}}`},
		},
	},
	{
		"Greek",
		[][2]string{
			{"916", `\(\Delta\)`}, {"928", `\(\Pi\)`}, {"931", `\(\Sigma\)`},
			{"934", `\(\Phi\)`}, {"936", `\(\Psi\)`}, {"937", `\(\Omega\)`},
			{"945", `\(\alpha\)`}, {"946", `\(\beta\)`}, {"947", `\(\gamma\)`},
			{"948", `\(\delta\)`}, {"949", `\(\varepsilon\)`}, {"952", `\(\theta\)`},
			{"954", `\(\kappa\)`}, {"955", `\(\lambda\)`}, {"956", `\(\mu\)`},
			{"957", `\(\nu\)`}, {"960", `\(\pi\)`}, {"961", `\(\rho\)`},
			{"964", `\(\tau\)`}, {"969", `\(\omega\)`},
			{"1013", `\varepsilon`},
		},
	},
	{
		"Math / special symbols",
		[][2]string{
			{"732", `{\~{}}`}, {"776", ""},
			{"8249", `\guilsinglleft{}`}, {"8364", `{\euro}`},
			{"8459", `\(\mathcal{H}\)`}, {"8466", `\(\mathcal{L}\)`}, {"8467", `\(\ell\)`},
			{"8482", `{\texttrademark}`}, {"8596", `\(\leftrightarrow\)`},
			{"8707", `\(\exists\)`}, {"8727", `\(\ast\)`}, {"8734", `\(\infty\)`},
			{"8800", `\(\neq\)`}, {"8834", `\(\subset\)`}, {"8869", `\(\perp\)`},
			{"8902", `\(\star\)`}, {"9633", `\(\square\)`}, {"64257", "fi"},
		},
	},
	{
		"Cyrillic — mapped to \\ cyrchar sequences for consistent BibTeX representation",
		[][2]string{
			{"1057", `{\cyrchar\CYRS}`},
		},
	},
	{
		"Emoji / flags",
		[][2]string{
			{"120132", ""}, {"127467", `\unicode{127467}`}, {"127470", `\unicode{127470}`},
			{"127801", `\unicode{127801}`}, {"127808", `\unicode{127808}`}, {"128077", ""}, {"131072", ""},
		},
	},
}

// quoteMapField quotes a CSV replacement value when it is all-whitespace or
// contains characters that would be ambiguous in the simple comma-split format.
func quoteMapField(s string) string {
	if s == "" {
		return ""
	}
	if strings.TrimSpace(s) == "" || strings.ContainsAny(s, `",`) || strings.Contains(s, "\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// unquoteMapField undoes quoteMapField: strips outer double-quotes and unescapes "".
func unquoteMapField(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
	}
	return s
}

func writeUnicodeMap(path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write unicode map %s: %s\n", path, err)
		return
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintln(w, "# unicode_map.csv — Unicode code points (decimal) to BibTeX/LaTeX replacements")
	fmt.Fprintln(w, "# Format: decimal_codepoint,replacement   (empty replacement = silently remove)")
	fmt.Fprintln(w, "# Lines starting with # are ignored on load")
	for _, sec := range defaultUnicodeMapSections {
		fmt.Fprintf(w, "#\n# %s\n", sec.header)
		for _, e := range sec.entries {
			fmt.Fprintf(w, "%s,%s\n", e[0], quoteMapField(e[1]))
		}
	}
	w.Flush()
}

func loadUnicodeMap(path string) {
	normaliseUnicodeMap = make(map[rune]string)

	f, err := os.Open(path)
	if err != nil {
		// No file — seed from defaults then write.
		for _, sec := range defaultUnicodeMapSections {
			for _, e := range sec.entries {
				cp, parseErr := strconv.Atoi(e[0])
				if parseErr == nil {
					normaliseUnicodeMap[rune(cp)] = e[1]
				}
			}
		}
		writeUnicodeMap(path)
		return
	}
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
		cpStr := strings.TrimSpace(line[:idx])
		replacement := unquoteMapField(line[idx+1:])
		cp, parseErr := strconv.Atoi(cpStr)
		if parseErr != nil || cp < 0 {
			continue
		}
		normaliseUnicodeMap[rune(cp)] = replacement
	}
}
