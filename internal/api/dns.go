package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"myinfra/internal/box"
	"myinfra/internal/dns"
	"myinfra/internal/store"
)

type zoneView struct {
	Name      string `json:"name"`
	AccountID string `json:"accountId"`
	Account   string `json:"account"`
	Provider  string `json:"provider"`
}

func (a *API) dnsClient() dns.Client {
	return dns.Client{HTTP: a.Verify.HTTP, OVHBase: a.Verify.OVHBase, TofuDir: tofuDir()}
}

func (a *API) dnsToken(ctx context.Context, owner, id string) (store.Secret, storedToken, error) {
	sec, err := a.Store.Secret(ctx, owner, id)
	if err != nil || sec.Kind != "dns_token" {
		return store.Secret{}, storedToken{}, errString("Ce compte DNS est introuvable.")
	}
	plain, err := a.Box.Open(box.Sealed{Ciphertext: sec.Ciphertext, Nonce: sec.Nonce, WrappedDEK: sec.WrappedDEK})
	if err != nil {
		return store.Secret{}, storedToken{}, errString("Jeton illisible.")
	}
	var tok storedToken
	if err := json.Unmarshal(plain, &tok); err != nil {
		return store.Secret{}, storedToken{}, errString("Jeton illisible.")
	}
	return sec, tok, nil
}

func credsOf(tok storedToken) dns.Creds {
	return dns.Creds{
		Token: tok.Token, ApplicationKey: tok.ApplicationKey,
		ApplicationSecret: tok.ApplicationSecret, ConsumerKey: tok.ConsumerKey,
	}
}

// zonesFresh is how long the zones of a DNS account are served without asking
// the provider again: they rarely change, and asking takes a few hundred ms.
const zonesFresh = 5 * time.Minute

type zonesHit struct {
	at    time.Time
	names []string
	busy  bool
}

var (
	zonesMu    sync.Mutex
	zonesCache = map[string]*zonesHit{}
)

// zonesOf lists the zones of a DNS account. A stale list is served at once and
// read again behind; only an account never read makes the page wait.
func (a *API) zonesOf(ctx context.Context, owner, id string) ([]string, error) {
	key := owner + "/" + id
	zonesMu.Lock()
	hit := zonesCache[key]
	if hit != nil && !hit.at.IsZero() {
		names := hit.names
		if time.Since(hit.at) >= zonesFresh && !hit.busy {
			hit.busy = true
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				fresh, err := a.readZones(ctx, owner, id)
				zonesMu.Lock()
				defer zonesMu.Unlock()
				hit.busy = false
				if err == nil {
					hit.names, hit.at = fresh, time.Now()
				}
			}()
		}
		zonesMu.Unlock()
		return names, nil
	}
	zonesMu.Unlock()
	names, err := a.readZones(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	zonesMu.Lock()
	zonesCache[key] = &zonesHit{at: time.Now(), names: names}
	zonesMu.Unlock()
	return names, nil
}

// forgetZones drops what is known of an account, after its token changes.
func forgetZones(owner, id string) {
	zonesMu.Lock()
	delete(zonesCache, owner+"/"+id)
	zonesMu.Unlock()
}

func (a *API) readZones(ctx context.Context, owner, id string) ([]string, error) {
	sec, tok, err := a.dnsToken(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	return a.dnsClient().Zones(ctx, sec.Provider, credsOf(tok))
}

func (a *API) zoneViews(ctx context.Context, owner string) ([]zoneView, error) {
	rows, err := a.Store.ListSecrets(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := []zoneView{}
	seen := map[string]bool{}
	for _, sec := range rows {
		if sec.Kind != "dns_token" {
			continue
		}
		names, err := a.zonesOf(ctx, owner, sec.ID)
		if err != nil {
			continue
		}
		for _, name := range names {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, zoneView{Name: name, AccountID: sec.ID, Account: sec.Name, Provider: sec.Provider})
		}
	}
	return out, nil
}

func (a *API) listZones(w http.ResponseWriter, r *http.Request) {
	owner := userID(r.Context())
	out, err := a.zoneViews(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) secretZones(w http.ResponseWriter, r *http.Request) {
	owner := userID(r.Context())
	sec, err := a.Store.Secret(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if sec.Kind != "dns_token" {
		writeError(w, http.StatusBadRequest, "Ce compte n'est pas un compte DNS.")
		return
	}
	names, err := a.zonesOf(r.Context(), owner, sec.ID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, names)
}

func (a *API) accountForZone(ctx context.Context, owner, zone string) (string, error) {
	rows, err := a.Store.ListSecrets(ctx, owner)
	if err != nil {
		return "", err
	}
	for _, sec := range rows {
		if sec.Kind != "dns_token" {
			continue
		}
		names, err := a.zonesOf(ctx, owner, sec.ID)
		if err != nil {
			continue
		}
		for _, name := range names {
			if name == zone {
				return sec.ID, nil
			}
		}
	}
	return "", errString("Ce domaine n'est pas sur un compte DNS.")
}

func (a *API) Publish(ctx context.Context) {
	a.publishPending(ctx)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.publishPending(context.Background())
		}
	}
}

func (a *API) publishPending(ctx context.Context) {
	if a.Replica {
		return
	}
	a.publishMu.Lock()
	defer a.publishMu.Unlock()
	rows, err := a.Store.PendingNames(ctx)
	if err != nil {
		return
	}
	deliver := false
	for _, row := range rows {
		addresses, err := a.Store.DestinationAddresses(ctx, row.DestinationKind, row.ServerID, row.ClusterID)
		if err != nil || len(addresses) == 0 {
			_ = a.Store.MarkName(ctx, row.ID, "pending_dns", "La machine n'a pas encore d'adresse.")
			continue
		}
		sec, tok, err := a.dnsToken(ctx, row.OwnerID, row.SecretID)
		if err != nil {
			_ = a.Store.MarkName(ctx, row.ID, "pending_dns", err.Error())
			continue
		}
		client := a.dnsClient()
		if err := client.EnsureAddresses(ctx, sec.Provider, credsOf(tok), row.Zone, row.Hostname, addresses); err != nil {
			_ = a.Store.MarkName(ctx, row.ID, "pending_dns", err.Error())
			continue
		}
		a.rememberPublished(row.ID, addresses)
		detail := ""
		if client.ForeignWildcard(ctx, row.Zone, addresses[0]) {
			detail = "Une adresse plus ancienne peut encore répondre pendant une heure."
		}
		if err := a.Store.MarkName(ctx, row.ID, "ready", detail); err != nil {
			continue
		}
		if row.DestinationKind == "cluster" {
			deliver = true
		}
	}
	a.resyncNames(ctx)
	if deliver {
		go a.deliverClusters(context.Background())
	}
}

// resyncNames follows the machines of a cluster: a node that joins is added
// to its names, a node that leaves is removed from them.
func (a *API) resyncNames(ctx context.Context) {
	rows, err := a.Store.ReadyNames(ctx)
	if err != nil {
		return
	}
	client := a.dnsClient()
	for _, row := range rows {
		addresses, err := a.Store.DestinationAddresses(ctx, row.DestinationKind, row.ServerID, row.ClusterID)
		if err != nil || len(addresses) == 0 {
			continue
		}
		key := strings.Join(addresses, ",")
		if a.publishedAs(row.ID) == key {
			continue
		}
		if a.publishedAs(row.ID) == "" && strings.Join(client.Announced(ctx, row.Zone, row.Hostname), ",") == key {
			a.rememberPublished(row.ID, addresses)
			continue
		}
		sec, tok, err := a.dnsToken(ctx, row.OwnerID, row.SecretID)
		if err != nil {
			continue
		}
		if err := client.EnsureAddresses(ctx, sec.Provider, credsOf(tok), row.Zone, row.Hostname, addresses); err != nil {
			slog.Info("dns", "name", row.Hostname, "err", err)
			continue
		}
		a.rememberPublished(row.ID, addresses)
	}
}

func (a *API) rememberPublished(id string, addresses []string) {
	a.namesMu.Lock()
	defer a.namesMu.Unlock()
	if a.published == nil {
		a.published = map[string]string{}
	}
	sorted := append([]string(nil), addresses...)
	sort.Strings(sorted)
	a.published[id] = strings.Join(sorted, ",")
}

func (a *API) publishedAs(id string) string {
	a.namesMu.Lock()
	defer a.namesMu.Unlock()
	return a.published[id]
}
