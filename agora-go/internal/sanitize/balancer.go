package sanitize

// Port of org.owasp.html.TagBalancingHtmlStreamEventReceiver (20220608.1), wired to what the empty
// HtmlPolicyBuilder().toFactory() policy and the HtmlStreamRenderer reduce to:
//
//   - ElementAndAttributePolicyBasedSanitizerPolicy has no element policy at all, so openTag only defers the element:
//     the policy never writes a tag and never closes one, and the only state that matters is skipText, which openTag
//     sets to "is the element one of SKIPPABLE_ELEMENT_CONTENT" and closeTag resets to false (no open element has an
//     adjusted name, so the text-container loop of closeTag never finds one). text() is forwarded to the renderer
//     unless skipText is set.
//   - HtmlStreamRenderer therefore only ever receives text, always in PCDATA mode: Encoding.encodePcdataOnto.
//
// The balancer still matters: implied/closed/resumed elements it generates call openTag/closeTag on the policy and
// thereby change skipText, a text it considers inter-element whitespace is dropped, and above its nesting limit
// (256) events are dropped.

const nestingLimit = 256

var skippableElementContent = map[string]bool{
	"script": true, "style": true, "noscript": true, "nostyle": true, "noembed": true, "noframes": true,
	"iframe": true, "object": true, "frame": true, "frameset": true, "title": true,
}

var (
	transparent  [nElementTypes]bool
	scopeForEnd  [nElementTypes]uint8
	scopesByElem [nElementTypes]uint8
	resumableEl  [nElementTypes]bool
	specialText  [nElementTypes]bool
	headerEl     [nElementTypes]bool
	skippableEl  [nElementTypes]bool
)

const (
	scopeIn       uint8 = 1
	scopeButton   uint8 = 2
	scopeListItem uint8 = 4
	scopeTable    uint8 = 8
	scopeSelect   uint8 = 16
	allScopes           = scopeIn | scopeButton | scopeListItem | scopeTable | scopeSelect
)

func init() {
	for _, n := range []string{"a", "audio", "canvas", "del", "ins", "map", "object", "video"} {
		transparent[indexForString(n)] = true
	}
	for i := range resumableEl {
		resumableEl[i] = bit(resumableBits[:], i)
		switch modeForTag(str16(elementNames[i])) {
		case modeCDATA, modeCDATASometimes, modeRCDATA, modePlainText:
			specialText[i] = true
		}
		headerEl[i] = isHeaderElementName(elementNames[i])
		skippableEl[i] = skippableElementContent[elementNames[i]]
	}

	inScopeElements := []string{"applet", "caption", "html", "table", "td", "th", "marquee", "object", "template"}
	for _, tn := range inScopeElements {
		scopesByElem[indexForString(tn)] |= scopeIn
	}
	for _, tns := range [][]string{{"dir", "ol", "ul"}, inScopeElements} {
		for _, tn := range tns {
			scopesByElem[indexForString(tn)] |= scopeListItem
		}
	}
	for _, tns := range [][]string{{"button"}, inScopeElements} {
		for _, tn := range tns {
			scopesByElem[indexForString(tn)] |= scopeButton
		}
	}
	for _, tn := range []string{"html", "table", "template"} {
		scopesByElem[indexForString(tn)] |= scopeTable
	}
	for i := range scopesByElem {
		scopesByElem[i] |= scopeSelect
	}
	for _, tn := range []string{"optgroup", "option"} {
		scopesByElem[indexForString(tn)] &^= scopeSelect
	}
	// The <nofeature> elements are scoped by everything.
	scopesByElem[indexForString("noembed")] = allScopes
	scopesByElem[indexForString("noframes")] = allScopes
	scopesByElem[indexForString("noscript")] = allScopes

	for i := range scopeForEnd {
		scopeForEnd[i] = scopeIn
	}
	for _, tn := range []string{"caption", "col", "colgroup", "table", "tbody", "tfoot", "thead", "tr", "td", "th"} {
		scopeForEnd[indexForString(tn)] = scopeTable
	}
	scopeForEnd[indexForString("select")] = scopeSelect
	scopeForEnd[indexForString("p")] = scopeButton // really.
	scopeForEnd[indexForString("li")] = scopeListItem
}

func isHeaderElementName(canon string) bool {
	return len(canon) == 2 && (canon[0]|32) == 'h' && canon[1] <= '9'
}

// pipeline is the balancer, the (reduced) policy and the renderer of one sanitize call.
type pipeline struct {
	// balancer
	openElements      []int
	openAs            int
	toResumeInReverse []int
	// policy
	skipText bool
	// renderer output
	out u16
}

func newPipeline(capacity int) *pipeline {
	return &pipeline{out: make(u16, 0, capacity)}
}

// --- policy -----------------------------------------------------------------------------------------------------

// policyOpenTag is the policy's openTag for an element name that is not one of the known element names.
func (p *pipeline) policyOpenTag(name u16) {
	s, ok := asciiString(name)
	p.skipText = ok && skippableElementContent[s]
}

// policyOpenTagIdx is the policy's openTag for a known element.
func (p *pipeline) policyOpenTagIdx(elIndex int) { p.skipText = skippableEl[elIndex] }

func (p *pipeline) policyCloseTag() { p.skipText = false }

func (p *pipeline) policyText(chunk u16) {
	if !p.skipText {
		p.render(chunk)
	}
}

// --- balancer ---------------------------------------------------------------------------------------------------

func (p *pipeline) openDocument() {
	p.skipText = false
}

func (p *pipeline) openTag(elementName u16) {
	canon := canonicalElementName(elementName)
	elIndex := indexForName(canon)
	// Treat unrecognized tags as void, but emit closing tags in closeTag().
	if elIndex == customElement {
		if len(p.openElements) < nestingLimit {
			p.policyOpenTag(elementName)
		}
		return
	}

	p.prepareForContent(elIndex)

	if len(p.openElements) < nestingLimit {
		p.policyOpenTagIdx(elIndex)
	}
	if !isVoidElement(canon) {
		p.push(elIndex)
	}
}

// push and truncate maintain openAs, the number of <a> on the stack (the reference scans the stack with
// IntVector.lastIndexOf each time, which is quadratic on deeply nested input).
func (p *pipeline) push(el int) {
	p.openElements = append(p.openElements, el)
	if el == tagA {
		p.openAs++
	}
}

func (p *pipeline) truncate(n int) {
	for i := len(p.openElements) - 1; i >= n; i-- {
		if p.openElements[i] == tagA {
			p.openAs--
		}
	}
	p.openElements = p.openElements[:n]
}

func (p *pipeline) prepareForContent(elIndex int) {
	nOpen := len(p.openElements)
	{
		top := tagBody
		if nOpen != 0 {
			top = p.openElements[nOpen-1]
		}
		// Open implied elements, such as list-items and table cells & rows.
		implied := impliedElements(top, elIndex)
		if len(implied) != 0 {
			startPos := 0
			for i, ie := range implied {
				if ie == top {
					startPos = i + 1
					break
				}
			}
			for i := startPos; i < len(implied); i++ {
				ie := implied[i]
				p.policyOpenTagIdx(ie) // not guarded by the nesting limit in the reference
				p.push(ie)
				nOpen++
			}
		}
	}

	if nOpen != 0 {
		top := p.openElements[nOpen-1]
		// Close all the elements that cannot contain the content to open.
		for {
			canContain := p.canContain(elIndex, top, nOpen-1) && !(elIndex == tagA && p.openAs > 0)
			if canContain {
				break
			}
			if len(p.openElements) < nestingLimit {
				p.policyCloseTag()
			}
			nOpen--
			p.truncate(nOpen)
			if resumableEl[top] && top != elIndex {
				p.toResumeInReverse = append(p.toResumeInReverse, top)
			}
			if nOpen == 0 {
				break
			}
			top = p.openElements[nOpen-1]
		}
	}

	for len(p.toResumeInReverse) != 0 {
		toResume := p.toResumeInReverse[len(p.toResumeInReverse)-1]
		// If toResume can contain elInfo AND the top of the stack can contain toResume, then we push toResume.
		nOpen = len(p.openElements)
		if (nOpen == 0 || p.canContain(toResume, p.openElements[nOpen-1], nOpen)) &&
			p.canContain(elIndex, toResume, nOpen) {
			p.toResumeInReverse = p.toResumeInReverse[:len(p.toResumeInReverse)-1]
			if len(p.openElements) < nestingLimit {
				p.policyOpenTagIdx(toResume)
			}
			p.push(toResume)
		} else {
			break
		}
	}
}

// canContain takes transparency into account when figuring out what can be contained.
func (p *pipeline) canContain(child, container, containerIndexOnStack int) bool {
	if containerIndexOnStack < 0 {
		panic("sanitize: containerIndexOnStack < 0") // Preconditions.checkArgument
	}
	if child == textNode && specialText[container] {
		// If there's a select element on the stack, then we need to be extra careful.
		for i := containerIndexOnStack - 1; i >= 0; i-- {
			if tagSelect == p.openElements[i] {
				return false
			}
		}
	}
	anc := container
	ancIndexOnStack := containerIndexOnStack
	for {
		if elementsCanContain(anc, child) {
			return true
		}
		if !transparent[anc] {
			return false
		}
		if ancIndexOnStack == 0 {
			return elementsCanContain(tagBody, child)
		}
		ancIndexOnStack--
		anc = p.openElements[ancIndexOnStack]
	}
}

func (p *pipeline) closeTag(elementName u16) {
	canon := canonicalElementName(elementName)
	elIndex := indexForName(canon)
	if elIndex == customElement { // Allow unrecognized end tags through.
		if len(p.openElements) < nestingLimit {
			p.policyCloseTag()
		}
		return
	}

	// Ensure that index is in the scope of closeable elements.
	blockingScopes := scopeForEnd[elIndex]

	index := -1
	if headerEl[elIndex] {
		// Let any of </h1>, </h2>, ... close other header tags.
		for i := len(p.openElements) - 1; i >= 0; i-- {
			openElementIndex := p.openElements[i]
			if headerEl[openElementIndex] {
				elIndex = openElementIndex
				index = i
				break
			}
			if scopesByElem[openElementIndex]&blockingScopes != 0 {
				break
			}
		}
	} else {
		for i := len(p.openElements) - 1; i >= 0; i-- {
			openElementIndex := p.openElements[i]
			if openElementIndex == elIndex {
				index = i
				break
			}
			if scopesByElem[openElementIndex]&blockingScopes != 0 {
				break
			}
		}
	}
	if index < 0 {
		return // Don't close unopened tags.
	}

	last := len(p.openElements)
	// Close all the elements that cannot contain the element to open.
	for {
		last--
		if last <= index {
			break
		}
		unclosed := p.openElements[last]
		p.truncate(last)
		if last+1 < nestingLimit {
			p.policyCloseTag()
		}
		if resumableEl[unclosed] {
			p.toResumeInReverse = append(p.toResumeInReverse, unclosed)
		}
	}
	if len(p.openElements) < nestingLimit {
		p.policyCloseTag()
	}
	p.truncate(index)
}

// isInterElementWhitespace is TagBalancingHtmlStreamEventReceiver.isInterElementWhitespace (true for "").
func isInterElementWhitespace(text u16) bool {
	for _, c := range text {
		if !(c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r') {
			return false
		}
	}
	return true
}

func (p *pipeline) text(text u16) {
	if isInterElementWhitespace(text) {
		nOpenElements := len(p.openElements)
		if nOpenElements != 0 {
			top := p.openElements[nOpenElements-1]
			// Use impliedElements as a proxy for whether or not a manufactured node is needed. If it is, then skip
			// the inter-element space and don't manufacture a node.
			if !canContainText(top) || len(impliedElements(top, tagA)) != 0 {
				return
			}
		}
	} else {
		p.prepareForContent(textNode)
	}

	if len(p.openElements) < nestingLimit {
		p.policyText(text)
	}
}
