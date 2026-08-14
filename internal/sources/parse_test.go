package sources

import (
	"math"
	"strings"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestParseFREDCSV(t *testing.T) {
	input := "observation_date,WALCL\n" +
		"2002-12-18,719542\n" +
		"2026-08-05,6748567\n" +
		"2026-08-12,6759955\n"

	series, err := ParseFREDCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(series.Values) != 3 {
		t.Fatalf("expected 3 observations, got %d", len(series.Values))
	}
	if got := series.Values[day(2026, time.August, 12)]; got != 6_759_955 {
		t.Errorf("2026-08-12 = %v, want 6759955", got)
	}
	if !series.Last.Equal(day(2026, time.August, 12)) {
		t.Errorf("Last = %v, want 2026-08-12", series.Last)
	}
}

// FRED emits an empty value for dates with no observation — bank holidays in the
// daily FX series, for instance. DEXUSEU alone had 279 such rows on 2026-08-14.
//
// Treating those as 0.0 would multiply the ECB balance sheet by zero on every
// holiday, collapsing the chart to zero roughly one day in twenty. The row must
// be skipped entirely so ForwardFill carries the previous value instead.
func TestParseFREDCSVSkipsEmptyValues(t *testing.T) {
	input := "observation_date,DEXUSEU\n" +
		"2025-12-30,1.1765\n" +
		"2025-12-31,1.1736\n" +
		"2026-01-01,\n" + // New Year's Day: published as empty
		"2026-01-02,1.1738\n"

	series, err := ParseFREDCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, present := series.Values[day(2026, time.January, 1)]; present {
		t.Error("a holiday with no observation must be absent, not present as zero")
	}
	if len(series.Values) != 3 {
		t.Errorf("expected 3 real observations, got %d", len(series.Values))
	}
	// The last real observation, not the empty row, sets the freshness date.
	if !series.Last.Equal(day(2026, time.January, 2)) {
		t.Errorf("Last = %v, want 2026-01-02", series.Last)
	}
}

func TestParseFREDCSVRejectsGarbageValue(t *testing.T) {
	input := "observation_date,X\n2026-08-12,not-a-number\n"
	if _, err := ParseFREDCSV(strings.NewReader(input)); err == nil {
		t.Error("a non-empty unparseable value must be an error, not a silent skip")
	}
}

func TestParseFREDCSVRejectsBadDate(t *testing.T) {
	input := "observation_date,X\n12/08/2026,100\n"
	if _, err := ParseFREDCSV(strings.NewReader(input)); err == nil {
		t.Error("an unexpected date format must be an error")
	}
}

func TestParseFREDCSVEmptySeriesIsAnError(t *testing.T) {
	input := "observation_date,X\n"
	if _, err := ParseFREDCSV(strings.NewReader(input)); err == nil {
		t.Error("a series with no observations must be an error, not an empty success")
	}
}

func TestParseBinanceKlines(t *testing.T) {
	// Two real rows, trimmed. Field 0 is openTime in ms, field 4 is the close.
	input := []byte(`[
	  ["1700352000000","36568.11","37500.00","36384.02","37359.86","21246.34",1700438399999,"781531839.43",862400,"10575.25","389089868.80","0"],
	  ["1755216000000","117000.00","119000.00","116000.00","118387.76","10000.00",1755302399999,"1000000.00",100000,"5000.00","500000.00","0"]
	]`)

	closes, err := ParseBinanceKlines(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(closes) != 2 {
		t.Fatalf("expected 2 closes, got %d", len(closes))
	}

	// 1700352000000 ms == 2023-11-19 00:00 UTC.
	if got := closes[day(2023, time.November, 19)]; math.Abs(got-37359.86) > 1e-6 {
		t.Errorf("2023-11-19 close = %v, want 37359.86", got)
	}
	if got := closes[day(2025, time.August, 15)]; math.Abs(got-118387.76) > 1e-6 {
		t.Errorf("2025-08-15 close = %v, want 118387.76", got)
	}
}

func TestParseBinanceKlinesRejectsShortRow(t *testing.T) {
	input := []byte(`[["1700352000000","36568.11"]]`)
	if _, err := ParseBinanceKlines(input); err == nil {
		t.Error("a kline row without a close field must be an error")
	}
}

func TestParseBinanceKlinesEmptyArray(t *testing.T) {
	closes, err := ParseBinanceKlines([]byte(`[]`))
	if err != nil {
		t.Fatalf("an empty page is the normal end of pagination, not an error: %v", err)
	}
	if len(closes) != 0 {
		t.Errorf("expected no closes, got %d", len(closes))
	}
}
