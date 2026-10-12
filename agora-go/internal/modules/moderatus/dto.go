package moderatus

import "agora/internal/jsonjava"

// The Moderatus answers are XML (Jackson XML annotations); with ?mediaType=json
// Jackson's JSON mapper ignores @JacksonXml* and writes the Kotlin property names,
// hence the two tags: `json` = property name, `xml` = @JacksonXmlProperty localName.

// ModeratusQagListXML is ModeratusQagListXml (@JacksonXmlRootElement "contents").
type ModeratusQagListXML struct {
	QagToModerateCount int               `json:"qagToModerateCount" xml:"nb_content"`
	QagsToModerate     []ModeratusQagXML `json:"qagsToModerate" xml:"msg,unwrapped"`
}

// RootName is @JacksonXmlRootElement(localName = "contents").
func (ModeratusQagListXML) RootName() string { return "contents" }

// ModeratusQagXML is ModeratusQagXml (username, title and description are CDATA).
type ModeratusQagXML struct {
	QagID       string `json:"qagId" xml:"content_id"`
	PostDate    string `json:"postDate" xml:"date"`
	UserID      string `json:"userId" xml:"user_id"`
	Username    string `json:"username" xml:"pseudo,cdata"`
	Title       string `json:"title" xml:"title,cdata"`
	Description string `json:"description" xml:"body,cdata"`
	MessageType string `json:"messageType" xml:"message_type"`
}

// messageTypeQuestion is the default value of ModeratusQagXml.messageType.
const messageTypeQuestion = "question"

// ModeratusQagListErrorXML is ModeratusQagListErrorXml.
type ModeratusQagListErrorXML struct {
	QagToModerateCount int    `json:"qagToModerateCount" xml:"nb_content"`
	ErrorMessage       string `json:"errorMessage" xml:"error"`
}

// RootName is @JacksonXmlRootElement(localName = "contents").
func (ModeratusQagListErrorXML) RootName() string { return "contents" }

// ModeratusQagLockResultsXML is ModeratusQagLockResultsXml: one unwrapped
// <confirmation> per result.
type ModeratusQagLockResultsXML struct {
	QagLockedResults []ModeratusQagLockResultXML `json:"qagLockedResults" xml:"confirmation,unwrapped"`
}

// RootName is @JacksonXmlRootElement(localName = "contents").
func (ModeratusQagLockResultsXML) RootName() string { return "contents" }

// ModeratusQagLockResultXML is ModeratusQagLockResultXml (three attributes; a null comment is omitted).
type ModeratusQagLockResultXML struct {
	QagID   string  `json:"qagId" xml:"content_id,attr"`
	Result  string  `json:"result" xml:"result,attr"`
	Comment *string `json:"comment" xml:"comment,attr"`
}

// emptyLockResultsXML is ModeratusQagLockResultsXml with an empty list. Jackson
// writes <contents/> when no child element was written (an empty unwrapped list
// writes nothing), where xmljava writes <contents></contents> for a struct that
// declares a child field: a field-less struct gives the exact bytes in XML and
// the JSON of the real object is written explicitly (see FOUNDATION FINDINGS).
type emptyLockResultsXML struct{}

// RootName is @JacksonXmlRootElement(localName = "contents").
func (emptyLockResultsXML) RootName() string { return "contents" }

// AppendJavaJSON implements jsonjava.Marshaler: {"qagLockedResults":[]}.
func (emptyLockResultsXML) AppendJavaJSON(dst []byte) []byte {
	return append(dst, `{"qagLockedResults":[]}`...)
}

var _ jsonjava.Marshaler = emptyLockResultsXML{}

// ModeratusQagModerateResultPageXML is ModeratusQagModerateResultPageXml (a null error is an empty element).
type ModeratusQagModerateResultPageXML struct {
	Result string  `json:"result" xml:"result"`
	Error  *string `json:"error" xml:"error"`
}

// RootName is @JacksonXmlRootElement(localName = "contents").
func (ModeratusQagModerateResultPageXML) RootName() string { return "contents" }
