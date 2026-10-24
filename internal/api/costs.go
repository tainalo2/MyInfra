package api

import (
	"context"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The theoretical monthly cost of machines, read as the home page reads it
// (web/src/measure.tsx hourlyRate): the hourly price, or the monthly one
// spread over 730 hours, then 730 hours a month. It is no invoice.

const hoursPerMonth = 730

// serverHourly is the hourly price of a machine and its currency, or ok
// false when it has none.
func serverHourly(v serverView) (float64, string, bool) {
	hourly, monthly, currency := "", "", ""
	switch {
	case v.Plan != nil:
		hourly, monthly, currency = v.Plan.Hourly, v.Plan.Monthly, v.Plan.Currency
	case v.Hosting != nil:
		hourly, monthly = v.Hosting.rates()
		currency = v.Hosting.Currency
	default:
		return 0, "", false
	}
	return hourlyOf(hourly, monthly, currency)
}

func hourlyOf(hourly, monthly, currency string) (float64, string, bool) {
	currency = currencyOf(currency)
	if n, err := strconv.ParseFloat(strings.TrimSpace(hourly), 64); err == nil && n > 0 && !math.IsInf(n, 0) {
		return n, currency, true
	}
	if n, err := strconv.ParseFloat(strings.TrimSpace(monthly), 64); err == nil && n > 0 && !math.IsInf(n, 0) {
		return n / hoursPerMonth, currency, true
	}
	return 0, currency, false
}

// currencyOf names a currency the way the screens group it: empty is euros.
func currencyOf(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return "EUR"
	}
	return code
}

// amounts adds monthly sums per currency; currencies are never converted.
type amounts map[string]float64

func (m amounts) add(currency string, monthly float64) { m[currencyOf(currency)] += monthly }

func (m amounts) plus(other amounts) amounts {
	out := amounts{}
	for k, v := range m {
		out[k] += v
	}
	for k, v := range other {
		out[k] += v
	}
	return out
}

func (m amounts) minus(other amounts) amounts {
	out := amounts{}
	for k, v := range m {
		out[k] += v
	}
	for k, v := range other {
		out[k] -= v
	}
	return out
}

type amountView struct {
	Currency string  `json:"currency"`
	Monthly  float64 `json:"monthly"`
}

func (m amounts) views() []amountView {
	out := make([]amountView, 0, len(m))
	for currency, monthly := range m {
		out = append(out, amountView{Currency: currency, Monthly: round2(monthly)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}

// String writes the sums as people read them: 41,20 €/mois + 12,00 $/mois.
func (m amounts) String() string {
	if len(m) == 0 {
		return "0,00 €/mois"
	}
	var parts []string
	for _, v := range m.views() {
		parts = append(parts, moneyText(v.Monthly, v.Currency)+"/mois")
	}
	return strings.Join(parts, " + ")
}

func amountsOf(views []amountView) amounts {
	out := amounts{}
	for _, v := range views {
		out.add(v.Currency, v.Monthly)
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func moneyText(v float64, currency string) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	whole := strconv.FormatFloat(round2(v), 'f', 2, 64)
	symbol := currencyOf(currency)
	switch symbol {
	case "EUR":
		symbol = "€"
	case "USD":
		symbol = "$"
	}
	return sign + strings.Replace(whole, ".", ",", 1) + " " + symbol
}

type serverCostView struct {
	Name     string  `json:"name"`
	Currency string  `json:"currency"`
	Monthly  float64 `json:"monthly"`
}

type costsView struct {
	Monthly []amountView     `json:"monthly"`
	Servers []serverCostView `json:"servers"`
	Note    string           `json:"note"`
	// Budget is MYINFRA_BUDGET_MONTHLY, when set.
	Budget float64 `json:"budget,omitempty"`
}

const costNote = "Coût théorique : prix horaire des offres, ou prix mensuel réparti sur 730 heures ; ni facture ni stockage en plus."

func (a *API) infraCosts(ctx context.Context, owner string) (amounts, []serverCostView, error) {
	rows, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	total := amounts{}
	var lines []serverCostView
	for _, row := range rows {
		hourly, currency, ok := serverHourly(a.toServer(row))
		if !ok {
			continue
		}
		total.add(currency, hourly*hoursPerMonth)
		lines = append(lines, serverCostView{Name: row.Name, Currency: currency, Monthly: round2(hourly * hoursPerMonth)})
	}
	return total, lines, nil
}

func (a *API) listCosts(w http.ResponseWriter, r *http.Request) {
	total, lines, err := a.infraCosts(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if lines == nil {
		lines = []serverCostView{}
	}
	writeJSON(w, http.StatusOK, costsView{Monthly: total.views(), Servers: lines, Note: costNote, Budget: monthlyBudget()})
}

// monthlyBudget is MYINFRA_BUDGET_MONTHLY, or 0 when it is not set.
func monthlyBudget() float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("MYINFRA_BUDGET_MONTHLY")), 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
