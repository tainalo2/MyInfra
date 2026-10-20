package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"myinfra/internal/deploy"
	"myinfra/internal/imagefile"
	"myinfra/internal/registry"
	"myinfra/internal/store"
)

// An image a person brings, sent or fetched from a URL, is kept by MyInfra
// and loaded on the servers over SSH, under myinfra.local/<name>:<tag>. The
// servers never reach MyInfra: it often runs behind a home router.

const imageLimit = 4 << 30

var (
	localName = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)
	localTag  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

type localImageView struct {
	ID        string    `json:"id"`
	Ref       string    `json:"ref"`
	Name      string    `json:"name"`
	Tag       string    `json:"tag"`
	Source    string    `json:"source"`
	SourceURL string    `json:"sourceUrl,omitempty"`
	Size      int64     `json:"size"`
	Platform  string    `json:"platform,omitempty"`
	Ports     []int     `json:"ports"`
	Volumes   []string  `json:"volumes"`
	UsedBy    []string  `json:"usedBy"`
	CreatedAt time.Time `json:"createdAt"`
}

func toLocalImageView(img store.LocalImage) localImageView {
	name, tag := splitLocal(img.Ref)
	view := localImageView{ID: img.ID, Ref: img.Ref, Name: name, Tag: tag, Source: img.Source, SourceURL: img.SourceURL,
		Size: img.Size, Platform: img.Platform, Ports: []int{}, Volumes: []string{}, UsedBy: img.UsedBy, CreatedAt: img.CreatedAt}
	if view.UsedBy == nil {
		view.UsedBy = []string{}
	}
	if conf, err := registry.ConfigImage(img.Config); err == nil {
		view.Ports = append(view.Ports, conf.Ports...)
		view.Volumes = append(view.Volumes, conf.Volumes...)
	}
	return view
}

func splitLocal(ref string) (string, string) {
	rest := strings.TrimPrefix(ref, imagefile.LocalPrefix)
	at := strings.LastIndex(rest, ":")
	if at < 0 {
		return rest, ""
	}
	return rest[:at], rest[at+1:]
}

func (a *API) listLocalImages(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Store.LocalImages(r.Context(), userID(r.Context()))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	out := make([]localImageView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toLocalImageView(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// uploadLocalImage keeps the archive sent as the body of the request.
func (a *API) uploadLocalImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, imageLimit+1<<20)
	a.keepLocalImage(w, r, r.Body, "upload", "", r.URL.Query().Get("name"), r.URL.Query().Get("tag"))
}

// fetchLocalImage downloads the archive from a raw URL, then keeps it.
func (a *API) fetchLocalImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL  string `json:"url"`
		Name string `json:"name"`
		Tag  string `json:"tag"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	target, err := url.Parse(strings.TrimSpace(body.URL))
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Host == "" {
		writeError(w, http.StatusBadRequest, "Indiquez l'adresse complète de l'archive, en https://.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Indiquez l'adresse complète de l'archive, en https://.")
		return
	}
	req.Header.Set("User-Agent", "MyInfra")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "L'archive ne se télécharge pas : "+target.Host+" ne répond pas.")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("L'archive ne se télécharge pas : %s répond %d.", target.Host, resp.StatusCode))
		return
	}
	a.keepLocalImage(w, r.WithContext(ctx), resp.Body, "url", target.String(), body.Name, body.Tag)
}

func (a *API) keepLocalImage(w http.ResponseWriter, r *http.Request, src io.Reader, source, sourceURL, name, tag string) {
	archive, err := imagefile.Read(src, os.TempDir(), imageLimit)
	if errors.Is(err, imagefile.ErrTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "L'image dépasse 4 Go.")
		return
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "L'image dépasse 4 Go.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer archive.Close()
	ref, err := localRef(name, tag, archive)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, size, err := archive.WriteAs(ref, os.TempDir())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer os.Remove(file)
	f, err := os.Open(file)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	defer f.Close()
	img := store.LocalImage{ID: newID(), OwnerID: userID(r.Context()), Ref: ref, Source: source, SourceURL: sourceURL,
		Size: size, Digest: archive.Digest, Platform: archive.Platform, Config: archive.Config}
	if err := a.Store.SaveLocalImage(r.Context(), img, f); err != nil {
		if errors.Is(err, store.ErrNameTaken) {
			writeError(w, http.StatusConflict, "Une image porte déjà le nom "+ref+". Changez la version.")
			return
		}
		a.mapErr(w, err)
		return
	}
	saved, err := a.Store.LocalImage(r.Context(), img.OwnerID, img.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toLocalImageView(saved))
}

// localRef names the image: the name and version given, or else those docker
// save recorded. A missing or latest version becomes the image's digest, so
// that a name always means the same image and a server never pulls it.
func localRef(name, tag string, archive *imagefile.Archive) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	tag = strings.TrimSpace(tag)
	if len(archive.Tags) > 0 {
		recorded, recordedTag := archive.Tags[0], ""
		if at := strings.LastIndex(recorded, ":"); at > strings.LastIndex(recorded, "/") {
			recorded, recordedTag = recorded[:at], recorded[at+1:]
		}
		if name == "" {
			name = strings.ToLower(recorded[strings.LastIndex(recorded, "/")+1:])
		}
		if tag == "" {
			tag = recordedTag
		}
	}
	if name == "" {
		return "", errString("Donnez un nom à l'image.")
	}
	if !localName.MatchString(name) {
		return "", errString("Le nom de l'image s'écrit en minuscules, chiffres, points et tirets.")
	}
	if tag == "" || tag == "latest" {
		tag = "sha-" + strings.TrimPrefix(archive.Digest, "sha256:")[:12]
	}
	if !localTag.MatchString(tag) {
		return "", errString("La version s'écrit en lettres, chiffres, points et tirets.")
	}
	return imagefile.LocalPrefix + name + ":" + tag, nil
}

func (a *API) deleteLocalImage(w http.ResponseWriter, r *http.Request) {
	owner := userID(r.Context())
	img, err := a.Store.LocalImage(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	err = a.Store.DeleteLocalImage(r.Context(), owner, img.ID)
	if err == nil {
		forgetLoaded(img.Ref)
	}
	if errors.Is(err, store.ErrImageInUse) {
		writeError(w, http.StatusConflict, "Des applications utilisent cette image. Changez leur image avant de la supprimer.")
		return
	}
	if err != nil {
		a.mapErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkLocalImages refuses an image MyInfra does not keep for this person.
func (a *API) checkLocalImages(ctx context.Context, owner string, images []string) error {
	for _, image := range images {
		if !imagefile.IsLocal(image) {
			continue
		}
		img, err := a.Store.LocalImageByRef(ctx, image)
		if err != nil || img.OwnerID != owner {
			return errString("L'image " + image + " n'est pas dans vos images importées.")
		}
	}
	return nil
}

// inspectImage reads what an image declares: from MyInfra for an image it
// keeps, from its registry otherwise.
func (a *API) inspectImage(ctx context.Context, image string) (registry.Image, error) {
	if imagefile.IsLocal(image) {
		img, err := a.Store.LocalImageByRef(ctx, image)
		if err != nil {
			return registry.Image{}, err
		}
		return registry.ConfigImage(img.Config)
	}
	return registry.Inspect(ctx, image)
}

// localTags are the versions MyInfra keeps of the image named like image.
func (a *API) localTags(ctx context.Context, owner, image string) []string {
	name, _ := splitLocal(image)
	rows, err := a.Store.LocalImages(ctx, owner)
	if err != nil {
		return nil
	}
	var tags []string
	for _, row := range rows {
		if other, tag := splitLocal(row.Ref); other == name {
			tags = append(tags, tag)
		}
	}
	return tags
}

// Each server is asked again every few minutes whether it still has an image:
// a disk cleanup on the node may have removed it.
const loadedFresh = 10 * time.Minute

var (
	loadedMu sync.Mutex
	loaded   = map[string]time.Time{}
	loading  = map[string]*sync.Mutex{}
)

// imageLoader says how a server keeps images: containerd for a cluster node,
// Docker for a machine alone.
type imageLoader struct {
	check string
	load  string
}

var (
	clusterLoader = imageLoader{
		check: "k3s ctr -n k8s.io images ls -q | grep -Fxq %s",
		load:  `f=$(mktemp) && cat > "$f" && k3s ctr -n k8s.io images import "$f"; rc=$?; rm -f "$f"; exit $rc`,
	}
	dockerLoader = imageLoader{
		check: "docker image inspect %s >/dev/null 2>&1",
		load:  "docker load",
	}
)

// localImagesOf keeps the images MyInfra keeps, once each.
func localImagesOf(images []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, image := range images {
		if imagefile.IsLocal(image) && !seen[image] {
			seen[image] = true
			out = append(out, image)
		}
	}
	return out
}

// loadImages makes sure a server holds each image MyInfra keeps among refs,
// sending the archive over SSH when it does not. It returns the images that
// could not be loaded, with why.
func (a *API) loadImages(ctx context.Context, server string, host deploy.Host, loader imageLoader, refs []string) map[string]error {
	failed := map[string]error{}
	for _, ref := range refs {
		key := server + "|" + ref
		loadedMu.Lock()
		fresh := time.Since(loaded[key]) < loadedFresh
		lock := loading[key]
		if lock == nil {
			lock = &sync.Mutex{}
			loading[key] = lock
		}
		loadedMu.Unlock()
		if fresh {
			continue
		}
		lock.Lock()
		err := a.loadImage(ctx, host, loader, ref)
		lock.Unlock()
		if err != nil {
			slog.Info("image", "server", server, "image", ref, "err", err)
			failed[ref] = err
			continue
		}
		loadedMu.Lock()
		loaded[key] = time.Now()
		loadedMu.Unlock()
	}
	return failed
}

func (a *API) loadImage(ctx context.Context, host deploy.Host, loader imageLoader, ref string) error {
	quoted := "'" + ref + "'"
	if _, err := deploy.SSH(ctx, host, fmt.Sprintf(loader.check, quoted)); err == nil {
		return nil
	}
	img, err := a.Store.LocalImageByRef(ctx, ref)
	if err != nil {
		return errString("L'image " + ref + " n'est plus dans les images importées.")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if _, err := deploy.SSHStream(ctx, host, loader.load, func() io.Reader { return a.Store.OpenLocalImage(ctx, img.ID) }); err != nil {
		return fmt.Errorf("chargement refusé : %v", err)
	}
	if _, err := deploy.SSH(ctx, host, fmt.Sprintf(loader.check, quoted)); err != nil {
		return errString("chargée mais introuvable ensuite : son architecture (" + img.Platform + ") ne convient peut-être pas à ce serveur.")
	}
	return nil
}

// forgetLoaded asks every server again, after an image is deleted or replaced.
func forgetLoaded(ref string) {
	loadedMu.Lock()
	defer loadedMu.Unlock()
	for key := range loaded {
		if strings.HasSuffix(key, "|"+ref) {
			delete(loaded, key)
		}
	}
}

// loadClusterImages loads on every node of a cluster the images MyInfra keeps
// that its applications run, and says, by image, why one could not be loaded.
func (a *API) loadClusterImages(ctx context.Context, leader store.ClusterLeader, apps []store.ClusterApp) map[string]string {
	var images []string
	for _, app := range apps {
		images = append(images, serviceImages(app.Services)...)
	}
	refs := localImagesOf(images)
	if len(refs) == 0 {
		return nil
	}
	servers, err := a.Store.ListServers(ctx, leader.OwnerID)
	if err != nil {
		return nil
	}
	failed := map[string]string{}
	for _, server := range servers {
		if server.ClusterID != leader.ClusterID || server.Status != "linked" || server.Address == "" {
			continue
		}
		keyIDs, err := a.Store.ServerKeyIDs(ctx, server.ID)
		key := ""
		if err == nil {
			key = a.privateKey(ctx, leader.OwnerID, keyIDs)
		}
		if key == "" {
			for _, ref := range refs {
				failed[ref] = fmt.Sprintf("L'image %s ne peut pas être chargée sur %s : MyInfra n'a pas de clé SSH pour ce serveur.", ref, server.Name)
			}
			continue
		}
		for ref, err := range a.loadImages(ctx, server.ID, deploy.Host{Address: server.Address, PrivateKey: key}, clusterLoader, refs) {
			failed[ref] = fmt.Sprintf("L'image %s n'a pas pu être chargée sur %s : %v", ref, server.Name, err)
		}
	}
	return failed
}

// imageTrouble is why an application cannot start yet, if one of its images
// could not be loaded.
func imageTrouble(services []store.Service, failed map[string]string) string {
	for _, svc := range services {
		if detail, ok := failed[svc.Image]; ok {
			return detail
		}
	}
	return ""
}
