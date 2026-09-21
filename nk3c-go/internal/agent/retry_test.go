package agent

import (
	"strings"
	"testing"
)

func TestRetryAvailableAtUsesIncreasingBackoff(t *testing.T) {
	one := retryAvailableAt("sqlite", 1)
	two := retryAvailableAt("sqlite", 2)
	three := retryAvailableAt("sqlite", 3)
	if !strings.Contains(one, "T") || !strings.Contains(two, "T") || !strings.Contains(three, "T") { t.Fatal("expected sqlite timestamps") }
	if one >= two || two >= three { t.Fatalf("backoff timestamps not increasing: %s %s %s", one, two, three) }
}
