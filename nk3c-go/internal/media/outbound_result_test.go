package media

import "testing"

func TestClassifySIPFailure(t *testing.T) {
	cases := map[int]string{486: "BUSY", 600: "BUSY", 404: "INVALID", 484: "INVALID", 603: "REFUSE", 408: "NA", 480: "NA", 487: "NA", 500: "NA", 0: "NA"}
	for status, want := range cases {
		if got := classifySIPFailure(status); got != want {
			t.Errorf("status %d: got %s want %s", status, got, want)
		}
	}
}
