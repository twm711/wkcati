package store

import "time"

// NowFor returns a timestamp accepted by the selected SQL dialect.
func NowFor(driver string) string {
	return TimeFor(driver, time.Now().UTC())
}

func TimeFor(driver string, t time.Time) string {
	if driver == "mysql" {
		return t.UTC().Format("2006-01-02 15:04:05")
	}
	return t.UTC().Format("2006-01-02T15:04:05+00:00")
}
