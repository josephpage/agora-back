package sanitize

// Port of org.owasp.html.HtmlLexer and org.owasp.html.HtmlInputSplitter (HtmlLexer.java, 20220608.1).
//
// The input is a Java String, i.e. a slice of UTF-16 code units, and every class test is the one of the
// reference JDK (Character.isWhitespace / isLetter on a char).

type tokenType uint8

const (
	ttAttrName tokenType = iota
	ttAttrValue
	ttQMarkMeta
	ttComment
	ttDirective
	ttUnescaped
	ttQString
	ttTagBegin
	ttTagEnd
	ttText
	ttIgnorable
	ttServerCode
)

type token struct {
	start, end int
	typ        tokenType
}

func (t token) matchesEq(input u16) bool {
	return t.end-t.start == 1 && input[t.start] == '='
}

// ---------------------------------------------------------------------------
// HtmlInputSplitter

type splitState uint8

const (
	ssNone splitState = iota
	ssTagName
	ssSlash
	ssBang
	ssBangDash
	ssComment
	ssCommentDash
	ssCommentDashDash
	ssDirective
	ssDone
	ssBogusComment
	ssServerCode
	ssServerCodePct
)

type splitter struct {
	input  u16
	offset int
	// inTag is true iff the current character is inside a tag.
	inTag bool
	// inEscapeExemptBlock is true inside a script, xmp, listing, ... element, whose content does not follow the
	// normal escaping rules.
	inEscapeExemptBlock bool
	// escapeExemptTagName is the name of the close tag required to end the current escape exempt block
	// (hasEscapeExemptTag is false for the reference's null).
	escapeExemptTagName u16
	hasEscapeExemptTag  bool
	textEscapingMode    escapingMode

	lastNonIgnorable    token
	hasLastNonIgnorable bool
}

func isIdentStart(ch uint16) bool {
	return ch >= 'A' && ch <= 'z' && (ch <= 'Z' || ch >= 'a')
}

func equalU16(a, b u16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// next is HtmlInputSplitter.produce: the next token or false at the end of the input.
func (s *splitter) next() (token, bool) {
	tok, ok := s.parseToken()
	if !ok {
		return token{}, false
	}
	// Handle escape-exempt blocks.
	if s.inEscapeExemptBlock {
		if tok.typ != ttServerCode {
			// RCDATA is classified as text since it can contain entities.
			if s.textEscapingMode == modeRCDATA {
				tok.typ = ttText
			} else {
				tok.typ = ttUnescaped
			}
		}
	} else {
		switch tok.typ {
		case ttTagBegin:
			canon := canonicalElementName(s.input[tok.start+1 : tok.end])
			if isTagFollowedByLiteralContent(canon) {
				s.escapeExemptTagName = canon
				s.hasEscapeExemptTag = true
				s.textEscapingMode = modeForTag(canon)
			}
		case ttTagEnd:
			s.inEscapeExemptBlock = s.hasEscapeExemptTag
		}
	}
	return tok, true
}

func (s *splitter) parseToken() (token, bool) {
	input := s.input
	start := s.offset
	limit := len(input)
	if start == limit {
		return token{}, false
	}

	end := start + 1
	var typ tokenType
	hasType := true

	ch := input[start]
	if s.inTag {
		switch {
		case ch == '>':
			typ = ttTagEnd
			s.inTag = false
		case ch == '/':
			if end != limit && input[end] == '>' {
				typ = ttTagEnd
				s.inTag = false
				end++
			} else {
				typ = ttText
			}
		case ch == '=':
			typ = ttText
		case ch == '"' || ch == '\'':
			typ = ttQString
			delim := ch
			for ; end < limit; end++ {
				if input[end] == delim {
					end++
					break
				}
			}
		case !javaIsWhitespace(ch):
			typ = ttText
			for ; end < limit; end++ {
				ch = input[end]
				// End a text chunk before />
				if (!s.hasLastNonIgnorable || !s.lastNonIgnorable.matchesEq(input)) &&
					ch == '/' && end+1 < limit && input[end+1] == '>' {
					break
				} else if ch == '>' || ch == '=' || javaIsWhitespace(ch) {
					break
				} else if ch == '"' || ch == '\'' {
					if end+1 < limit {
						ch2 := input[end+1]
						if javaIsWhitespace(ch2) || ch2 == '>' || ch2 == '/' {
							end++
							break
						}
					}
				}
			}
		default:
			// We skip whitespace tokens inside tag bodies.
			typ = ttIgnorable
			for end < limit && javaIsWhitespace(input[end]) {
				end++
			}
		}
	} else if ch == '<' {
		if end == limit {
			typ = ttText
		} else {
			ch = input[end]
			hasType = false
			state := ssNone
			switch ch {
			case '/': // close tag?
				state = ssSlash
				end++
			case '!': // Comment or declaration
				if !s.inEscapeExemptBlock {
					state = ssBang
				}
				end++
			case '?':
				if !s.inEscapeExemptBlock {
					state = ssBogusComment
				}
				end++
			case '%':
				state = ssServerCode
				end++
			default:
				if isIdentStart(ch) && !s.inEscapeExemptBlock {
					state = ssTagName
					end++
				} else if ch == '<' {
					typ = ttText
					hasType = true
				} else {
					end++
				}
			}
			if state != ssNone {
			charloop:
				for end < limit {
					ch = input[end]
					switch state {
					case ssTagName:
						if javaIsWhitespace(ch) || ch == '>' || ch == '/' || ch == '<' {
							// End processing of an escape exempt block when we see a corresponding end tag.
							if s.inEscapeExemptBlock && input[start+1] == '/' &&
								s.textEscapingMode != modePlainText &&
								equalU16(canonicalElementName(input[start+2:end]), s.escapeExemptTagName) {
								s.inEscapeExemptBlock = false
								s.escapeExemptTagName = nil
								s.hasEscapeExemptTag = false
								s.textEscapingMode = modePCDATA
							}
							typ = ttTagBegin
							hasType = true
							// Don't process content as attributes if we're inside an escape exempt block.
							s.inTag = !s.inEscapeExemptBlock
							state = ssDone
							break charloop
						}
					case ssSlash:
						if javaIsLetter(ch) {
							state = ssTagName
						} else {
							if ch == '<' {
								typ = ttText
								hasType = true
							} else {
								end++
							}
							break charloop
						}
					case ssBang:
						if ch == '-' {
							state = ssBangDash
						} else {
							state = ssDirective
						}
					case ssBangDash:
						if ch == '-' {
							state = ssComment
						} else {
							state = ssDirective
						}
					case ssComment:
						if ch == '-' {
							state = ssCommentDash
						}
					case ssCommentDash:
						if ch == '-' {
							state = ssCommentDashDash
						} else {
							state = ssCommentDash
						}
					case ssCommentDashDash:
						if ch == '>' {
							state = ssDone
							typ = ttComment
							hasType = true
						} else if ch == '-' {
							state = ssCommentDashDash
						} else {
							state = ssCommentDash
						}
					case ssDirective:
						if ch == '>' {
							typ = ttDirective
							hasType = true
							state = ssDone
						}
					case ssBogusComment:
						if ch == '>' {
							typ = ttQMarkMeta
							hasType = true
							state = ssDone
						}
					case ssServerCode:
						if ch == '%' {
							state = ssServerCodePct
						}
					case ssServerCodePct:
						if ch == '>' {
							typ = ttServerCode
							hasType = true
							state = ssDone
						} else if ch != '%' {
							state = ssServerCode
						}
					case ssDone:
						panic("sanitize: unexpectedly DONE while lexing HTML token stream")
					}
					end++
					if state == ssDone {
						break
					}
				}
				if end == limit {
					switch state {
					case ssDone:
					case ssBogusComment:
						typ = ttQMarkMeta
						hasType = true
					case ssComment, ssCommentDash, ssCommentDashDash:
						typ = ttComment
						hasType = true
					case ssDirective, ssServerCode, ssServerCodePct:
						typ = ttServerCode
						hasType = true
					case ssTagName:
						typ = ttTagBegin
						hasType = true
					default:
						typ = ttText
						hasType = true
					}
				}
			}
		}
	} else {
		hasType = false
	}
	if !hasType {
		for end < limit && input[end] != '<' {
			end++
		}
		typ = ttText
	}

	s.offset = end
	result := token{start, end, typ}
	if typ != ttIgnorable {
		s.lastNonIgnorable = result
		s.hasLastNonIgnorable = true
	}
	return result, true
}

// ---------------------------------------------------------------------------
// HtmlLexer

type lexState uint8

const (
	lsOutsideTag lexState = iota
	lsInTag
	lsSawName
	lsSawEq
)

type lexer struct {
	input     u16
	splitter  splitter
	state     lexState
	lookahead []token
}

func newLexer(input u16) *lexer {
	return &lexer{input: input, splitter: splitter{input: input}}
}

func (l *lexer) readToken() (token, bool) {
	if len(l.lookahead) != 0 {
		t := l.lookahead[0]
		l.lookahead = l.lookahead[1:]
		return t, true
	}
	return l.splitter.next()
}

func (l *lexer) peekToken(i int) (token, bool) {
	for len(l.lookahead) <= i {
		t, ok := l.splitter.next()
		if !ok {
			break
		}
		l.lookahead = append(l.lookahead, t)
	}
	if len(l.lookahead) > i {
		return l.lookahead[i], true
	}
	return token{}, false
}

func (l *lexer) pushbackToken(t token) {
	l.lookahead = append([]token{t}, l.lookahead...)
}

// next is HtmlLexer.produce (the reference's hasNext()/next() pair): the next token or false at the end of the input.
func (l *lexer) next() (token, bool) {
	for {
		tok, ok := l.readToken()
		if !ok {
			return token{}, false
		}
		switch tok.typ {
		// Keep track of whether we're inside a tag or not.
		case ttTagBegin:
			l.state = lsInTag
		case ttTagEnd:
			if l.state == lsSawEq {
				// Distinguish <input type=checkbox checked=> from <input type=checkbox checked>
				l.pushbackToken(tok)
				l.state = lsInTag
				return token{tok.start, tok.start, ttAttrValue}, true
			}
			l.state = lsOutsideTag
		// Drop ignorable tokens.
		case ttIgnorable:
			continue
		// Collapse adjacent text nodes if we're outside a tag, or otherwise, recognize attribute names and values.
		default:
			switch l.state {
			case lsOutsideTag:
				if tok.typ == ttText || tok.typ == ttUnescaped {
					tok = l.collapseSubsequent(tok)
				}
			case lsInTag:
				if tok.typ == ttText && !tok.matchesEq(l.input) {
					tok.typ = ttAttrName // reclassify
					l.state = lsSawName
				}
			case lsSawName:
				if tok.typ == ttText {
					if tok.matchesEq(l.input) {
						l.state = lsSawEq
						continue // skip the '=' token
					}
					tok.typ = ttAttrName
				} else {
					l.state = lsInTag
				}
			case lsSawEq:
				if tok.typ == ttText || tok.typ == ttQString {
					if tok.typ == ttText {
						// Collapse adjacent text nodes to properly handle <a onclick=this.clicked=true> and
						// <a title=foo bar>
						tok = l.collapseAttributeName(tok)
					}
					tok.typ = ttAttrValue
					l.state = lsInTag
				}
			}
		}
		return tok, true
	}
}

// collapseSubsequent collapses all the following tokens of the same type into tok.
func (l *lexer) collapseSubsequent(tok token) token {
	collapsed := tok
	for {
		next, ok := l.peekToken(0)
		if !ok || next.typ != tok.typ {
			break
		}
		collapsed = token{collapsed.start, next.end, collapsed.typ}
		l.readToken()
	}
	return collapsed
}

func (l *lexer) collapseAttributeName(tok token) token {
	// We want to collapse tokens into the value that are not parts of an attribute value. We should include any space
	// or text adjacent to the value, but should stop at any of the following constructions:
	//   space end-of-file              e.g. name=foo_
	//   space valueless-attrib-name    e.g. name=foo checked
	//   space tag-end                  e.g. name=foo />
	//   space text space? '='          e.g. name=foo bar=
	nToMerge := 0
	for {
		t, ok := l.peekToken(nToMerge)
		if !ok {
			break
		}
		if t.typ == ttIgnorable {
			tk, ok := l.peekToken(nToMerge + 1)
			if !ok {
				break
			}
			if tk.typ != ttText {
				break
			}
			if isValuelessAttribute(l.input[tk.start:tk.end]) {
				break
			}
			eq, ok := l.peekToken(nToMerge + 2)
			if ok && eq.typ == ttIgnorable {
				eq, ok = l.peekToken(nToMerge + 3)
			}
			if !ok || eq.matchesEq(l.input) {
				break
			}
		} else if t.typ != ttText {
			break
		}
		nToMerge++
	}
	if nToMerge == 0 {
		return tok
	}
	end := tok.end
	for {
		t, _ := l.readToken()
		end = t.end
		nToMerge--
		if nToMerge <= 0 {
			break
		}
	}
	return token{tok.start, end, ttText}
}

var valuelessAttribNames = map[string]bool{
	"checked": true, "compact": true, "declare": true, "defer": true, "disabled": true,
	"ismap": true, "multiple": true, "nohref": true, "noresize": true, "noshade": true,
	"nowrap": true, "readonly": true, "selected": true,
}

// isValuelessAttribute is HtmlLexer.isValuelessAttribute. The reference first applies canonicalAttributeName, which
// keeps names that contain ':' or that are one of the mixed-case SVG attribute names as they are and ASCII-lower-cases
// the others; none of the former can be one of the (all lower-case, colon-free) valueless names even once
// lower-cased, so membership of the ASCII-lower-cased name is equivalent.
func isValuelessAttribute(name u16) bool {
	s, ok := asciiString(name)
	if !ok {
		return false
	}
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c | 0x20
		}
	}
	return valuelessAttribNames[string(b)]
}
