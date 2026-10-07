package sanitize

// Port of org.owasp.html.HtmlSanitizer.sanitize driving the pipeline of balancer.go, i.e. the result of
// HtmlPolicyBuilder().toFactory().sanitize(html) of OWASP java-html-sanitizer 20220608.1.
//
// The attribute names and values of the tags are not decoded or normalized: the empty policy drops every element
// together with its attributes and none of that processing can fail, so only the tokens are consumed.

// owaspSanitize returns the output of PolicyFactory.sanitize for the empty policy.
func owaspSanitize(html u16) u16 {
	p := newPipeline(len(html))
	p.openDocument()

	lx := newLexer(html)
	for {
		tok, ok := lx.next()
		if !ok {
			break
		}
		switch tok.typ {
		case ttText:
			p.text(decodeHTML(html[tok.start:tok.end]))
		case ttUnescaped:
			p.text(stripBannedCodeunits(html[tok.start:tok.end]))
		case ttTagBegin:
			if html[tok.start+1] == '/' { // A close tag.
				p.closeTag(canonicalElementName(html[tok.start+2 : tok.end]))
				for {
					t, ok := lx.next()
					if !ok || t.typ == ttTagEnd { // skip tokens until we see a ">"
						break
					}
				}
			} else {
				for {
					t, ok := lx.next()
					if !ok || t.typ == ttTagEnd {
						break
					}
				}
				p.openTag(canonicalElementName(html[tok.start+1 : tok.end]))
			}
		default:
			// Ignore comments, XML prologues, processing instructions, and other stuff that shouldn't show up in the
			// output.
		}
	}

	// closeDocument only closes elements, which the policy never writes.
	return p.out
}
