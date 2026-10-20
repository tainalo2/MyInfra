package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/imagefile"
	"myinfra/internal/manifest"
	"myinfra/internal/market"
	"myinfra/internal/registry"
	"myinfra/internal/store"
	"myinfra/internal/usage"
)

var (
	rolloutMu   sync.Mutex
	rolloutBusy = map[string]bool{}
)

func (a *API) catalogueVersions(w http.ResponseWriter, r *http.Request) {
	card, ok := market.CardOf(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Cet article est introuvable.")
		return
	}
	if len(card.Services) == 0 {
		writeError(w, http.StatusNotFound, "Cet article est introuvable.")
		return
	}
	published := card.Services[0]
	for _, svc := range card.Services {
		if svc.Published {
			published = svc
			break
		}
	}
	tags, err := registry.Tags(r.Context(), published.Image)
	detail := ""
	if err != nil {
		detail = "Les versions n'ont pas pu être lues."
		if tag := registry.Tag(published.Image); tag != "" {
			tags = []string{tag}
		}
	}
	if tags == nil {
		tags = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": published.Name, "image": published.Image, "tags": tags, "detail": detail,
	})
}

func (a *API) applicationReleases(w http.ResponseWriter, r *http.Request) {
	app, err := a.Store.Application(r.Context(), userID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	type view struct {
		Name   string   `json:"name"`
		Image  string   `json:"image"`
		Update string   `json:"update,omitempty"`
		Tags   []string `json:"tags"`
		Detail string   `json:"detail,omitempty"`
	}
	out := make([]view, len(app.Services))
	var group sync.WaitGroup
	for i, svc := range app.Services {
		group.Add(1)
		go func(i int, svc store.Service) {
			defer group.Done()
			item := view{Name: svc.Name, Image: svc.Image, Tags: []string{}}
			var tags []string
			var tagErr error
			if imagefile.IsLocal(svc.Image) {
				// The versions are the ones brought to MyInfra under this name.
				tags = registry.Choose(a.localTags(r.Context(), userID(r.Context()), svc.Image))
			} else {
				tags, tagErr = registry.Tags(r.Context(), svc.Image)
			}
			if tagErr != nil {
				item.Detail = "Les versions n'ont pas pu être lues."
			} else {
				item.Tags = tags
				if next := registry.Suggest(svc.Image, tags); next != "" {
					if updated, withErr := registry.WithTag(svc.Image, next); withErr == nil {
						item.Update = updated
					}
				}
			}
			out[i] = item
		}(i, svc)
	}
	group.Wait()
	if out == nil {
		out = []view{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": out})
}

func (a *API) replaceImages(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Services []struct {
			Name  string `json:"name"`
			Image string `json:"image"`
		} `json:"services"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	app, err := a.Store.Application(r.Context(), userID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if app.SourceKind != "image" {
		writeError(w, http.StatusConflict, "Cette application ne part pas d'une image.")
		return
	}
	if app.Rollout == "updating" {
		writeError(w, http.StatusConflict, "Une mise à jour est déjà en cours.")
		return
	}
	if len(app.Services) == 0 || len(body.Services) != len(app.Services) {
		writeError(w, http.StatusBadRequest, "Indiquez chaque image.")
		return
	}
	current := map[string]string{}
	previous := make([]store.ImageSnap, 0, len(app.Services))
	for _, svc := range app.Services {
		current[svc.Name] = svc.Image
		previous = append(previous, store.ImageSnap{Name: svc.Name, Image: svc.Image})
	}
	images := map[string]string{}
	for _, item := range body.Services {
		image := strings.TrimSpace(item.Image)
		if _, ok := current[item.Name]; !ok || images[item.Name] != "" {
			writeError(w, http.StatusBadRequest, "Indiquez chaque image.")
			return
		}
		if !acceptableImage(image) {
			writeError(w, http.StatusBadRequest, "Indiquez le chemin de l'image, par exemple ghostfolio/ghostfolio:2.168.0.")
			return
		}
		images[item.Name] = image
	}
	wanted := make([]string, 0, len(images))
	for _, image := range images {
		wanted = append(wanted, image)
	}
	if err := a.checkLocalImages(r.Context(), userID(r.Context()), wanted); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	changed := false
	for name, image := range images {
		if image != current[name] {
			changed = true
		}
	}
	if !changed {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	if err := a.Store.BeginRollout(r.Context(), app.ID, app.RunStatus, previous); err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.SetServiceImages(r.Context(), app.ID, images); err != nil {
		_ = a.Store.FinishRollout(r.Context(), app.ID, "", "", "")
		a.mapErr(w, err)
		return
	}
	a.startRollout(app.ID)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "updating"})
}

func (a *API) startRollout(id string) {
	rolloutMu.Lock()
	if rolloutBusy[id] {
		rolloutMu.Unlock()
		return
	}
	rolloutBusy[id] = true
	rolloutMu.Unlock()
	go func() {
		defer func() {
			rolloutMu.Lock()
			delete(rolloutBusy, id)
			rolloutMu.Unlock()
		}()
		a.watchRollout(id)
	}()
}

func (a *API) resumeRollouts() {
	if a.Replica {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ids, started, err := a.Store.PendingRollouts(ctx)
	if err != nil {
		return
	}
	for i, id := range ids {
		if time.Since(started[i]) > 4*time.Minute {
			go a.restoreRollout(id)
			continue
		}
		a.startRollout(id)
	}
}

func (a *API) watchRollout(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	app, err := a.Store.RolloutApp(ctx, id)
	if err != nil {
		return
	}
	if err := a.applyRollout(ctx, app); err != nil || !a.imagesReady(ctx, app) {
		a.restoreRollout(id)
		return
	}
	_ = a.Store.FinishRollout(context.Background(), id, "", "", "running")
}

func (a *API) restoreRollout(id string) {
	if a.Replica {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	status, images, err := a.Store.RolloutImages(ctx, id)
	detail := "La nouvelle image n'a pas démarré. L'image précédente a été rétablie."
	if err != nil {
		_ = a.Store.FinishRollout(ctx, id, "rolled_back", detail, "")
		return
	}
	back := map[string]string{}
	for _, item := range images {
		back[item.Name] = item.Image
	}
	if err := a.Store.SetServiceImages(ctx, id, back); err == nil {
		if app, loadErr := a.Store.RolloutApp(ctx, id); loadErr == nil {
			_ = a.applyRollout(ctx, app)
		}
	}
	if status == "" {
		status = "failed"
	}
	_ = a.Store.FinishRollout(ctx, id, "rolled_back", detail, status)
}

func (a *API) applyRollout(ctx context.Context, app store.RolloutApp) error {
	if app.ClusterID == "" {
		return nil
	}
	apps, err := a.Store.ClusterApps(ctx, app.ClusterID)
	if err != nil {
		return err
	}
	var found *store.ClusterApp
	for i := range apps {
		if apps[i].ID == app.ID {
			found = &apps[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("application absente")
	}
	host, err := a.clusterHost(ctx, app.OwnerID, app.ClusterID)
	if err != nil {
		return err
	}
	parts, ok := a.clusterServices(ctx, host, app.OwnerID, app.ClusterID, *found)
	if !ok {
		return fmt.Errorf("stockage du cluster indisponible")
	}
	env, err := a.openVars(found.Env)
	if err != nil {
		return err
	}
	spec := manifest.App{ID: found.ID, Paused: found.Paused, Hosts: found.Hosts, Zones: found.Zones, Env: env,
		Services: parts.Services, Databases: parts.Databases, Secrets: parts.Secrets}
	if err := a.attachShared(ctx, app.OwnerID, found.ID, &spec); err != nil {
		return err
	}
	if !manifest.Deployable(spec) {
		return fmt.Errorf("manifeste refusé")
	}
	raw := manifest.Render(spec)
	if !strings.HasSuffix(raw, "\n") {
		raw += "\n"
	}
	_, err = deploy.SSH(ctx, host, deploy.KubectlApply(raw))
	return err
}

func (a *API) imagesReady(ctx context.Context, app store.RolloutApp) bool {
	if app.ClusterID != "" {
		host, err := a.clusterHost(ctx, app.OwnerID, app.ClusterID)
		if err != nil {
			return false
		}
		return a.clusterImagesReady(ctx, host, app)
	}
	return a.serverImagesReady(ctx, app)
}

func (a *API) clusterImagesReady(ctx context.Context, host deploy.Host, app store.RolloutApp) bool {
	want := map[string]string{}
	for _, svc := range app.Services {
		want[svc.Name] = svc.Image
	}
	ns := manifest.Namespace(app.ID)
	deadline := time.Now().Add(3 * time.Minute)
	for {
		ready, failed := a.clusterRollout(ctx, host, ns, want)
		if failed {
			return false
		}
		if ready {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(5 * time.Second):
		}
	}
}

func (a *API) clusterRollout(ctx context.Context, host deploy.Host, namespace string, want map[string]string) (ready, failed bool) {
	out, err := deploy.SSH(ctx, host, fmt.Sprintf("k3s kubectl -n '%s' get deploy,sts,pods -o json", namespace))
	if err != nil || strings.TrimSpace(out) == "" {
		return false, false
	}
	var listed struct {
		Items []struct {
			Kind     string `json:"kind"`
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Replicas *int `json:"replicas"`
				Template struct {
					Spec struct {
						Containers []struct {
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
			Status struct {
				Phase             string `json:"phase"`
				UpdatedReplicas   int    `json:"updatedReplicas"`
				AvailableReplicas int    `json:"availableReplicas"`
				ReadyReplicas     int    `json:"readyReplicas"`
				ContainerStatuses []struct {
					Image string `json:"image"`
					State struct {
						Waiting struct {
							Reason string `json:"reason"`
						} `json:"waiting"`
						Terminated struct {
							Reason string `json:"reason"`
						} `json:"terminated"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		return false, false
	}
	workloads := 0
	settled := 0
	for _, item := range listed.Items {
		if item.Kind == "Pod" {
			if podFailedFor(want, item.Status.ContainerStatuses) {
				return false, true
			}
			continue
		}
		if item.Kind != "Deployment" && item.Kind != "StatefulSet" {
			continue
		}
		workloads++
		image := ""
		if len(item.Spec.Template.Spec.Containers) > 0 {
			image = item.Spec.Template.Spec.Containers[0].Image
		}
		if !sameImage(image, want[item.Metadata.Name]) {
			continue
		}
		copies := 1
		if item.Spec.Replicas != nil && *item.Spec.Replicas > 0 {
			copies = *item.Spec.Replicas
		}
		readyCopies := item.Status.AvailableReplicas
		if item.Kind == "StatefulSet" {
			readyCopies = item.Status.ReadyReplicas
		}
		if item.Status.UpdatedReplicas >= copies && readyCopies >= copies {
			settled++
		}
	}
	return workloads > 0 && settled == workloads, false
}

func podFailedFor(want map[string]string, containers []struct {
	Image string `json:"image"`
	State struct {
		Waiting struct {
			Reason string `json:"reason"`
		} `json:"waiting"`
		Terminated struct {
			Reason string `json:"reason"`
		} `json:"terminated"`
	} `json:"state"`
}) bool {
	for _, container := range containers {
		matches := false
		for _, image := range want {
			if sameImage(container.Image, image) {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		pod := usage.Pod{Reason: container.State.Waiting.Reason}
		if pod.Reason == "" {
			pod.Reason = container.State.Terminated.Reason
		}
		if usage.PodFailed(pod) {
			return true
		}
	}
	return false
}

func (a *API) serverImagesReady(ctx context.Context, app store.RolloutApp) bool {
	if app.ServerID == "" {
		return false
	}
	server, err := a.Store.Server(ctx, app.OwnerID, app.ServerID)
	if err != nil || server.Status != "linked" || server.Address == "" {
		return false
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, server.ID)
	if err != nil {
		return false
	}
	key := a.privateKey(ctx, app.OwnerID, keyIDs)
	if key == "" {
		return false
	}
	host := deploy.Host{Address: server.Address, PrivateKey: key}
	want := map[string]string{}
	var names []string
	for _, svc := range app.Services {
		for _, name := range rolloutContainers(app.ID, svc) {
			names = append(names, name)
			want[name] = svc.Image
		}
	}
	if len(names) == 0 {
		return false
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		ready, failed := a.inspectImages(ctx, host, names, want)
		if failed {
			return false
		}
		if ready {
			return true
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(8 * time.Second):
		}
	}
}

func (a *API) inspectImages(ctx context.Context, host deploy.Host, names []string, want map[string]string) (ready, failed bool) {
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "docker inspect -f '{{.State.Status}} {{.RestartCount}} {{.Config.Image}}' '%s' 2>/dev/null || echo 'missing 0'\n", name)
	}
	out, err := deploy.SSH(ctx, host, b.String())
	if err != nil {
		return false, false
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < len(names) {
		return false, false
	}
	ready = true
	for i, name := range names {
		fields := strings.Fields(lines[i])
		if len(fields) < 3 {
			return false, false
		}
		status, restarts, image := fields[0], fields[1], fields[2]
		if !sameImage(image, want[name]) {
			ready = false
			continue
		}
		if status == "running" {
			continue
		}
		ready = false
		if status == "exited" || status == "dead" || restarts != "0" {
			return false, true
		}
	}
	return ready, false
}

func rolloutContainers(appID string, svc store.Service) []string {
	replicas := svc.Replicas
	if svc.Disk || replicas < 1 {
		replicas = 1
	}
	base := "myinfra-" + strings.ReplaceAll(appID, "-", "") + "-" + svc.Name
	if replicas == 1 {
		return []string{base}
	}
	names := make([]string, replicas)
	for i := 1; i <= replicas; i++ {
		names[i-1] = fmt.Sprintf("%s-%d", base, i)
	}
	return names
}

func sameImage(have, want string) bool {
	have = strings.TrimPrefix(strings.TrimPrefix(have, "docker.io/"), "index.docker.io/")
	want = strings.TrimPrefix(strings.TrimPrefix(want, "docker.io/"), "index.docker.io/")
	return have == want
}
