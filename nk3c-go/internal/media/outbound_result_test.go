package media

import "testing"

func TestSIPFailureDetail(t *testing.T) {
	if got := sipFailureDetail(486, `Q.850;cause=17;text="User busy"`); got != `SIP 486; Reason: Q.850;cause=17;text="User busy"` {
		t.Fatalf("got %q", got)
	}
	if got := sipFailureDetail(0, ""); got != "SIP 0" {
		t.Fatalf("got %q", got)
	}
}

func TestClassifySIPFailure(t *testing.T) {
	cases := map[int]string{486: "BUSY", 600: "BUSY", 404: "INVALID", 484: "INVALID", 603: "REFUSE", 408: "NA", 480: "NA", 487: "NA", 500: "NA", 0: "NA"}
	for status, want := range cases {
		if got := classifySIPFailure(status); got != want {
			t.Errorf("status %d: got %s want %s", status, got, want)
		}
	}
}
