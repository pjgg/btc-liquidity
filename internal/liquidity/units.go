// Package liquidity normalises central-bank source data and derives the two
// liquidity series this project plots.
//
// Every series consumed here is published in a different unit. Everything is
// converted to millions of US dollars in this package, once, before any
// arithmetic happens elsewhere. Nothing downstream should ever see a raw value.
//
// Units as published (verified against FRED on 2026-08-14):
//
//	WALCL       Millions of U.S. Dollars
//	WTREGEN     Millions of U.S. Dollars
//	RRPONTSYD   Billions of U.S. Dollars
//	ECBASSETSW  Millions of Euros
//	JPNASSETS   100 Million Yen
//	DEXUSEU     U.S. Dollars to One Euro
//	DEXJPUS     Japanese Yen to One U.S. Dollar
package liquidity

import (
	"errors"
	"fmt"
	"time"
)

// ErrStaleSeries reports a source that stopped updating. Callers check for it
// with errors.Is to distinguish a dead upstream from a programming mistake.
var ErrStaleSeries = errors.New("source series is stale")

// stalenessLimitDays bounds how long each publication cadence may stay quiet.
// A single global threshold cannot work: it would either miss a dead weekly
// series or false-alarm on a healthy monthly one.
var stalenessLimitDays = map[string]int{
	"daily":   10,
	"weekly":  30,
	"monthly": 120,
}

// BillionsToMUSD rescales a series published in billions.
//
// Both repo series need it, and they are opposites that differ by one letter:
// RRPONTSYD (reverse repo, securities *sold* by the Fed) drains cash, while
// RPONTSYD (repo, securities *purchased*) injects it. Naming this after either
// one would invite wiring the wrong series to the wrong sign.
func BillionsToMUSD(billionsUSD float64) float64 {
	return billionsUSD * 1000.0
}

// ECBToMUSD converts the ECB balance sheet from millions of euros to millions
// of dollars. DEXUSEU quotes dollars per euro, so the rate multiplies.
func ECBToMUSD(millionsEUR, usdPerEUR float64) float64 {
	return millionsEUR * usdPerEUR
}

// BoJToMUSD converts the Bank of Japan balance sheet from hundreds of millions
// of yen to millions of dollars.
//
// DEXJPUS quotes yen per dollar, so the rate divides. This runs opposite to the
// euro conversion and is the easiest of the five to invert by accident.
func BoJToMUSD(hundredMillionYen, yenPerUSD float64) float64 {
	return hundredMillionYen * 100.0 / yenPerUSD
}

// FedNetLiquidity is the Fed balance sheet less the two large drains on it:
// the Treasury General Account and the reverse repo facility.
func FedNetLiquidity(walclMUSD, wtregenMUSD, rrpMUSD float64) float64 {
	return walclMUSD - wtregenMUSD - rrpMUSD
}

// GlobalCBBalance sums the Fed, ECB and BoJ balance sheets in millions of USD.
//
// China is absent: no PBoC series is published on FRED, and every international
// M2 series tested during design was discontinued. The page states this.
func GlobalCBBalance(walclMUSD, ecbAssetsMUSD, bojAssetsMUSD float64) float64 {
	return walclMUSD + ecbAssetsMUSD + bojAssetsMUSD
}

// CheckFreshness fails when a series has gone quiet for longer than its own
// cadence allows, rather than letting ForwardFill carry a dead value forever.
func CheckFreshness(seriesID string, lastObservation time.Time, frequency string, today time.Time) error {
	limit, ok := stalenessLimitDays[frequency]
	if !ok {
		return fmt.Errorf("unknown frequency %q for %s", frequency, seriesID)
	}

	age := int(today.Sub(lastObservation).Hours() / 24)
	if age > limit {
		return fmt.Errorf(
			"%w: %s last updated %s (%d days ago), exceeding the %d-day limit for a %s series; it may have been discontinued",
			ErrStaleSeries, seriesID, lastObservation.Format(time.DateOnly), age, limit, frequency,
		)
	}
	return nil
}

// ForwardFill expands sparse observations onto a continuous daily index.
//
// Liquidity series are stock variables — a balance sheet holds its value between
// publications — so gaps carry the last value forward. Interpolating would
// invent intermediate readings that were never observed.
//
// An observation from before start seeds the window: a balance sheet published
// last month still holds its value today, so the most recent observation at or
// before start is carried in. Without this, a monthly series would blank the
// first weeks of every range.
//
// Dates before the first observation of all are omitted rather than back-filled.
func ForwardFill(observations map[time.Time]float64, start, end time.Time) map[time.Time]float64 {
	filled := make(map[time.Time]float64)
	if len(observations) == 0 {
		return filled
	}

	var current float64
	var seedDate time.Time
	seen := false
	for d, v := range observations {
		if d.After(start) {
			continue
		}
		if !seen || d.After(seedDate) {
			current, seedDate, seen = v, d, true
		}
	}

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if v, ok := observations[d]; ok {
			current, seen = v, true
		}
		if seen {
			filled[d] = current
		}
	}
	return filled
}
