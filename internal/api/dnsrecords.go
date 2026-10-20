package api

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"

	"myinfra/internal/dns"
	"myinfra/internal/store"
)

// A name of a zone, with what MyInfra knows of it: which application or
// MyInfra itself uses it, the addresses it should answer with, and those it
// answers with at the provider.
type dnsNameView struct {
	Name     string        `json:"name"`
	Records  []dnsRecordIP `json:"records"`
	Expected []string      `json:"expected,omitempty"`
	// Use is app, myinfra, or empty for a name MyInfra does not write.
	Use     string `json:"use,omitempty"`
	UseID   string `json:"useId,omitempty"`
	UseName string `json:"useName,omitempty"`
	// State is ok, differs, missing, or unmanaged.
	State string `json:"state"`
}

type dnsRecordIP struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	// Server is the server of MyInfra with this address, when there is one.
	Server string `json:"server,omitempty"`
}

type dnsZoneView struct {
	Zone     string        `json:"zone"`
	Account  string        `json:"account"`
	Provider string        `json:"provider"`
	Error    string        `json:"error,omitempty"`
	Names    []dnsNameView `json:"names"`
}

// managedName is a name MyInfra writes, and what it should answer with.
type managedName struct {
	use, id, label string
	expected       []string
}

// managedNames are the names MyInfra writes: those of the applications, and
// its own when it runs in a cluster.
func (a *API) managedNames(ctx context.Context, owner string) (map[string]managedName, error) {
	out := map[string]managedName{}
	rows, err := a.Store.OwnedNames(ctx, owner)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		addresses, _ := a.Store.DestinationAddresses(ctx, row.DestinationKind, row.ServerID, row.ClusterID)
		out[strings.ToLower(row.Hostname)] = managedName{use: "app", id: row.AppID, label: row.AppName, expected: addresses}
	}
	if app, ok := a.selfRecord(ctx); ok && app.Hostname != "" {
		addresses, _ := a.Store.DestinationAddresses(ctx, "cluster", "", app.ClusterID)
		out[strings.ToLower(app.Hostname)] = managedName{use: "myinfra", id: selfAppID, label: "MyInfra", expected: addresses}
	}
	return out, nil
}

func (a *API) listDNSRecords(w http.ResponseWriter, r *http.Request) {
	owner := userID(r.Context())
	managed, err := a.managedNames(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	servers := map[string]string{}
	if rows, err := a.Store.ListServers(r.Context(), owner); err == nil {
		for _, row := range rows {
			if row.Address != "" {
				servers[row.Address] = row.Name
			}
		}
	}
	secrets, err := a.Store.ListSecrets(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var (
		mu    sync.Mutex
		group sync.WaitGroup
		zones []dnsZoneView
	)
	for _, sec := range secrets {
		if sec.Kind != "dns_token" {
			continue
		}
		names, err := a.zonesOf(r.Context(), owner, sec.ID)
		if err != nil {
			mu.Lock()
			zones = append(zones, dnsZoneView{Account: sec.Name, Provider: sec.Provider, Error: err.Error(), Names: []dnsNameView{}})
			mu.Unlock()
			continue
		}
		for _, zone := range names {
			group.Add(1)
			go func(sec store.Secret, zone string) {
				defer group.Done()
				view := a.zoneRecords(r.Context(), owner, sec, zone, managed, servers)
				mu.Lock()
				zones = append(zones, view)
				mu.Unlock()
			}(sec, zone)
		}
	}
	group.Wait()
	sort.Slice(zones, func(i, j int) bool { return zones[i].Zone < zones[j].Zone })
	if zones == nil {
		zones = []dnsZoneView{}
	}
	writeJSON(w, http.StatusOK, zones)
}

func (a *API) zoneRecords(ctx context.Context, owner string, sec store.Secret, zone string, managed map[string]managedName, servers map[string]string) dnsZoneView {
	view := dnsZoneView{Zone: zone, Account: sec.Name, Provider: sec.Provider, Names: []dnsNameView{}}
	_, tok, err := a.dnsToken(ctx, owner, sec.ID)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	records, err := a.dnsClient().Records(ctx, sec.Provider, credsOf(tok), zone)
	if err != nil {
		view.Error = err.Error()
		return view
	}
	byName := map[string]*dnsNameView{}
	var order []string
	for _, record := range records {
		item := byName[record.Name]
		if item == nil {
			item = &dnsNameView{Name: record.Name, Records: []dnsRecordIP{}}
			byName[record.Name] = item
			order = append(order, record.Name)
		}
		item.Records = append(item.Records, dnsRecordIP{Type: record.Type, Target: record.Target, Server: servers[record.Target]})
	}
	// A name MyInfra writes but the zone lacks is shown too.
	for name := range managed {
		if byName[name] == nil && strings.HasSuffix(name, "."+zone) {
			byName[name] = &dnsNameView{Name: name, Records: []dnsRecordIP{}}
			order = append(order, name)
		}
	}
	sort.Strings(order)
	for _, name := range order {
		item := byName[name]
		item.State = "unmanaged"
		if use, ok := managed[name]; ok {
			item.Use, item.UseID, item.UseName, item.Expected = use.use, use.id, use.label, use.expected
			targets := make([]string, 0, len(item.Records))
			for _, record := range item.Records {
				targets = append(targets, record.Target)
			}
			switch {
			case len(targets) == 0:
				item.State = "missing"
			case sameAddresses(targets, use.expected):
				item.State = "ok"
			default:
				item.State = "differs"
			}
		}
		view.Names = append(view.Names, *item)
	}
	return view
}

// dnsAccountOf finds the DNS account of a zone and opens it.
func (a *API) dnsAccountOf(ctx context.Context, owner, zone string) (store.Secret, dns.Creds, error) {
	account, err := a.accountForZone(ctx, owner, zone)
	if err != nil {
		return store.Secret{}, dns.Creds{}, err
	}
	sec, tok, err := a.dnsToken(ctx, owner, account)
	if err != nil {
		return store.Secret{}, dns.Creds{}, err
	}
	return sec, credsOf(tok), nil
}

type dnsNameBody struct {
	Zone string `json:"zone"`
	Name string `json:"name"`
}

func readDNSName(w http.ResponseWriter, r *http.Request) (dnsNameBody, bool) {
	var body dnsNameBody
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return body, false
	}
	body.Zone = strings.ToLower(strings.Trim(strings.TrimSpace(body.Zone), "."))
	body.Name = strings.ToLower(strings.Trim(strings.TrimSpace(body.Name), "."))
	if body.Zone == "" || (body.Name != body.Zone && !strings.HasSuffix(body.Name, "."+body.Zone)) {
		writeError(w, http.StatusBadRequest, "Ce nom n'est pas dans ce domaine.")
		return body, false
	}
	return body, true
}

// dropDNSName removes the address records of a name MyInfra does not use:
// one left behind by a removed copy or server. A name in use would be
// written again at once; it is changed from its application instead.
func (a *API) dropDNSName(w http.ResponseWriter, r *http.Request) {
	body, ok := readDNSName(w, r)
	if !ok {
		return
	}
	owner := userID(r.Context())
	managed, err := a.managedNames(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if use, ok := managed[body.Name]; ok {
		writeCoded(w, http.StatusConflict, "dns_name_used", "Ce nom sert à "+use.label+" : changez ses adresses depuis sa page.", map[string]string{"name": use.label})
		return
	}
	sec, creds, err := a.dnsAccountOf(r.Context(), owner, body.Zone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.dnsClient().DropAddress(r.Context(), sec.Provider, creds, body.Zone, body.Name); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.note(r.Context(), owner, newID(), "dns.drop", "warn", "Enregistrement DNS supprimé : "+body.Name+".", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "dropped"})
}

// publishDNSName writes a name MyInfra uses again, with the addresses it
// should answer with: after a change made by hand at the provider.
func (a *API) publishDNSName(w http.ResponseWriter, r *http.Request) {
	body, ok := readDNSName(w, r)
	if !ok {
		return
	}
	owner := userID(r.Context())
	managed, err := a.managedNames(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	use, ok := managed[body.Name]
	if !ok || len(use.expected) == 0 {
		writeError(w, http.StatusConflict, "MyInfra n'écrit pas ce nom.")
		return
	}
	sec, creds, err := a.dnsAccountOf(r.Context(), owner, body.Zone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.dnsClient().EnsureAddresses(r.Context(), sec.Provider, creds, body.Zone, body.Name, use.expected); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	a.note(r.Context(), owner, newID(), "dns.publish", "info", "Enregistrement DNS republié : "+body.Name+".", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
}
