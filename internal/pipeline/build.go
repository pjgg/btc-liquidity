// Package pipeline assembles raw source observations into the daily liquidity
// table the page consumes.
package pipeline

import (
	"time"

	"github.com/pjgg/btc-liquidity/internal/liquidity"
)

// Inputs holds the raw observations exactly as published, in their own units
// and on their own publication dates.
type Inputs struct {
	WALCL     map[time.Time]float64 // millions USD, weekly
	WTREGEN   map[time.Time]float64 // millions USD, weekly
	RRP       map[time.Time]float64 // billions USD, daily — reverse repo, a drain
	ECBAssets map[time.Time]float64 // millions EUR, weekly
	BoJAssets map[time.Time]float64 // 100 million yen, monthly
	USDPerEUR map[time.Time]float64 // DEXUSEU
	YenPerUSD map[time.Time]float64 // DEXJPUS

	// Diagnostics for whether liquidity is being injected or drained. They do
	// not feed either headline series; they answer a different question.
	Reserves       map[time.Time]float64 // millions USD, weekly — WRESBAL
	Repo           map[time.Time]float64 // billions USD, daily — RPONTSYD, an injection
	DiscountWindow map[time.Time]float64 // millions USD, weekly — WLCFLPCL
}

// Row is one day of the liquidity table. Components are carried alongside the
// derived series so any figure on the page can be audited without refetching.
type Row struct {
	Date          time.Time
	WALCLMUSD     float64
	WTREGENMUSD   float64
	RRPMUSD       float64
	ECBAssetsMUSD float64
	BoJAssetsMUSD float64

	ReservesMUSD       float64
	RepoMUSD           float64
	DiscountWindowMUSD float64

	FedNetLiqMUSD float64
	GlobalCBMUSD  float64
}

// Build converts every series to millions of USD and expands them onto a daily
// index between start and end inclusive.
//
// Currency conversion happens before forward-filling, so each balance sheet is
// converted at the FX rate of its own observation date. Converting afterwards
// would revalue a six-week-old BoJ figure at today's rate and fabricate a jump
// in the global aggregate every time the yen moved.
//
// A day missing any component produces no row, rather than a row with a zero
// standing in for the missing input.
func Build(inputs Inputs, start, end time.Time) []Row {
	ecbMUSD := convert(inputs.ECBAssets, inputs.USDPerEUR, liquidity.ECBToMUSD)
	bojMUSD := convert(inputs.BoJAssets, inputs.YenPerUSD, liquidity.BoJToMUSD)

	walcl := liquidity.ForwardFill(inputs.WALCL, start, end)
	wtregen := liquidity.ForwardFill(inputs.WTREGEN, start, end)
	rrp := liquidity.ForwardFill(rescale(inputs.RRP), start, end)
	ecb := liquidity.ForwardFill(ecbMUSD, start, end)
	boj := liquidity.ForwardFill(bojMUSD, start, end)
	reserves := liquidity.ForwardFill(inputs.Reserves, start, end)
	repo := liquidity.ForwardFill(rescale(inputs.Repo), start, end)
	discount := liquidity.ForwardFill(inputs.DiscountWindow, start, end)

	rows := make([]Row, 0, int(end.Sub(start).Hours()/24)+1)
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		w, okW := walcl[date]
		tga, okT := wtregen[date]
		r, okR := rrp[date]
		e, okE := ecb[date]
		b, okB := boj[date]
		res, okRes := reserves[date]
		rp, okRp := repo[date]
		dw, okDw := discount[date]
		if !okW || !okT || !okR || !okE || !okB || !okRes || !okRp || !okDw {
			continue
		}

		rows = append(rows, Row{
			Date:               date,
			WALCLMUSD:          w,
			WTREGENMUSD:        tga,
			RRPMUSD:            r,
			ECBAssetsMUSD:      e,
			BoJAssetsMUSD:      b,
			ReservesMUSD:       res,
			RepoMUSD:           rp,
			DiscountWindowMUSD: dw,
			FedNetLiqMUSD:      liquidity.FedNetLiquidity(w, tga, r),
			GlobalCBMUSD:       liquidity.GlobalCBBalance(w, e, b),
		})
	}
	return rows
}

// rescale converts a whole series published in billions to millions.
func rescale(billions map[time.Time]float64) map[time.Time]float64 {
	millions := make(map[time.Time]float64, len(billions))
	for date, value := range billions {
		millions[date] = liquidity.BillionsToMUSD(value)
	}
	return millions
}

// convert applies an FX conversion to each observation using the rate published
// on that same date. An observation with no rate for its date is dropped: a
// missing rate read as zero would zero out the balance sheet.
func convert(
	amounts, rates map[time.Time]float64,
	conversion func(amount, rate float64) float64,
) map[time.Time]float64 {
	converted := make(map[time.Time]float64, len(amounts))
	for date, amount := range amounts {
		rate, ok := rates[date]
		if !ok || rate == 0 {
			continue
		}
		converted[date] = conversion(amount, rate)
	}
	return converted
}
