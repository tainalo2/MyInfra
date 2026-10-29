package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/pack"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

// withAddress fills {{url}}, {{host}} and their {{…:service}} forms with
// the names the public services answer on now, in the application's
// variables and in each service's own variables and files: a new name
// reaches them at the next delivery, and the copies restart with it.
func withAddress(app *manifest.App, hosts map[string]string) {
	app.Env = fillMap(app.Env, hosts)
	for i := range app.Services {
		app.Services[i].Spec = specAt(app.Services[i].Spec, hosts)
	}
}

// specAt is a service's spec with its addresses filled in; the stored one
// keeps its tokens.
func specAt(s spec.Service, hosts map[string]string) spec.Service {
	s.Env = fillMap(s.Env, hosts)
	if len(s.Files) > 0 {
		files := make([]spec.File, len(s.Files))
		for i, file := range s.Files {
			files[i] = spec.File{Path: file.Path, Content: pack.FillAddresses(file.Content, hosts)}
		}
		s.Files = files
	}
	return s
}

func fillMap(values map[string]string, hosts map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = pack.FillAddresses(value, hosts)
	}
	return out
}

// addresses are the names an application's public services answer on now,
// by service, and "" for the main one, which is also under its own name.
func (a *API) addresses(ctx context.Context, appID string, published []string) (map[string]string, error) {
	hosts, err := a.Store.Addresses(ctx, appID)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for service := range hosts {
		if service != "" {
			named[service] = true
		}
	}
	if main := store.MainService(published, named); main != "" && hosts[main] == "" {
		hosts[main] = hosts[""]
	}
	return hosts, nil
}

func publishedOf(services []store.Service) []string {
	var out []string
	for _, svc := range services {
		if svc.Published {
			out = append(out, svc.Name)
		}
	}
	return out
}

// domainAddresses are the names the public services answer on, as
// Store.Addresses reads them: "" for the main one.
func domainAddresses(app store.Application) map[string]string {
	out := map[string]string{}
	custom := map[string]bool{}
	for _, domain := range app.Domains {
		switch {
		case domain.Kind == "custom" && domain.Status == "ready" && !custom[domain.Service]:
			out[domain.Service], custom[domain.Service] = domain.Hostname, true
		case domain.Kind == "default" && out[domain.Service] == "":
			out[domain.Service] = domain.Hostname
		}
	}
	if out[""] == "" && app.Hostname != "" {
		out[""] = app.Hostname
	}
	return out
}

// publicHost is the name the application answers on: its own domain when
// one is ready, else the one MyInfra gave it, as Store.Address reads it.
func publicHost(app store.Application) string {
	fallback := ""
	for _, domain := range app.Domains {
		if domain.Service != "" {
			continue
		}
		if domain.Kind == "custom" && domain.Status == "ready" {
			return domain.Hostname
		}
		if domain.Kind == "default" && fallback == "" {
			fallback = domain.Hostname
		}
	}
	if fallback == "" {
		return app.Hostname
	}
	return fallback
}

// appPackage writes a running application as a package, to keep or share:
// its address becomes {{url}} and {{host}} again, and each secret value a
// {{secret:…}} drawn anew at the next installation.
func appPackage(app store.Application) pack.Package {
	// Each public service's address becomes its token again, the longest
	// first: sonarr.media.example.org before media.example.org.
	type address struct{ host, url, token string }
	var known []address
	for service, host := range domainAddresses(app) {
		suffix := ""
		if service != "" {
			suffix = ":" + service
		}
		known = append(known, address{host, "{{url" + suffix + "}}", "{{host" + suffix + "}}"})
	}
	sort.Slice(known, func(i, j int) bool { return len(known[i].host) > len(known[j].host) })
	general := func(value string) string {
		for _, item := range known {
			value = strings.ReplaceAll(value, "https://"+item.host, item.url)
			value = strings.ReplaceAll(value, item.host, item.token)
		}
		return value
	}
	p := pack.Package{Type: pack.Type, Version: pack.Version, Name: app.Name}
	shared := shareableVars(app.Variables)
	if len(app.Variables) > 0 {
		p.Env = map[string]string{}
	}
	for key := range app.Variables {
		if value, ok := shared[key]; ok {
			p.Env[key] = general(value)
		} else {
			p.Env[key] = "{{secret:" + key + "}}"
		}
	}
	for _, svc := range app.Services {
		out := pack.Service{
			Name: svc.Name, Image: svc.Image, Public: svc.Published, Port: svc.Listen,
			Args: append([]string(nil), svc.Args...),
		}
		if svc.Spec.NoPort {
			// The store keeps a port for every service; this one uses none.
			out.Port = 0
		}
		if svc.Replicas > 1 {
			out.Replicas = svc.Replicas
		}
		out.Service = svc.Spec
		out.Volumes = nil
		if svc.Disk {
			out.Volumes = append(out.Volumes, spec.Volume{Path: svc.DiskPath, SizeGi: svc.DiskGi})
		}
		out.Volumes = append(out.Volumes, svc.Spec.Volumes...)
		if len(svc.Spec.Env) > 0 {
			out.Env = map[string]string{}
			for key, value := range svc.Spec.Env {
				out.Env[key] = general(value)
			}
		}
		out.Files = nil
		for _, file := range svc.Spec.Files {
			out.Files = append(out.Files, spec.File{Path: file.Path, Content: general(file.Content)})
		}
		p.Services = append(p.Services, out)
	}
	// Shared disks are declared once, with their size, as one writes them.
	sizes := map[string]int{}
	for i := range p.Services {
		for j := range p.Services[i].Volumes {
			volume := &p.Services[i].Volumes[j]
			if volume.Disk == "" {
				continue
			}
			if _, seen := sizes[volume.Disk]; !seen {
				p.Disks = append(p.Disks, pack.SharedDisk{Name: volume.Disk, SizeGi: sizeGi(volume.SizeGi)})
			}
			sizes[volume.Disk] = volume.SizeGi
			// The server is this installation's: a package names none.
			volume.SizeGi, volume.Node = 0, ""
		}
	}
	return p
}

func (a *API) applicationPackage(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	p := appPackage(app)
	text, err := pack.YAML(p)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	problems := pack.Check(p)
	if problems == nil {
		problems = []pack.Problem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"package": p, "yaml": string(text), "problems": problems, "address": publicHost(app)})
}

// keepApplicationPackage keeps a running application as one of the person's
// packages, to install it again elsewhere.
func (a *API) keepApplicationPackage(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	p := appPackage(app)
	if problems := pack.Check(p); len(problems) > 0 {
		writeProblems(w, problems)
		return
	}
	a.keepPackage(w, r, p, true)
}

// setServices changes the services of a running application: add, remove,
// rename or change one. What would erase data is listed first and only
// done once confirmed.
func (a *API) setServices(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Services []pack.Service `json:"services"`
		Confirm  bool           `json:"confirm"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := ownerID(r.Context())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.SourceKind != "image" {
		writeError(w, http.StatusConflict, "Cette application ne part pas d'une image.")
		return
	}
	if blocked := rolloutBlocked(app); blocked != "" {
		writeError(w, http.StatusConflict, blocked)
		return
	}
	p := pack.Package{Type: pack.Type, Version: pack.Version, Name: "application", Services: body.Services}
	if problems := pack.Check(p); len(problems) > 0 {
		writeProblems(w, problems)
		return
	}
	expanded, _, err := pack.Expand(p, publicHost(app), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	next, err := packServices(expanded)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.fillFromImages(r.Context(), next, false)
	quietServices(next)
	change, err := planServices(app.Services, next, expanded)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(change.lost) > 0 && !body.Confirm {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "Des données seront effacées : " + strings.Join(change.lost, ", ") + ".",
			"code":  "services_data_loss",
			"lost":  change.lost,
		})
		return
	}
	if err := a.checkLocalImages(r.Context(), owner, serviceImages(next)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.reserveGPU(r.Context(), owner, app.DestinationKind, app.ServerID, app.ClusterID, app.ID, next); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err := a.checkExposed(r.Context(), app.DestinationKind, app.ServerID, app.ClusterID, app.ID, next); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	source := app.Source
	for _, svc := range next {
		if svc.Published {
			source = svc.Image
			break
		}
	}
	if err := a.Store.ReplaceServices(r.Context(), owner, app.ID, source, next); err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), owner, newID(), "app.update", "info", "Services de "+app.Name+" modifiés.", "", app.ClusterID)
	if app.DestinationKind == "cluster" && app.ClusterID != "" {
		go a.deliverServices(owner, app.ClusterID, app.ID, change)
		if exposes(app.Services) || exposes(next) {
			go a.openFirewall(owner, app.ClusterID)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "updating"})
}

// serviceChange is what a change of services leaves behind on a cluster:
// the workloads of services gone, and the disks the person agreed to erase.
type serviceChange struct {
	gone   []string
	claims []claimGone
	lost   []string
	// silent lost their port: their Kubernetes Service has no reason left.
	silent []string
}

// claimGone is a disk to erase: its claim, or the claims of a StatefulSet
// copy after copy (prefix).
type claimGone struct {
	name, prefix string
}

// planServices matches the services asked to the running ones by name and
// says what goes. A service keeps its identity, backup and database; what
// cannot change in place is refused.
func planServices(current, next []store.Service, asked []pack.Service) (serviceChange, error) {
	var change serviceChange
	before := map[string]store.Service{}
	// A shared disk stays on the server it was placed on.
	nodes := map[string]string{}
	for _, svc := range current {
		before[svc.Name] = svc
		for _, volume := range svc.Spec.Volumes {
			if volume.Disk != "" && volume.Node != "" {
				nodes[volume.Disk] = volume.Node
			}
		}
	}
	for i := range next {
		for j := range next[i].Spec.Volumes {
			volume := &next[i].Spec.Volumes[j]
			if volume.Disk != "" && volume.Node == "" {
				volume.Node = nodes[volume.Disk]
			}
		}
	}
	kept := map[string]bool{}
	for i := range next {
		svc := &next[i]
		prev, ok := before[svc.Name]
		if !ok {
			continue
		}
		kept[svc.Name] = true
		svc.ID, svc.Backup, svc.Database = prev.ID, prev.Backup, prev.Database
		if asked[i].Replicas == 0 {
			svc.Replicas = prev.Replicas
		} else if svc.Replicas != prev.Replicas {
			return change, fmt.Errorf("Les copies de %s se changent depuis la redondance de l'application.", svc.Name)
		}
		guarded := prev.Backup != "" || prev.Database != ""
		if guarded && (svc.Disk != prev.Disk || svc.DiskPath != prev.DiskPath) {
			return change, fmt.Errorf("Le disque de %s est sauvegardé ou répliqué : il ne change pas ici.", svc.Name)
		}
		if prev.Disk && svc.Disk && prev.DiskGi != svc.DiskGi {
			return change, fmt.Errorf("La taille du disque de %s ne change pas après sa création.", svc.Name)
		}
		if svc.Spec.NoPort && !prev.Spec.NoPort {
			change.silent = append(change.silent, svc.Name)
		}
		stateful := prev.Disk && prev.Replicas > 1
		if prev.Disk && !svc.Disk {
			change.lost = append(change.lost, svc.Name+" ("+prev.DiskPath+")")
			change.claims = append(change.claims, mainClaim(prev.Name, stateful))
		}
		// Extra disks are known by their place: one in the middle cannot go
		// without the next ones taking its data.
		for j, old := range prev.Spec.Volumes {
			if j >= len(svc.Spec.Volumes) {
				// A shared disk goes only once no service mounts it: below.
				if old.Disk == "" {
					change.lost = append(change.lost, svc.Name+" ("+old.Path+")")
					change.claims = append(change.claims, extraClaim(prev.Name, j, stateful))
				}
				continue
			}
			now := svc.Spec.Volumes[j]
			if now.Disk != old.Disk {
				return change, fmt.Errorf("Le disque %s de %s ne passe pas d'un disque à un autre : retirez-le, puis ajoutez le nouveau.", old.Path, svc.Name)
			}
			if now.Path != old.Path {
				for k, other := range prev.Spec.Volumes {
					if k != j && other.Path == now.Path {
						return change, fmt.Errorf("Retirez les disques en plus de %s en partant du dernier.", svc.Name)
					}
				}
			}
			if sizeGi(now.SizeGi) != sizeGi(old.SizeGi) {
				return change, fmt.Errorf("La taille du disque %s de %s ne change pas après sa création.", old.Path, svc.Name)
			}
		}
	}
	for _, prev := range current {
		if kept[prev.Name] {
			continue
		}
		if prev.Backup != "" || prev.Database != "" {
			return change, fmt.Errorf("Le service %s garde ses données répliquées : il ne se retire pas ici.", prev.Name)
		}
		change.gone = append(change.gone, prev.Name)
		stateful := prev.Disk && prev.Replicas > 1
		if prev.Disk {
			change.lost = append(change.lost, prev.Name+" ("+prev.DiskPath+")")
			change.claims = append(change.claims, mainClaim(prev.Name, stateful))
		}
		for j, old := range prev.Spec.Volumes {
			if old.Disk != "" {
				continue
			}
			change.lost = append(change.lost, prev.Name+" ("+old.Path+")")
			change.claims = append(change.claims, extraClaim(prev.Name, j, stateful))
		}
	}
	// A shared disk is erased with the last service that mounts it.
	after := sharedNames(next)
	for _, name := range sortedSet(sharedNames(current)) {
		if !after[name] {
			change.lost = append(change.lost, "disque partagé "+name)
			change.claims = append(change.claims, claimGone{name: manifest.SharedClaim(name)})
		}
	}
	return change, nil
}

func sharedNames(services []store.Service) map[string]bool {
	out := map[string]bool{}
	for _, svc := range services {
		for _, volume := range svc.Spec.Volumes {
			if volume.Disk != "" {
				out[volume.Disk] = true
			}
		}
	}
	return out
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sizeGi(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// mainClaim and extraClaim name the claims manifest.Render gives a disk.
func mainClaim(service string, stateful bool) claimGone {
	if stateful {
		return claimGone{prefix: "data-" + service + "-"}
	}
	return claimGone{name: service}
}

func extraClaim(service string, index int, stateful bool) claimGone {
	if stateful {
		return claimGone{prefix: fmt.Sprintf("v%d-%s-", index+1, service)}
	}
	return claimGone{name: fmt.Sprintf("%s.v%d", service, index+1)}
}

// deliverServices applies the new services on the cluster, then removes
// what the services gone left there, and the disks the person let go.
func (a *API) deliverServices(owner, clusterID, appID string, change serviceChange) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a.deliverClusters(ctx)
	if len(change.gone) == 0 && len(change.claims) == 0 && len(change.silent) == 0 {
		return
	}
	host, err := a.clusterHost(ctx, owner, clusterID)
	if err != nil {
		slog.Info("services", "app", appID, "err", err)
		return
	}
	ns := manifest.Namespace(appID)
	kubectl := "k3s kubectl -n " + ns + " "
	for _, name := range change.silent {
		_, _ = deploy.SSH(ctx, host, kubectl+"delete svc "+name+" --ignore-not-found --wait=false")
	}
	for _, name := range change.gone {
		_, _ = deploy.SSH(ctx, host, kubectl+"delete deploy,statefulset,svc,pdb "+name+" --ignore-not-found --wait=false")
		_, _ = deploy.SSH(ctx, host, kubectl+"delete svc "+name+"-copies --ignore-not-found --wait=false")
		_, _ = deploy.SSH(ctx, host, kubectl+"delete configmap "+name+".files backup-"+name+" --ignore-not-found --wait=false")
		_, _ = deploy.SSH(ctx, host, kubectl+"delete secret backup-"+name+" --ignore-not-found --wait=false")
	}
	var names []string
	var prefixes []string
	for _, claim := range change.claims {
		if claim.name != "" {
			names = append(names, claim.name)
		} else {
			prefixes = append(prefixes, claim.prefix)
		}
	}
	if len(prefixes) > 0 {
		out, err := deploy.SSH(ctx, host, kubectl+"get pvc -o name")
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				name := strings.TrimPrefix(strings.TrimSpace(line), "persistentvolumeclaim/")
				for _, prefix := range prefixes {
					if name != "" && strings.HasPrefix(name, prefix) {
						names = append(names, name)
					}
				}
			}
		}
	}
	if len(names) > 0 {
		_, _ = deploy.SSH(ctx, host, kubectl+"delete pvc "+strings.Join(names, " ")+" --ignore-not-found --wait=false")
	}
}
