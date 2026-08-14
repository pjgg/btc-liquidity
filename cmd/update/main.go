// Command update refreshes the CSV files that back the chart.
//
// It is run by the scheduled GitHub Action, which commits whatever changes.
// Both feeds are keyless, so no secrets are configured anywhere.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pjgg/btc-liquidity/internal/liquidity"
	"github.com/pjgg/btc-liquidity/internal/pipeline"
	"github.com/pjgg/btc-liquidity/internal/sources"
	"github.com/pjgg/btc-liquidity/internal/store"
)

const btcSymbol = "BTCUSDT"

// The PBoC balance sheet, mirrored from China's National Bureau of Statistics.
// FRED carries no PBoC series and every international M2 series there is
// discontinued, so this is the only keyless route to it. Annual, in hundreds of
// millions of yuan, and published well after the year it covers.
const (
	pbocProvider = "NBS"
	pbocDataset  = "A_A0L05"
	pbocSeries   = "A0L0501"
)

var (
	btcHeader       = []string{"date", "close_usd"}
	liquidityHeader = []string{
		"date",
		"walcl_musd", "wtregen_musd", "rrp_musd", "ecb_assets_musd", "boj_assets_musd",
		"pboc_assets_musd",
		"reserves_musd", "repo_musd", "discount_window_musd",
		"fed_net_liq_musd", "global_cb_musd", "global_cb_fxnow_musd",
	}
)

// fredSeries lists every series to pull, with the cadence its freshness is
// judged against.
//
// The FX series carry daily observations but FRED releases them in weekly
// batches, so they are checked on the weekly threshold. Judging them as daily
// would raise a false alarm every time the run landed late in a release week.
var fredSeries = []struct{ id, frequency string }{
	{"WALCL", "weekly"},
	{"WTREGEN", "weekly"},
	{"RRPONTSYD", "daily"},
	{"ECBASSETSW", "weekly"},
	{"JPNASSETS", "monthly"},
	{"DEXUSEU", "weekly"},
	{"DEXJPUS", "weekly"},
	{"DEXCHUS", "weekly"},

	// Whether liquidity is being injected or drained. RPONTSYD is the repo the
	// Fed lends into the system; RRPONTSYD above is its opposite. One letter
	// apart, opposite signs.
	{"WRESBAL", "weekly"},
	{"RPONTSYD", "daily"},
	{"WLCFLPCL", "weekly"},
}

type metadata struct {
	GeneratedAtUTC  string            `json:"generated_at_utc"`
	Rows            map[string]int    `json:"rows"`
	LastObservation map[string]string `json:"last_observation"`
	Notes           []string          `json:"notes"`
}

func main() {
	dataDir := flag.String("data-dir", "data", "directory holding the CSV files")
	startFlag := flag.String("start", "2018-01-01", "first date to backfill (YYYY-MM-DD)")
	flag.Parse()

	if err := run(*dataDir, *startFlag); err != nil {
		log.Fatalf("update failed: %v", err)
	}
}

func run(dataDir, startFlag string) error {
	start, err := time.ParseInLocation(time.DateOnly, startFlag, time.UTC)
	if err != nil {
		return fmt.Errorf("bad --start %q: %w", startFlag, err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := sources.NewClient()

	fetched := make(map[string]sources.Series, len(fredSeries))
	lastObservation := make(map[string]string, len(fredSeries)+1)
	for _, series := range fredSeries {
		s, err := client.FetchFRED(ctx, series.id)
		if err != nil {
			return err
		}
		if err := liquidity.CheckFreshness(series.id, s.Last, series.frequency, today); err != nil {
			return err
		}
		fetched[series.id] = s
		lastObservation[series.id] = s.Last.Format(time.DateOnly)
		log.Printf("%-11s %5d observations, last %s", series.id, len(s.Values), lastObservation[series.id])
	}

	pboc, err := client.FetchDBnomics(ctx, pbocProvider, pbocDataset, pbocSeries)
	if err != nil {
		return err
	}
	if err := liquidity.CheckFreshness("PBOC_ASSETS", pboc.Last, "annual", today); err != nil {
		return err
	}
	lastObservation["PBOC_ASSETS"] = pboc.Last.Format(time.DateOnly)
	log.Printf("%-11s %5d observations, last %s (anual)", "PBOC", len(pboc.Values), lastObservation["PBOC_ASSETS"])

	closes, err := client.FetchBinanceDailyCloses(ctx, btcSymbol, start)
	if err != nil {
		return err
	}
	if len(closes) == 0 {
		return fmt.Errorf("binance returned no candles for %s since %s", btcSymbol, startFlag)
	}
	log.Printf("%-11s %5d daily closes", btcSymbol, len(closes))

	rows := pipeline.Build(pipeline.Inputs{
		WALCL:      fetched["WALCL"].Values,
		WTREGEN:    fetched["WTREGEN"].Values,
		RRP:        fetched["RRPONTSYD"].Values,
		ECBAssets:  fetched["ECBASSETSW"].Values,
		BoJAssets:  fetched["JPNASSETS"].Values,
		PBoCAssets: pboc.Values,
		USDPerEUR:  fetched["DEXUSEU"].Values,
		YenPerUSD:  fetched["DEXJPUS"].Values,
		YuanPerUSD: fetched["DEXCHUS"].Values,

		Reserves:       fetched["WRESBAL"].Values,
		Repo:           fetched["RPONTSYD"].Values,
		DiscountWindow: fetched["WLCFLPCL"].Values,
	}, start, today)
	if len(rows) == 0 {
		return fmt.Errorf("no liquidity rows built between %s and %s", startFlag, today.Format(time.DateOnly))
	}

	btcPath := filepath.Join(dataDir, "btc.csv")
	btcTable, err := store.Load(btcPath, btcHeader)
	if err != nil {
		return err
	}
	for date, close := range closes {
		btcTable.Upsert(date.Format(time.DateOnly), []string{money(close)})
	}
	if err := btcTable.Save(btcPath); err != nil {
		return err
	}

	liquidityPath := filepath.Join(dataDir, "liquidity.csv")
	liquidityTable, err := store.Load(liquidityPath, liquidityHeader)
	if err != nil {
		return err
	}
	for _, row := range rows {
		liquidityTable.Upsert(row.Date.Format(time.DateOnly), []string{
			money(row.WALCLMUSD),
			money(row.WTREGENMUSD),
			money(row.RRPMUSD),
			money(row.ECBAssetsMUSD),
			money(row.BoJAssetsMUSD),
			money(row.PBoCAssetsMUSD),
			money(row.ReservesMUSD),
			money(row.RepoMUSD),
			money(row.DiscountWindowMUSD),
			money(row.FedNetLiqMUSD),
			money(row.GlobalCBMUSD),
			money(row.GlobalCBFXNowMUSD),
		})
	}
	if err := liquidityTable.Save(liquidityPath); err != nil {
		return err
	}

	lastObservation["BTCUSDT"] = btcTable.LastDate()
	meta := metadata{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Rows: map[string]int{
			"btc":       btcTable.Len(),
			"liquidity": liquidityTable.Len(),
		},
		LastObservation: lastObservation,
		Notes: []string{
			"All liquidity figures are in millions of USD.",
			"global_cb_musd = Fed + ECB + BoJ + PBoC, all converted to millions of USD.",
			"The PBoC component is annual and lags by over a year, so it holds flat between steps.",
			"global_cb_fxnow_musd is the same aggregate converted at the latest FX rate throughout, which removes currency movement from the series.",
			"Liquidity series are forward-filled between publications; they are stock variables.",
		},
	}
	if err := writeJSON(filepath.Join(dataDir, "meta.json"), meta); err != nil {
		return err
	}

	log.Printf("wrote %d BTC rows and %d liquidity rows to %s", btcTable.Len(), liquidityTable.Len(), dataDir)
	return nil
}

// money formats to two decimals, which is enough for both a BTC price and a
// figure in millions of dollars, and keeps the daily diff small.
func money(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}
