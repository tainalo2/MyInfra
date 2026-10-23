package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"myinfra/internal/market"
	"myinfra/internal/pack"
	"myinfra/internal/store"
)

type packageView struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Summary   string        `json:"summary"`
	Category  string        `json:"category"`
	Services  []string      `json:"services"`
	Inputs    []pack.Input  `json:"inputs"`
	UpdatedAt time.Time     `json:"updatedAt"`
	Package   *pack.Package `json:"package,omitempty"`
	// YAML is the package as a person reads and edits it.
	YAML string `json:"yaml,omitempty"`
}

func toPackage(row store.Package, full bool) packageView {
	var p pack.Package
	_ = json.Unmarshal(row.Body, &p)
	view := packageView{
		ID: row.ID, Name: row.Name, Summary: p.Summary, Category: p.Category,
		Services: []string{}, Inputs: p.Inputs, UpdatedAt: row.UpdatedAt,
	}
	if view.Inputs == nil {
		view.Inputs = []pack.Input{}
	}
	for _, svc := range p.Services {
		view.Services = append(view.Services, svc.Name)
	}
	if full {
		view.Package = &p
		if text, err := pack.YAML(p); err == nil {
			view.YAML = string(text)
		}
	}
	return view
}

// writeProblems answers a package that cannot be kept, with every problem.
func writeProblems(w http.ResponseWriter, problems []pack.Problem) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": problems[0].Error(), "problems": problems})
}

// readPackageText reads a package sent as YAML or JSON text.
func readPackageText(w http.ResponseWriter, r *http.Request) (pack.Package, []pack.Problem, bool) {
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return pack.Package{}, nil, false
	}
	p, problems := pack.Parse([]byte(body.Text))
	return p, problems, true
}

func (a *API) listPackages(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListPackages(r.Context(), ownerID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]packageView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPackage(row, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) getPackage(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.Package(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPackage(row, true))
}

// checkPackage reads a text as the editor sends it while a person types:
// the package as far as it reads, what is wrong in it, and nothing kept.
// readable tells the form it may take the package over.
func (a *API) checkPackage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	p, problems, readable := pack.Read([]byte(body.Text))
	if problems == nil {
		problems = []pack.Problem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"package": p, "problems": problems, "readable": readable})
}

// formatPackage writes the package the form holds as the YAML the advanced
// window shows.
func (a *API) formatPackage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Package pack.Package `json:"package"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	text, err := pack.YAML(body.Package)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	problems := pack.Check(body.Package)
	if problems == nil {
		problems = []pack.Problem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"yaml": string(text), "problems": problems})
}

func (a *API) createPackage(w http.ResponseWriter, r *http.Request) {
	p, problems, ok := readPackageText(w, r)
	if !ok {
		return
	}
	if len(problems) > 0 {
		writeProblems(w, problems)
		return
	}
	a.keepPackage(w, r, p, false)
}

func (a *API) updatePackage(w http.ResponseWriter, r *http.Request) {
	p, problems, ok := readPackageText(w, r)
	if !ok {
		return
	}
	if len(problems) > 0 {
		writeProblems(w, problems)
		return
	}
	raw, err := json.Marshal(p)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	owner := ownerID(r.Context())
	if err := a.Store.UpdatePackage(r.Context(), owner, r.PathValue("id"), strings.TrimSpace(p.Name), raw); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err := a.Store.Package(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPackage(row, true))
}

func (a *API) deletePackage(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.DeletePackage(r.Context(), ownerID(r.Context()), r.PathValue("id")); err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// keepPackage stores a valid package. renamed finds a free name, for an
// import or a copy, instead of refusing one already used.
func (a *API) keepPackage(w http.ResponseWriter, r *http.Request, p pack.Package, renamed bool) {
	owner := ownerID(r.Context())
	base := strings.TrimSpace(p.Name)
	id := newID()
	for n := 1; ; n++ {
		p.Name = base
		if n > 1 {
			p.Name = fmt.Sprintf("%s (%d)", base, n)
		}
		raw, err := json.Marshal(p)
		if err != nil {
			a.mapErr(w, err)
			return
		}
		err = a.Store.CreatePackage(r.Context(), owner, id, p.Name, raw)
		if err == nil {
			break
		}
		if !renamed || !errors.Is(err, store.ErrNameTaken) || n == 20 {
			a.mapErr(w, err)
			return
		}
	}
	row, err := a.Store.Package(r.Context(), owner, id)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toPackage(row, true))
}

func (a *API) exportPackage(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.Package(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var p pack.Package
	if err := json.Unmarshal(row.Body, &p); err != nil {
		a.mapErr(w, err)
		return
	}
	format := r.URL.Query().Get("format")
	var text []byte
	contentType, ext := "application/yaml; charset=utf-8", ".yaml"
	if format == "json" {
		text, err = pack.JSON(p)
		contentType, ext = "application/json; charset=utf-8", ".json"
	} else {
		text, err = pack.YAML(p)
	}
	if err != nil {
		a.mapErr(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName(row.Name)+ext+`"`)
	_, _ = w.Write(text)
}

// importPackage keeps a package sent as a file or pasted, under a free name.
func (a *API) importPackage(w http.ResponseWriter, r *http.Request) {
	p, problems, ok := readPackageText(w, r)
	if !ok {
		return
	}
	if len(problems) > 0 {
		writeProblems(w, problems)
		return
	}
	a.keepPackage(w, r, p, true)
}

func (a *API) importPackageURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	target, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		writeError(w, http.StatusBadRequest, "Indiquez une URL https:// vers un fichier YAML ou JSON.")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Le fichier est injoignable.")
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "Le fichier répond "+res.Status+".")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, pack.MaxSize+1))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	p, problems := pack.Parse(raw)
	if len(problems) > 0 {
		writeProblems(w, problems)
		return
	}
	a.keepPackage(w, r, p, true)
}

// copyArticle keeps a catalogue article as a package, to change it.
func (a *API) copyArticle(w http.ResponseWriter, r *http.Request) {
	article, ok := market.ArticleOf(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "Cet article est introuvable.")
		return
	}
	a.keepPackage(w, r, pack.FromArticle(article), true)
}

// installPackage creates an application from a package, as from the catalogue.
func (a *API) installPackage(w http.ResponseWriter, r *http.Request) {
	row, err := a.Store.Package(r.Context(), ownerID(r.Context()), r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var p pack.Package
	if err := json.Unmarshal(row.Body, &p); err != nil {
		a.mapErr(w, err)
		return
	}
	a.installFrom(w, r, func(host string, inputs map[string]string) ([]store.Service, map[string]string, error) {
		services, env, err := pack.Expand(p, host, inputs)
		if err != nil {
			return nil, nil, err
		}
		out, err := packServices(services)
		if err == nil {
			// A port left out is the one the image exposes.
			a.fillFromImages(r.Context(), out, false)
		}
		return out, env, err
	})
}

// packServices turns the services of a package into those a store keeps:
// the first volume of each is its disk.
func packServices(services []pack.Service) ([]store.Service, error) {
	out := make([]store.Service, 0, len(services))
	for _, svc := range services {
		if !acceptableImage(svc.Image) {
			return nil, fmt.Errorf("L'image %s est illisible.", svc.Image)
		}
		replicas := svc.Replicas
		if replicas < 1 {
			replicas = 1
		}
		disk, rest := svc.Disk()
		next := store.Service{
			ID: newID(), Name: svc.Name, Image: svc.Image, Published: svc.Public,
			Listen: svc.Port, Args: svc.Args, Replicas: replicas, Spec: rest,
		}
		if disk != nil {
			next.Disk, next.DiskPath, next.DiskGi = true, disk.Path, disk.SizeGi
		}
		out = append(out, next)
	}
	return out, nil
}
