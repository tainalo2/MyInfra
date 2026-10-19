package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

func (c Client) openStack(ctx context.Context, creds Creds, location string) (Catalog, error) {
	base := strings.TrimRight(strings.TrimSpace(creds.Token), "/")
	base = strings.TrimSuffix(base, "/v3")
	base = strings.TrimSuffix(base, "/identity")
	if base == "" {
		return Catalog{}, errEmpty("Indiquez l'adresse Keystone.")
	}
	token, project, endpoints, err := infomaniakAuth(ctx, c.http(), base, creds.ApplicationKey, creds.ApplicationSecret)
	if err != nil {
		return Catalog{}, err
	}
	locations := infomaniakLocations(endpoints)
	if len(locations) == 0 {
		locations = []Location{{ID: "region", Label: "Région"}}
	}
	chosen := pickLocation(locations, location, locations[0].ID)
	compute := endpointURL(endpoints, "compute", chosen)
	if compute == "" {
		return Catalog{}, errEmpty("Le provider ne répond pas.")
	}
	header := map[string]string{"X-Auth-Token": token}
	path := strings.TrimRight(compute, "/") + "/flavors/detail"
	body, status, err := fetch(ctx, c.http(), http.MethodGet, path, nil, header)
	if err != nil {
		return Catalog{}, err
	}
	if status == http.StatusNotFound && project != "" {
		body, status, err = fetch(ctx, c.http(), http.MethodGet, strings.TrimRight(compute, "/")+"/"+project+"/flavors/detail", nil, header)
		if err != nil {
			return Catalog{}, err
		}
	}
	if status >= 300 {
		return Catalog{}, errEmpty("Le provider ne répond pas.")
	}
	flavors, err := parseInfomaniakFlavors(body)
	if err != nil {
		return Catalog{}, err
	}
	cat := infomaniakCatalog(locations, chosen, flavors, nil)
	cat.Provider = "openstack"
	return cat, nil
}

func (c Client) proxmox(ctx context.Context, endpoint, token, location string) (Catalog, error) {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if base == "" || strings.TrimSpace(token) == "" {
		return Catalog{}, errEmpty("Indiquez l'adresse et le jeton.")
	}
	header := map[string]string{"Authorization": "PVEAPIToken=" + strings.TrimSpace(token)}
	body, status, err := fetch(ctx, c.http(), http.MethodGet, base+"/api2/json/nodes", nil, header)
	if err != nil || status >= 300 {
		return Catalog{}, errEmpty("Le provider ne répond pas.")
	}
	var nodes struct {
		Data []struct {
			Node string `json:"node"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &nodes); err != nil || len(nodes.Data) == 0 {
		return Catalog{}, errEmpty("Aucun nœud.")
	}
	locations := make([]Location, 0, len(nodes.Data))
	for _, node := range nodes.Data {
		locations = append(locations, Location{ID: node.Node, Label: node.Node})
	}
	chosen := pickLocation(locations, location, locations[0].ID)
	body, status, err = fetch(ctx, c.http(), http.MethodGet, base+"/api2/json/nodes/"+chosen+"/qemu", nil, header)
	if err != nil || status >= 300 {
		return Catalog{}, errEmpty("Le provider ne répond pas.")
	}
	var guests struct {
		Data []struct {
			VMID int    `json:"vmid"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &guests); err != nil {
		return Catalog{}, err
	}
	offers := make([]Offer, 0, len(guests.Data))
	for _, guest := range guests.Data {
		name := guest.Name
		if name == "" {
			name = strconv.Itoa(guest.VMID)
		}
		offers = append(offers, Offer{ID: strconv.Itoa(guest.VMID), Name: name, Location: chosen, VCPU: 1, MemoryMB: 1024, DiskGB: 8})
	}
	return Catalog{Provider: "proxmox", Currency: "EUR", Location: chosen, Locations: locations, Periods: []string{"month"}, Offers: offers}, nil
}
