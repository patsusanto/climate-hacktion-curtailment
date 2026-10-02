package wire

import (
	"encoding/json"
	"strings"
)

// The playground streams one JSON event per line: {"type":"meta"|"step"|"done"|"error", ...}.
// The page reads either plain lines (NDJSON) or the same lines as SSE "data:" events.

const (
	FormatNDJSON = "ndjson"
	FormatSSE    = "sse"
)

// StreamFormat says how to stream to a client, from its Accept header: NDJSON if it asks for
// it, otherwise SSE if it asks for that, otherwise "" (the client wants no stream).
func StreamFormat(accept string) string {
	accept = strings.ToLower(accept)
	switch {
	case strings.Contains(accept, "application/x-ndjson"):
		return FormatNDJSON
	case strings.Contains(accept, "text/event-stream"):
		return FormatSSE
	}
	return ""
}

// ContentType is the Content-Type for a stream format.
func ContentType(format string) string {
	if format == FormatSSE {
		return "text/event-stream"
	}
	return "application/x-ndjson"
}

type envelope struct {
	Type    string   `json:"type"`
	Meta    *Meta    `json:"meta,omitempty"`
	Tick    *Tick    `json:"tick,omitempty"`
	Summary *Summary `json:"summary,omitempty"`
	Message string   `json:"message,omitempty"`
}

func line(e envelope) []byte {
	raw, err := json.Marshal(e)
	if err != nil { // these types hold only numbers and strings, so this cannot happen
		panic(err)
	}
	return append(raw, '\n')
}

// MetaLine, StepLine, DoneLine and ErrorLine are one event each: JSON followed by a newline.
func MetaLine(m Meta) []byte      { return line(envelope{Type: "meta", Meta: &m}) }
func StepLine(t Tick) []byte      { return line(envelope{Type: "step", Tick: &t}) }
func DoneLine(s Summary) []byte   { return line(envelope{Type: "done", Summary: &s}) }
func ErrorLine(msg string) []byte { return line(envelope{Type: "error", Message: msg}) }

// Encode is what to write for one event line in the given format. In SSE each event is a single
// "data:" line followed by a blank line, with no "id:" or "event:" lines, because the page
// parses every line it receives.
func Encode(format string, eventLine []byte) []byte {
	if format != FormatSSE {
		return eventLine
	}
	out := make([]byte, 0, len(eventLine)+7)
	out = append(out, "data: "...)
	out = append(out, eventLine...)
	return append(out, '\n')
}
