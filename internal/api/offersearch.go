package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"myinfra/internal/catalog"
	"myinfra/internal/offerfind"
	"myinfra/internal/place"
)

// An offer search reads the catalog of each location of each provider
// account, then lets offerfind choose. Provider APIs are slow and sometimes
// down: each read is bounded in time and in number, kept ten minutes, and a
// failing account is told, not fatal.

const (
	offerCacheLife   = 10 * time.Minute
	offerReadTimeout = 20 * time.Second
	offerParallel    = 4
	offerReadsMax    = 60
)

type cachedCatalog struct {
	cat catalog.Catalog
	at  time.Time
}

var (
	offerCacheMu sync.Mutex
	offerCache   = map[string]cachedCatalog{}
)

func (a *API) cachedCatalog(ctx context.Context, owner, accountID, location string) (catalog.Catalog, error) {
	key := owner + "|" + accountID + "|" + location
	offerCacheMu.Lock()
	hit, ok := offerCache[key]
	offerCacheMu.Unlock()
	if ok && time.Since(hit.at) < offerCacheLife {
		return hit.cat, nil
	}
	ctx, cancel := context.WithTimeout(ctx, offerReadTimeout)
	defer cancel()
	cat, err := a.catalogFor(ctx, owner, accountID, location, "")
	if err != nil {
		return catalog.Catalog{}, err
	}
	offerCacheMu.Lock()
	offerCache[key] = cachedCatalog{cat: cat, at: time.Now()}
	if location == "" && cat.Location != "" {
		offerCache[owner+"|"+accountID+"|"+cat.Location] = cachedCatalog{cat: cat, at: time.Now()}
	}
	offerCacheMu.Unlock()
	return cat, nil
}

type offerSearchView struct {
	offerfind.Result
	Searched []string `json:"searched"`
	Warnings []string `json:"warnings"`
}

type account struct {
	ID, Name, Provider string
}

// gatherOffers reads the catalogs of the accounts at the locations where
// keep says to look.
func (a *API) gatherOffers(ctx context.Context, owner string, accounts []account, keep func(account, catalog.Location, *place.Place) bool, budget *int) ([]offerfind.Candidate, []string, []string) {
	var mu sync.Mutex
	var out []offerfind.Candidate
	var searched, warnings []string
	var wg sync.WaitGroup
	for _, acc := range accounts {
		wg.Add(1)
		go func(acc account) {
			defer wg.Done()
			first, err := a.cachedCatalog(ctx, owner, acc.ID, "")
			if err != nil {
				mu.Lock()
				warnings = append(warnings, acc.Name+" ("+acc.Provider+") ne répond pas : "+err.Error())
				mu.Unlock()
				return
			}
			locations := first.Locations
			if len(locations) == 0 {
				locations = []catalog.Location{{ID: first.Location, Label: first.Location}}
			}
			var picked []catalog.Location
			for _, loc := range locations {
				var where *place.Place
				if p, ok := place.Of(loc.ID, loc.Label); ok {
					where = &p
				}
				if keep(acc, loc, where) {
					picked = append(picked, loc)
				}
			}
			sem := make(chan struct{}, offerParallel)
			var inner sync.WaitGroup
			for _, loc := range picked {
				mu.Lock()
				if *budget <= 0 {
					warnings = append(warnings, acc.Name+" : tous les lieux n'ont pas été lus (limite de "+strconv.Itoa(offerReadsMax)+" lectures par recherche).")
					mu.Unlock()
					break
				}
				*budget--
				mu.Unlock()
				inner.Add(1)
				sem <- struct{}{}
				go func(loc catalog.Location) {
					defer inner.Done()
					defer func() { <-sem }()
					cat := first
					if loc.ID != first.Location {
						var err error
						if cat, err = a.cachedCatalog(ctx, owner, acc.ID, loc.ID); err != nil {
							mu.Lock()
							warnings = append(warnings, acc.Name+" à "+loc.ID+" : "+err.Error())
							mu.Unlock()
							return
						}
					}
					var where *place.Place
					if p, ok := place.Of(loc.ID, loc.Label); ok {
						where = &p
					}
					mu.Lock()
					defer mu.Unlock()
					searched = append(searched, acc.Name+"/"+loc.ID)
					for _, o := range cat.Offers {
						out = append(out, offerfind.Candidate{
							AccountID: acc.ID, Account: acc.Name, Provider: acc.Provider, Currency: cat.Currency,
							Location: loc, Place: where, Offer: o,
						})
					}
				}(loc)
			}
			inner.Wait()
		}(acc)
	}
	wg.Wait()
	sort.Strings(searched)
	sort.Strings(warnings)
	return out, searched, warnings
}

// searchOffers looks where the person asked first; when nothing there
// fits, it reads everywhere else too, to propose the nearest.
func (a *API) searchOffers(ctx context.Context, owner string, want offerfind.Want) (offerSearchView, error) {
	rows, err := a.Store.ListSecrets(ctx, owner)
	if err != nil {
		return offerSearchView{}, err
	}
	var accounts []account
	for _, row := range rows {
		if row.Kind == "provider_token" {
			accounts = append(accounts, account{ID: row.ID, Name: row.Name, Provider: row.Provider})
		}
	}
	fits := func(acc account, loc catalog.Location, where *place.Place) bool {
		if len(want.Providers) > 0 {
			ok := false
			for _, p := range want.Providers {
				if strings.EqualFold(p, acc.Provider) || strings.EqualFold(p, acc.Name) || p == acc.ID {
					ok = true
				}
			}
			if !ok {
				return false
			}
		}
		return offerfind.InPlace(offerfind.Candidate{Location: loc, Place: where}, want)
	}
	budget := offerReadsMax
	found, searched, warnings := a.gatherOffers(ctx, owner, accounts, fits, &budget)
	result := offerfind.Find(found, want)
	if len(result.Matches) == 0 && len(accounts) > 0 {
		seen := map[string]bool{}
		for _, c := range found {
			seen[c.AccountID+"|"+c.Location.ID] = true
		}
		rest, more, moreWarnings := a.gatherOffers(ctx, owner, accounts, func(acc account, loc catalog.Location, _ *place.Place) bool {
			return !seen[acc.ID+"|"+loc.ID]
		}, &budget)
		found = append(found, rest...)
		searched = append(searched, more...)
		warnings = append(warnings, moreWarnings...)
		result = offerfind.Find(found, want)
	}
	if searched == nil {
		searched = []string{}
	}
	if warnings == nil {
		warnings = []string{}
	}
	return offerSearchView{Result: result, Searched: searched, Warnings: warnings}, nil
}

func (a *API) offerSearch(w http.ResponseWriter, r *http.Request) {
	var want offerfind.Want
	if err := readJSON(w, r, &want); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if want.Period != "" && want.Period != "hour" && want.Period != "month" {
		writeError(w, http.StatusBadRequest, "Choisissez l'heure ou le mois.")
		return
	}
	view, err := a.searchOffers(r.Context(), ownerID(r.Context()), want)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}
