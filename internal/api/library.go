package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"myinfra/internal/blueprint"
	"myinfra/internal/library"
	"myinfra/internal/market"
	"myinfra/internal/pack"
	"myinfra/internal/selfupdate"
	"myinfra/internal/store"
)

// libraryEvery is how often the copy that drives looks for a new release of
// the library; the other copies follow it through the database.
const libraryEvery = 6 * time.Hour

// Library follows the release of the library this installation adopted:
// at start, then each time the copy that fetches keeps a newer one.
func (a *API) Library(ctx context.Context) {
	a.adoptKept(ctx)
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.adoptKept(ctx)
		}
	}
}

// adoptKept reads the release kept in the database when it is newer than
// the library proposed now. It is checked again, as at download.
func (a *API) adoptKept(ctx context.Context) {
	if library.FromDir() != "" {
		return
	}
	repo := library.Repo()
	name, err := a.Store.LibraryReleaseName(ctx, repo)
	if err != nil || name == "" || !selfupdate.Newer(name, library.Current().Release) {
		return
	}
	row, err := a.Store.LibraryRelease(ctx, repo)
	if err != nil {
		return
	}
	lib, err := library.Open(library.Bundle{Release: row.Release, Archive: row.Archive, Sums: row.Sums, Signature: row.Signature}, library.Keys(), library.Engine)
	if err != nil {
		slog.Warn("bibliothèque gardée refusée", "release", row.Release, "err", err)
		return
	}
	library.Set(lib)
	slog.Info("bibliothèque", "release", lib.Release, "left", len(lib.Left))
}

// checkLibrary fetches a newer release of the library, then offers the new
// versions of the packages and templates people took from it.
func (a *API) checkLibrary(ctx context.Context) error {
	if library.FromDir() == "" {
		if err := a.fetchLibrary(ctx); err != nil {
			// The library proposed now stays; the copies are still compared to it.
			slog.Warn("bibliothèque", "err", err)
		}
	}
	return a.checkLibraryCopies(ctx)
}

func (a *API) fetchLibrary(ctx context.Context) error {
	repo := library.Repo()
	release, assets, err := library.Latest(ctx, repo)
	if err != nil {
		return err
	}
	if !selfupdate.Newer(release, library.Current().Release) {
		return nil
	}
	b, err := library.Download(ctx, release, assets)
	if err != nil {
		return err
	}
	lib, err := library.Open(b, library.Keys(), library.Engine)
	if err != nil {
		return err
	}
	err = a.Store.SaveLibraryRelease(ctx, store.LibraryRelease{Repo: repo, Release: lib.Release, Archive: b.Archive, Sums: b.Sums, Signature: b.Signature})
	if err != nil {
		return err
	}
	library.Set(lib)
	slog.Info("nouvelle bibliothèque", "release", lib.Release, "left", len(lib.Left))
	return nil
}

// libraryUpdate is what the updates page says of a copy.
type libraryUpdate struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	// Item is the id of the package or template in the library.
	Item    string `json:"item"`
	Changes string `json:"changes,omitempty"`
	// Edited is true when the person changed their copy since they took it.
	Edited bool `json:"edited"`
}

func (a *API) checkLibraryCopies(ctx context.Context) error {
	copies, err := a.Store.LibraryCopies(ctx)
	if err != nil {
		return err
	}
	lib := library.Current()
	var found []store.Update
	for _, row := range copies {
		var origin library.Origin
		if json.Unmarshal(row.Library, &origin) != nil {
			continue
		}
		version, item := lib.Newest(origin)
		if version == "" {
			continue
		}
		detail, _ := json.Marshal(libraryUpdate{
			Kind: row.Kind, ID: row.ID, Item: origin.ID, Changes: item.Changes, Edited: bodySum(row.Body) != origin.Sum,
		})
		found = append(found, store.Update{
			TargetID: row.Kind + ":" + row.ID, OwnerID: row.OwnerID, Current: origin.Version, Available: version,
			Level: library.Level(origin.Version, version), Detail: detail,
		})
	}
	return a.Store.SaveUpdates(ctx, "library", found, nil)
}

// bodySum is the sum of a body as JSON, the same however the database wrote
// it: a copy is edited only when what it says changed.
func bodySum(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return library.Sum(raw)
	}
	canon, err := json.Marshal(v)
	if err != nil {
		return library.Sum(raw)
	}
	return library.Sum(canon)
}

// libraryMark tells a package or template that came from the library.
type libraryMark struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// Newer is the version the library offers, "" when the copy is up to date.
	Newer  string `json:"newer,omitempty"`
	Edited bool   `json:"edited"`
}

func markOf(origin, body []byte) *libraryMark {
	var o library.Origin
	if len(origin) == 0 || json.Unmarshal(origin, &o) != nil || o.Kind == "" {
		return nil
	}
	newer, _ := library.Current().Newest(o)
	return &libraryMark{ID: o.ID, Version: o.Version, Newer: newer, Edited: bodySum(body) != o.Sum}
}

// libraryInfo says which library MyInfra proposes.
func (a *API) libraryInfo(w http.ResponseWriter, r *http.Request) {
	lib := library.Current()
	writeJSON(w, http.StatusOK, map[string]any{
		"repo": library.Repo(), "release": lib.Release, "packages": len(lib.Packages),
		"templates": len(lib.Templates), "left": lib.Left, "dir": library.FromDir() != "",
	})
}

// libraryLogo serves a logo the library carries.
func (a *API) libraryLogo(w http.ResponseWriter, r *http.Request) {
	lib := library.Current()
	raw, kind, ok := lib.Logo(r.PathValue("slug"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	tag := `"` + lib.Release + `"`
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", kind)
	// A logo is shown, never run: an SVG keeps no script.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = w.Write(raw)
}

// updateLibraryCopy puts the version the library offers in place of a
// person's package or template. The applications installed from it do not
// change: they were copied when installed.
func (a *API) updateLibraryCopy(w http.ResponseWriter, r *http.Request) {
	owner := ownerID(r.Context())
	kind, id := r.PathValue("kind"), r.PathValue("id")
	var err error
	switch kind {
	case "package":
		err = a.updatePackageCopy(r.Context(), owner, id)
	case "template":
		err = a.updateTemplateCopy(r, owner, id)
	default:
		writeError(w, http.StatusNotFound, "Introuvable.")
		return
	}
	if errors.Is(err, errUpToDate) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		a.mapErr(w, err)
		return
	}
	_ = a.Store.ForgetUpdate(r.Context(), "library", kind+":"+id)
	w.WriteHeader(http.StatusNoContent)
}

var errUpToDate = errors.New("Cette copie est déjà à la dernière version de la bibliothèque.")

func originOf(raw []byte) (library.Origin, bool) {
	var o library.Origin
	if json.Unmarshal(raw, &o) != nil || o.Kind == "" {
		return o, false
	}
	return o, true
}

func (a *API) updatePackageCopy(ctx context.Context, owner, id string) error {
	row, err := a.Store.Package(ctx, owner, id)
	if err != nil {
		return err
	}
	origin, ok := originOf(row.Library)
	if !ok {
		return errUpToDate
	}
	lib := library.Current()
	version, _ := lib.Newest(origin)
	item, found := lib.Package(origin.ID)
	if version == "" || !found {
		return errUpToDate
	}
	p := item.Pkg
	raw, err := json.Marshal(packageNamed(p, row.Name))
	if err != nil {
		return err
	}
	if err := a.Store.UpdatePackage(ctx, owner, id, row.Name, raw); err != nil {
		return err
	}
	origin.Version, origin.Sum = version, bodySum(raw)
	mark, _ := json.Marshal(origin)
	return a.Store.SetPackageLibrary(ctx, owner, id, mark)
}

// packageNamed keeps the name the person gave their copy.
func packageNamed(p pack.Package, name string) pack.Package {
	var out pack.Package
	raw, _ := json.Marshal(p)
	_ = json.Unmarshal(raw, &out)
	out.Name = name
	return out
}

func (a *API) updateTemplateCopy(r *http.Request, owner, id string) error {
	ctx := r.Context()
	row, err := a.Store.Template(ctx, owner, id)
	if err != nil {
		return err
	}
	origin, ok := originOf(row.Library)
	if !ok {
		return errUpToDate
	}
	version, _ := library.Current().Newest(origin)
	starter, found := market.StarterOf(origin.ID)
	if version == "" || !found {
		return errUpToDate
	}
	var old blueprint.Body
	_ = json.Unmarshal(row.Body, &old)
	body, err := starterBody(starter, "")
	if err != nil {
		return err
	}
	// What the person filled in stays: the plan of each server, the names
	// of each application.
	for i, server := range body.Servers {
		for _, before := range old.Servers {
			if before.Name == server.Name {
				before.ControlPlane = server.ControlPlane
				body.Servers[i] = before
			}
		}
	}
	for i, app := range body.Applications {
		for _, before := range old.Applications {
			if before.Name == app.Name && len(before.Names) > 0 {
				body.Applications[i].Names = before.Names
			}
		}
	}
	if old.ClusterName != "" {
		body.ClusterName = old.ClusterName
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if err := a.Store.UpdateTemplate(ctx, owner, id, row.Name, raw); err != nil {
		return err
	}
	origin.Version, origin.Sum = version, bodySum(raw)
	mark, _ := json.Marshal(origin)
	return a.Store.SetTemplateLibrary(ctx, owner, id, mark)
}
