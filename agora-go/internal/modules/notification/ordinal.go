package notification

import (
	"math/big"

	"agora/internal/javacompat"
)

// parseStoredType reads the TEXT column `type` the way Hibernate reads an ORDINAL enum: the column
// is read with the pgjdbc ResultSet.getByte(), whose lenient text parsing is observable (checked on
// the reference with 35 spellings), then used as an index in the constants.
//
//   - the text is trimmed (String.trim: every character up to U+0020), an empty text is an error;
//   - Byte.parseByte: an optional sign and Unicode decimal digits, in [-128, 127];
//   - otherwise new BigDecimal(text).toBigInteger() (truncated toward zero) in [-128, 127]:
//     "1.0", "5.99", "-0.5", "1e0", "100e-2", ".5", "1." are all accepted;
//   - the byte must be an ordinal of the enum (0..5), else ArrayIndexOutOfBounds.
//
// ok is false when Kotlin fails with an exception (HTTP 500).
func parseStoredType(text string) (TypeNotification, bool) {
	t := javacompat.JavaStringTrim(text)
	if t == "" {
		return 0, false
	}
	v, ok := parseByteJava(t)
	if !ok {
		v, ok = parseBigDecimalTruncated(t)
		if !ok || v < -128 || v > 127 {
			return 0, false
		}
	}
	return TypeByOrdinal(v)
}

// parseByteJava is Byte.parseByte(s, 10).
func parseByteJava(s string) (int, bool) {
	rs := []rune(s)
	i := 0
	neg := false
	if len(rs) > 0 && (rs[0] == '-' || rs[0] == '+') {
		neg = rs[0] == '-'
		i = 1
	}
	if i == len(rs) {
		return 0, false
	}
	v := 0
	for ; i < len(rs); i++ {
		d := javacompat.JavaCharacterDigit(rs[i], 10)
		if d < 0 {
			return 0, false
		}
		v = v*10 + d
		if v > 128 {
			return 0, false
		}
	}
	if neg {
		v = -v
	}
	if v < -128 || v > 127 {
		return 0, false
	}
	return v, true
}

// parseBigDecimalTruncated is new BigDecimal(s).toBigInteger() for the values that fit an int; ok is
// false for a malformed text and for a value too large to matter (the caller range checks it).
func parseBigDecimalTruncated(s string) (int, bool) {
	rs := []rune(s)
	i := 0
	neg := false
	if len(rs) > 0 && (rs[0] == '-' || rs[0] == '+') {
		neg = rs[0] == '-'
		i = 1
	}
	var digits []byte
	scale := 0
	sawDigit, sawDot := false, false
	for ; i < len(rs); i++ {
		r := rs[i]
		if r == '.' {
			if sawDot {
				return 0, false
			}
			sawDot = true
			continue
		}
		d := javacompat.JavaCharacterDigit(r, 10)
		if d < 0 {
			break
		}
		sawDigit = true
		digits = append(digits, byte('0'+d))
		if sawDot {
			scale++
		}
	}
	if !sawDigit {
		return 0, false
	}
	exp := 0
	if i < len(rs) {
		if rs[i] != 'e' && rs[i] != 'E' {
			return 0, false
		}
		i++
		eneg := false
		if i < len(rs) && (rs[i] == '-' || rs[i] == '+') {
			eneg = rs[i] == '-'
			i++
		}
		if i == len(rs) {
			return 0, false
		}
		for ; i < len(rs); i++ {
			d := javacompat.JavaCharacterDigit(rs[i], 10)
			if d < 0 {
				return 0, false
			}
			exp = exp*10 + d
			if exp > 1<<20 {
				exp = 1 << 20 // far beyond any byte; keeps the arithmetic bounded
			}
		}
		if eneg {
			exp = -exp
		}
	}
	unscaled, _ := new(big.Int).SetString(string(digits), 10)
	if unscaled.Sign() == 0 {
		return 0, true
	}
	// value = unscaled * 10^(exp - scale), truncated toward zero
	shift := exp - scale
	var out *big.Int
	switch {
	case shift >= 0:
		if shift > 40 {
			return 0, false // at least 10^40: not a byte
		}
		out = new(big.Int).Mul(unscaled, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(shift)), nil))
	default:
		if -shift > len(digits) {
			return 0, true
		}
		out = new(big.Int).Quo(unscaled, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-shift)), nil))
	}
	if !out.IsInt64() || out.Int64() > 1<<20 {
		return 0, false
	}
	v := int(out.Int64())
	if neg {
		v = -v
	}
	return v, true
}
