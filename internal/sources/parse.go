// Package sources fetches and parses the upstream data feeds.
//
// Parsing is kept separate from fetching so the formats can be tested without
// network access — which matters, because both feeds have quirks that are easy
// to get silently wrong.
package sources

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"
)

// Series is a parsed FRED series plus the date of its most recent real
// observation, which the freshness guard needs.
type Series struct {
	Values map[time.Time]float64
	Last   time.Time
}

// ParseFREDCSV reads the two-column CSV served by fredgraph.csv.
//
// FRED publishes an empty value for dates it has no observation for — bank
// holidays in the daily FX series, most commonly. Those rows are skipped rather
// than recorded as zero: a zero FX rate would zero out the balance sheet that
// gets converted with it. A non-empty value that will not parse is still an
// error, because that means the format changed.
func ParseFREDCSV(r io.Reader) (Series, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = 2

	records, err := reader.ReadAll()
	if err != nil {
		return Series{}, fmt.Errorf("reading FRED csv: %w", err)
	}
	if len(records) < 2 {
		return Series{}, errors.New("FRED csv has no observations")
	}

	series := Series{Values: make(map[time.Time]float64, len(records)-1)}
	for i, record := range records[1:] {
		date, err := time.ParseInLocation(time.DateOnly, record[0], time.UTC)
		if err != nil {
			return Series{}, fmt.Errorf("FRED csv line %d: bad date %q: %w", i+2, record[0], err)
		}

		if record[1] == "" {
			continue // no observation for this date; ForwardFill covers the gap
		}

		value, err := strconv.ParseFloat(record[1], 64)
		if err != nil {
			return Series{}, fmt.Errorf("FRED csv line %d: bad value %q: %w", i+2, record[1], err)
		}

		series.Values[date] = value
		if date.After(series.Last) {
			series.Last = date
		}
	}

	if len(series.Values) == 0 {
		return Series{}, errors.New("FRED csv contained no usable observations")
	}
	return series, nil
}

// binanceCloseIndex is the position of the close price within a kline row.
// Binance returns each candle as a heterogeneous array rather than an object.
const (
	binanceOpenTimeIndex = 0
	binanceCloseIndex    = 4
	binanceMinFields     = binanceCloseIndex + 1
)

// ParseBinanceKlines reads daily candles and keeps only the UTC date and close.
//
// An empty array is the normal end of pagination, not an error.
func ParseBinanceKlines(data []byte) (map[time.Time]float64, error) {
	var rows [][]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("decoding binance klines: %w", err)
	}

	closes := make(map[time.Time]float64, len(rows))
	for i, row := range rows {
		if len(row) < binanceMinFields {
			return nil, fmt.Errorf("binance kline %d has %d fields, need at least %d", i, len(row), binanceMinFields)
		}

		openMillis, err := jsonNumber(row[binanceOpenTimeIndex])
		if err != nil {
			return nil, fmt.Errorf("binance kline %d: open time: %w", i, err)
		}
		close, err := jsonNumber(row[binanceCloseIndex])
		if err != nil {
			return nil, fmt.Errorf("binance kline %d: close: %w", i, err)
		}

		date := time.UnixMilli(int64(openMillis)).UTC().Truncate(24 * time.Hour)
		closes[date] = close
	}
	return closes, nil
}

// jsonNumber reads a value Binance may send either quoted or bare — open times
// come back as bare numbers and prices as strings.
func jsonNumber(raw json.RawMessage) (float64, error) {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strconv.ParseFloat(asString, 64)
	}

	var asFloat float64
	if err := json.Unmarshal(raw, &asFloat); err != nil {
		return 0, fmt.Errorf("value %s is neither a number nor a numeric string", raw)
	}
	return asFloat, nil
}

// ParseDBnomicsSeries reads a single series from the DBnomics API.
//
// DBnomics returns observations as two parallel arrays, and the value array has
// mixed types: real numbers plus the string "NA" for periods with no data. Those
// are dropped rather than coerced, so a missing year never becomes a zero balance
// sheet.
//
// Periods arrive as "YYYY", "YYYY-MM" or "YYYY-MM-DD". The first two are stock
// figures for the end of the period they name, so they map to the last day of that
// year or month — dating an annual balance sheet to 1 January would shift it
// twelve months earlier than it belongs.
func ParseDBnomicsSeries(data []byte) (Series, error) {
	var payload struct {
		Series struct {
			Docs []struct {
				Period []string          `json:"period"`
				Value  []json.RawMessage `json:"value"`
			} `json:"docs"`
		} `json:"series"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Series{}, fmt.Errorf("decoding dbnomics response: %w", err)
	}
	if len(payload.Series.Docs) == 0 {
		return Series{}, errors.New("dbnomics response contained no series")
	}

	doc := payload.Series.Docs[0]
	if len(doc.Period) != len(doc.Value) {
		return Series{}, fmt.Errorf(
			"dbnomics returned %d periods for %d values", len(doc.Period), len(doc.Value))
	}

	series := Series{Values: make(map[time.Time]float64, len(doc.Period))}
	for i, period := range doc.Period {
		var value float64
		if err := json.Unmarshal(doc.Value[i], &value); err != nil {
			continue // "NA" or any other non-numeric marker: no observation
		}

		date, err := parseDBnomicsPeriod(period)
		if err != nil {
			return Series{}, err
		}

		series.Values[date] = value
		if date.After(series.Last) {
			series.Last = date
		}
	}

	if len(series.Values) == 0 {
		return Series{}, errors.New("dbnomics series contained no usable observations")
	}
	return series, nil
}

func parseDBnomicsPeriod(period string) (time.Time, error) {
	switch len(period) {
	case len("YYYY"):
		year, err := strconv.Atoi(period)
		if err != nil {
			return time.Time{}, fmt.Errorf("bad annual period %q: %w", period, err)
		}
		return time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC), nil

	case len("YYYY-MM"):
		month, err := time.ParseInLocation("2006-01", period, time.UTC)
		if err != nil {
			return time.Time{}, fmt.Errorf("bad monthly period %q: %w", period, err)
		}
		return month.AddDate(0, 1, -1), nil

	case len("YYYY-MM-DD"):
		return time.ParseInLocation(time.DateOnly, period, time.UTC)
	}
	return time.Time{}, fmt.Errorf("unrecognised dbnomics period %q", period)
}
