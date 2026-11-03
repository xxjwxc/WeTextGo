package fst

import (
	"strings"
)

// 移植自 wetext/token_parser.py：tagger 输出的是
// `fraction { numerator: "1" denominator: "5" }` 这种中间表示，
// 字段顺序会影响下一步 verbalizer 的读法（中文 1/5 要读成「五分之一」），
// 因此需要按语种规定的字段顺序重排后再交给 verbalizer。

type token struct {
	name    string
	keys    []string
	members map[string]string
}

func (t *token) String(orders map[string][]string) string {
	order := t.keys
	if canonical, ok := orders[t.name]; ok {
		if t.members["preserve_order"] != "true" {
			order = make([]string, 0, len(canonical)+len(t.keys))
			order = append(order, canonical...)
			for _, k := range t.keys {
				if !containsString(canonical, k) {
					order = append(order, k)
				}
			}
		}
	}

	var b strings.Builder
	b.WriteString(t.name)
	b.WriteString(" {")
	for _, k := range order {
		v, ok := t.members[k]
		if !ok {
			continue
		}
		b.WriteString(" ")
		b.WriteString(k)
		b.WriteString(`: "`)
		b.WriteString(escapeValue(v))
		b.WriteString(`"`)
	}
	b.WriteString(" }")

	return b.String()
}

func escapeValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	return strings.ReplaceAll(v, `"`, `\"`)
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

type tokenParser struct {
	text  []byte
	index int
}

// reorderTokens 按语种字段顺序重排 token 流。解析失败时返回原串，
// 由调用方决定是否降级——宁可原样输出，也不要把句子弄坏。
func reorderTokens(input string, orders map[string][]string) string {
	if input == "" {
		return input
	}

	p := &tokenParser{text: []byte(input)}
	tokens, ok := p.parse()
	if !ok {
		return input
	}

	parts := make([]string, 0, len(tokens))
	for i := range tokens {
		parts = append(parts, tokens[i].String(orders))
	}

	return strings.Join(parts, " ")
}

func (p *tokenParser) eof() bool { return p.index >= len(p.text) }

func (p *tokenParser) cur() byte {
	if p.eof() {
		return 0
	}
	return p.text[p.index]
}

func (p *tokenParser) advance() { p.index++ }

func (p *tokenParser) skipSpaces() bool {
	for !p.eof() && p.text[p.index] == ' ' {
		p.index++
	}
	return !p.eof()
}

func (p *tokenParser) expect(s string) bool {
	if p.index+len(s) > len(p.text) || string(p.text[p.index:p.index+len(s)]) != s {
		return false
	}
	p.index += len(s)
	return true
}

func (p *tokenParser) parseKey() (string, bool) {
	start := p.index
	for !p.eof() {
		c := p.text[p.index]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			p.index++
			continue
		}
		break
	}
	if p.index == start {
		return "", false
	}
	return string(p.text[start:p.index]), true
}

func (p *tokenParser) parseValue() (string, bool) {
	var b strings.Builder
	for !p.eof() && p.text[p.index] != '"' {
		c := p.text[p.index]
		if c == '\\' {
			p.advance()
			if p.eof() {
				return "", false
			}
			c = p.text[p.index]
			if c != '"' && c != '\\' {
				b.WriteByte('\\')
			}
		}
		b.WriteByte(c)
		p.advance()
	}
	return b.String(), true
}

func (p *tokenParser) parse() ([]token, bool) {
	var tokens []token

	for p.skipSpaces() {
		name, ok := p.parseKey()
		if !ok || !p.expect(" { ") {
			return nil, false
		}

		cur := token{name: name, members: map[string]string{}}
		closed := false
		for p.skipSpaces() {
			if p.cur() == '}' {
				p.advance()
				closed = true
				break
			}
			key, ok := p.parseKey()
			if !ok || !p.expect(`: "`) {
				return nil, false
			}
			value, ok := p.parseValue()
			if !ok || !p.expect(`"`) {
				return nil, false
			}
			if _, dup := cur.members[key]; dup {
				return nil, false
			}
			cur.keys = append(cur.keys, key)
			cur.members[key] = value
		}
		if !closed {
			return nil, false
		}

		tokens = append(tokens, cur)
	}

	if len(tokens) == 0 {
		return nil, false
	}

	return tokens, true
}
