package sanitize

// Port of org.springframework.web.util.HtmlUtils.htmlUnescape (spring-web 6.0.14): HtmlCharacterEntityDecoder with
// HtmlCharacterEntityReferences (the HTML 4.0 entity references of HtmlCharacterEntityReferences.properties).
//
// Numeric references are decoded with Integer.parseInt and cast to (char): &#x1F600; becomes U+F600, &#+65; is 'A'
// and &#-1; is U+FFFF.

const maxReferenceSize = 10

var springEntityMap = func() map[string]uint16 {
	m := make(map[string]uint16, len(springEntityRefs))
	for _, e := range springEntityRefs {
		m[e.name] = e.char
	}
	return m
}()

// convertToCharacter is HtmlCharacterEntityReferences.convertToCharacter.
func springConvertToCharacter(name u16) (uint16, bool) {
	s, ok := asciiString(name)
	if !ok {
		return 0, false
	}
	c, ok := springEntityMap[s]
	return c, ok
}

type entityDecoder struct {
	original u16
	decoded  u16

	currentPosition                int
	nextPotentialReferencePosition int
	nextSemicolonPosition          int
}

// htmlUnescape is HtmlUtils.htmlUnescape.
func htmlUnescape(input u16) u16 {
	d := &entityDecoder{
		original:                       input,
		decoded:                        make(u16, 0, len(input)),
		nextPotentialReferencePosition: -1,
		nextSemicolonPosition:          -2,
	}
	return d.decode()
}

func (d *entityDecoder) decode() u16 {
	for d.currentPosition < len(d.original) {
		d.findNextPotentialReference(d.currentPosition)
		d.copyCharactersTillPotentialReference()
		d.processPossibleReference()
	}
	return d.decoded
}

func (d *entityDecoder) findNextPotentialReference(startPosition int) {
	d.nextPotentialReferencePosition = startPosition
	if v := d.nextSemicolonPosition - maxReferenceSize; v > startPosition {
		d.nextPotentialReferencePosition = v
	}

	for {
		d.nextPotentialReferencePosition = indexOf(d.original, '&', d.nextPotentialReferencePosition)

		if d.nextSemicolonPosition != -1 && d.nextSemicolonPosition < d.nextPotentialReferencePosition {
			d.nextSemicolonPosition = indexOf(d.original, ';', d.nextPotentialReferencePosition+1)
		}

		if d.nextPotentialReferencePosition == -1 {
			break
		}
		if d.nextSemicolonPosition == -1 {
			d.nextPotentialReferencePosition = -1
			break
		}
		if d.nextSemicolonPosition-d.nextPotentialReferencePosition < maxReferenceSize {
			break
		}

		d.nextPotentialReferencePosition++
	}
}

func (d *entityDecoder) copyCharactersTillPotentialReference() {
	if d.nextPotentialReferencePosition != d.currentPosition {
		skipUntilIndex := len(d.original)
		if d.nextPotentialReferencePosition != -1 {
			skipUntilIndex = d.nextPotentialReferencePosition
		}
		d.decoded = append(d.decoded, d.original[d.currentPosition:skipUntilIndex]...)
		d.currentPosition = skipUntilIndex
	}
}

func (d *entityDecoder) processPossibleReference() {
	if d.nextPotentialReferencePosition != -1 {
		isNumberedReference := d.original[d.currentPosition+1] == '#'
		var wasProcessable bool
		if isNumberedReference {
			wasProcessable = d.processNumberedReference()
		} else {
			wasProcessable = d.processNamedReference()
		}
		if wasProcessable {
			d.currentPosition = d.nextSemicolonPosition + 1
		} else {
			d.decoded = append(d.decoded, d.original[d.currentPosition])
			d.currentPosition++
		}
	}
}

func (d *entityDecoder) processNumberedReference() bool {
	referenceChar := d.original[d.nextPotentialReferencePosition+2]
	isHex := referenceChar == 'x' || referenceChar == 'X'
	var value int32
	var ok bool
	if !isHex {
		value, ok = javaParseInt(d.referenceSubstring(2), 10)
	} else {
		value, ok = javaParseInt(d.referenceSubstring(3), 16)
	}
	if !ok {
		return false // NumberFormatException
	}
	d.decoded = append(d.decoded, uint16(value)) // (char) value
	return true
}

func (d *entityDecoder) processNamedReference() bool {
	c, ok := springConvertToCharacter(d.referenceSubstring(1))
	if ok {
		d.decoded = append(d.decoded, c)
		return true
	}
	return false
}

func (d *entityDecoder) referenceSubstring(offset int) u16 {
	return d.original[d.nextPotentialReferencePosition+offset : d.nextSemicolonPosition]
}

// javaParseInt is Integer.parseInt(String, radix): an optional sign, then at least one Character.digit digit (digits
// of any script are accepted); ok is false when the reference throws NumberFormatException.
func javaParseInt(s u16, radix int) (int32, bool) {
	length := len(s)
	if length == 0 {
		return 0, false
	}
	negative := false
	limit := int32(-0x7fffffff)
	i := 0
	if first := s[0]; first < '0' { // Possible leading "+" or "-"
		if first == '-' {
			negative = true
			limit = -0x80000000
		} else if first != '+' {
			return 0, false
		}
		if length == 1 { // Cannot have lone "+" or "-"
			return 0, false
		}
		i++
	}
	multmin := limit / int32(radix)
	var result int32
	for i < length {
		digit := javaDigit(s[i], radix)
		i++
		if digit < 0 || result < multmin {
			return 0, false
		}
		result *= int32(radix)
		if result < limit+int32(digit) {
			return 0, false
		}
		result -= int32(digit)
	}
	if negative {
		return result, true
	}
	return -result, true
}
