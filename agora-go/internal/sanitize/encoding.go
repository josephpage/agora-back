package sanitize

import "sync"

// Port of org.owasp.html.Encoding and of org.owasp.html.HtmlEntities (20220608.1).

// ---------------------------------------------------------------------------
// Banned code units

func isBannedASCII(ch uint16) bool { return !(ch == '\t' || ch == '\n' || ch == '\r') }

// longestPrefixOfGoodCodeunits is Encoding.longestPrefixOfGoodCodeunits: the number of code units at the front of s
// that form XML Characters, -1 if all of s does.
func longestPrefixOfGoodCodeunits(s u16) int {
	n := len(s)
	for i := 0; i < n; i++ {
		ch := s[i]
		if ch < 0x20 {
			if isBannedASCII(ch) {
				return i
			}
		} else if ch >= 0xd800 {
			if ch <= 0xdfff {
				if i+1 < n && isSurrogatePair(ch, s[i+1]) {
					i++ // Skip over low surrogate since we know it's ok.
				} else {
					return i
				}
			} else if ch&0xfffe == 0xfffe {
				return i
			}
		}
	}
	return -1
}

// stripBannedCodeunitsFrom is Encoding.stripBannedCodeunits(StringBuilder, start): it compacts sb in place.
func stripBannedCodeunitsFrom(sb u16, start int) u16 {
	k := start
	n := len(sb)
	for i := start; i < n; i++ {
		ch := sb[i]
		if ch < 0x20 {
			if isBannedASCII(ch) {
				continue
			}
		} else if ch >= 0xd800 {
			if ch <= 0xdfff {
				if i+1 < n {
					next := sb[i+1]
					if isSurrogatePair(ch, next) {
						sb[k] = ch
						k++
						sb[k] = next
						k++
						i++
					}
				}
				continue
			} else if ch&0xfffe == 0xfffe {
				continue
			}
		}
		sb[k] = ch
		k++
	}
	return sb[:k]
}

// stripBannedCodeunits is Encoding.stripBannedCodeunits(String): it never modifies s.
func stripBannedCodeunits(s u16) u16 {
	safeLimit := longestPrefixOfGoodCodeunits(s)
	if safeLimit < 0 {
		return s
	}
	sb := make(u16, len(s))
	copy(sb, s)
	return stripBannedCodeunitsFrom(sb, safeLimit)
}

func indexOf(s u16, ch uint16, from int) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(s); i++ {
		if s[i] == ch {
			return i
		}
	}
	return -1
}

// decodeHTML is Encoding.decodeHtml(s, false) (the text, not an attribute value: attribute values never reach the
// output of the empty policy): it decodes the HTML entities and drops the code units that are not XML Characters.
func decodeHTML(s u16) u16 {
	firstAmp := indexOf(s, '&', 0)
	safeLimit := longestPrefixOfGoodCodeunits(s)
	if firstAmp&safeLimit < 0 { // both are -1
		return s
	}

	n := len(s)
	sb := make(u16, 0, n)
	pos := 0
	amp := firstAmp
	for amp >= 0 {
		sb = append(sb, s[pos:amp]...)
		end := appendDecodedEntity(s, amp, n, &sb)
		pos = end
		amp = indexOf(s, '&', end)
	}
	sb = append(sb, s[pos:n]...)

	start := firstAmp
	if firstAmp < 0 {
		start = safeLimit
	} else if safeLimit >= 0 && safeLimit < firstAmp {
		start = safeLimit
	}
	return stripBannedCodeunitsFrom(sb, start)
}

// ---------------------------------------------------------------------------
// HtmlEntities

type trieEdge struct {
	ch   byte
	node int32
}

type trieNode struct {
	edges    []trieEdge // sorted by ch
	terminal bool
	value    u16
}

var (
	entityOnce        sync.Once
	entityNodes       []trieNode
	longestEntityName int
)

func buildEntityTrie() {
	entityNodes = make([]trieNode, 1, 12000)
	for _, kv := range entityPairs {
		k := kv[0]
		if len(k) > longestEntityName {
			longestEntityName = len(k)
		}
		cur := int32(0)
		for i := 0; i < len(k); i++ {
			c := k[i]
			next := int32(-1)
			edges := entityNodes[cur].edges
			pos := 0
			for pos < len(edges) && edges[pos].ch < c {
				pos++
			}
			if pos < len(edges) && edges[pos].ch == c {
				next = edges[pos].node
			} else {
				entityNodes = append(entityNodes, trieNode{})
				next = int32(len(entityNodes) - 1)
				edges = append(edges, trieEdge{})
				copy(edges[pos+1:], edges[pos:])
				edges[pos] = trieEdge{c, next}
				entityNodes[cur].edges = edges
			}
			cur = next
		}
		entityNodes[cur].terminal = true
		entityNodes[cur].value = utf16Encode(kv[1])
	}
}

func utf16Encode(s string) u16 {
	out := make(u16, 0, len(s))
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// lookup is Trie.lookup(char): the child node for ch, or -1.
func trieLookup(node int32, ch uint16) int32 {
	if ch >= 0x80 {
		return -1
	}
	for _, e := range entityNodes[node].edges {
		if uint16(e.ch) == ch {
			return e.node
		}
		if uint16(e.ch) > ch {
			break
		}
	}
	return -1
}

func isHTMLIDContinueChar(ch uint16) bool {
	chLower := ch | 32
	return (ch >= '0' && ch <= '9') || (chLower >= 'a' && chLower <= 'z') || ch == '-'
}

// appendDecodedEntity is HtmlEntities.appendDecodedEntity(html, offset, limit, false, sb) (mayComplete is always
// true outside attribute values): it decodes any HTML entity at offset (named or numeric),
// appends it to sb and returns the offset after the end of the decoded sequence.
func appendDecodedEntity(html u16, offset, limit int, sb *u16) int {
	ch := html[offset]
	if ch != '&' {
		*sb = append(*sb, ch)
		return offset + 1
	}

	if offset+2 >= limit {
		*sb = append(*sb, '&')
		return offset + 1
	}
	entityOnce.Do(buildEntityTrie)
	// Cap limit to limit the amount of time spent processing inputs like &a&a&a&a...
	if l := offset + (1 + longestEntityName); l < limit {
		limit = l
	}

	// Now we know where the entity ends, and that there is at least one character in the entity name
	ch1 := html[offset+1]
	ch2 := html[offset+2]
	var codepoint int32 = -1
	tail := limit
	if ch1 == '#' {
		// numeric entity
		if ch2 == 'x' || ch2 == 'X' {
			if limit == offset+3 { // No digits
				*sb = append(*sb, '&')
				return offset + 1
			}
			codepoint = 0
			// hex literal
		hexloop:
			for i := offset + 3; i < limit; i++ {
				digit := html[i]
				if !isHTMLIDContinueChar(digit) {
					if i == offset+3 {
						codepoint = -1
					}
					if digit == ';' {
						i++
					}
					tail = i
					break
				}
				switch digit & 0xfff8 {
				case 0x30, 0x38: // ASCII 48-57 are '0'-'9'
					decDig := int32(digit & 0xf)
					if decDig < 10 {
						codepoint = (codepoint << 4) | decDig
					} else {
						codepoint = -1
						break hexloop
					}
				case 0x40, 0x60: // ASCII 65-70 and 97-102 are 'A'-'Z' && 'a'-'z'
					hexDig := int32(digit & 0x7)
					if hexDig != 0 && hexDig < 7 {
						codepoint = (codepoint << 4) | (hexDig + 9)
					} else {
						codepoint = -1
						break hexloop
					}
				default:
					codepoint = -1
					break hexloop
				}
			}
			if codepoint > 0x10FFFF {
				codepoint = 0xfffd // Unknown.
			}
		} else {
			codepoint = 0
			// decimal literal
		decloop:
			for i := offset + 2; i < limit; i++ {
				digit := html[i]
				if !isHTMLIDContinueChar(digit) {
					if i == offset+2 {
						codepoint = -1
					}
					if digit == ';' {
						i++
					}
					tail = i
					break
				}
				switch digit & 0xfff8 {
				case 0x30, 0x38:
					decDig := int32(digit) - '0'
					if decDig < 10 {
						codepoint = (codepoint * 10) + decDig
					} else {
						codepoint = -1
						break decloop
					}
				default:
					codepoint = -1
					break decloop
				}
			}
			if codepoint > 0x10FFFF {
				codepoint = 0xfffd // Unknown.
			}
		}
	} else {
		longest := int32(-1)
		t := int32(0)
		for i := offset + 1; i < limit; i++ {
			t = trieLookup(t, html[i])
			if t < 0 {
				break
			}
			if entityNodes[t].terminal {
				longest = t
				tail = i + 1
			}
		}
		// Try again, case insensitively.
		if longest < 0 {
			t = 0
			for i := offset + 1; i < limit; i++ {
				nameChar := html[i]
				if 'Z' >= nameChar && nameChar >= 'A' {
					nameChar |= 32
				}
				t = trieLookup(t, nameChar)
				if t < 0 {
					break
				}
				if entityNodes[t].terminal {
					longest = t
					tail = i + 1
				}
			}
		}
		if longest >= 0 {
			*sb = append(*sb, entityNodes[longest].value...)
			return tail
		}
	}
	if codepoint < 0 {
		*sb = append(*sb, '&')
		return offset + 1
	}
	// StringBuilder.appendCodePoint: a surrogate code point is appended as the lone char.
	if codepoint >= 0x10000 {
		c := codepoint - 0x10000
		*sb = append(*sb, uint16(0xD800+(c>>10)), uint16(0xDC00+(c&0x3FF)))
	} else {
		*sb = append(*sb, uint16(codepoint))
	}
	return tail
}

// ---------------------------------------------------------------------------
// Encoding.encodePcdataOnto (the only encoder the renderer ever uses with the empty policy)

// replacements maps ASCII chars that need to be encoded to an equivalent HTML entity; ok is false for the chars
// that are written as they are.
var replacements = func() (r [0x80]struct {
	s  string
	ok bool
}) {
	for i := 0; i < ' '; i++ {
		// We elide control characters so that the output is in the intersection of valid HTML5 and XML.
		if i != '\t' && i != '\n' && i != '\r' {
			r[i].ok = true // "" (elide)
		}
	}
	set := func(c byte, s string) { r[c].s, r[c].ok = s, true }
	set('"', "&#34;")
	set('&', "&amp;")
	set('\'', "&#39;")
	set('+', "&#43;") // UTF-7 special.
	set('<', "&lt;")
	set('=', "&#61;")
	set('>', "&gt;")
	set('@', "&#64;")
	set('`', "&#96;")
	return
}()

const braceReplacementPCDATA = "{<!-- -->"

var hexNumeral = [16]byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f'}

func appendASCII(dst u16, s string) u16 {
	for i := 0; i < len(s); i++ {
		dst = append(dst, uint16(s[i]))
	}
	return dst
}

// appendNumericEntity is Encoding.appendNumericEntity.
func appendNumericEntity(codepoint int, out u16) u16 {
	out = append(out, '&', '#')
	if codepoint < 100 {
		if codepoint < 10 {
			out = append(out, uint16('0'+codepoint))
		} else {
			out = append(out, uint16('0'+codepoint/10), uint16('0'+codepoint%10))
		}
	} else {
		var nDigits int
		switch {
		case codepoint < 0x100:
			nDigits = 2
		case codepoint < 0x1000:
			nDigits = 3
		case codepoint < 0x10000:
			nDigits = 4
		case codepoint < 0x100000:
			nDigits = 5
		default:
			nDigits = 6
		}
		out = append(out, 'x')
		for digit := nDigits - 1; digit >= 0; digit-- {
			out = append(out, uint16(hexNumeral[(codepoint>>(uint(digit)<<2))&0xf]))
		}
	}
	return append(out, ';')
}

// render is HtmlStreamRenderer.text in PCDATA mode: Encoding.encodePcdataOnto(text, out). The output buffer is a
// StringBuilder in the reference, which matters for the Indic ZWNJ elision at its end.
func (p *pipeline) render(text u16) {
	out := p.out
	n := len(text)
	pos := 0
	for i := 0; i < n; i++ {
		ch := text[i]
		if ch < 0x80 { // Handles all ASCII.
			r := replacements[ch]
			repl, has := r.s, r.ok
			if ch == '{' && !has {
				if i+1 == n || text[i+1] == '{' {
					repl, has = braceReplacementPCDATA, true
				}
			}
			if has {
				out = append(out, text[pos:i]...)
				out = appendASCII(out, repl)
				pos = i + 1
			}
		} else if (0x93A <= ch && ch <= 0xC4C) &&
			(
			// Devanagari vowel
			ch <= 0x94F ||
				// Benagli vowels
				0x985 <= ch && ch <= 0x994 ||
				0x9BE <= ch && ch < 0x9CC || // 0x9CC (Bengali AU) is ok
				0x9E0 <= ch && ch <= 0x9E3 ||
				// Telugu vowels
				0xC05 <= ch && ch <= 0xC14 ||
				0xC3E <= ch && ch != 0xC48 /* 0xC48 (Telugu AI) is ok */) {
			// https://manishearth.github.io/blog/2018/02/15/picking-apart-the-crashing-ios-string/
			// The ZWNJ before such a vowel is eliminated.
			if pos < i {
				if text[i-1] == 0x200C { // ZWNJ
					out = append(out, text[pos:i-1]...)
					// Drop the ZWNJ on the floor.
					pos = i
				}
			} else if l := len(out); l != 0 {
				if out[l-1] == 0x200C { // ZWNJ
					out = out[:l-1]
				}
			}
		} else if ch >= 0xd800 {
			if ch <= 0xdfff {
				if i+1 < n && isSurrogatePair(ch, text[i+1]) {
					// Emit supplemental codepoints as entity so that they cannot be mis-encoded as UTF-8 of
					// surrogates instead of UTF-8 proper and get involved in UTF-16/UCS-2 confusion.
					codepoint := (int(ch)-0xD800)<<10 + (int(text[i+1]) - 0xDC00) + 0x10000
					out = append(out, text[pos:i]...)
					out = appendNumericEntity(codepoint, out)
					i++
					pos = i + 1
				} else {
					out = append(out, text[pos:i]...)
					// Elide the orphaned surrogate.
					pos = i + 1
				}
			} else if ch >= 0xfe60 {
				// Is a control character or possible full-width version of a special character, a BOM, or one of
				// the FE60 block that might be elided or normalized to an HTML special character.
				out = append(out, text[pos:i]...)
				pos = i + 1
				if ch&0xfffe == 0xfffe {
					// Elide since not an the XML Character.
				} else {
					out = appendNumericEntity(int(ch), out)
				}
			}
		} else if ch == 0x1FEF { // Normalizes to backtick.
			out = append(out, text[pos:i]...)
			out = appendASCII(out, "&#8175;")
			pos = i + 1
		}
	}
	out = append(out, text[pos:n]...)
	p.out = out
}
