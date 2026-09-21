package media

import "testing"

func TestClassifySIPFailureResults(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{486, "BUSY"}, {600, "BUSY"}, {404, "INVALID"}, {484, "INVALID"},
		{603, "REFUSE"}, {607, "REFUSE"}, {408, "NA"}, {480, "NA"}, {487, "NA"}, {500, "NA"},
	}
	for _, tc := range cases {
		if got := classifySIPFailure(tc.status); got != tc.want {
			t.Fatalf("status %d: got %s want %s", tc.status, got, tc.want)
		}
	}
}
