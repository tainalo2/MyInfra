package api

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"myinfra/internal/box"
	"myinfra/internal/cert"
	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
)

// Traefik keeps the certificates it obtained in /data, which a restart empties.
// Let's Encrypt gives at most five certificates a week for the same name: a few
// restarts or new clusters and a name has no certificate for days. MyInfra
// keeps the files, sealed, and puts them back before Traefik starts.

const certificateSweep = 5 * time.Minute

// readCertificates prints "acme-zone.json <base64>" for each file of Traefik.
const readCertificates = `k3s kubectl -n kube-system exec deploy/traefik -- sh -c 'for f in /data/acme-*.json; do [ -s "$f" ] || continue; printf "%s " "${f##*/}"; base64 "$f" | tr -d "\n"; echo; done' 2>/dev/null || true`

// applyCertificates writes the Secret applied on the server's side. A client
// side apply copies the whole Secret into an annotation that may not exceed
// 256 KiB, which a few zones of certificates go past; the server keeps that
// annotation up to date while it is there, so it goes first.
var applyCertificates = "k3s kubectl -n kube-system annotate secret " + manifest.CertificateSecret +
	" kubectl.kubernetes.io/last-applied-configuration- >/dev/null 2>&1; " +
	"k3s kubectl apply --server-side --force-conflicts --field-manager=myinfra -f -"

// keepCertificates saves what Traefik obtained and gives the cluster back what
// MyInfra kept for its zones. It runs before the proxy is configured, so that a
// new cluster finds the certificates at the first start of Traefik.
func (a *API) keepCertificates(ctx context.Context, host deploy.Host, leader store.ClusterLeader, zones []string) {
	if !a.certificatesDue(leader.ClusterID) {
		return
	}
	kept, err := a.openCertificates(ctx, leader.OwnerID)
	if err != nil {
		slog.Info("certificats", "err", err)
		return
	}
	if out, err := deploy.SSH(ctx, host, readCertificates); err == nil {
		for name, raw := range parseCertificateFiles(out) {
			merged, err := mergeCertificates(kept[name], raw, time.Now())
			if err != nil || string(merged) == string(kept[name]) {
				continue
			}
			if err := a.sealCertificates(ctx, leader.OwnerID, name, merged); err != nil {
				slog.Info("certificats", "err", err)
				continue
			}
			kept[name] = merged
		}
	}
	files := map[string][]byte{}
	for _, zone := range zones {
		name := "acme-" + cert.Resolver(zone) + ".json"
		if raw, ok := kept[name]; ok {
			files[name] = raw
		}
	}
	secret := manifest.CertificateFiles(files)
	key := "certificates:" + leader.ClusterID
	if a.appliedRecently(key, digest(secret)) {
		return
	}
	if _, err := deploy.SSHInput(ctx, host, applyCertificates, []byte(secret)); err != nil {
		slog.Info("certificats", "err", err)
		return
	}
	_, _ = a.rememberApplied(key, digest(secret))
}

func (a *API) certificatesDue(clusterID string) bool {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	if a.certificateSwept == nil {
		a.certificateSwept = map[string]time.Time{}
	}
	if time.Since(a.certificateSwept[clusterID]) < certificateSweep {
		return false
	}
	a.certificateSwept[clusterID] = time.Now()
	return true
}

func (a *API) openCertificates(ctx context.Context, owner string) (map[string][]byte, error) {
	rows, err := a.Store.CertificateStores(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, row := range rows {
		plain, err := a.Box.Open(box.Sealed{Ciphertext: row.Ciphertext, Nonce: row.Nonce, WrappedDEK: row.WrappedDEK})
		if err != nil {
			return nil, fmt.Errorf("certificats illisibles")
		}
		out[row.Resolver] = plain
	}
	return out, nil
}

func (a *API) sealCertificates(ctx context.Context, owner, name string, raw []byte) error {
	sealed, err := a.Box.Seal(raw)
	if err != nil {
		return err
	}
	return a.Store.SaveCertificateStore(ctx, owner, store.CertificateStore{
		Resolver: name, Ciphertext: sealed.Ciphertext, Nonce: sealed.Nonce, WrappedDEK: sealed.WrappedDEK,
	})
}

var certificateFile = regexp.MustCompile(`^acme-[a-z0-9-]+\.json$`)

func parseCertificateFiles(out string) map[string][]byte {
	files := map[string][]byte{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for scanner.Scan() {
		name, encoded, ok := strings.Cut(strings.TrimSpace(scanner.Text()), " ")
		if !ok || !certificateFile.MatchString(name) {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil || !json.Valid(raw) {
			continue
		}
		files[name] = raw
	}
	return files
}

type certificateResolver struct {
	Account      json.RawMessage   `json:"Account"`
	Certificates []json.RawMessage `json:"Certificates"`
}

// mergeCertificates joins a kept file and the file of a cluster. For each name
// the certificate that ends last wins; expired ones go.
func mergeCertificates(kept, fresh []byte, now time.Time) ([]byte, error) {
	out := map[string]*certificateResolver{}
	for _, raw := range [][]byte{kept, fresh} {
		if len(raw) == 0 {
			continue
		}
		var doc map[string]*certificateResolver
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		for resolver, part := range doc {
			if part == nil {
				continue
			}
			into := out[resolver]
			if into == nil {
				into = &certificateResolver{}
				out[resolver] = into
			}
			if len(part.Account) > 0 && string(part.Account) != "null" {
				into.Account = part.Account
			}
			into.Certificates = append(into.Certificates, part.Certificates...)
		}
	}
	for _, part := range out {
		best := map[string]json.RawMessage{}
		ends := map[string]time.Time{}
		for _, raw := range part.Certificates {
			name, end := certificateOf(raw)
			if name == "" || !end.After(now) {
				continue
			}
			if end.After(ends[name]) || ends[name].IsZero() {
				best[name], ends[name] = raw, end
			}
		}
		names := make([]string, 0, len(best))
		for name := range best {
			names = append(names, name)
		}
		sort.Strings(names)
		part.Certificates = part.Certificates[:0]
		for _, name := range names {
			part.Certificates = append(part.Certificates, best[name])
		}
	}
	return json.Marshal(out)
}

// certificateOf reads the main name of an entry and the end of its certificate.
func certificateOf(raw json.RawMessage) (string, time.Time) {
	var entry struct {
		Domain struct {
			Main string `json:"main"`
		} `json:"domain"`
		Certificate []byte `json:"certificate"`
	}
	if json.Unmarshal(raw, &entry) != nil || entry.Domain.Main == "" {
		return "", time.Time{}
	}
	block, _ := pem.Decode(entry.Certificate)
	if block == nil {
		return "", time.Time{}
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", time.Time{}
	}
	return strings.ToLower(entry.Domain.Main), parsed.NotAfter
}

// readCertificateErrors prints the last ACME failures of Traefik, colours removed.
const readCertificateErrors = `k3s kubectl -n kube-system logs deploy/traefik --since=2h 2>/dev/null | grep 'Unable to obtain ACME certificate' | sed 's/\x1b\[[0-9;]*m//g' | tail -n 200 || true`

var certificateRetry = regexp.MustCompile(`retry after (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) UTC`)

// certificateTrouble says, for each name, why Let's Encrypt gave no
// certificate, and from when asking again can succeed.
type certificateTrouble struct {
	Detail string
	Retry  time.Time
}

func parseCertificateErrors(out string, names []string) map[string]certificateTrouble {
	found := map[string]certificateTrouble{}
	for _, line := range strings.Split(out, "\n") {
		for _, name := range names {
			if !strings.Contains(line, "["+name+"]") {
				continue
			}
			trouble := certificateTrouble{}
			switch {
			case strings.Contains(line, "rateLimited"):
				trouble.Detail = "Let's Encrypt refuse un nouveau certificat pour " + name + " : il en a déjà délivré 5 cette semaine pour ce nom."
				if match := certificateRetry.FindStringSubmatch(line); len(match) == 2 {
					if at, err := time.Parse("2006-01-02 15:04:05", match[1]); err == nil {
						trouble.Retry = at
						trouble.Detail += " Nouvel essai automatique après le " + at.Local().Format("02/01 à 15:04") + "."
					}
				}
			case strings.Contains(line, "unauthorized") || strings.Contains(line, "connection") || strings.Contains(line, "timeout"):
				trouble.Detail = "Let's Encrypt n'a pas pu joindre " + name + " pour vérifier le nom : l'enregistrement DNS ou le port 443 ne répond pas encore."
			default:
				trouble.Detail = "Le certificat de " + name + " n'a pas pu être obtenu."
			}
			// The last line of a name is the most recent attempt.
			found[name] = trouble
		}
	}
	return found
}

const certificateRetryEvery = 30 * time.Minute

// certificateGrace is how long a new address may take to get its certificate
// before MyInfra looks for the reason.
const certificateGrace = 2 * time.Minute

// explainCertificates records why the names of an application have no valid
// certificate, and asks Traefik again once Let's Encrypt allows it: Traefik
// does not try again by itself until its configuration changes.
func (a *API) explainCertificates(ctx context.Context, host deploy.Host, clusterID string, app store.ClusterApp) {
	names := append([]string(nil), app.Hosts...)
	out, err := deploy.SSH(ctx, host, readCertificateErrors)
	if err != nil {
		return
	}
	troubles := parseCertificateErrors(out, names)
	details := make([]string, 0, len(troubles))
	retry := len(troubles) > 0
	for _, name := range names {
		trouble, ok := troubles[name]
		if !ok {
			continue
		}
		details = append(details, trouble.Detail)
		if !trouble.Retry.IsZero() && time.Now().Before(trouble.Retry) {
			retry = false
		}
	}
	detail := strings.Join(details, " ")
	if detail == "" {
		detail = "En attente du certificat HTTPS."
	}
	_ = a.Store.SetRunDetail(ctx, app.ID, detail)
	if retry && a.retryDue(clusterID) {
		// The certificates are seeded back at start: restarting costs none.
		if _, err := deploy.SSH(ctx, host, "k3s kubectl -n kube-system rollout restart deploy/traefik"); err != nil {
			slog.Info("certificats", "err", err)
		}
	}
}

func (a *API) retryDue(clusterID string) bool {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	if a.certificateRetried == nil {
		a.certificateRetried = map[string]time.Time{}
	}
	// The first sight only starts the clock: Traefik may still get it alone.
	last, seen := a.certificateRetried[clusterID]
	if !seen {
		a.certificateRetried[clusterID] = time.Now()
		return false
	}
	if time.Since(last) < certificateRetryEvery {
		return false
	}
	a.certificateRetried[clusterID] = time.Now()
	return true
}
