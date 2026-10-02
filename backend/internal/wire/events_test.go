package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamFormat(t *testing.T) {
	cases := map[string]string{
		"application/x-ndjson, text/event-stream": FormatNDJSON, // what the page sends
		"application/x-ndjson":                    FormatNDJSON,
		"text/event-stream":                       FormatSSE,
		"Text/Event-Stream, */*":                  FormatSSE,
		"text/event-stream, application/x-ndjson": FormatNDJSON, // NDJSON wins when both are offered
		"*/*":              "",
		"application/json": "",
		"":                 "",
	}
	for accept, want := range cases {
		if got := StreamFormat(accept); got != want {
			t.Errorf("StreamFormat(%q) = %q, want %q", accept, got, want)
		}
	}
	if ContentType(FormatSSE) != "text/event-stream" || ContentType(FormatNDJSON) != "application/x-ndjson" {
		t.Error("content types")
	}
}

func TestEventLinesAreOneObjectEach(t *testing.T) {
	lines := map[string][]byte{
		"meta":  MetaLine(Meta{Window: Window{N: 3}}),
		"step":  StepLine(Tick{I: 1, Action: "hold"}),
		"done":  DoneLine(Summary{SavingsAud: 1.5}),
		"error": ErrorLine("boom"),
	}
	for kind, b := range lines {
		s := string(b)
		if !strings.HasSuffix(s, "\n") || strings.Count(s, "\n") != 1 {
			t.Errorf("%s is not exactly one line: %q", kind, s)
		}
		if !strings.HasPrefix(s, `{"type":"`+kind+`"`) {
			t.Errorf("%s does not start with its type: %.60q", kind, s)
		}
		var e map[string]json.RawMessage
		if err := json.Unmarshal(b, &e); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	// the payload sits under the key the page reads
	var step struct {
		Tick struct{ I int } `json:"tick"`
	}
	json.Unmarshal(lines["step"], &step)
	if step.Tick.I != 1 {
		t.Errorf("step payload: %s", lines["step"])
	}
	// zero values still appear: the page needs i, soc and cash even when they are 0
	if !strings.Contains(string(StepLine(Tick{})), `"i":0`) || !strings.Contains(string(StepLine(Tick{})), `"cumulative_self_aud":0`) {
		t.Error("a zero tick must still carry its fields")
	}
}

func TestEncode(t *testing.T) {
	l := StepLine(Tick{I: 7})
	if string(Encode(FormatNDJSON, l)) != string(l) {
		t.Error("NDJSON is the line itself")
	}
	sse := string(Encode(FormatSSE, l))
	if sse != "data: "+string(l)+"\n" || !strings.HasSuffix(sse, "\n\n") {
		t.Errorf("SSE: %q", sse)
	}
	if strings.Contains(sse, "id:") || strings.Contains(sse, "event:") {
		t.Error("SSE must have data lines only")
	}
}
