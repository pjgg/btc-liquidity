package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func TestLoadMissingFileGivesEmptyTable(t *testing.T) {
	table, err := Load(tempPath(t, "absent.csv"), []string{"date", "close_usd"})
	if err != nil {
		t.Fatalf("a missing file is the first run, not an error: %v", err)
	}
	if table.Len() != 0 {
		t.Errorf("expected an empty table, got %d rows", table.Len())
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := tempPath(t, "btc.csv")

	table, err := Load(path, []string{"date", "close_usd"})
	if err != nil {
		t.Fatal(err)
	}
	table.Upsert("2026-08-13", []string{"63490.86"})
	table.Upsert("2026-08-14", []string{"62850.29"})
	if err := table.Save(path); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(path, []string{"date", "close_usd"})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Len() != 2 {
		t.Fatalf("expected 2 rows after reload, got %d", reloaded.Len())
	}
	if got := reloaded.Get("2026-08-14")[0]; got != "62850.29" {
		t.Errorf("2026-08-14 = %q, want 62850.29", got)
	}
}

// The Action reruns on the same date whenever it is retried or dispatched
// manually. A second run must correct that date's row, never append a duplicate.
func TestUpsertIsIdempotent(t *testing.T) {
	table := newTable([]string{"date", "close_usd"})

	table.Upsert("2026-08-14", []string{"62850.29"})
	table.Upsert("2026-08-14", []string{"62900.00"})

	if table.Len() != 1 {
		t.Fatalf("expected 1 row after two upserts of one date, got %d", table.Len())
	}
	if got := table.Get("2026-08-14")[0]; got != "62900.00" {
		t.Errorf("the later value must win, got %q", got)
	}
}

func TestSaveSortsByDate(t *testing.T) {
	path := tempPath(t, "sorted.csv")

	table := newTable([]string{"date", "close_usd"})
	table.Upsert("2026-08-14", []string{"3"})
	table.Upsert("2026-08-12", []string{"1"})
	table.Upsert("2026-08-13", []string{"2"})
	if err := table.Save(path); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	want := []string{
		"date,close_usd",
		"2026-08-12,1",
		"2026-08-13,2",
		"2026-08-14,3",
	}
	if len(lines) != len(want) {
		t.Fatalf("expected %d lines, got %d: %q", len(want), len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestUpsertRejectsWrongColumnCount(t *testing.T) {
	table := newTable([]string{"date", "a", "b"})
	defer func() {
		if recover() == nil {
			t.Error("a row with the wrong number of columns must not be accepted silently")
		}
	}()
	table.Upsert("2026-08-14", []string{"only-one"})
}

func TestLoadRejectsHeaderMismatch(t *testing.T) {
	path := tempPath(t, "mismatch.csv")
	if err := os.WriteFile(path, []byte("date,something_else\n2026-08-14,1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A schema change must be loud. Silently reading the old columns into the
	// new layout would shift every value one column to the left.
	if _, err := Load(path, []string{"date", "close_usd"}); err == nil {
		t.Error("expected an error when the on-disk header does not match")
	}
}

func TestLastDateReportsMaximum(t *testing.T) {
	table := newTable([]string{"date", "v"})
	if table.LastDate() != "" {
		t.Error("an empty table has no last date")
	}

	table.Upsert("2026-08-12", []string{"1"})
	table.Upsert("2026-08-14", []string{"3"})
	table.Upsert("2026-08-13", []string{"2"})

	if got := table.LastDate(); got != "2026-08-14" {
		t.Errorf("LastDate = %q, want 2026-08-14", got)
	}
}
