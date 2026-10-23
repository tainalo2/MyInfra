package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/pack"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

// publicHost is the name the application answers on: its own domain when
// one is ready, else the one MyInfra gave it.
func publicHost(app store.Application) string {
	fallback := app.Hostname
	for _, domain := range app.Domains {
		if domain.Kind == "custom" && domain.Status == "ready" {
			return domain.Hostname
		}
		if domain.Kind == "default" && fallback == "" {
			fallback = domain.Hostname
		}
	}
	return fallback
}

// appPackage writes a running application as a package, to keep or share:
// its address becomes {{url}} and {{host}} again, and each secret value a
// {{secret:…}} drawn anew at the next installation.
func appPackage(app store.Application) pack.Package {
	host := publicHost(app)
	general := func(value string) string {
		if host == "" {
			return value
		}
		value = strings.ReplaceAll(value, "https://"+host, "{{url}}")
		return strings.ReplaceAll(value, host, "{{host}}")
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
	writeJSON(w, http.StatusOK, map[string]any{"package": p, "yaml": string(text), "problems": problems})
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
	source := app.Source
	for _, svc := range next {
		if svc.Published {
			source = svc.Image
		}
	}
	if err := a.Store.ReplaceServices(r.Context(), owner, app.ID, source, next); err != nil {
		a.mapErr(w, err)
		return
	}
	a.note(r.Context(), owner, newID(), "app.update", "info", "Services de "+app.Name+" modifiés.", "", app.ClusterID)
	if app.DestinationKind == "cluster" && app.ClusterID != "" {
		go a.deliverServices(owner, app.ClusterID, app.ID, change)
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
	for _, svc := range current {
		before[svc.Name] = svc
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
				change.lost = append(change.lost, svc.Name+" ("+old.Path+")")
				change.claims = append(change.claims, extraClaim(prev.Name, j, stateful))
				continue
			}
			now := svc.Spec.Volumes[j]
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
			change.lost = append(change.lost, prev.Name+" ("+old.Path+")")
			change.claims = append(change.claims, extraClaim(prev.Name, j, stateful))
		}
	}
	return change, nil
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
