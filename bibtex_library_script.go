/*
 *
 * Module: bibtex_library_script
 *
 * This module implements the parser and evaluator for .script files, which
 * specify group assignment rules for BibTeX library entries.
 *
 * Creator: Henderik A. Proper (erikproper@gmail.com)
 *
 * Version of: 29.05.2026
 *
 */

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// ─── Token ─────────────────────────────────────────────────────────────────────

type stok int

const (
	stokEOF    stok = iota
	stokIdent       // bare identifier: keyword or entry type
	stokString      // "quoted string"
	stokNumber      // integer literal
	stokSemi        // ;
	stokColon       // :
	stokGT          // >
	stokGTE         // >=
	stokLT          // <
	stokLTE         // <=
	stokEQ          // = (numeric field comparison)
)

type scriptToken struct {
	kind stok
	val  string
	line int
}

type scriptLexer struct {
	src    []rune
	pos    int
	line   int
	peeked *scriptToken
}

func newScriptLexer(src string) *scriptLexer {
	return &scriptLexer{src: []rune(src), line: 1}
}

func (lx *scriptLexer) skipSpaceAndComments() {
	for lx.pos < len(lx.src) {
		ch := lx.src[lx.pos]
		if ch == '\n' {
			lx.line++
			lx.pos++
		} else if unicode.IsSpace(ch) {
			lx.pos++
		} else if ch == '#' {
			for lx.pos < len(lx.src) && lx.src[lx.pos] != '\n' {
				lx.pos++
			}
		} else {
			break
		}
	}
}

func (lx *scriptLexer) next() scriptToken {
	if lx.peeked != nil {
		t := *lx.peeked
		lx.peeked = nil
		return t
	}
	return lx.scan()
}

func (lx *scriptLexer) peek() scriptToken {
	if lx.peeked == nil {
		t := lx.scan()
		lx.peeked = &t
	}
	return *lx.peeked
}

func (lx *scriptLexer) scan() scriptToken {
	lx.skipSpaceAndComments()
	if lx.pos >= len(lx.src) {
		return scriptToken{kind: stokEOF, line: lx.line}
	}
	line := lx.line
	ch := lx.src[lx.pos]

	switch ch {
	case ';':
		lx.pos++
		return scriptToken{kind: stokSemi, line: line}
	case ':':
		lx.pos++
		return scriptToken{kind: stokColon, line: line}
	case '>':
		lx.pos++
		if lx.pos < len(lx.src) && lx.src[lx.pos] == '=' {
			lx.pos++
			return scriptToken{kind: stokGTE, val: ">=", line: line}
		}
		return scriptToken{kind: stokGT, val: ">", line: line}
	case '<':
		lx.pos++
		if lx.pos < len(lx.src) && lx.src[lx.pos] == '=' {
			lx.pos++
			return scriptToken{kind: stokLTE, val: "<=", line: line}
		}
		return scriptToken{kind: stokLT, val: "<", line: line}
	case '=':
		lx.pos++
		return scriptToken{kind: stokEQ, val: "=", line: line}
	case '"':
		lx.pos++
		var buf strings.Builder
		for lx.pos < len(lx.src) {
			c := lx.src[lx.pos]
			if c == '"' {
				lx.pos++
				break
			}
			if c == '\\' && lx.pos+1 < len(lx.src) && lx.src[lx.pos+1] == '"' {
				buf.WriteRune('"')
				lx.pos += 2
				continue
			}
			if c == '\n' {
				lx.line++
			}
			buf.WriteRune(c)
			lx.pos++
		}
		return scriptToken{kind: stokString, val: buf.String(), line: line}
	default:
		if unicode.IsDigit(ch) {
			start := lx.pos
			for lx.pos < len(lx.src) && unicode.IsDigit(lx.src[lx.pos]) {
				lx.pos++
			}
			return scriptToken{kind: stokNumber, val: string(lx.src[start:lx.pos]), line: line}
		}
		if unicode.IsLetter(ch) || ch == '_' {
			start := lx.pos
			for lx.pos < len(lx.src) {
				c := lx.src[lx.pos]
				if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '-' || c == '_' {
					lx.pos++
				} else {
					break
				}
			}
			return scriptToken{kind: stokIdent, val: string(lx.src[start:lx.pos]), line: line}
		}
		lx.pos++ // skip unknown character
		return lx.scan()
	}
}

// ─── AST ───────────────────────────────────────────────────────────────────────

type scriptStmt interface{ isStmt() }

type sBlock           struct{ stmts []scriptStmt }
type sAddToGroup      struct{ group string }
type sRemoveFromGroup struct{ group string }
type sIssueWarning    struct{ msg string }
type sIf              struct {
	cond  scriptCond
	then  scriptStmt
	elifs []sElif
	else_ scriptStmt
}
type sElif struct {
	cond scriptCond
	body scriptStmt
}

func (*sBlock) isStmt()           {}
func (*sAddToGroup) isStmt()      {}
func (*sRemoveFromGroup) isStmt() {}
func (*sIssueWarning) isStmt()    {}
func (*sIf) isStmt()              {}

type scriptCond interface{ isCond() }

type sCondAnd              struct{ left, right scriptCond }
type sCondEntryType        struct{ entryType string }
type sCondGroupSetAny      struct{ set string; negated bool }
type sCondGroupSetMultiple struct{ set string }
type sCondInGroup          struct{ group string; negated bool }
type sCondFieldIncludes    struct{ field, value string }
type sCondFieldEquals      struct{ field, value string }
type sCondFieldEmpty       struct{ field string; negated bool }
type sCondFieldNumCmp      struct{ field, op string; value int }

func (*sCondAnd) isCond()              {}
func (*sCondEntryType) isCond()        {}
func (*sCondGroupSetAny) isCond()      {}
func (*sCondGroupSetMultiple) isCond() {}
func (*sCondInGroup) isCond()          {}
func (*sCondFieldIncludes) isCond()    {}
func (*sCondFieldEquals) isCond()      {}
func (*sCondFieldEmpty) isCond()       {}
func (*sCondFieldNumCmp) isCond()      {}

type scriptProgram struct {
	groupSets map[string][]string
	rules     []*sIf
}

// ─── Parser ─────────────────────────────────────────────────────────────────────

type scriptParser struct {
	lx     *scriptLexer
	errors []string
}

func (p *scriptParser) errorf(line int, format string, args ...any) {
	p.errors = append(p.errors, fmt.Sprintf("line %d: "+format, append([]any{line}, args...)...))
}

func (p *scriptParser) is(word string) bool {
	t := p.lx.peek()
	return t.kind == stokIdent && strings.EqualFold(t.val, word)
}

// eat consumes the next token if it matches word; reports an error and leaves
// the token unconsumed otherwise (so the caller can attempt recovery).
func (p *scriptParser) eat(word string) bool {
	if p.is(word) {
		p.lx.next()
		return true
	}
	t := p.lx.peek()
	p.errorf(t.line, "expected %q, got %q", word, t.val)
	return false
}

func (p *scriptParser) eatString() (string, bool) {
	t := p.lx.peek()
	if t.kind != stokString {
		p.errorf(t.line, "expected quoted string, got %q", t.val)
		return "", false
	}
	p.lx.next()
	return t.val, true
}

func (p *scriptParser) optSemi() {
	if p.lx.peek().kind == stokSemi {
		p.lx.next()
	}
}

func (p *scriptParser) parseProgram() *scriptProgram {
	prog := &scriptProgram{groupSets: map[string][]string{}}
	for p.lx.peek().kind != stokEOF {
		if p.is("define") {
			p.parseDefine(prog)
		} else if p.is("if") {
			if rule := p.parseIf(); rule != nil {
				prog.rules = append(prog.rules, rule)
			}
		} else {
			p.lx.next()
		}
		p.optSemi()
	}
	return prog
}

func (p *scriptParser) parseDefine(prog *scriptProgram) {
	p.eat("define")
	p.eat("the")
	p.eat("group")
	p.eat("set")
	name, ok := p.eatString()
	if !ok {
		return
	}
	p.eat("as")
	if p.lx.peek().kind == stokColon {
		p.lx.next()
	}
	var groups []string
	for p.lx.peek().kind == stokString {
		groups = append(groups, p.lx.next().val)
	}
	prog.groupSets[name] = groups
}

func (p *scriptParser) parseIf() *sIf {
	p.eat("if")
	cond := p.parseCond()
	p.eat("then")
	then := p.parseStmt()
	if then == nil {
		return nil
	}
	node := &sIf{cond: cond, then: then}
	for p.is("elif") {
		p.lx.next()
		ec := p.parseCond()
		p.eat("then")
		eb := p.parseStmt()
		node.elifs = append(node.elifs, sElif{ec, eb})
	}
	if p.is("else") {
		p.lx.next()
		node.else_ = p.parseStmt()
	}
	return node
}

func (p *scriptParser) parseStmt() scriptStmt {
	t := p.lx.peek()
	switch {
	case p.is("begin"):
		return p.parseBlock()
	case p.is("if"):
		return p.parseIf()
	case p.is("add"):
		return p.parseAdd()
	case p.is("remove"):
		return p.parseRemove()
	case p.is("issue"):
		return p.parseIssue()
	default:
		p.errorf(t.line, "expected statement, got %q", t.val)
		p.lx.next() // consume to prevent infinite loop in block parsing
		return nil
	}
}

func (p *scriptParser) parseBlock() scriptStmt {
	p.eat("begin")
	var stmts []scriptStmt
	for !p.is("end") && p.lx.peek().kind != stokEOF {
		s := p.parseStmt()
		if s != nil {
			stmts = append(stmts, s)
		}
		p.optSemi()
	}
	p.eat("end")
	return &sBlock{stmts}
}

func (p *scriptParser) parseAdd() scriptStmt {
	p.eat("add")
	p.eat("it")
	p.eat("to")
	p.eat("the")
	p.eat("group")
	group, _ := p.eatString()
	return &sAddToGroup{group}
}

func (p *scriptParser) parseRemove() scriptStmt {
	p.eat("remove")
	p.eat("it")
	p.eat("from")
	p.eat("the")
	p.eat("group")
	group, _ := p.eatString()
	return &sRemoveFromGroup{group}
}

func (p *scriptParser) parseIssue() scriptStmt {
	p.eat("issue")
	p.eat("warning")
	msg, _ := p.eatString()
	return &sIssueWarning{msg}
}

func (p *scriptParser) parseCond() scriptCond {
	left := p.parseSimpleCond()
	for p.is("and") {
		p.lx.next()
		right := p.parseSimpleCond()
		left = &sCondAnd{left, right}
	}
	return left
}

func (p *scriptParser) parseSimpleCond() scriptCond {
	if !p.eat("the") {
		return nil
	}
	t := p.lx.peek()
	if t.kind != stokIdent {
		p.errorf(t.line, "expected identifier after \"the\", got %q", t.val)
		return nil
	}
	if strings.EqualFold(t.val, "entry") {
		p.lx.next()
		return p.parseEntryCond()
	}
	field := strings.ToLower(p.lx.next().val)
	p.eat("field")
	return p.parseFieldCond(field)
}

func (p *scriptParser) parseEntryCond() scriptCond {
	if !p.eat("is") {
		return nil
	}
	if p.is("not") {
		p.lx.next()
		if p.is("a") {
			p.lx.next()
			p.eat("member")
			p.eat("of")
			p.eat("a")
			p.eat("group")
			p.eat("in")
			p.eat("the")
			p.eat("group")
			p.eat("set")
			name, _ := p.eatString()
			return &sCondGroupSetAny{set: name, negated: true}
		}
		if p.is("in") {
			p.lx.next()
			p.eat("the")
			p.eat("group")
			name, _ := p.eatString()
			return &sCondInGroup{group: name, negated: true}
		}
		t := p.lx.peek()
		p.errorf(t.line, "unexpected token after \"not\" in entry condition: %q", t.val)
		return nil
	}
	if p.is("a") {
		p.lx.next()
		p.eat("member")
		p.eat("of")
		p.eat("a")
		p.eat("group")
		p.eat("in")
		p.eat("the")
		p.eat("group")
		p.eat("set")
		name, _ := p.eatString()
		return &sCondGroupSetAny{set: name, negated: false}
	}
	if p.is("member") {
		p.lx.next()
		p.eat("of")
		p.eat("more")
		p.eat("than")
		p.eat("one")
		p.eat("group")
		p.eat("in")
		p.eat("the")
		p.eat("group")
		p.eat("set")
		name, _ := p.eatString()
		return &sCondGroupSetMultiple{set: name}
	}
	if p.is("in") {
		p.lx.next()
		p.eat("the")
		p.eat("group")
		name, _ := p.eatString()
		return &sCondInGroup{group: name, negated: false}
	}
	if p.is("of") {
		p.lx.next()
		p.eat("type")
		t := p.lx.next()
		return &sCondEntryType{entryType: strings.ToLower(t.val)}
	}
	t := p.lx.peek()
	p.errorf(t.line, "unexpected token in entry condition: %q", t.val)
	return nil
}

func (p *scriptParser) parseFieldCond(field string) scriptCond {
	if p.is("includes") {
		p.lx.next()
		val, _ := p.eatString()
		return &sCondFieldIncludes{field: field, value: val}
	}
	if p.is("equals") {
		p.lx.next()
		val, _ := p.eatString()
		return &sCondFieldEquals{field: field, value: val}
	}
	if p.is("is") {
		p.lx.next()
		negated := false
		if p.is("not") {
			p.lx.next()
			negated = true
		}
		p.eat("empty")
		return &sCondFieldEmpty{field: field, negated: negated}
	}
	t := p.lx.peek()
	switch t.kind {
	case stokGT, stokGTE, stokLT, stokLTE, stokEQ:
		op := p.lx.next().val
		numTok := p.lx.peek()
		if numTok.kind != stokNumber {
			p.errorf(numTok.line, "expected number after %q, got %q", op, numTok.val)
			return nil
		}
		p.lx.next()
		n, _ := strconv.Atoi(numTok.val)
		return &sCondFieldNumCmp{field: field, op: op, value: n}
	default:
		p.errorf(t.line, "expected field condition operator, got %q", t.val)
		return nil
	}
}

// ─── Evaluator ─────────────────────────────────────────────────────────────────

// scriptNameInField returns true when searchName (or any of its aliases) is
// present as one of the " and "-separated names in fieldVal.
func scriptNameInField(l *TBibTeXLibrary, fieldVal, searchName string) bool {
	canonical := searchName
	if mapped, ok := l.NameAliasToName[searchName]; ok {
		canonical = mapped
	}
	for _, name := range strings.Split(fieldVal, " and ") {
		name = strings.TrimSpace(name)
		if name == canonical {
			return true
		}
		if mapped, ok := l.NameAliasToName[name]; ok && mapped == canonical {
			return true
		}
	}
	return false
}

func scriptEvalCond(l *TBibTeXLibrary, key string, prog *scriptProgram, cond scriptCond) bool {
	if cond == nil {
		return false
	}
	switch c := cond.(type) {
	case *sCondAnd:
		return scriptEvalCond(l, key, prog, c.left) && scriptEvalCond(l, key, prog, c.right)
	case *sCondEntryType:
		return strings.ToLower(l.EntryFieldValueity(key, EntryTypeField)) == c.entryType
	case *sCondGroupSetAny:
		for _, g := range prog.groupSets[c.set] {
			if l.GroupEntries[g].Set().Contains(key) {
				return !c.negated
			}
		}
		return c.negated
	case *sCondGroupSetMultiple:
		count := 0
		for _, g := range prog.groupSets[c.set] {
			if l.GroupEntries[g].Set().Contains(key) {
				count++
				if count > 1 {
					return true
				}
			}
		}
		return false
	case *sCondInGroup:
		return l.GroupEntries[c.group].Set().Contains(key) != c.negated
	case *sCondFieldIncludes:
		val := l.EntryFieldValueity(key, c.field)
		if c.field == "author" || c.field == "editor" {
			return scriptNameInField(l, val, c.value)
		}
		return strings.Contains(val, c.value)
	case *sCondFieldEquals:
		return l.EntryFieldValueity(key, c.field) == c.value
	case *sCondFieldEmpty:
		return (l.EntryFieldValueity(key, c.field) == "") != c.negated
	case *sCondFieldNumCmp:
		n, err := strconv.Atoi(l.EntryFieldValueity(key, c.field))
		if err != nil {
			return false
		}
		switch c.op {
		case ">":
			return n > c.value
		case ">=":
			return n >= c.value
		case "<":
			return n < c.value
		case "<=":
			return n <= c.value
		case "=":
			return n == c.value
		}
	}
	return false
}

func scriptEvalStmt(l *TBibTeXLibrary, key string, prog *scriptProgram, stmt scriptStmt) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *sBlock:
		for _, sub := range s.stmts {
			scriptEvalStmt(l, key, prog, sub)
		}
	case *sAddToGroup:
		if !l.GroupEntries[s.group].Set().Contains(key) {
			if err := addBibGroupEntry(s.group, key); err == nil {
				l.GroupEntries.AddValueToStringSetMap(s.group, key)
				bibEntriesModified = true
				SpinnerInterrupt()
				l.Progress("Added %s to group %q", key, s.group)
			}
		}
	case *sRemoveFromGroup:
		if l.GroupEntries[s.group].Set().Contains(key) {
			if err := removeBibGroupEntry(s.group, key); err == nil {
				l.GroupEntries.DeleteValueFromStringSetMap(s.group, key)
				bibEntriesModified = true
				SpinnerInterrupt()
				l.Progress("Removed %s from group %q", key, s.group)
			}
		}
	case *sIssueWarning:
		l.Warning("%s: %s", key, s.msg)
	case *sIf:
		if scriptEvalCond(l, key, prog, s.cond) {
			scriptEvalStmt(l, key, prog, s.then)
			return
		}
		for _, elif := range s.elifs {
			if scriptEvalCond(l, key, prog, elif.cond) {
				scriptEvalStmt(l, key, prog, elif.body)
				return
			}
		}
		if s.else_ != nil {
			scriptEvalStmt(l, key, prog, s.else_)
		}
	}
}

// ApplyScript parses the script at path and evaluates all rules against every
// entry in l.
func (l *TBibTeXLibrary) ApplyScript(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		l.Warning("Cannot read script file %s: %s", path, err)
		return
	}
	parser := &scriptParser{lx: newScriptLexer(string(src))}
	prog := parser.parseProgram()
	if len(parser.errors) > 0 {
		for _, e := range parser.errors {
			l.Warning("Script error in %s: %s", path, e)
		}
		return
	}
	total := countBibEntries()
	count := 0
	spinner := l.NewSpinner(fmt.Sprintf("Applying script %s", path))
	forEachBibEntryKey(func(key string) bool {
		count++
		spinner.Update(count, total)
		for _, rule := range prog.rules {
			scriptEvalStmt(l, key, prog, rule)
		}
		return true
	})
	spinner.Stop()
}
