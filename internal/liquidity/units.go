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
//	NBS PBoC    100 Million Yuan (anual)
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
	// The PBoC balance sheet is annual and published well after the year it
	// covers, so a year and a half of age is normal rather than a fault.
	"annual": 800,
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

// HundredMillionLocalToMUSD converts a balance sheet published in hundreds of
// millions of local currency into millions of dollars.
//
// Both the Bank of Japan (100 million yen) and the People's Bank of China
// (100 million yuan) publish this way, and DEXJPUS and DEXCHUS both quote local
// currency per dollar, so the rate divides. That runs opposite to the euro
// conversion, where the rate multiplies, and is the easiest of these to invert
// by accident.
func HundredMillionLocalToMUSD(hundredMillionLocal, localPerUSD float64) float64 {
	return hundredMillionLocal * 100.0 / localPerUSD
}

// FedNetLiquidity is the Fed balance sheet less the two large drains on it:
// the Treasury General Account and the reverse repo facility.
func FedNetLiquidity(walclMUSD, wtregenMUSD, rrpMUSD float64) float64 {
	return walclMUSD - wtregenMUSD - rrpMUSD
}

// GlobalCBBalance sums the Fed, ECB, BoJ and PBoC balance sheets in millions of
// USD.
//
// The PBoC figure is annual and lags by over a year, which is why it was left out
// at first. It is in now because without China the aggregate is not comparable to
// the ~24 trillion figure these charts usually quote — China alone is around a
// quarter of it. Its own contribution therefore moves once a year and holds flat
// in between; the page reports each component's age so that is visible.
func GlobalCBBalance(walclMUSD, ecbAssetsMUSD, bojAssetsMUSD, pbocAssetsMUSD float64) float64 {
	return walclMUSD + ecbAssetsMUSD + bojAssetsMUSD + pbocAssetsMUSD
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
