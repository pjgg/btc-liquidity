package liquidity

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// Every FRED series arrives in a different unit. Getting one of these wrong
// produces a chart that looks plausible and is entirely false, so each
// conversion is pinned to a real observation taken from FRED on 2026-08-14.

func closeTo(t *testing.T, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("got %.4f, want %.4f (tolerance %.4f)", got, want, tol)
	}
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestBillionsToMUSD(t *testing.T) {
	// RRPONTSYD is published in billions; 0.450 billion == 450 million.
	closeTo(t, BillionsToMUSD(0.450), 450.0, 1e-9)
	closeTo(t, BillionsToMUSD(0.0), 0.0, 1e-9)
}

func TestECBToMUSD(t *testing.T) {
	// ECBASSETSW 2026-08-07 = 5_923_023 millions EUR, DEXUSEU = 1.1559 USD/EUR.
	closeTo(t, ECBToMUSD(5_923_023.0, 1.1559), 6_846_422.2857, 0.01)
}

func TestHundredMillionLocalToMUSD(t *testing.T) {
	// JPNASSETS 2026-07-01 = 6_442_957 (100M yen) => 644.3 trillion yen.
	// At DEXJPUS = 157.54 yen/USD that is ~4.09 trillion USD.
	got := HundredMillionLocalToMUSD(6_442_957.0, 157.54)
	closeTo(t, got, 4_089_727.6882, 0.01)

	// Sanity: the BoJ balance sheet is trillions of dollars, not billions.
	if got < 3_000_000 || got > 6_000_000 {
		t.Errorf("BoJ balance sheet %.0f MUSD is outside a plausible range", got)
	}
}

func TestECBConversionUsesRateNotReciprocal(t *testing.T) {
	// DEXUSEU is USD per EUR, so a euro worth more dollars means more dollars.
	weak := ECBToMUSD(1000.0, 1.05)
	strong := ECBToMUSD(1000.0, 1.20)
	if strong <= weak {
		t.Errorf("a stronger euro must convert to more dollars: %.2f vs %.2f", strong, weak)
	}
}

func TestHundredMillionConversionUsesReciprocal(t *testing.T) {
	// DEXJPUS is YEN per USD, so a weaker yen (more yen per dollar) means fewer
	// dollars. This runs opposite to the euro conversion and is the easiest of
	// the five to invert by accident.
	strongYen := HundredMillionLocalToMUSD(1000.0, 100.0)
	weakYen := HundredMillionLocalToMUSD(1000.0, 200.0)
	if weakYen >= strongYen {
		t.Errorf("a weaker yen must convert to fewer dollars: %.2f vs %.2f", weakYen, strongYen)
	}
}

func TestFedNetLiquidity(t *testing.T) {
	// Real observation, 2026-08-12: WALCL 6_759_955, WTREGEN 963_950,
	// RRPONTSYD 0.725bn -> 725 MUSD.
	closeTo(t, FedNetLiquidity(6_759_955.0, 963_950.0, 725.0), 5_795_280.0, 1e-6)
}

func TestFedNetLiquidityFallsWhenTGARises(t *testing.T) {
	base := FedNetLiquidity(6_000_000.0, 500_000.0, 1000.0)
	drained := FedNetLiquidity(6_000_000.0, 900_000.0, 1000.0)
	if drained >= base {
		t.Errorf("a larger TGA must drain net liquidity: %.2f vs %.2f", drained, base)
	}
}

func TestGlobalCBBalance(t *testing.T) {
	walcl := 6_759_955.0
	// PBoC 2024 = 440_513.312973013 (100M yuan) at DEXCHUS 6.7474 -> 6_528_614 MUSD.
	got := GlobalCBBalance(walcl, 6_846_422.2857, 4_089_727.6882, 6528637.8898)
	closeTo(t, got, 24224742.8637, 0.01)

	// The published figure these charts quote is around 24 trillion USD; without
	// China this came out near 17.7 and was not comparable.
	if got < 20e6 || got > 28e6 {
		t.Errorf("global aggregate %.0f MUSD is outside the expected 20-28 trillion band", got)
	}

	if got <= walcl {
		t.Errorf("the global aggregate must exceed the Fed alone: %.2f vs %.2f", got, walcl)
	}
}

// A discontinued series must fail loudly rather than forward-fill forever. This
// is the check that would have caught the dead M2 series during design.
func TestCheckFreshness(t *testing.T) {
	today := day(2026, time.August, 14)

	tests := []struct {
		name      string
		seriesID  string
		last      time.Time
		frequency string
		wantErr   bool
	}{
		{"fresh daily", "RRPONTSYD", day(2026, time.August, 13), "daily", false},
		{"stale daily", "RRPONTSYD", day(2026, time.July, 1), "daily", true},
		{"fresh weekly", "ECBASSETSW", day(2026, time.August, 7), "weekly", false},
		// BoJ is 44 days stale here and that is entirely normal for monthly data.
		{"fresh monthly", "JPNASSETS", day(2026, time.July, 1), "monthly", false},
		// The same 44 days would breach the weekly limit — thresholds must differ.
		{"44d under weekly limit", "JPNASSETS", day(2026, time.July, 1), "weekly", true},
		// MYAGM2EZM196N really did stop in 2017.
		{"discontinued", "MYAGM2EZM196N", day(2017, time.March, 1), "monthly", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckFreshness(tc.seriesID, tc.last, tc.frequency, today)
			if tc.wantErr && err == nil {
				t.Fatalf("expected a staleness error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestCheckFreshnessErrorNamesSeriesAndDate(t *testing.T) {
	err := CheckFreshness("DEADSERIES", day(2020, time.January, 1), "monthly", day(2026, time.August, 14))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrStaleSeries) {
		t.Errorf("error must wrap ErrStaleSeries, got %v", err)
	}
	for _, want := range []string{"DEADSERIES", "2020-01-01"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message %q must mention %q", err.Error(), want)
		}
	}
}

func TestCheckFreshnessRejectsUnknownFrequency(t *testing.T) {
	err := CheckFreshness("X", day(2026, time.August, 13), "hourly", day(2026, time.August, 14))
	if err == nil {
		t.Fatal("expected an error for an unknown frequency")
	}
	if errors.Is(err, ErrStaleSeries) {
		t.Error("an unknown frequency is a programming error, not a staleness error")
	}
}

// Liquidity series are stock variables, so gaps carry the last value forward.
func TestForwardFill(t *testing.T) {
	observations := map[time.Time]float64{
		day(2026, time.August, 1): 100.0,
		day(2026, time.August, 4): 200.0,
	}
	filled := ForwardFill(observations, day(2026, time.August, 1), day(2026, time.August, 5))

	want := map[int]float64{1: 100.0, 2: 100.0, 3: 100.0, 4: 200.0, 5: 200.0}
	for d, w := range want {
		got, ok := filled[day(2026, time.August, d)]
		if !ok {
			t.Fatalf("2026-08-%02d missing from the filled series", d)
		}
		closeTo(t, got, w, 1e-9)
	}
}

func TestForwardFillDoesNotInterpolate(t *testing.T) {
	observations := map[time.Time]float64{
		day(2026, time.August, 1): 100.0,
		day(2026, time.August, 3): 300.0,
	}
	filled := ForwardFill(observations, day(2026, time.August, 1), day(2026, time.August, 3))

	// Interpolation would give 200.0 here, inventing a reading never observed.
	closeTo(t, filled[day(2026, time.August, 2)], 100.0, 1e-9)
}

func TestForwardFillLeavesNoGaps(t *testing.T) {
	observations := map[time.Time]float64{day(2026, time.August, 1): 1.0}
	filled := ForwardFill(observations, day(2026, time.August, 1), day(2026, time.August, 10))
	if len(filled) != 10 {
		t.Errorf("expected 10 daily entries, got %d", len(filled))
	}
}

func TestForwardFillOmitsDatesBeforeFirstObservation(t *testing.T) {
	observations := map[time.Time]float64{day(2026, time.August, 5): 1.0}
	filled := ForwardFill(observations, day(2026, time.August, 1), day(2026, time.August, 6))

	if _, ok := filled[day(2026, time.August, 1)]; ok {
		t.Error("dates before the first observation must not be back-filled")
	}
	closeTo(t, filled[day(2026, time.August, 6)], 1.0, 1e-9)
}

// A stock variable observed before the window still holds its value inside it.
// The BoJ publishes monthly, so a window starting mid-month has its most recent
// observation behind the start date — dropping it would blank the first weeks
// of every range.
func TestForwardFillSeedsFromObservationBeforeStart(t *testing.T) {
	observations := map[time.Time]float64{day(2026, time.August, 1): 4_089_727.0}
	filled := ForwardFill(observations, day(2026, time.August, 5), day(2026, time.August, 7))

	if len(filled) != 3 {
		t.Fatalf("expected 3 days carried from the earlier observation, got %d", len(filled))
	}
	closeTo(t, filled[day(2026, time.August, 5)], 4_089_727.0, 1e-9)
	closeTo(t, filled[day(2026, time.August, 7)], 4_089_727.0, 1e-9)
}

func TestForwardFillSeedsFromTheLatestObservationBeforeStart(t *testing.T) {
	observations := map[time.Time]float64{
		day(2026, time.July, 1):   100.0,
		day(2026, time.August, 1): 200.0, // the one that should win
	}
	filled := ForwardFill(observations, day(2026, time.August, 5), day(2026, time.August, 5))
	closeTo(t, filled[day(2026, time.August, 5)], 200.0, 1e-9)
}

func TestForwardFillEmptyInput(t *testing.T) {
	filled := ForwardFill(map[time.Time]float64{}, day(2026, time.August, 1), day(2026, time.August, 5))
	if len(filled) != 0 {
		t.Errorf("empty input must give empty output, got %d entries", len(filled))
	}
}
