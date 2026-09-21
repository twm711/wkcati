package store

import "time"

// NowFor returns a timestamp accepted by the selected SQL dialect.
func NowFor(driver string) string {
	if driver == "mysql" {
		return time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	return NowISO()
}
