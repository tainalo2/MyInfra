package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"myinfra/internal/imagefile"
	"myinfra/internal/registry"
	"myinfra/internal/store"
)

// How often each kind of update is looked for. Nothing is applied: a person
// sees what is available and chooses when.
const (
	appsEvery    = 6 * time.Hour
	myinfraEvery = 6 * time.Hour
	serversEvery = 12 * time.Hour
)

type updateClock struct {
	mu       sync.Mutex
	last     map[string]time.Time
	checking map[string]bool
}

var updates = updateClock{last: map[string]time.Time{}, checking: map[string]bool{}}

// Updates looks for updates in the background, each kind at its own pace.
func (a *API) Updates(ctx context.Context) {
	a.resumeUpdateJobs()
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	a.checkDue(ctx, false)
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// A job whose runner stopped, after an error of MyInfra itself, goes on.
			a.resumeUpdateJobs()
			a.checkDue(ctx, false)
		}
	}
}

// checkDue runs the checks whose time has come, or all of them when forced.
func (a *API) checkDue(ctx context.Context, force bool) {
	checks := []struct {
		kind  string
		every time.Duration
		run   func(context.Context) error
	}{
		{"myinfra", myinfraEvery, a.checkMyInfra},
		{"app", appsEvery, a.checkApps},
		{"server", serversEvery, a.checkServers},
		{"library", libraryEvery, a.checkLibrary},
	}
	var group sync.WaitGroup
	for _, check := range checks {
		updates.mu.Lock()
		due := force || time.Since(updates.last[check.kind]) >= check.every
		if !due || updates.checking[check.kind] {
			updates.mu.Unlock()
			continue
		}
		updates.checking[check.kind] = true
		updates.mu.Unlock()
		group.Add(1)
		go func() {
			defer group.Done()
			err := check.run(ctx)
			updates.mu.Lock()
			defer updates.mu.Unlock()
			updates.checking[check.kind] = false
			if err != nil {
				slog.Warn("recherche de mises à jour", "kind", check.kind, "err", err)
				return
			}
			updates.last[check.kind] = time.Now()
		}()
	}
	group.Wait()
}

func checkingUpdates() bool {
	updates.mu.Lock()
	defer updates.mu.Unlock()
	for _, busy := range updates.checking {
		if busy {
			return true
		}
	}
	return false
}

// serviceUpdate is a new image for one service of an application.
type serviceUpdate struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Next  string `json:"next"`
	Level string `json:"level"`
}

type appUpdateDetail struct {
	Services []serviceUpdate `json:"services"`
}

// levelRank orders the levels of an update from the most to the least work.
var levelRank = map[string]int{"major": 5, "minor": 4, "patch": 3, "build": 2, "security": 6, "reboot": 6, "packages": 1}

func (a *API) checkApps(ctx context.Context) error {
	owner, _, err := a.Store.User(ctx)
	if err != nil {
		return nil
	}
	apps, err := a.Store.ListApplications(ctx, owner)
	if err != nil {
		return err
	}
	var found []store.Update
	for _, row := range apps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		app, err := a.Store.Application(ctx, owner, row.ID)
		if err != nil || app.SourceKind != "image" {
			continue
		}
		var detail appUpdateDetail
		for _, svc := range app.Services {
			if next, level := a.nextImage(ctx, svc.Image); next != "" {
				detail.Services = append(detail.Services, serviceUpdate{Name: svc.Name, Image: svc.Image, Next: next, Level: level})
			}
		}
		if len(detail.Services) == 0 {
			continue
		}
		top := detail.Services[0]
		for _, item := range detail.Services[1:] {
			if levelRank[item.Level] > levelRank[top.Level] {
				top = item
			}
		}
		raw, _ := json.Marshal(detail)
		found = append(found, store.Update{
			TargetID: app.ID, OwnerID: owner, Current: registry.Unpinned(top.Image), Available: registry.Unpinned(top.Next), Level: top.Level, Detail: raw,
		})
	}
	return a.Store.SaveUpdates(ctx, "app", found, nil)
}

// nextImage finds what an image can be updated to: a newer version of the
// same flavor, or else a new build of the same tag.
func (a *API) nextImage(ctx context.Context, image string) (string, string) {
	if imagefile.IsLocal(image) {
		return "", ""
	}
	if tags, err := registry.Tags(ctx, image); err == nil {
		if tag := registry.Suggest(image, tags); tag != "" {
			if next, err := registry.WithTag(image, tag); err == nil {
				return next, registry.Level(image, next)
			}
		}
	}
	remote, err := registry.Digest(ctx, image)
	if err != nil {
		return "", ""
	}
	have := registry.DigestOf(image)
	if have == "" {
		if have, err = a.Store.ImageBaseline(ctx, registry.Unpinned(image), remote); err != nil {
			return "", ""
		}
	}
	if have == remote {
		return "", ""
	}
	next, err := registry.Pinned(image, remote)
	if err != nil {
		return "", ""
	}
	return next, "build"
}

type updateView struct {
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Current   string          `json:"current"`
	Available string          `json:"available"`
	Level     string          `json:"level"`
	Detail    json.RawMessage `json:"detail"`
	CheckedAt time.Time       `json:"checkedAt"`
}

func (a *API) listUpdates(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	rows, err := a.Store.Updates(r.Context(), owner)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	names := map[string]string{}
	if apps, err := a.Store.ListApplications(r.Context(), owner); err == nil {
		for _, app := range apps {
			names["app:"+app.ID] = app.Name
		}
	}
	if servers, err := a.Store.ListServers(r.Context(), owner); err == nil {
		for _, server := range servers {
			names["server:"+server.ID] = server.Name
		}
	}
	if packages, err := a.Store.ListPackages(r.Context(), owner); err == nil {
		for _, p := range packages {
			names["library:package:"+p.ID] = p.Name
		}
	}
	if templates, err := a.Store.ListTemplates(r.Context(), owner); err == nil {
		for _, t := range templates {
			names["library:template:"+t.ID] = t.Name
		}
	}
	out := []updateView{}
	for _, row := range rows {
		name := names[row.Kind+":"+row.TargetID]
		if row.Kind == "myinfra" {
			name = "MyInfra"
		} else if name == "" {
			// The target was removed since the check.
			continue
		}
		if row.Kind == "server" {
			// A server that no longer answers, or is gone, cannot be updated.
			if state, _ := a.reachOf(row.TargetID); state != "" {
				continue
			}
		}
		out = append(out, updateView{
			Kind: row.Kind, ID: row.TargetID, Name: name, Current: row.Current, Available: row.Available,
			Level: row.Level, Detail: row.Detail, CheckedAt: row.CheckedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindOrder(out[i].Kind) < kindOrder(out[j].Kind)
		}
		return out[i].Name < out[j].Name
	})
	updates.mu.Lock()
	var last time.Time
	for _, at := range updates.last {
		if at.After(last) {
			last = at
		}
	}
	updates.mu.Unlock()
	view := map[string]any{"updates": out, "checking": checkingUpdates(), "version": a.versionView()}
	if !last.IsZero() {
		view["checkedAt"] = last
	}
	writeJSON(w, http.StatusOK, view)
}

func kindOrder(kind string) int {
	switch kind {
	case "myinfra":
		return 0
	case "app":
		return 1
	case "server":
		return 2
	default:
		return 3
	}
}

func (a *API) checkUpdatesNow(w http.ResponseWriter, r *http.Request) {
	if a.Replica {
		writeError(w, http.StatusConflict, "Cette copie de MyInfra ne pilote pas les machines.")
		return
	}
	go a.checkDue(context.Background(), true)
	writeJSON(w, http.StatusAccepted, map[string]bool{"checking": true})
}

// updateApplication applies the update found for an application.
func (a *API) updateApplication(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	app, err := a.Store.Application(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	found, err := a.Store.UpdateOf(r.Context(), "app", app.ID)
	if err != nil {
		writeError(w, http.StatusConflict, "Aucune mise à jour n'est disponible pour cette application.")
		return
	}
	var detail appUpdateDetail
	if err := json.Unmarshal(found.Detail, &detail); err != nil || len(detail.Services) == 0 {
		writeError(w, http.StatusConflict, "Aucune mise à jour n'est disponible pour cette application.")
		return
	}
	images := map[string]string{}
	for _, item := range detail.Services {
		for _, svc := range app.Services {
			// The image changed since the check: the person chose another one.
			if svc.Name == item.Name && svc.Image == item.Image {
				images[svc.Name] = item.Next
			}
		}
	}
	if len(images) == 0 {
		_ = a.Store.ForgetUpdate(r.Context(), "app", app.ID)
		writeError(w, http.StatusConflict, "L'image a changé depuis la dernière recherche. Relancez la recherche.")
		return
	}
	a.rollImages(w, r, app, images)
}
