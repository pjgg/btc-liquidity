package pipeline

import (
	"math"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func closeTo(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("got %.4f, want %.4f", got, want)
	}
}

// A minimal but complete set of inputs: one observation per series.
func baseInputs() Inputs {
	return Inputs{
		WALCL:     map[time.Time]float64{day(2026, time.August, 5): 6_759_955},
		WTREGEN:   map[time.Time]float64{day(2026, time.August, 5): 963_950},
		RRP:       map[time.Time]float64{day(2026, time.August, 5): 0.725},
		ECBAssets: map[time.Time]float64{day(2026, time.August, 5): 5_923_023},
		BoJAssets: map[time.Time]float64{day(2026, time.August, 1): 6_442_957},
		USDPerEUR: map[time.Time]float64{day(2026, time.August, 5): 1.1559},
		YenPerUSD: map[time.Time]float64{day(2026, time.August, 1): 157.54},

		Reserves:       map[time.Time]float64{day(2026, time.August, 5): 2_944_059},
		Repo:           map[time.Time]float64{day(2026, time.August, 5): 0.002},
		DiscountWindow: map[time.Time]float64{day(2026, time.August, 5): 5_644},
	}
}

// RRPONTSYD and RPONTSYD differ by one letter and mean opposite things: the
// reverse repo drains cash from the system, the repo injects it. Crossing them
// would flip the sign of the headline series while still producing a plausible
// looking chart.
func TestRepoAndReverseRepoAreNotCrossed(t *testing.T) {
	inputs := baseInputs()
	inputs.RRP = map[time.Time]float64{day(2026, time.August, 5): 0.725}  // 725 MUSD
	inputs.Repo = map[time.Time]float64{day(2026, time.August, 5): 0.002} //   2 MUSD

	rows := Build(inputs, day(2026, time.August, 5), day(2026, time.August, 5))
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	closeTo(t, rows[0].RRPMUSD, 725.0)
	closeTo(t, rows[0].RepoMUSD, 2.0)

	// Only the reverse repo is subtracted from net liquidity. If the two were
	// swapped this would come out 723 MUSD higher.
	closeTo(t, rows[0].FedNetLiqMUSD, 6_759_955.0-963_950.0-725.0)
}

func TestDiagnosticsAreCarriedThrough(t *testing.T) {
	rows := Build(baseInputs(), day(2026, time.August, 5), day(2026, time.August, 5))
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	closeTo(t, rows[0].ReservesMUSD, 2_944_059.0)
	closeTo(t, rows[0].DiscountWindowMUSD, 5_644.0)

	// Reserves are published in millions already and must not be rescaled.
	if rows[0].ReservesMUSD > 1e7 {
		t.Errorf("reserves look rescaled by 1000: %.0f", rows[0].ReservesMUSD)
	}
}

func TestBuildProducesDerivedSeries(t *testing.T) {
	rows := Build(baseInputs(), day(2026, time.August, 5), day(2026, time.August, 5))
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	row := rows[0]
	closeTo(t, row.RRPMUSD, 725.0)
	closeTo(t, row.ECBAssetsMUSD, 6_846_422.2857)
	closeTo(t, row.BoJAssetsMUSD, 4_089_727.6882)
	closeTo(t, row.FedNetLiqMUSD, 5_795_280.0)
	closeTo(t, row.GlobalCBMUSD, 17_696_104.9739)
}

// The spec requires each balance sheet to be converted at the FX rate of its
// own observation date. Converting a six-week-old BoJ figure at today's rate
// would fabricate a jump in the global aggregate whenever the yen moves.
func TestBalanceSheetsUseFXOfTheirOwnObservationDate(t *testing.T) {
	inputs := baseInputs()
	// The yen collapses after the BoJ's observation date.
	inputs.YenPerUSD[day(2026, time.August, 5)] = 315.08 // twice as many yen per dollar

	rows := Build(inputs, day(2026, time.August, 5), day(2026, time.August, 5))
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	// The BoJ figure was observed on 08-01 at 157.54 and must stay converted at
	// that rate. Using the 08-05 rate would roughly halve it.
	closeTo(t, rows[0].BoJAssetsMUSD, 4_089_727.6882)
}

// Liquidity series are stock variables: between publications the last value
// stands, so every day in the range gets a row.
func TestBuildForwardFillsAcrossGaps(t *testing.T) {
	rows := Build(baseInputs(), day(2026, time.August, 5), day(2026, time.August, 9))
	if len(rows) != 5 {
		t.Fatalf("expected 5 daily rows, got %d", len(rows))
	}
	for _, row := range rows {
		closeTo(t, row.FedNetLiqMUSD, 5_795_280.0)
	}
	if !rows[0].Date.Equal(day(2026, time.August, 5)) {
		t.Errorf("first row is %v, want 2026-08-05", rows[0].Date)
	}
	if !rows[4].Date.Equal(day(2026, time.August, 9)) {
		t.Errorf("last row is %v, want 2026-08-09", rows[4].Date)
	}
}

// A day missing any component yields no row at all, rather than a row with a
// zero standing in for the missing input.
func TestBuildSkipsDaysWithAnyMissingComponent(t *testing.T) {
	inputs := baseInputs()
	inputs.ECBAssets = map[time.Time]float64{day(2026, time.August, 8): 5_923_023}
	inputs.USDPerEUR = map[time.Time]float64{day(2026, time.August, 8): 1.1559}

	rows := Build(inputs, day(2026, time.August, 5), day(2026, time.August, 9))

	// Rows only start once the ECB series has its first observation.
	if len(rows) != 2 {
		t.Fatalf("expected rows only from 08-08, got %d", len(rows))
	}
	if !rows[0].Date.Equal(day(2026, time.August, 8)) {
		t.Errorf("first row is %v, want 2026-08-08", rows[0].Date)
	}
}

// An FX rate missing on the balance-sheet date (a bank holiday) must not
// convert at zero.
func TestBuildSkipsBalanceSheetWithoutAnFXRate(t *testing.T) {
	inputs := baseInputs()
	inputs.USDPerEUR = map[time.Time]float64{} // holiday: FRED published nothing

	rows := Build(inputs, day(2026, time.August, 5), day(2026, time.August, 5))
	if len(rows) != 0 {
		t.Fatalf("a missing FX rate must drop the row, not convert at zero; got %d rows with ECB=%.2f",
			len(rows), rows[0].ECBAssetsMUSD)
	}
}

func TestBuildEmptyInputsGivesNoRows(t *testing.T) {
	rows := Build(Inputs{}, day(2026, time.August, 1), day(2026, time.August, 10))
	if len(rows) != 0 {
		t.Errorf("expected no rows from empty inputs, got %d", len(rows))
	}
}

func TestBuildReturnsRowsInDateOrder(t *testing.T) {
	rows := Build(baseInputs(), day(2026, time.August, 5), day(2026, time.August, 12))
	for i := 1; i < len(rows); i++ {
		if !rows[i].Date.After(rows[i-1].Date) {
			t.Fatalf("rows out of order at %d: %v then %v", i, rows[i-1].Date, rows[i].Date)
		}
	}
}
