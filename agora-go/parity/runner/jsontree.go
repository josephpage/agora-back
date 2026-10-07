package runner

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// node is a JSON value keeping its raw text, object key order and escaping.
type node struct {
	kind  byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' bool, 'z' null
	raw   []byte
	keys  []string
	vals  []*node
	elems []*node
	str   string
}

type jparser struct {
	b   []byte
	pos int
}

// parseJSON parses data into a node tree; trailing whitespace allowed.
func parseJSON(data []byte) (*node, error) {
	p := &jparser{b: data}
	p.ws()
	n, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.pos != len(p.b) {
		return nil, fmt.Errorf("trailing data at %d", p.pos)
	}
	return n, nil
}

func (p *jparser) ws() {
	for p.pos < len(p.b) {
		switch p.b[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jparser) value() (*node, error) {
	if p.pos >= len(p.b) {
		return nil, fmt.Errorf("unexpected end")
	}
	start := p.pos
	switch c := p.b[p.pos]; {
	case c == '{':
		p.pos++
		n := &node{kind: 'o'}
		p.ws()
		if p.pos < len(p.b) && p.b[p.pos] == '}' {
			p.pos++
			n.raw = p.b[start:p.pos]
			return n, nil
		}
		for {
			p.ws()
			k, err := p.value()
			if err != nil {
				return nil, err
			}
			if k.kind != 's' {
				return nil, fmt.Errorf("object key not a string at %d", p.pos)
			}
			p.ws()
			if p.pos >= len(p.b) || p.b[p.pos] != ':' {
				return nil, fmt.Errorf("expected ':' at %d", p.pos)
			}
			p.pos++
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, k.str)
			n.vals = append(n.vals, v)
			p.ws()
			if p.pos < len(p.b) && p.b[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.b) && p.b[p.pos] == '}' {
				p.pos++
				n.raw = p.b[start:p.pos]
				return n, nil
			}
			return nil, fmt.Errorf("expected ',' or '}' at %d", p.pos)
		}
	case c == '[':
		p.pos++
		n := &node{kind: 'a'}
		p.ws()
		if p.pos < len(p.b) && p.b[p.pos] == ']' {
			p.pos++
			n.raw = p.b[start:p.pos]
			return n, nil
		}
		for {
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			n.elems = append(n.elems, v)
			p.ws()
			if p.pos < len(p.b) && p.b[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.b) && p.b[p.pos] == ']' {
				p.pos++
				n.raw = p.b[start:p.pos]
				return n, nil
			}
			return nil, fmt.Errorf("expected ',' or ']' at %d", p.pos)
		}
	case c == '"':
		p.pos++
		for p.pos < len(p.b) {
			if p.b[p.pos] == '\\' {
				p.pos += 2
				continue
			}
			if p.b[p.pos] == '"' {
				p.pos++
				raw := p.b[start:p.pos]
				var s string
				if err := json.Unmarshal(raw, &s); err != nil {
					return nil, err
				}
				return &node{kind: 's', raw: raw, str: s}, nil
			}
			p.pos++
		}
		return nil, fmt.Errorf("unterminated string")
	case c == 't' || c == 'f' || c == 'n':
		for _, lit := range []string{"true", "false", "null"} {
			if len(p.b)-p.pos >= len(lit) && string(p.b[p.pos:p.pos+len(lit)]) == lit {
				p.pos += len(lit)
				k := byte('b')
				if lit == "null" {
					k = 'z'
				}
				return &node{kind: k, raw: p.b[start:p.pos]}, nil
			}
		}
		return nil, fmt.Errorf("bad literal at %d", p.pos)
	default:
		for p.pos < len(p.b) {
			ch := p.b[p.pos]
			if (ch >= '0' && ch <= '9') || ch == '-' || ch == '+' || ch == '.' || ch == 'e' || ch == 'E' {
				p.pos++
				continue
			}
			break
		}
		if p.pos == start {
			return nil, fmt.Errorf("unexpected char %q at %d", c, p.pos)
		}
		raw := p.b[start:p.pos]
		if _, err := strconv.ParseFloat(string(raw), 64); err != nil {
			return nil, err
		}
		return &node{kind: 'n', raw: raw}, nil
	}
}
