package sources

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	DefaultFREDBaseURL    = "https://fred.stlouisfed.org/graph/fredgraph.csv"
	DefaultBinanceBaseURL = "https://api.binance.com/api/v3/klines"

	// Binance caps a klines page at 1000 candles.
	binancePageLimit = 1000

	// A page loop over daily candles needs a handful of iterations. This only
	// exists so a misbehaving upstream cannot spin forever inside the Action.
	maxBinancePages = 64

	// Guards against an upstream that starts streaming something unbounded.
	maxResponseBytes = 32 << 20
)

// Client fetches the upstream feeds. Both base URLs are fields so tests can
// point them at a local server.
type Client struct {
	HTTP           *http.Client
	FREDBaseURL    string
	BinanceBaseURL string
}

func NewClient() *Client {
	return &Client{
		HTTP:           &http.Client{Timeout: 60 * time.Second},
		FREDBaseURL:    DefaultFREDBaseURL,
		BinanceBaseURL: DefaultBinanceBaseURL,
	}
}

// FetchFRED downloads one series from the keyless fredgraph.csv endpoint.
func (c *Client) FetchFRED(ctx context.Context, seriesID string) (Series, error) {
	endpoint := fmt.Sprintf("%s?id=%s", c.FREDBaseURL, url.QueryEscape(seriesID))

	body, err := c.get(ctx, endpoint)
	if err != nil {
		return Series{}, fmt.Errorf("fetching FRED series %s: %w", seriesID, err)
	}

	series, err := ParseFREDCSV(bytes.NewReader(body))
	if err != nil {
		return Series{}, fmt.Errorf("parsing FRED series %s: %w", seriesID, err)
	}
	return series, nil
}

// FetchBinanceDailyCloses walks forward from start in pages of 1000 candles.
//
// Binance returns at most 1000 rows per call, so a multi-year backfill needs
// several requests. The loop advances past the newest candle it received and
// stops as soon as a page fails to move the cursor forward, which covers both
// an empty page and an upstream that keeps replaying the same window.
func (c *Client) FetchBinanceDailyCloses(ctx context.Context, symbol string, start time.Time) (map[time.Time]float64, error) {
	closes := make(map[time.Time]float64)
	cursor := start.UTC().Truncate(24 * time.Hour)

	for page := 0; page < maxBinancePages; page++ {
		endpoint := fmt.Sprintf(
			"%s?symbol=%s&interval=1d&limit=%d&startTime=%d",
			c.BinanceBaseURL, url.QueryEscape(symbol), binancePageLimit, cursor.UnixMilli(),
		)

		body, err := c.get(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("fetching binance klines from %s: %w", cursor.Format(time.DateOnly), err)
		}

		page, err := ParseBinanceKlines(body)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return closes, nil
		}

		newest := cursor
		for date, close := range page {
			closes[date] = close
			if date.After(newest) {
				newest = date
			}
		}

		if !newest.After(cursor) {
			return closes, nil // no forward progress; stop rather than loop
		}
		cursor = newest.AddDate(0, 0, 1)

		if cursor.After(time.Now().UTC()) {
			return closes, nil
		}
	}

	return nil, fmt.Errorf("binance pagination exceeded %d pages", maxBinancePages)
}

func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		preview, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		return nil, fmt.Errorf("HTTP %d from %s: %s", response.StatusCode, endpoint, preview)
	}

	return io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
}
