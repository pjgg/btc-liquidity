package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewClient()
	client.HTTP = server.Client()
	client.FREDBaseURL = server.URL
	client.BinanceBaseURL = server.URL
	return client, server
}

func TestFetchFRED(t *testing.T) {
	var gotQuery string
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		fmt.Fprint(w, "observation_date,WALCL\n2026-08-12,6759955\n")
	})

	series, err := client.FetchFRED(context.Background(), "WALCL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotQuery != "id=WALCL" {
		t.Errorf("query = %q, want id=WALCL", gotQuery)
	}
	if got := series.Values[day(2026, time.August, 12)]; got != 6_759_955 {
		t.Errorf("value = %v, want 6759955", got)
	}
}

func TestFetchFREDPropagatesHTTPError(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	})

	_, err := client.FetchFRED(context.Background(), "DEADSERIES")
	if err == nil {
		t.Fatal("expected an error on HTTP 404")
	}
	if !strings.Contains(err.Error(), "DEADSERIES") {
		t.Errorf("error should name the series, got %v", err)
	}
}

// Binance caps a page at 1000 candles, so a backfill spans several requests.
func TestFetchBinancePaginates(t *testing.T) {
	start := day(2026, time.August, 1)

	var requests int
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch requests {
		case 1:
			fmt.Fprint(w, klinesJSON(start, 3))
		case 2:
			fmt.Fprint(w, klinesJSON(start.AddDate(0, 0, 3), 2))
		default:
			fmt.Fprint(w, "[]") // exhausted
		}
	})

	closes, err := client.FetchBinanceDailyCloses(context.Background(), "BTCUSDT", start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(closes) != 5 {
		t.Fatalf("expected 5 candles across pages, got %d", len(closes))
	}
	if requests != 3 {
		t.Errorf("expected 3 requests (2 pages + 1 empty), got %d", requests)
	}
	if _, ok := closes[start.AddDate(0, 0, 4)]; !ok {
		t.Error("the last candle of the second page is missing")
	}
}

// An upstream that keeps replaying the same window must stop the loop rather
// than spin until the pagination cap.
func TestFetchBinanceStopsWhenCursorDoesNotAdvance(t *testing.T) {
	start := day(2026, time.August, 1)

	var requests int
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, klinesJSON(start, 1)) // always the same single candle
	})

	closes, err := client.FetchBinanceDailyCloses(context.Background(), "BTCUSDT", start)
	if err != nil {
		t.Fatalf("a stalled cursor should end the walk, not error: %v", err)
	}
	if len(closes) != 1 {
		t.Errorf("expected the single candle, got %d", len(closes))
	}
	if requests > 2 {
		t.Errorf("expected the loop to stop after detecting no progress, made %d requests", requests)
	}
}

func TestFetchBinanceEmptyFirstPage(t *testing.T) {
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "[]")
	})

	closes, err := client.FetchBinanceDailyCloses(context.Background(), "BTCUSDT", day(2026, time.August, 1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(closes) != 0 {
		t.Errorf("expected no candles, got %d", len(closes))
	}
}

// klinesJSON builds n consecutive daily candles starting at from, in the shape
// Binance returns: open time as a bare number, prices as strings.
func klinesJSON(from time.Time, n int) string {
	rows := make([]string, 0, n)
	for i := range n {
		date := from.AddDate(0, 0, i)
		rows = append(rows, fmt.Sprintf(
			`[%d,"1","2","3","%d.5","4",%d,"5",6,"7","8","0"]`,
			date.UnixMilli(), 60000+i, date.AddDate(0, 0, 1).UnixMilli()-1,
		))
	}
	return "[" + strings.Join(rows, ",") + "]"
}
