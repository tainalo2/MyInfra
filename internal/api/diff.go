package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"myinfra/internal/commit"
	"myinfra/internal/store"
)

// The assistant's diff staging: proposed patches wait in the database,
// per account, until the Commit Gateway writes the chosen ones to the live
// template repository. Each step checks the right on the server:
//
//   - consultation of the staging: any signed-in account;
//   - opening a staging, adding a patch: deploy or admin;
//   - commit: admin, or the explicit "write templates" permission.
func (a *API) diffRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/diff/staging", a.auth(a.diffStaging))
	mux.HandleFunc("POST /api/diff/staging", a.deployAuth(a.steady(a.openStaging)))
	mux.HandleFunc("PUT /api/diff/staging/{id}/patches", a.deployAuth(a.steady(a.patchStaging)))
	mux.HandleFunc("GET /api/diff/stream", a.auth(a.diffStream))
	mux.HandleFunc("POST /api/diff/commit", a.auth(a.steady(a.commitStaging)))
}

type diffPatch struct {
	ID      string `json:"id"`
	HunkID  string `json:"hunkId"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type diffLine struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type diffHunk struct {
	ID        string     `json:"id"`
	StagingID string     `json:"stagingId"`
	File      string     `json:"file"`
	Header    string     `json:"header"`
	Lines     []diffLine `json:"lines"`
	Status    string     `json:"status"`
	Committed bool       `json:"committed"`
}

type diffStagingView struct {
	SessionID string     `json:"sessionId"`
	Hunks     []diffHunk `json:"hunks"`
}

func (p diffPatch) toCommit() commit.Patch {
	return commit.Patch{ID: p.ID, HunkID: p.HunkID, Path: p.Path, Content: p.Content}
}

func hunkOf(stagingID string, p commit.Patch) diffHunk {
	h := diffHunk{ID: p.ID, StagingID: stagingID, File: p.Path, Header: p.HunkID, Status: "pending", Lines: []diffLine{}}
	for _, line := range strings.Split(strings.TrimRight(p.Content, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
			continue
		case strings.HasPrefix(line, "+"):
			h.Lines = append(h.Lines, diffLine{Type: "add", Text: line[1:]})
		case strings.HasPrefix(line, "-"):
			h.Lines = append(h.Lines, diffLine{Type: "remove", Text: line[1:]})
		default:
			h.Lines = append(h.Lines, diffLine{Type: "context", Text: strings.TrimPrefix(line, " ")})
		}
	}
	return h
}

func stagingView(rows []store.Staging) diffStagingView {
	view := diffStagingView{Hunks: []diffHunk{}}
	for _, row := range rows {
		if view.SessionID == "" {
			view.SessionID = row.ID
		}
		var patches []commit.Patch
		if err := json.Unmarshal(row.Patches, &patches); err != nil {
			continue
		}
		for _, p := range patches {
			view.Hunks = append(view.Hunks, hunkOf(row.ID, p))
		}
	}
	return view
}

// diffStaging lists the account's pending hunks. Consultation only: the
// read-only role sees it too.
func (a *API) diffStaging(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.ListStagings(r.Context(), userID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stagingView(rows))
}

func validPatches(in []diffPatch) ([]commit.Patch, bool) {
	out := make([]commit.Patch, 0, len(in))
	for _, p := range in {
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Path) == "" || p.Content == "" {
			return nil, false
		}
		out = append(out, p.toCommit())
	}
	return out, true
}

// openStaging opens a functional context: a staging the assistant's
// proposals will land in. Deploy or admin.
func (a *API) openStaging(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind     string      `json:"kind"`
		TargetID string      `json:"targetId"`
		Name     string      `json:"name"`
		Context  string      `json:"context"`
		Patches  []diffPatch `json:"patches"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if body.Kind == "" {
		body.Kind = "template"
	}
	if body.Kind != "template" && body.Kind != "package" {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	patches, ok := validPatches(body.Patches)
	if !ok {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	raw, _ := json.Marshal(patches)
	id := newStagingID()
	if err := a.Store.CreateStaging(r.Context(), userID(r.Context()), id, body.Kind, body.TargetID, body.Name, []byte("{}"), raw, body.Context); err != nil {
		a.mapErr(w, err)
		return
	}
	hunks := make([]diffHunk, 0, len(patches))
	for _, p := range patches {
		hunks = append(hunks, hunkOf(id, p))
	}
	a.announce(userID(r.Context()), hunks, nil)
	writeJSON(w, http.StatusCreated, map[string]string{"sessionId": id})
}

// patchStaging is patch_template: the assistant's proposal joins the
// staging. Deploy or admin; nothing touches the live repository.
func (a *API) patchStaging(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Patches []diffPatch `json:"patches"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	added, ok := validPatches(body.Patches)
	if !ok || len(added) == 0 {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	owner := userID(r.Context())
	row, err := a.Store.Staging(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	var patches []commit.Patch
	_ = json.Unmarshal(row.Patches, &patches)
	known := map[string]bool{}
	for _, p := range patches {
		known[p.ID] = true
	}
	patches = append(patches, added...)
	raw, _ := json.Marshal(patches)
	if err := a.Store.UpdateStaging(r.Context(), owner, row.ID, row.Body, raw, row.Context); err != nil {
		a.mapErr(w, err)
		return
	}
	row.Patches = raw
	hunks := make([]diffHunk, 0, len(added))
	for _, p := range added {
		hunks = append(hunks, hunkOf(row.ID, p))
	}
	a.announce(owner, hunks, known)
	writeJSON(w, http.StatusOK, stagingView([]store.Staging{row}))
}

// commitStaging hands the chosen hunks to the Commit Gateway. The right is
// checked here and again in the gateway: admin, or the explicit "write
// templates" permission. Anyone else gets 403 and the repository stays as
// it was, whatever the UI showed.
func (a *API) commitStaging(w http.ResponseWriter, r *http.Request) {
	actor := a.commitActor(r.Context())
	if !actor.CanWrite() {
		writeError(w, http.StatusForbidden, "Droits insuffisants.")
		return
	}
	var body struct {
		SessionID string   `json:"sessionId"`
		HunkIDs   []string `json:"hunkIds"`
	}
	if err := readJSON(w, r, &body); err != nil || len(body.HunkIDs) == 0 {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if a.Commit == nil {
		writeError(w, http.StatusServiceUnavailable, "Le dépôt de modèles n'est pas configuré.")
		return
	}
	if body.SessionID != "" {
		if _, err := a.Store.Staging(r.Context(), userID(r.Context()), body.SessionID); err != nil {
			a.mapErr(w, err)
			return
		}
	}
	if err := a.Commit.Apply(r.Context(), actor, body.HunkIDs); err != nil {
		switch {
		case errors.Is(err, commit.ErrForbidden):
			writeError(w, http.StatusForbidden, "Droits insuffisants.")
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "Introuvable.")
		default:
			writeError(w, http.StatusConflict, "Le commit n'a pas abouti : "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, body.HunkIDs)
}

func newStagingID() string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "stg_" + hex.EncodeToString(buf)
}
