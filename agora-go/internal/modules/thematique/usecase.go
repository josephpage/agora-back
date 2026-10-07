package thematique

import (
	"sort"
	"unicode/utf8"
)

// idThematiqueAutre is ListThematiqueUseCase.ID_THEMATIQUE_AUTRE: sorted last.
const idThematiqueAutre = "47897e51-8e94-4920-a26a-1b1e5e232e82"

// sortThematiques is ListThematiqueUseCase.getThematiqueList's
// sortedWith(compareBy({ it.id == ID_THEMATIQUE_AUTRE }, { it.label })):
// stable, false < true, then String.compareTo on the label.
func sortThematiques(list []Thematique) []Thematique {
	out := append([]Thematique(nil), list...)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].ID == idThematiqueAutre, out[j].ID == idThematiqueAutre
		if ai != aj {
			return !ai // false (not "autre") first
		}
		return compareUTF16(out[i].Label, out[j].Label) < 0
	})
	return out
}

// utf16Lead is the first UTF-16 code unit of r.
func utf16Lead(r rune) rune {
	if r >= 0x10000 {
		return 0xD800 + ((r - 0x10000) >> 10)
	}
	return r
}

// compareUTF16 is java.lang.String.compareTo (lexicographic on UTF-16 code
// units, which differs from UTF-8 byte order when a supplementary character is
// compared with a BMP character >= U+E000). Only the sign is meaningful.
func compareUTF16(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			la, lb := utf16Lead(ra), utf16Lead(rb)
			switch {
			case la < lb:
				return -1
			case la > lb:
				return 1
			case ra < rb: // same lead surrogate: the trail surrogates order like the code points
				return -1
			default:
				return 1
			}
		}
		a, b = a[sa:], b[sb:]
	}
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return -1
	}
	return 1
}
