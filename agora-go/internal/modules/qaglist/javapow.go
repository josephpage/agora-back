package qaglist

import (
	"math"
	"math/big"
	"sync"
)

// javaPow is Math.pow. Go's math.Pow is not the JVM's: for x^1.5 the results differ in the last
// bit for most whole numbers (pow(9, 1.5) is 27.000000000000004 in Go, 27 in Java) and the trending
// score is compared between QaGs, so a one-ulp difference could reorder two equal scores.
// HotSpot's intrinsic gives the correctly rounded value; it is computed here with 256-bit
// arithmetic and memoised for whole-number bases (the bases are `hours + 2.0`).
func javaPow(x, y float64) float64 {
	switch {
	case y == 0:
		return 1
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case math.IsInf(y, 0) && math.Abs(x) == 1:
		return math.NaN() // Java: NaN, C and Go: 1
	case x == 0 || math.IsInf(x, 0) || math.IsInf(y, 0):
		return math.Pow(x, y)
	}
	neg := false
	if x < 0 {
		if y != math.Trunc(y) {
			return math.NaN()
		}
		neg = math.Mod(y, 2) != 0 // odd integer exponent (|y| < 2^53 here, else even)
		x = -x
	}
	r := correctlyRoundedPow(x, y)
	if neg {
		return -r
	}
	return r
}

type powKey struct{ x, y float64 }

var powMemo sync.Map // powKey -> float64

func correctlyRoundedPow(x, y float64) float64 {
	if y == 1.5 {
		// the default exponent: the 50 whole numbers (>= 6209, i.e. QaGs moderated more than 258 days ago)
		// where the JVM's result is not the correctly rounded one, checked against the JVM for every x < 1<<17
		if bits, ok := jvmPow15Exceptions[x]; ok {
			return math.Float64frombits(bits)
		}
	}
	memo := x == math.Trunc(x) && x < 1<<20
	if memo {
		if v, ok := powMemo.Load(powKey{x, y}); ok {
			return v.(float64)
		}
	}
	v := bigPow(x, y)
	if memo {
		powMemo.Store(powKey{x, y}, v)
	}
	return v
}

const powPrec = 256

func bigFloat(v float64) *big.Float { return new(big.Float).SetPrec(powPrec).SetFloat64(v) }

// bigLn2 is ln 2 = 2 atanh(1/3).
var bigLn2 = func() *big.Float {
	third := new(big.Float).SetPrec(powPrec).Quo(bigFloat(1), bigFloat(3))
	return new(big.Float).SetPrec(powPrec).Mul(bigFloat(2), atanh(third))
}()

// atanh is the series z + z^3/3 + z^5/5 + ... for |z| <= 1/3.
func atanh(z *big.Float) *big.Float {
	sum := new(big.Float).SetPrec(powPrec).Set(z)
	z2 := new(big.Float).SetPrec(powPrec).Mul(z, z)
	term := new(big.Float).SetPrec(powPrec).Set(z)
	for k := int64(3); k < 400; k += 2 {
		term.Mul(term, z2)
		t := new(big.Float).SetPrec(powPrec).Quo(term, new(big.Float).SetPrec(powPrec).SetInt64(k))
		sum.Add(sum, t)
		if t.Sign() == 0 || t.MantExp(nil)-sum.MantExp(nil) < -powPrec-8 {
			break
		}
	}
	return sum
}

// bigLn is ln(x) for x > 0.
func bigLn(x float64) *big.Float {
	m, e := math.Frexp(x) // x = m * 2^e, m in [0.5, 1)
	bm := bigFloat(m)
	z := new(big.Float).SetPrec(powPrec).Quo(new(big.Float).SetPrec(powPrec).Sub(bm, bigFloat(1)), new(big.Float).SetPrec(powPrec).Add(bm, bigFloat(1)))
	ln := new(big.Float).SetPrec(powPrec).Mul(bigFloat(2), atanh(z))
	return ln.Add(ln, new(big.Float).SetPrec(powPrec).Mul(bigFloat(float64(e)), bigLn2))
}

// bigPow is x^y rounded to the nearest double (x > 0 and finite, y finite and not 0).
func bigPow(x, y float64) float64 {
	t := new(big.Float).SetPrec(powPrec).Mul(bigFloat(y), bigLn(x))
	// exp(t) = 2^k * exp(r), r = t - k ln 2
	kf, _ := new(big.Float).SetPrec(powPrec).Quo(t, bigLn2).Float64()
	k := math.Floor(kf + 0.5)
	if k > 1100 {
		return math.Inf(1)
	}
	if k < -1200 {
		return 0
	}
	r := new(big.Float).SetPrec(powPrec).Sub(t, new(big.Float).SetPrec(powPrec).Mul(bigFloat(k), bigLn2))
	sum := bigFloat(1)
	term := bigFloat(1)
	for n := int64(1); n < 200; n++ {
		term.Mul(term, r)
		term.Quo(term, new(big.Float).SetPrec(powPrec).SetInt64(n))
		sum.Add(sum, term)
		if term.Sign() == 0 || term.MantExp(nil) < -powPrec-8 {
			break
		}
	}
	sum.SetMantExp(sum, int(k))
	f, _ := sum.Float64()
	return f
}
