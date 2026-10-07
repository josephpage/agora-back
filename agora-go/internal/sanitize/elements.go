package sanitize

// Port of org.owasp.html.HtmlElementTables (the part used by TagBalancingHtmlStreamEventReceiver) and of
// org.owasp.html.HtmlTextEscapingMode. The data comes from HtmlElementTablesCanned (elements_gen.go).

// textNode is HtmlElementTables.TEXT_NODE, the pseudo element index of text nodes.
const textNode = -1

// customElement is the index of "xcustom", the placeholder for any unknown element name.
const customElement = nElementTypes - 1

var elementIndex = func() map[string]int {
	m := make(map[string]int, nElementTypes)
	for i, n := range elementNames {
		m[n] = i
	}
	return m
}()

// asciiString converts name to a Go string; ok is false when it holds a non-ASCII code unit (such a name can never
// equal one of the ASCII names the tables know).
func asciiString(name u16) (string, bool) {
	for _, c := range name {
		if c >= 0x80 {
			return "", false
		}
	}
	b := make([]byte, len(name))
	for i, c := range name {
		b[i] = byte(c)
	}
	return string(b), true
}

// indexForName is HtmlElementNames.getElementNameIndex: unknown names map to the custom element.
func indexForName(canonName u16) int {
	s, ok := asciiString(canonName)
	if !ok {
		return customElement
	}
	return indexForString(s)
}

func indexForString(s string) int {
	if i, ok := elementIndex[s]; ok {
		return i
	}
	return customElement
}

func bit(words []uint32, i int) bool { return words[i>>5]&(1<<(uint(i)&31)) != 0 }

func canContainText(i int) bool { return textContentModel[i]&8 != 0 }

var (
	tagA      = indexForString("a")
	tagBody   = indexForString("body")
	tagSelect = indexForString("select")
	tagOption = indexForString("option")
	tagOL     = indexForString("ol")
	tagUL     = indexForString("ul")
	tagLI     = indexForString("li")
	tagScript = indexForString("script")
	tagStyle  = indexForString("style")

	noFeature = func() (s [nElementTypes]bool) {
		s[indexForString("noscript")] = true
		s[indexForString("noframes")] = true
		s[indexForString("noembed")] = true
		return
	}()
)

// elementsCanContain is HtmlElementTables.canContain(parent, child).
func elementsCanContain(parent, child int) bool {
	if noFeature[parent] {
		return true
	}
	if child == textNode {
		return canContainText(parent)
	}
	if parent < 0 || parent >= nElementTypes || child >= nElementTypes {
		panic("sanitize: element index out of range") // Preconditions.checkElementIndex
	}
	return bit(canContainBits[:], parent*nElementTypes+child)
}

type freeWrapper struct {
	desc              int
	allowedContainers []bool
	implied           []int
}

func newFreeWrapper(desc int, allowed []int, implied []int) *freeWrapper {
	max := -1
	for _, a := range allowed {
		if a > max {
			max = a
		}
	}
	w := &freeWrapper{desc: desc, allowedContainers: make([]bool, max+1), implied: implied}
	for _, a := range allowed {
		w.allowedContainers[a] = true
	}
	return w
}

var freeWrappers = func() []*freeWrapper {
	idx := indexForString
	dir, ol, ul, li := idx("dir"), idx("ol"), idx("ul"), idx("li")
	sel, option := idx("select"), idx("option")
	// The reference spells "opgroup" (sic): that is not an element name, so it resolves to the custom element and
	// the real optgroup gets no free wrapper.
	optgroup := idx("opgroup")
	table, tbody, tfoot, thead := idx("table"), idx("tbody"), idx("tfoot"), idx("thead")
	tr, td, th := idx("tr"), idx("td"), idx("th")
	caption, col, colgroup := idx("caption"), idx("col"), idx("colgroup")
	ws := []*freeWrapper{
		newFreeWrapper(li, []int{dir, ol, ul, li}, []int{ul}),
		newFreeWrapper(option, []int{sel, optgroup, option}, []int{sel}),
		newFreeWrapper(optgroup, []int{sel, optgroup}, []int{sel}),
		newFreeWrapper(td, []int{tr, td, th}, []int{table, tbody, tr}),
		newFreeWrapper(th, []int{tr, td, th}, []int{table, tbody, tr}),
		newFreeWrapper(tr, []int{tbody, thead, tfoot, tr, td, th}, []int{table, tbody}),
		newFreeWrapper(tbody, []int{table, thead, tbody, tfoot}, []int{table}),
		newFreeWrapper(thead, []int{table, thead, tbody, tfoot}, []int{table}),
		newFreeWrapper(tfoot, []int{table, thead, tbody, tfoot}, []int{table}),
		newFreeWrapper(caption, []int{table}, []int{table}),
		newFreeWrapper(col, []int{colgroup}, []int{table, colgroup}),
		newFreeWrapper(colgroup, []int{table}, []int{table}),
	}
	maxDesc := -1
	for _, w := range ws {
		if w.desc > maxDesc {
			maxDesc = w.desc
		}
	}
	arr := make([]*freeWrapper, maxDesc+1)
	for _, w := range ws {
		arr[w.desc] = w
	}
	return arr
}()

var (
	liTagArr     = []int{tagLI}
	optionTagArr = []int{tagOption}
)

// impliedElements is HtmlElementTables.impliedElements(anc, desc).
func impliedElements(anc, desc int) []int {
	// <style> and <script> are allowed anywhere.
	if desc == tagScript || desc == tagStyle {
		return nil
	}
	if desc != textNode && desc < len(freeWrappers) {
		if w := freeWrappers[desc]; w != nil {
			if anc < len(w.allowedContainers) && !w.allowedContainers[anc] {
				return w.implied
			}
		}
	}
	if desc != textNode {
		if implied := impliedElementTable[[2]int{anc, desc}]; len(implied) != 0 {
			return implied
		}
	}
	var oneImplied []int
	if anc == tagOL || anc == tagUL {
		oneImplied = liTagArr
	} else if anc == tagSelect {
		oneImplied = optionTagArr
	}
	if oneImplied != nil {
		if desc != oneImplied[0] {
			// The reference returns LI_TAG_ARR here even for <select> ("why are we dropping OPTION_AG_ARR?").
			return liTagArr
		}
	}
	return nil
}

// HtmlTextEscapingMode.
type escapingMode uint8

const (
	modePCDATA escapingMode = iota
	modeCDATA
	modeCDATASometimes
	modeRCDATA
	modePlainText
	modeVoid
)

var escapingModes = map[string]escapingMode{
	"iframe":    modeCDATA,
	"listing":   modeCDATASometimes,
	"xmp":       modeCDATA,
	"comment":   modeCDATASometimes,
	"plaintext": modePlainText,
	"script":    modeCDATA,
	"style":     modeCDATA,
	"textarea":  modeRCDATA,
	"title":     modeRCDATA,
	"area":      modeVoid, "base": modeVoid, "br": modeVoid, "col": modeVoid, "command": modeVoid,
	"embed": modeVoid, "hr": modeVoid, "img": modeVoid, "input": modeVoid, "keygen": modeVoid,
	"link": modeVoid, "meta": modeVoid, "param": modeVoid, "source": modeVoid, "track": modeVoid,
	"wbr": modeVoid, "basefont": modeVoid, "isindex": modeVoid,
}

// modeForTag is HtmlTextEscapingMode.getModeForTag.
func modeForTag(canonTagName u16) escapingMode {
	s, ok := asciiString(canonTagName)
	if !ok {
		return modePCDATA
	}
	if m, ok := escapingModes[s]; ok {
		return m
	}
	return modePCDATA
}

func isTagFollowedByLiteralContent(canonTagName u16) bool {
	m := modeForTag(canonTagName)
	return m != modePCDATA && m != modeVoid
}

func isVoidElement(canonTagName u16) bool { return modeForTag(canonTagName) == modeVoid }

// Canonical element names (HtmlLexer.canonicalElementName).
var mixedCaseForeignElementNames = map[string]bool{
	"animateColor": true, "animateMotion": true, "animateTransform": true, "clipPath": true, "feBlend": true,
	"feColorMatrix": true, "feComponentTransfer": true, "feComposite": true, "feConvolveMatrix": true,
	"feDiffuseLighting": true, "feDisplacementMap": true, "feDistantLight": true, "feDropShadow": true,
	"feFlood": true, "feFuncA": true, "feFuncB": true, "feFuncG": true, "feFuncR": true,
	"feGaussianBlur": true, "feImage": true, "feMerge": true, "feMergeNode": true, "feMorphology": true,
	"feOffset": true, "fePointLight": true, "feSpecularLighting": true, "feSpotLight": true, "feTile": true,
	"feTurbulence": true, "foreignObject": true, "linearGradient": true, "radialGradient": true,
	"solidColor": true, "textArea": true, "textPath": true,
}

// canonicalElementName is HtmlLexer.canonicalElementName: ASCII-lower-cases names that are neither namespaced nor
// one of the mixed-case SVG/MathML names.
func canonicalElementName(name u16) u16 {
	for _, c := range name {
		if c == ':' {
			return name
		}
	}
	if s, ok := asciiString(name); ok && mixedCaseForeignElementNames[s] {
		return name
	}
	return asciiLower(name)
}
