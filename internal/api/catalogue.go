package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/dns"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/registry"
	"myinfra/internal/store"
)

func (a *API) listCatalogue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, market.Groups())
}

func (a *API) getCatalogue(w http.ResponseWriter, r *http.Request) {
	card, ok := market.CardOf(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Cet article est introuvable.")
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (a *API) installCatalogue(w http.ResponseWriter, r *http.Request) {
	a.installFrom(w, r, func(host string, inputs map[string]string) ([]store.Service, map[string]string, error) {
		// A catalogue application is a package: it installs as one.
		services, env, err := market.Expand(r.PathValue("id"), host, inputs)
		if err != nil {
			return nil, nil, err
		}
		out, err := packServices(services)
		return out, env, err
	})
}

// expandFunc gives the services and the variables of an application about
// to be installed, its values filled in for its public name.
type expandFunc func(host string, inputs map[string]string) ([]store.Service, map[string]string, error)

// installFrom creates an application from a catalogue article or a package:
// its name, destination, domains and redundancy come from the request.
func (a *API) installFrom(w http.ResponseWriter, r *http.Request, expand expandFunc) {
	var body struct {
		Name  string `json:"name"`
		Names []struct {
			Subdomain string `json:"subdomain"`
			Zone      string `json:"zone"`
			// Service is the public service the name leads to; empty or
			// the first public one is the main.
			Service string `json:"service"`
		} `json:"names"`
		DestinationKind string            `json:"destinationKind"`
		ServerID        string            `json:"serverId"`
		ClusterID       string            `json:"clusterId"`
		Inputs          map[string]string `json:"inputs"`
		Tag             string            `json:"tag"`
		// Copies is the redundancy asked: 0 or 1 keeps the article's own.
		Copies int `json:"copies"`
		// DiskNode is the server of the cluster whose disk holds the
		// shared disks; empty lets the cluster choose.
		DiskNode string `json:"diskNode"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if a.refuseBusyTarget(w, r, body.ServerID, body.ClusterID) {
		return
	}
	name, ok := cleanName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "Donnez un nom à l'application.")
		return
	}
	if body.DestinationKind != "server" && body.DestinationKind != "cluster" {
		writeError(w, http.StatusBadRequest, "Choisissez une machine ou un cluster.")
		return
	}
	if body.DestinationKind == "server" && strings.TrimSpace(body.ServerID) == "" {
		writeError(w, http.StatusBadRequest, "Choisissez une machine.")
		return
	}
	if body.DestinationKind == "cluster" && strings.TrimSpace(body.ClusterID) == "" {
		writeError(w, http.StatusBadRequest, "Choisissez un cluster.")
		return
	}
	if len(body.Names) > 10 {
		writeError(w, http.StatusBadRequest, "Dix noms au plus.")
		return
	}
	defaultHost := slug(name) + "." + appsDomain(a.AppsDomain)
	publicHost := defaultHost
	in := store.NewApplication{
		ID: newID(), OwnerID: ownerID(r.Context()), Name: name,
		SourceKind: "image", DestinationKind: body.DestinationKind,
		DefaultID: newID(), DefaultHost: defaultHost,
	}
	if body.DestinationKind == "server" {
		server, err := a.Store.Server(r.Context(), in.OwnerID, body.ServerID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if server.ClusterID != "" {
			writeError(w, http.StatusBadRequest, "Cette machine appartient à un cluster.")
			return
		}
		in.ServerID = body.ServerID
	} else {
		origin, _, _, _, err := a.Store.ClusterKube(r.Context(), in.OwnerID, body.ClusterID)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		if origin != "created" {
			writeError(w, http.StatusConflict, "Ce cluster est lu par Argo CD.")
			return
		}
		in.ClusterID = body.ClusterID
	}
	seen := map[string]bool{}
	accounts := map[string]string{}
	for _, item := range body.Names {
		host, err := dns.Join(item.Subdomain, item.Zone)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if seen[host] {
			writeError(w, http.StatusBadRequest, "Ce nom est déjà dans la liste.")
			return
		}
		seen[host] = true
		zone := strings.ToLower(strings.Trim(strings.TrimSpace(item.Zone), "."))
		secretID, found := accounts[zone]
		if !found {
			looked, lookupErr := a.accountForZone(r.Context(), in.OwnerID, zone)
			if lookupErr != nil {
				writeError(w, http.StatusBadRequest, lookupErr.Error())
				return
			}
			secretID = looked
			accounts[zone] = secretID
		}
		if publicHost == defaultHost {
			publicHost = host
		}
		in.Names = append(in.Names, store.Name{ID: newID(), Hostname: host, Zone: zone, SecretID: secretID, Service: strings.TrimSpace(item.Service)})
	}
	services, env, err := expand(publicHost, body.Inputs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	main, err := servicesNames(&in, services, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if node := strings.TrimSpace(body.DiskNode); node != "" {
		if err := a.placeDisks(r.Context(), in.OwnerID, in.ClusterID, node, services); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if tag := strings.TrimSpace(body.Tag); tag != "" {
		for i, svc := range services {
			if svc.Name != main {
				continue
			}
			next, tagErr := registry.WithTag(svc.Image, tag)
			if tagErr != nil {
				writeError(w, http.StatusBadRequest, "Cette version est illisible.")
				return
			}
			services[i].Image = next
		}
	}
	if !manifest.EnvOK(env) {
		writeError(w, http.StatusBadRequest, "Une variable est illisible.")
		return
	}
	in.Variables = env
	in.Services = services
	if body.Copies > 1 {
		if err := a.shapeRedundancy(r.Context(), in.OwnerID, in.ClusterID, in.Services, body.Copies); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// The application keeps the copies asked, as an install by hand does;
	// without, those of the article's published service.
	in.Replicas = 1
	for _, svc := range in.Services {
		if svc.Name == main {
			in.Source = svc.Image
			in.Replicas = svc.Replicas
		}
	}
	if body.Copies > 1 {
		in.Replicas = body.Copies
	}
	if main == "" && len(in.Services) > 0 {
		// Nothing public: the application only opens ports to the Internet.
		in.Source = in.Services[0].Image
	}
	if err := a.checkExposed(r.Context(), in.DestinationKind, in.ServerID, in.ClusterID, in.ID, in.Services); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	sealed, err := a.sealVars(in.Variables)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	in.Variables = sealed
	if err := a.Store.CreateApplication(r.Context(), in); err != nil {
		a.mapErr(w, err)
		return
	}
	if len(in.Names) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		a.publishPending(ctx)
		cancel()
	}
	app, err := a.Store.Application(r.Context(), in.OwnerID, in.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if in.DestinationKind == "cluster" && len(in.Services) > 0 {
		go a.deliverClusters(context.Background())
		if exposes(in.Services) {
			go a.openFirewall(in.OwnerID, in.ClusterID)
		}
	}
	writeJSON(w, http.StatusCreated, toApp(app))
}

// servicesNames ties the names of a new application to its public services:
// a name for the main one, the first public, is kept without service; one
// for another names it. Each public service besides the main one gets the
// address MyInfra gives it, <service>-<application>.<apps domain>.
func servicesNames(in *store.NewApplication, services []store.Service, app string) (string, error) {
	main := ""
	published := map[string]bool{}
	for _, svc := range services {
		if svc.Published {
			if main == "" {
				main = svc.Name
			}
			published[svc.Name] = true
		}
	}
	for i, n := range in.Names {
		switch {
		case n.Service == "" || n.Service == main:
			in.Names[i].Service = ""
		case !published[n.Service]:
			return "", fmt.Errorf("Le nom %s mène au service %s, qui n'est pas public.", n.Hostname, n.Service)
		}
	}
	domain := in.DefaultHost[strings.Index(in.DefaultHost, ".")+1:]
	for _, svc := range services {
		if svc.Published && svc.Name != main {
			in.ServiceDefaults = append(in.ServiceDefaults, store.Name{
				ID: newID(), Hostname: slug(svc.Name) + "-" + slug(app) + "." + domain, Service: svc.Name,
			})
		}
	}
	return main, nil
}

// placeDisks keeps the shared disks of a new application on one server of
// its cluster: the one with room for them, which the cluster cannot know.
func (a *API) placeDisks(ctx context.Context, owner, clusterID, serverID string, services []store.Service) error {
	if clusterID == "" {
		return fmt.Errorf("Le serveur d'un disque partagé se choisit sur un cluster.")
	}
	server, err := a.Store.Server(ctx, owner, serverID)
	if err != nil || server.ClusterID != clusterID {
		return fmt.Errorf("Ce serveur n'est pas dans le cluster de l'application.")
	}
	placed := false
	for i := range services {
		for j := range services[i].Spec.Volumes {
			if services[i].Spec.Volumes[j].Disk != "" {
				services[i].Spec.Volumes[j].Node = server.ID
				placed = true
			}
		}
	}
	if !placed {
		return fmt.Errorf("Cette application n'a pas de disque partagé à placer.")
	}
	return nil
}
