package api

import (
	"context"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	"myinfra/internal/notify"
)

var (
	budgetMu     sync.Mutex
	budgetWarned string
)

// WatchBudget sends an optional alert when theoretical spend exceeds MYINFRA_BUDGET_MONTHLY.
func (a *API) WatchBudget(ctx context.Context) {
	limit := strings.TrimSpace(os.Getenv("MYINFRA_BUDGET_MONTHLY"))
	if limit == "" || !notify.Enabled() || a.Replica {
		return
	}
	cap, ok := new(big.Rat).SetString(limit)
	if !ok || cap.Sign() <= 0 {
		return
	}
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkBudget(ctx, cap)
		}
	}
}

func (a *API) checkBudget(ctx context.Context, cap *big.Rat) {
	id, _, err := a.Store.User(ctx)
	if err != nil {
		return
	}
	for _, owner := range []string{id} {
		servers, err := a.Store.ListServers(ctx, owner)
		if err != nil {
			continue
		}
		total := new(big.Rat)
		currency := ""
		for _, row := range servers {
			view := a.toServer(row)
			if view.Reach == "gone" || view.Cost == nil {
				continue
			}
			amount, ok := new(big.Rat).SetString(view.Cost.Amount)
			if !ok {
				continue
			}
			total.Add(total, amount)
			if currency == "" {
				currency = view.Cost.Currency
			}
		}
		if total.Cmp(cap) < 0 {
			budgetMu.Lock()
			if budgetWarned == owner {
				budgetWarned = ""
			}
			budgetMu.Unlock()
			continue
		}
		budgetMu.Lock()
		if budgetWarned == owner {
			budgetMu.Unlock()
			continue
		}
		budgetWarned = owner
		budgetMu.Unlock()
		notify.Send(ctx, "Budget MyInfra dépassé",
			"Dépense théorique "+total.FloatString(2)+" "+currency+" (plafond "+cap.FloatString(2)+"). Les serveurs absents chez le provider ne sont pas comptés.")
	}
}
