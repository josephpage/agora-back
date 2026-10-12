package strapi

import "testing"

// A null element in a link's children fails the rendering like the other containers
// (Kotlin: NullPointerException in joinToString, HTTP 500).
func TestLinkNullChildPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	n := &LinkNode{URL: "u", Children: []*TextNode{nil}}
	_ = n.ToHTML()
}
