package sanitize

import (
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf16"

	"agora/parity/oracle"
)

// The fast path of Sanitize (isPlainText) is only valid if it is the identity of the whole pipeline: it is compared
// with the oracle on strings made of the characters of its alphabet plus the characters just outside of it.

var fastAlphabet = []string{
	"a", "b", "Z", "0", "9", " ", "  ", "\t", "\n", "\r", ".", ",", ";", ":", "!", "?", "-", "_", "(", ")", "[", "]", "}", "/", "\\", "*", "%", "#", "$", "~", "|", "^",
	"\"", "'", "+", "=", ">", "@", "`", "{", "{{", "é", "à", "ç", "É", "œ", "€", "…", "«", "»", "\u00a0", "\u0080", "\u009f", "Ā", "Α", "Ж", "א", "ا",
	"ह", "\u0c4d", "΅", "\u1ff0", "\u200c", "\u200d", "\u2028", "あ", "中", "\ud7ff", "\ue000", "\uf8ff", "﹟", "क", "अ",
}

var outsideAlphabet = []string{
	"<", "&", "&amp;", "{", "{{", "\x00", "\x01", "\x1f", "\x7f", "\u093a", "ा", "ॏ", "অ", "া", "ৌ", "అ", "\u0c3e", "\u0c48", "\u0c4c", "\u0c4d", /* inside */
	"`", "﹠", "＜", "�", "\ufffe", "\uffff", "\U00010000", "\U0001F600", "\U0010FFFF", "\xff", "\xc3",
}

// TestOracleFastPathEveryRune compares every non-surrogate BMP char (and the boundaries of the supplementary ones) in
// the contexts where isPlainText decides.
func TestOracleFastPathEveryRune(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	var inputs []string
	for r := rune(0); r <= 0xFFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		c := string(r)
		inputs = append(inputs, c, "a"+c, c+"a", "{"+c, c+"{", "\u200c"+c, c+"\u200c", "{"+c+"}", c+c)
	}
	for _, r := range []rune{0x10000, 0x1F600, 0x10FFFF, 0x1D800, 0x1DC00} {
		inputs = append(inputs, string(r), "a"+string(r), string(r)+"a")
	}
	var mu sync.Mutex
	var failures []string
	var next, fast atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0)*2; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(inputs) {
					return
				}
				s := inputs[i]
				if isPlainText(s) {
					fast.Add(1)
				}
				var res oracle.Text
				if err := oracle.Call("sanitize", map[string]any{"content": s, "maxLength": 1 << 28}, &res); err != nil {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("oracle error on %q: %v", s, err))
					mu.Unlock()
					continue
				}
				if got, want := Sanitize(s, 1<<28), dbString(res.Utf16); got != want {
					mu.Lock()
					if len(failures) < 20 {
						failures = append(failures, fmt.Sprintf("Sanitize(%q) = %q, oracle %q (plain=%v)", s, got, want, isPlainText(s)))
					}
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	for _, f := range failures {
		t.Error(f)
	}
	t.Logf("%d strings compared, %d took the fast path", len(inputs), fast.Load())
}

func TestOracleFastPath(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	n := 120000
	if testing.Short() {
		n = 5000
	}
	r := rand.New(rand.NewSource(99))
	inputs := make([]string, n)
	for i := range inputs {
		var sb strings.Builder
		for j, k := 0, r.Intn(12); j < k; j++ {
			switch {
			case r.Intn(12) == 0:
				sb.WriteString(outsideAlphabet[r.Intn(len(outsideAlphabet))])
			default:
				sb.WriteString(fastAlphabet[r.Intn(len(fastAlphabet))])
			}
		}
		inputs[i] = sb.String()
	}

	var fast, total atomic.Int64
	var mu sync.Mutex
	var failures []string
	var next atomic.Int64
	var wg sync.WaitGroup
	lengths := []int{0, 1, 5, 50, 200, 400}
	for w := 0; w < runtime.GOMAXPROCS(0)*2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rr := rand.New(rand.NewSource(int64(w)))
			for {
				i := int(next.Add(1)) - 1
				if i >= n {
					return
				}
				s := inputs[i]
				plain := isPlainText(s)
				total.Add(1)
				if plain {
					fast.Add(1)
				}
				for _, ml := range []int{lengths[rr.Intn(len(lengths))], 1 << 28} {
					var res oracle.Text
					if err := oracle.Call("sanitize", map[string]any{"content": s, "maxLength": ml}, &res); err != nil {
						mu.Lock()
						failures = append(failures, fmt.Sprintf("oracle error on %q: %v", s, err))
						mu.Unlock()
						return
					}
					want := dbString(res.Utf16)
					if got := Sanitize(s, ml); got != want {
						mu.Lock()
						if len(failures) < 20 {
							failures = append(failures, fmt.Sprintf("Sanitize(%q, %d) = %q, oracle %q (plain=%v)", s, ml, got, want, plain))
						}
						mu.Unlock()
					}
					// when it applies, the fast path is the identity before take
					if plain && ml == 1<<28 && s != want {
						mu.Lock()
						failures = append(failures, fmt.Sprintf("plain %q: not the identity, oracle %q", s, want))
						mu.Unlock()
					}
				}
			}
		}(w)
	}
	wg.Wait()
	for _, f := range failures {
		t.Error(f)
	}
	t.Logf("fast path: %d strings compared, %d took the fast path", total.Load(), fast.Load())
	if fast.Load() < int64(n)/4 {
		t.Errorf("the generator hardly exercises the fast path: %d/%d", fast.Load(), n)
	}
}

// dbString: lone surrogates are stored as '?' by the JDBC driver.
func dbString(u []uint16) string {
	var sb strings.Builder
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c <= 0xDBFF && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF:
			sb.WriteRune(utf16.DecodeRune(rune(c), rune(u[i+1])))
			i++
		case c >= 0xD800 && c <= 0xDFFF:
			sb.WriteByte('?')
		default:
			sb.WriteRune(rune(c))
		}
	}
	return sb.String()
}

// TestFastPathEqualsGeneralPath needs no oracle: the fast path must equal the (oracle verified) general path.
func TestFastPathEqualsGeneralPath(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	fast := 0
	for i := 0; i < 200000; i++ {
		var sb strings.Builder
		for j, k := 0, r.Intn(14); j < k; j++ {
			if r.Intn(10) == 0 {
				sb.WriteString(outsideAlphabet[r.Intn(len(outsideAlphabet))])
			} else {
				sb.WriteString(fastAlphabet[r.Intn(len(fastAlphabet))])
			}
		}
		s := sb.String()
		if !isPlainText(s) {
			continue
		}
		fast++
		for _, n := range []int{0, 1, 3, 7, 50, 1 << 20} {
			if got, want := Sanitize(s, n), fromUTF16(SanitizeUTF16(toUTF16(s), n)); got != want {
				t.Fatalf("Sanitize(%q, %d) = %q, general path %q", s, n, got, want)
			}
		}
	}
	if fast < 1000 {
		t.Fatalf("only %d plain strings", fast)
	}
}
