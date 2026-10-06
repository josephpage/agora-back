package strapi_test

import (
	"encoding/json"
	"math/rand"
	"testing"

	"agora/internal/jsonjava"
	"agora/internal/strapi"
	"agora/parity/oracle"
)

func randomNode(r *rand.Rand, depth int) any {
	types := []any{"text", "link", "list-item", "heading", "paragraph", "list", "quote", "image", nil, 3, "TEXT"}
	n := map[string]any{}
	if t := types[r.Intn(len(types))]; t != nil || r.Intn(2) == 0 {
		n["type"] = t
	}
	texts := []any{"hello", "<b>x</b>", "é😀", "", nil, 12, true, "a<br/>"}
	if r.Intn(4) != 0 {
		n["text"] = texts[r.Intn(len(texts))]
	}
	for _, m := range []string{"bold", "italic", "underline", "strikethrough", "code"} {
		if r.Intn(4) == 0 {
			vals := []any{true, false, nil, "true", 1, 0}
			n[m] = vals[r.Intn(len(vals))]
		}
	}
	if r.Intn(3) == 0 {
		levels := []any{1, 2, 3, 4, 5, 6, 7, 0, nil, "2", 2.5}
		n["level"] = levels[r.Intn(len(levels))]
	}
	if r.Intn(3) == 0 {
		fm := []any{"ordered", "unordered", nil, 1}
		n["format"] = fm[r.Intn(len(fm))]
	}
	if r.Intn(3) == 0 {
		u := []any{"https://x.fr/a?b=1&c=2", nil, "", 5}
		n["url"] = u[r.Intn(len(u))]
	}
	if depth < 3 && r.Intn(5) != 0 {
		var ch []any
		for i := 0; i < r.Intn(4); i++ {
			ch = append(ch, randomNode(r, depth+1))
		}
		if ch == nil {
			n["children"] = []any{}
		} else {
			n["children"] = ch
		}
	} else if r.Intn(4) == 0 {
		n["children"] = nil
	}
	return n
}

func TestOracleRichText(t *testing.T) {
	if !oracle.Available() {
		t.Skip("PARITY_ORACLE!=1")
	}
	r := rand.New(rand.NewSource(3))
	agree, total := 0, 0
	for i := 0; i < 1500; i++ {
		var arr []any
		for j := 0; j < 1+r.Intn(3); j++ {
			arr = append(arr, randomNode(r, 0))
		}
		in, _ := json.Marshal(arr)
		var want string
		jerr := oracle.Call("richTextToHtml", map[string]any{"json": string(in)}, &want)
		var rt strapi.RichText
		gerr := jsonjava.Unmarshal(in, &rt)
		var got string
		if gerr == nil {
			func() {
				defer func() {
					if p := recover(); p != nil {
						gerr = jsonjava.ErrPanic
					}
				}()
				got = rt.ToHTML()
			}()
		}
		total++
		if (jerr == nil) != (gerr == nil) {
			t.Errorf("acceptance differs for %s: go err=%v java err=%v", in, gerr, jerr)
			continue
		}
		if jerr == nil && got != want {
			t.Errorf("html differs for %s:\n go   %q\n java %q", in, got, want)
			continue
		}
		agree++
	}
	t.Logf("%d/%d agree", agree, total)
}
