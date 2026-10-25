package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"myinfra/internal/library"
	"myinfra/internal/store"
)

// testLibrary is the library of internal/library/testdata, at a release,
// with changes; the library MyInfra proposes comes back after the test.
func testLibrary(t *testing.T, release string, change func(fstest.MapFS)) {
	t.Helper()
	files := fstest.MapFS{}
	root := "../library/testdata/lib"
	err := fs.WalkDir(os.DirFS(root), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(root + "/" + name)
		files[name] = &fstest.MapFile{Data: raw}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(files)
	}
	lib, err := library.Read(files, release, "")
	if err != nil {
		t.Fatal(err)
	}
	previous := library.Current()
	library.Set(lib)
	t.Cleanup(func() { library.Set(previous) })
}

func libraryCall(t *testing.T, handler http.HandlerFunc, method string, values map[string]string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, "/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(withOwner(withUser(req.Context(), "u1"), "u1"))
	for key, value := range values {
		req.SetPathValue(key, value)
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// A package and a template taken from the library are offered its newer
// versions, and updated without losing what the person filled in.
func TestLibraryCopiesFollowTheLibrary(t *testing.T) {
	a, _ := leadAPI(t, false, store.CopyRole{})
	ctx := t.Context()
	if err := a.Store.CreateFirstUser(ctx, "u1", "hash"); err != nil {
		t.Fatal(err)
	}
	testLibrary(t, "1.0.0", nil)

	rec := libraryCall(t, a.copyArticle, http.MethodPost, map[string]string{"id": "umami"}, nil)
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var pkg packageView
	_ = json.Unmarshal(rec.Body.Bytes(), &pkg)
	if pkg.Library == nil || pkg.Library.ID != "umami" || pkg.Library.Version != "1.0.0" || pkg.Library.Newer != "" || pkg.Library.Edited {
		t.Fatalf("%+v", pkg.Library)
	}
	rec = libraryCall(t, a.useStarter, http.MethodPost, map[string]string{"id": "site"}, map[string]string{"name": "Mon site"})
	if rec.Code != http.StatusCreated {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var tpl templateView
	_ = json.Unmarshal(rec.Body.Bytes(), &tpl)
	if tpl.Library == nil || tpl.Library.Version != "1.0.0" || len(tpl.Body.Applications) != 2 || tpl.Body.Applications[1].Names[0].Subdomain != "notes" {
		t.Fatalf("%+v", tpl)
	}
	// The person chooses where the server goes.
	var body map[string]any
	row, _ := a.Store.Template(ctx, "u1", tpl.ID)
	_ = json.Unmarshal(row.Body, &body)
	body["servers"].([]any)[0].(map[string]any)["offerId"] = "cx22"
	raw, _ := json.Marshal(body)
	if err := a.Store.UpdateTemplate(ctx, "u1", tpl.ID, row.Name, raw); err != nil {
		t.Fatal(err)
	}

	if err := a.checkLibraryCopies(ctx); err != nil {
		t.Fatal(err)
	}
	if rows, _ := a.Store.Updates(ctx, "u1"); len(rows) != 0 {
		t.Fatalf("%+v", rows)
	}

	// A new release: umami and the template move on.
	testLibrary(t, "1.1.0", func(f fstest.MapFS) {
		f["packages/umami/package.yaml"].Data = []byte(strings.Replace(string(f["packages/umami/package.yaml"].Data), "v2.17.0", "v2.18.0", 1))
		f["packages/umami/item.yaml"].Data = []byte("version: 1.1.0\nchanges: Umami 2.18.\n")
		f["templates/site/item.yaml"].Data = []byte("version: 2.0.0\n")
	})
	if err := a.checkLibraryCopies(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := a.Store.Updates(ctx, "u1")
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %+v", err, rows)
	}
	for _, row := range rows {
		if row.Kind != "library" {
			t.Fatalf("%+v", row)
		}
		var detail libraryUpdate
		_ = json.Unmarshal(row.Detail, &detail)
		switch detail.Kind {
		case "package":
			if row.Available != "1.1.0" || row.Level != "minor" || detail.Changes != "Umami 2.18." || detail.Edited {
				t.Fatalf("%+v %+v", row, detail)
			}
		case "template":
			if row.Available != "2.0.0" || row.Level != "major" || !detail.Edited {
				t.Fatalf("%+v %+v", row, detail)
			}
		}
	}

	rec = libraryCall(t, a.updateLibraryCopy, http.MethodPost, map[string]string{"kind": "package", "id": pkg.ID}, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatal(rec.Code, rec.Body.String())
	}
	kept, _ := a.Store.Package(ctx, "u1", pkg.ID)
	if !strings.Contains(string(kept.Body), "v2.18.0") || toPackage(kept, false).Library.Version != "1.1.0" {
		t.Fatal(string(kept.Body), string(kept.Library))
	}
	rec = libraryCall(t, a.updateLibraryCopy, http.MethodPost, map[string]string{"kind": "template", "id": tpl.ID}, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatal(rec.Code, rec.Body.String())
	}
	row, _ = a.Store.Template(ctx, "u1", tpl.ID)
	updated := toTemplate(row)
	if updated.Library.Version != "2.0.0" || updated.Body.Servers[0].OfferID != "cx22" || !strings.Contains(string(row.Body), "v2.18.0") {
		t.Fatalf("%+v %s", updated.Library, row.Body)
	}
	if rows, _ := a.Store.Updates(ctx, "u1"); len(rows) != 0 {
		t.Fatalf("%+v", rows)
	}
	again := libraryCall(t, a.updateLibraryCopy, http.MethodPost, map[string]string{"kind": "package", "id": pkg.ID}, nil)
	if again.Code != http.StatusConflict {
		t.Fatal(again.Code)
	}
}

func TestLibraryLogo(t *testing.T) {
	testLibrary(t, "1.0.0", nil)
	a := &API{}
	rec := libraryCall(t, a.libraryLogo, http.MethodGet, map[string]string{"slug": "memos"}, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" || rec.Header().Get("ETag") != `"1.0.0"` {
		t.Fatal(rec.Code, rec.Header())
	}
	if rec := libraryCall(t, a.libraryLogo, http.MethodGet, map[string]string{"slug": "absent"}, nil); rec.Code != http.StatusNotFound {
		t.Fatal(rec.Code)
	}
}
