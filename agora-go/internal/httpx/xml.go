package httpx

import (
	"errors"

	"agora/internal/xmljava"
)

func xmlMarshal(v any) []byte { return xmljava.Marshal(v) }

func xmlMarshalChecked(v any) ([]byte, error) { return xmljava.MarshalChecked(v) }

// xmlDecodeBody would read an application/xml request body with Jackson's
// XmlMapper. No client sends XML bodies; this is recorded as divergence C-XML-BODY.
func xmlDecodeBody(body []byte, v any) error {
	return errors.New("XML request bodies are not supported")
}
