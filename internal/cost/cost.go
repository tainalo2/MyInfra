package cost

import (
	"math/big"
	"time"
)

// Since estimates what a machine has cost since it was opened.
// An hourly price is used when it exists. Otherwise the monthly price is
// spread over 730 hours. The result is a theoretical amount, not an invoice.
func Since(hourly, monthly string, from, now time.Time) (string, bool) {
	rate := hourly
	monthlyRate := false
	if rate == "" {
		rate = monthly
		monthlyRate = true
	}
	if rate == "" {
		return "", false
	}
	price, ok := new(big.Rat).SetString(rate)
	if !ok || price.Sign() < 0 {
		return "", false
	}
	if !now.After(from) {
		return format(new(big.Rat)), true
	}
	elapsed := new(big.Rat).SetFrac(big.NewInt(now.Sub(from).Nanoseconds()), big.NewInt(int64(time.Hour)))
	amount := new(big.Rat).Mul(price, elapsed)
	if monthlyRate {
		amount.Quo(amount, big.NewRat(730, 1))
	}
	return format(amount), true
}

func format(amount *big.Rat) string {
	value := new(big.Float).SetPrec(128).SetMode(big.ToNearestAway).SetRat(amount)
	return value.Text('f', 4)
}
