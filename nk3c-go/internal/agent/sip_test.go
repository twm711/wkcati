package agent

import "testing"

func TestParseQ850(t *testing.T) {
	cause, text := parseQ850(`SIP 486; Reason: Q.850;cause=17;text="User busy"`)
	if cause != 17 || text != "User busy" {
		t.Fatalf("got %v %v", cause, text)
	}
	cause, text = parseQ850("SIP 480")
	if cause != nil || text != nil {
		t.Fatalf("unexpected %v %v", cause, text)
	}
}
