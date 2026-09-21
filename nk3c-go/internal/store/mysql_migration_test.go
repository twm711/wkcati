package store

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestMySQLMigrationsAreOrderedAndHaveGooseSections(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("migrations", "mysql"))
	if err != nil {
		t.Fatal(err)
	}
	versions := []int{}
	re := regexp.MustCompile(`^(\d+)_.*\.sql$`)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		m := re.FindStringSubmatch(entry.Name())
		if len(m) != 2 {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		versions = append(versions, v)
		body, err := os.ReadFile(filepath.Join("migrations", "mysql", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "-- +goose Up") || !strings.Contains(text, "-- +goose Down") {
			t.Fatalf("%s missing goose sections", entry.Name())
		}
		if entry.Name() == "038_demo_seed.sql" {
			for _, token := range []string{"prj_project", "qnr_questionnaire", "smp_sample", "smp_status_code", "ivr_flow"} {
				if !strings.Contains(text, token) { t.Fatalf("demo seed missing %s", token) }
			}
		}
	}
	sort.Ints(versions)
	for i := 1; i < len(versions); i++ {
		if versions[i] == versions[i-1] {
			t.Fatalf("duplicate migration version %d", versions[i])
		}
	}
}
