package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"myinfra/internal/dns"
	"myinfra/internal/dnsplan"
	"myinfra/internal/store"
)

// Records that MyInfra does not write itself are read and changed here, by
// record set (one name, one type). Each change is planned first with
// dnsplan, which refuses what MyInfra manages and says what else it touches;
// the routes that write apply the same plan.

// zoneOf finds the zone of a name among those of the DNS accounts: the
// longest that contains it.
func (a *API) zoneOf(ctx context.Context, owner, name, hint string) (string, error) {
	name = strings.ToLower(strings.Trim(strings.TrimSpace(name), "."))
	zones, err := a.zoneViews(ctx, owner)
	if err != nil {
		return "", err
	}
	best := ""
	var names []string
	for _, z := range zones {
		names = append(names, z.Name)
		if hint != "" && !strings.EqualFold(z.Name, strings.Trim(hint, ".")) {
			continue
		}
		if (name == z.Name || strings.HasSuffix(name, "."+z.Name)) && len(z.Name) > len(best) {
			best = z.Name
		}
	}
	if best == "" {
		sort.Strings(names)
		if len(names) == 0 {
			return "", errString("Aucun compte DNS n'est enregistré : ajoutez-en un dans Réglages › DNS.")
		}
		return "", errString("Ce nom n'est dans aucune zone des comptes DNS. Zones : " + strings.Join(names, ", ") + ".")
	}
	return best, nil
}

// planDNS reads what the change touches and plans it.
func (a *API) planDNS(ctx context.Context, owner string, change dnsplan.Change) (dnsplan.Plan, store.Secret, dns.Creds, error) {
	zone, err := a.zoneOf(ctx, owner, change.Name, change.Zone)
	if err != nil {
		return dnsplan.Plan{}, store.Secret{}, dns.Creds{}, err
	}
	change.Zone = zone
	sec, creds, err := a.dnsAccountOf(ctx, owner, zone)
	if err != nil {
		return dnsplan.Plan{}, store.Secret{}, dns.Creds{}, err
	}
	name := strings.ToLower(strings.Trim(strings.TrimSpace(change.Name), "."))
	client := a.dnsClient()
	current, err := client.Entries(ctx, sec.Provider, creds, zone, name)
	if err != nil {
		return dnsplan.Plan{}, store.Secret{}, dns.Creds{}, err
	}
	// The wildcards above the name, to tell when a record hides one.
	parts := strings.Split(name, ".")
	for i := 1; i < len(parts); i++ {
		parent := strings.Join(parts[i:], ".")
		if parent != zone && !strings.HasSuffix(parent, "."+zone) {
			break
		}
		wild, err := client.Entries(ctx, sec.Provider, creds, zone, "*."+parent)
		if err != nil {
			return dnsplan.Plan{}, store.Secret{}, dns.Creds{}, err
		}
		current = append(current, wild...)
	}
	managed, err := a.managedNames(ctx, owner)
	if err != nil {
		return dnsplan.Plan{}, store.Secret{}, dns.Creds{}, err
	}
	uses := map[string]string{}
	for host, m := range managed {
		uses[host] = m.label
	}
	return dnsplan.Make(change, current, uses), sec, creds, nil
}

func (a *API) listZoneEntries(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	zone := strings.ToLower(strings.Trim(r.PathValue("zone"), "."))
	sec, creds, err := a.dnsAccountOf(r.Context(), owner, zone)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := a.dnsClient().Entries(r.Context(), sec.Provider, creds, zone, r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	kind := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type")))
	managed, _ := a.managedNames(r.Context(), owner)
	out := []map[string]any{}
	for _, e := range entries {
		if kind != "" && e.Type != kind {
			continue
		}
		row := map[string]any{"name": e.Name, "type": e.Type, "content": e.Content}
		if e.TTL > 1 {
			row["ttl"] = e.TTL
		}
		if e.Type == "MX" {
			row["priority"] = e.Priority
		}
		if e.Proxied != nil && *e.Proxied {
			row["proxied"] = true
		}
		if m, ok := managed[e.Name]; ok && (e.Type == "A" || e.Type == "AAAA") {
			row["usedBy"] = m.label
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) planDNSChange(w http.ResponseWriter, r *http.Request) {
	var change dnsplan.Change
	if err := readJSON(w, r, &change); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	plan, _, _, err := a.planDNS(r.Context(), ownerID(r.Context()), change)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// applyDNSChange makes a planned change. The plan is made again here, so
// that the refusals hold whoever calls.
func (a *API) applyDNSChange(w http.ResponseWriter, r *http.Request) {
	var change dnsplan.Change
	if err := readJSON(w, r, &change); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if r.Method == http.MethodDelete {
		change.Delete = true
		change.Values = nil
	}
	owner := ownerID(r.Context())
	plan, sec, creds, err := a.planDNS(r.Context(), owner, change)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if plan.Refused != "" {
		writeError(w, http.StatusConflict, plan.Refused)
		return
	}
	if plan.Noop {
		writeJSON(w, http.StatusOK, plan)
		return
	}
	client := a.dnsClient()
	c := plan.Change
	// What stands in the way of a CNAME, or a CNAME in the way, goes first.
	gone := map[string]bool{}
	for _, e := range plan.Removes {
		if e.Type != c.Type && !gone[e.Type] {
			gone[e.Type] = true
			if err := client.DeleteRecordSet(r.Context(), sec.Provider, creds, c.Zone, c.Name, e.Type); err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
		}
	}
	if c.Delete {
		err = client.DeleteRecordSet(r.Context(), sec.Provider, creds, c.Zone, c.Name, c.Type)
	} else {
		err = client.SetRecordSet(r.Context(), sec.Provider, creds, c.Zone, dns.RecordSet{Name: c.Name, Type: c.Type, Values: c.Values, TTL: c.TTL, Proxied: c.Proxied})
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if c.Delete {
		a.note(r.Context(), owner, newID(), "dns.delete", "warn", "Enregistrements DNS supprimés : "+c.Name+" "+c.Type+".", "", "")
	} else {
		var values []string
		for _, v := range c.Values {
			values = append(values, v.Content)
		}
		a.note(r.Context(), owner, newID(), "dns.set", "info", "Enregistrements DNS écrits : "+c.Name+" "+c.Type+" → "+strings.Join(values, ", ")+".", "", "")
	}
	writeJSON(w, http.StatusOK, plan)
}
