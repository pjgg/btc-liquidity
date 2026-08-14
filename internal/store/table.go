// Package store holds the CSV files that act as this project's database.
//
// The files are committed to the repository by the scheduled Action and served
// verbatim by GitHub Pages, so they are kept sorted and stable: a rewrite that
// reorders rows would produce a large diff every day for no reason.
package store

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Table is a CSV keyed by its first column, which is always an ISO date.
type Table struct {
	header []string
	rows   map[string][]string // date -> values, excluding the date itself
}

func newTable(header []string) *Table {
	return &Table{header: header, rows: make(map[string][]string)}
}

// Load reads path, or returns an empty table if it does not exist yet.
//
// A header that disagrees with the expected one is an error rather than a
// migration: reading old columns into a new layout would shift every value.
func Load(path string, header []string) (*Table, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return newTable(header), nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(records) == 0 {
		return newTable(header), nil
	}
	if !slices.Equal(records[0], header) {
		return nil, fmt.Errorf(
			"%s has header %v but this build expects %v; delete the file to rebuild it",
			path, records[0], header,
		)
	}

	table := newTable(header)
	for _, record := range records[1:] {
		table.rows[record[0]] = record[1:]
	}
	return table, nil
}

// Upsert sets the row for date, replacing any existing one.
//
// The Action reruns on the same date whenever it is retried or dispatched
// manually, so overwriting is the correct behaviour and appending is not.
func (t *Table) Upsert(date string, values []string) {
	if len(values) != len(t.header)-1 {
		panic(fmt.Sprintf(
			"store: row for %s has %d values, header expects %d",
			date, len(values), len(t.header)-1,
		))
	}
	t.rows[date] = values
}

func (t *Table) Get(date string) []string { return t.rows[date] }

func (t *Table) Len() int { return len(t.rows) }

// Dates returns every date present, in ascending order. ISO dates sort
// correctly as strings, so no parsing is needed.
func (t *Table) Dates() []string {
	dates := make([]string, 0, len(t.rows))
	for date := range t.rows {
		dates = append(dates, date)
	}
	slices.Sort(dates)
	return dates
}

// LastDate is the most recent date present, or "" when the table is empty.
func (t *Table) LastDate() string {
	dates := t.Dates()
	if len(dates) == 0 {
		return ""
	}
	return dates[len(dates)-1]
}

// Save writes the table to path, sorted by date, creating parent directories.
func (t *Table) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.Write(t.header); err != nil {
		return fmt.Errorf("writing header to %s: %w", path, err)
	}
	for _, date := range t.Dates() {
		if err := writer.Write(append([]string{date}, t.rows[date]...)); err != nil {
			return fmt.Errorf("writing row %s to %s: %w", date, path, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flushing %s: %w", path, err)
	}
	return file.Close()
}
