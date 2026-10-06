package strapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"agora/internal/jsonjava"
)

// RichNode is the sealed interface StrapiRichText.
type RichNode interface{ ToHTML() string }

// RichText is List<StrapiRichText> (Strapi "blocks" rich text).
type RichText []RichNode

// ToHTML is List<StrapiRichText>.toHtml(): concatenation minus a trailing "<br/>".
func (l RichText) ToHTML() string {
	var b strings.Builder
	for _, n := range l {
		b.WriteString(n.ToHTML())
	}
	return strings.TrimSuffix(b.String(), "<br/>")
}

// ToHTMLBody is List<StrapiRichText>.toHtmlBody().
func (l RichText) ToHTMLBody() string { return "<body>" + l.ToHTML() + "</body>" }

// UnmarshalJavaTree implements jsonjava.TreeUnmarshaler with Jackson's
// polymorphic rules (@JsonTypeInfo NAME / EXISTING_PROPERTY "type",
// defaultImpl = StrapiRichUnknownNode).
func (l *RichText) UnmarshalJavaTree(tree any) error {
	arr, ok := tree.([]any)
	if !ok {
		return errors.New("rich text: expected array")
	}
	out := make(RichText, 0, len(arr))
	for _, e := range arr {
		if e == nil {
			out = append(out, nil)
			continue
		}
		n, err := decodeRichNode(e)
		if err != nil {
			return err
		}
		out = append(out, n)
	}
	*l = out
	return nil
}

// TextNode is StrapiRichTextNode.
type TextNode struct {
	Text          string `json:"text"`
	Bold          *bool  `json:"bold"`
	Underline     *bool  `json:"underline"`
	Italic        *bool  `json:"italic"`
	Strikethrough *bool  `json:"strikethrough"`
	Code          *bool  `json:"code"`
}

func isTrue(b *bool) bool { return b != nil && *b }

// ToHTML wraps marks in the order b, i, u, del, code.
func (n *TextNode) ToHTML() string {
	h := n.Text
	if isTrue(n.Bold) {
		h = "<b>" + h + "</b>"
	}
	if isTrue(n.Italic) {
		h = "<i>" + h + "</i>"
	}
	if isTrue(n.Underline) {
		h = "<u>" + h + "</u>"
	}
	if isTrue(n.Strikethrough) {
		h = "<del>" + h + "</del>"
	}
	if isTrue(n.Code) {
		h = "<code>" + h + "</code>"
	}
	return h
}

// LinkNode is StrapiRichLinkNode (children: List<StrapiRichTextNode>).
type LinkNode struct {
	URL      string
	Children []*TextNode
}

// ToHTML renders <a href="url">children</a> (no escaping, like Kotlin).
func (n *LinkNode) ToHTML() string {
	var b strings.Builder
	for _, c := range n.Children {
		if c != nil {
			b.WriteString(c.ToHTML())
		}
	}
	return `<a href="` + n.URL + `">` + strings.TrimSuffix(b.String(), "<br/>") + "</a>"
}

// containerNode covers list-item, heading, list, paragraph, quote, unknown.
type containerNode struct {
	kind     string
	level    *int
	format   string
	children RichText
}

func (n *containerNode) ToHTML() string {
	inner := n.children.ToHTML()
	switch n.kind {
	case "list-item":
		return "<li>" + inner + "</li>"
	case "heading":
		if n.level != nil && *n.level >= 1 && *n.level <= 6 {
			return fmt.Sprintf("<h%d>%s</h%d>", *n.level, inner, *n.level)
		}
		lvl := "null"
		if n.level != nil {
			lvl = fmt.Sprint(*n.level)
		}
		slog.Warn("Erreur dans la conversion Strapi des titres, level '" + lvl + "' non reconnu")
		return inner
	case "list":
		if n.format == "ordered" {
			return "<ol>" + inner + "</ol>"
		}
		return "<ul>" + inner + "</ul>"
	case "paragraph":
		return "<p>" + inner + "</p>"
	case "quote":
		return "<blockquote>" + inner + "</blockquote>"
	}
	return inner // StrapiRichUnknownNode
}

func typeID(obj map[string]any) (string, bool) {
	v, ok := obj["type"]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	}
	return "", false
}

func decodeRichNode(tree any) (RichNode, error) {
	obj, ok := tree.(map[string]any)
	if !ok {
		return nil, errors.New("rich text node: expected object")
	}
	id, _ := typeID(obj)
	switch id {
	case "text":
		var t TextNode
		if err := jsonjava.UnmarshalTree(tree, &t); err != nil {
			return nil, err
		}
		return &t, nil
	case "link":
		return decodeLink(obj)
	case "list-item", "heading", "paragraph", "list", "quote":
		return decodeContainer(id, obj)
	default:
		return decodeContainer("unknown", obj)
	}
}

func decodeLink(obj map[string]any) (RichNode, error) {
	var tmp struct {
		URL string `json:"url"`
	}
	if err := jsonjava.UnmarshalTree(obj, &tmp); err != nil {
		return nil, err
	}
	rawChildren, present := obj["children"]
	if !present || rawChildren == nil {
		return nil, errors.New("link: missing children")
	}
	arr, ok := rawChildren.([]any)
	if !ok {
		return nil, errors.New("link: children not an array")
	}
	n := &LinkNode{URL: tmp.URL, Children: make([]*TextNode, 0, len(arr))}
	for _, e := range arr {
		if e == nil {
			n.Children = append(n.Children, nil)
			continue
		}
		eo, ok := e.(map[string]any)
		if !ok {
			return nil, errors.New("link child: expected object")
		}
		if id, _ := typeID(eo); id != "text" {
			// declared element type StrapiRichTextNode: other subtypes (or the
			// defaultImpl) are not assignable → InvalidTypeIdException.
			return nil, errors.New("link child: not a text node")
		}
		var t TextNode
		if err := jsonjava.UnmarshalTree(eo, &t); err != nil {
			return nil, err
		}
		n.Children = append(n.Children, &t)
	}
	return n, nil
}

func decodeContainer(kind string, obj map[string]any) (RichNode, error) {
	n := &containerNode{kind: kind}
	rawChildren, present := obj["children"]
	if !present || rawChildren == nil {
		return nil, fmt.Errorf("%s: missing children", kind)
	}
	if err := n.children.UnmarshalJavaTree(rawChildren); err != nil {
		return nil, err
	}
	switch kind {
	case "heading":
		var tmp struct {
			Level *int `json:"level"`
		}
		if err := jsonjava.UnmarshalTree(obj, &tmp); err != nil {
			return nil, err
		}
		n.level = tmp.Level
	case "list":
		var tmp struct {
			Format string `json:"format"`
		}
		if err := jsonjava.UnmarshalTree(obj, &tmp); err != nil {
			return nil, err
		}
		n.format = tmp.Format
	}
	return n, nil
}

// MediaPicture is StrapiMediaPicture.
type MediaPicture struct {
	Formats               *MediaPictureFormats `json:"formats"`
	PictureURLNotOptimized string              `json:"url"`
}

// MediaURL is mediaUrl(): formats.medium.url, else url.
func (m *MediaPicture) MediaURL() string {
	if m.Formats != nil && m.Formats.Medium != nil {
		return m.Formats.Medium.URL
	}
	return m.PictureURLNotOptimized
}

// MediaPictureFormats is StrapiMediaPictureFormats.
type MediaPictureFormats struct {
	Medium *MediaPictureFormatMedium `json:"medium"`
}

// MediaPictureFormatMedium is StrapiMediaPictureFormatMedium.
type MediaPictureFormatMedium struct {
	URL string `json:"url"`
}

// MediaVideo is StrapiMediaVideo.
type MediaVideo struct {
	URL string `json:"url"`
}

// MediaPdf is StrapiMediaPdf.
type MediaPdf struct {
	URL string `json:"url"`
}
