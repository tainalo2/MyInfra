package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

type syncVersion struct {
	Seq    int64
	Origin string
}

func (v syncVersion) after(o syncVersion) bool {
	if v.Seq != o.Seq {
		return v.Seq > o.Seq
	}
	return v.Origin > o.Origin
}

type syncUser struct {
	ID           string    `json:"id"`
	PasswordHash string    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
	Seq          int64     `json:"seq"`
	Origin       string    `json:"origin"`
}

type syncSecret struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"ownerId"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
	Provider    string    `json:"provider"`
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"publicKey"`
	Hint        string    `json:"hint"`
	KeyID       string    `json:"keyId"`
	HasPrivate  bool      `json:"hasPrivate"`
	Ciphertext  []byte    `json:"ciphertext"`
	Nonce       []byte    `json:"nonce"`
	WrappedDEK  []byte    `json:"wrappedDek"`
	CreatedAt   time.Time `json:"createdAt"`
	Seq         int64     `json:"seq"`
	Origin      string    `json:"origin"`
}

type syncAppSecret struct {
	ID         string          `json:"id"`
	OwnerID    string          `json:"ownerId"`
	Name       string          `json:"name"`
	Ciphertext []byte          `json:"ciphertext"`
	Nonce      []byte          `json:"nonce"`
	WrappedDEK []byte          `json:"wrappedDek"`
	Apps       json.RawMessage `json:"apps"`
	CreatedAt  time.Time       `json:"createdAt"`
	Seq        int64           `json:"seq"`
	Origin     string          `json:"origin"`
}

type syncCluster struct {
	ID             string    `json:"id"`
	OwnerID        string    `json:"ownerId"`
	Name           string    `json:"name"`
	OriginCluster  string    `json:"clusterOrigin"`
	KubeCiphertext []byte    `json:"kubeCiphertext"`
	KubeNonce      []byte    `json:"kubeNonce"`
	KubeWrapped    []byte    `json:"kubeWrapped"`
	CreatedAt      time.Time `json:"createdAt"`
	Seq            int64     `json:"seq"`
	Origin         string    `json:"origin"`
}

type syncServer struct {
	ID               string          `json:"id"`
	OwnerID          string          `json:"ownerId"`
	ClusterID        *string         `json:"clusterId"`
	Name             string          `json:"name"`
	OriginServer     string          `json:"serverOrigin"`
	Status           string          `json:"status"`
	Address          string          `json:"address"`
	JoinToken        string          `json:"joinToken"`
	ProviderID       string          `json:"providerId"`
	ProviderServerID string          `json:"providerServerId"`
	Plan             json.RawMessage `json:"plan"`
	Deploy           json.RawMessage `json:"deploy"`
	ControlPlane     bool            `json:"controlPlane"`
	WGIndex          int             `json:"wgIndex,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	Seq              int64           `json:"seq"`
	Origin           string          `json:"origin"`
}

type syncKey struct {
	ServerID string `json:"serverId"`
	SecretID string `json:"secretId"`
	Seq      int64  `json:"seq"`
	Origin   string `json:"origin"`
}

func (r syncKey) ref() string { return r.ServerID + "/" + r.SecretID }

type syncApp struct {
	ID              string          `json:"id"`
	OwnerID         string          `json:"ownerId"`
	Name            string          `json:"name"`
	SourceKind      string          `json:"sourceKind"`
	Source          string          `json:"source"`
	DestinationKind string          `json:"destinationKind"`
	ServerID        *string         `json:"serverId"`
	ClusterID       *string         `json:"clusterId"`
	Replicas        int             `json:"replicas"`
	Variables       json.RawMessage `json:"variables"`
	RunStatus       string          `json:"runStatus"`
	Paused          bool            `json:"paused"`
	Rollout         string          `json:"rollout"`
	RolloutDetail   string          `json:"rolloutDetail"`
	PreparingSince  *time.Time      `json:"preparingSince,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	Seq             int64           `json:"seq"`
	Origin          string          `json:"origin"`
}

type syncService struct {
	ID            string `json:"id"`
	ApplicationID string `json:"applicationId"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Published     bool   `json:"published"`
	Disk          bool   `json:"disk"`
	Replicas      int    `json:"replicas"`
	Listen        int    `json:"listen"`
	DiskPath      string `json:"diskPath"`
	DiskGi        int    `json:"diskGi"`
	Args          string `json:"args"`
	// Spec is left out when empty: a row reads the same as before it existed.
	Spec   json.RawMessage `json:"spec,omitempty"`
	Seq    int64           `json:"seq"`
	Origin string          `json:"origin"`
}

type syncDomain struct {
	ID            string    `json:"id"`
	ApplicationID string    `json:"applicationId"`
	Hostname      string    `json:"hostname"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	Zone          string    `json:"zone"`
	SecretID      string    `json:"secretId"`
	Detail        string    `json:"detail"`
	CreatedAt     time.Time `json:"createdAt"`
	Seq           int64     `json:"seq"`
	Origin        string    `json:"origin"`
}

type syncTemplate struct {
	ID        string          `json:"id"`
	OwnerID   string          `json:"ownerId"`
	Name      string          `json:"name"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"createdAt"`
	Seq       int64           `json:"seq"`
	Origin    string          `json:"origin"`
}

type syncDestruction struct {
	ID        string          `json:"id"`
	OwnerID   string          `json:"ownerId"`
	Kind      string          `json:"kind"`
	TargetID  string          `json:"targetId"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"createdAt"`
	Seq       int64           `json:"seq"`
	Origin    string          `json:"origin"`
}

type syncRun struct {
	ID         string          `json:"id"`
	OwnerID    string          `json:"ownerId"`
	TemplateID string          `json:"templateId"`
	Status     string          `json:"status"`
	ClusterID  string          `json:"clusterId"`
	Body       json.RawMessage `json:"body"`
	CreatedAt  time.Time       `json:"createdAt"`
	Seq        int64           `json:"seq"`
	Origin     string          `json:"origin"`
}

type syncRollback struct {
	ApplicationID string          `json:"applicationId"`
	Images        json.RawMessage `json:"images"`
	RunStatus     string          `json:"runStatus"`
	StartedAt     time.Time       `json:"startedAt"`
	Seq           int64           `json:"seq"`
	Origin        string          `json:"origin"`
}

type syncUpdate struct {
	Kind      string          `json:"kind"`
	TargetID  string          `json:"targetId"`
	OwnerID   string          `json:"ownerId"`
	Current   string          `json:"current"`
	Available string          `json:"available"`
	Level     string          `json:"level"`
	Detail    json.RawMessage `json:"detail"`
	CheckedAt time.Time       `json:"checkedAt"`
	Seq       int64           `json:"seq"`
	Origin    string          `json:"origin"`
}

func (r syncUpdate) ref() string { return r.Kind + "/" + r.TargetID }

type syncUpdateJob struct {
	ID         string     `json:"id"`
	OwnerID    string     `json:"ownerId"`
	ClusterID  string     `json:"clusterId"`
	ServerID   string     `json:"serverId"`
	Status     string     `json:"status"`
	Steps      []byte     `json:"steps"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Seq        int64      `json:"seq"`
	Origin     string     `json:"origin"`
}

type syncTomb struct {
	Table   string          `json:"table"`
	ID      string          `json:"id"`
	Seq     int64           `json:"seq"`
	Origin  string          `json:"origin"`
	Payload json.RawMessage `json:"payload"`
}

type SyncDoc struct {
	Users        []syncUser        `json:"users"`
	Secrets      []syncSecret      `json:"secrets"`
	Clusters     []syncCluster     `json:"clusters"`
	Servers      []syncServer      `json:"servers"`
	Keys         []syncKey         `json:"keys"`
	Applications []syncApp         `json:"applications"`
	Services     []syncService     `json:"services"`
	Domains      []syncDomain      `json:"domains"`
	Templates    []syncTemplate    `json:"templates"`
	Destructions []syncDestruction `json:"destructions"`
	Runs         []syncRun         `json:"runs"`
	Rollbacks    []syncRollback    `json:"rollbacks"`
	AppSecrets   []syncAppSecret   `json:"appSecrets"`
	Updates      []syncUpdate      `json:"updates"`
	UpdateJobs   []syncUpdateJob   `json:"updateJobs"`
	Tombs        []syncTomb        `json:"tombs"`
}

func (d SyncDoc) Hash() string {
	raw, _ := json.Marshal(d.canon())
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func mergeDocs(local, remote SyncDoc) SyncDoc {
	tombs := append(append([]syncTomb{}, local.Tombs...), remote.Tombs...)
	doc := SyncDoc{Tombs: tombs}
	doc.Users, doc.Tombs = mergeUsers(local.Users, remote.Users, doc.Tombs)
	doc.Secrets, doc.Tombs = mergeSecrets(local.Secrets, remote.Secrets, doc.Tombs)
	doc.Clusters, doc.Tombs = mergeClusters(local.Clusters, remote.Clusters, doc.Tombs)
	doc.Servers, doc.Tombs = mergeServers(local.Servers, remote.Servers, doc.Tombs)
	doc.Keys, doc.Tombs = mergeKeys(local.Keys, remote.Keys, doc.Tombs)
	doc.Applications, doc.Tombs = mergeApps(local.Applications, remote.Applications, doc.Tombs)
	doc.Services, doc.Tombs = mergeServices(local.Services, remote.Services, doc.Tombs)
	doc.Domains, doc.Tombs = mergeDomains(local.Domains, remote.Domains, doc.Tombs)
	doc.Templates, doc.Tombs = mergeTemplates(local.Templates, remote.Templates, doc.Tombs)
	doc.Destructions, doc.Tombs = mergeDestructions(local.Destructions, remote.Destructions, doc.Tombs)
	doc.Runs, doc.Tombs = mergeRuns(local.Runs, remote.Runs, doc.Tombs)
	doc.Rollbacks, doc.Tombs = mergeRollbacks(local.Rollbacks, remote.Rollbacks, doc.Tombs)
	doc.AppSecrets, doc.Tombs = mergeAppSecrets(local.AppSecrets, remote.AppSecrets, doc.Tombs)
	doc.Updates, doc.Tombs = mergeUpdates(local.Updates, remote.Updates, doc.Tombs)
	doc.UpdateJobs, doc.Tombs = mergeUpdateJobs(local.UpdateJobs, remote.UpdateJobs, doc.Tombs)
	return reconcile(doc).canon()
}

func mergeUsers(a, b []syncUser, tombs []syncTomb) ([]syncUser, []syncTomb) {
	return mergeByID("users", a, b, tombs, func(r syncUser) string { return r.ID }, func(r syncUser) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeSecrets(a, b []syncSecret, tombs []syncTomb) ([]syncSecret, []syncTomb) {
	return mergeByID("secrets", a, b, tombs, func(r syncSecret) string { return r.ID }, func(r syncSecret) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeClusters(a, b []syncCluster, tombs []syncTomb) ([]syncCluster, []syncTomb) {
	return mergeByID("clusters", a, b, tombs, func(r syncCluster) string { return r.ID }, func(r syncCluster) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeServers(a, b []syncServer, tombs []syncTomb) ([]syncServer, []syncTomb) {
	return mergeByID("servers", a, b, tombs, func(r syncServer) string { return r.ID }, func(r syncServer) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeKeys(a, b []syncKey, tombs []syncTomb) ([]syncKey, []syncTomb) {
	return mergeByID("server_keys", a, b, tombs, func(r syncKey) string { return r.ref() }, func(r syncKey) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeApps(a, b []syncApp, tombs []syncTomb) ([]syncApp, []syncTomb) {
	return mergeByID("applications", a, b, tombs, func(r syncApp) string { return r.ID }, func(r syncApp) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeServices(a, b []syncService, tombs []syncTomb) ([]syncService, []syncTomb) {
	return mergeByID("application_services", a, b, tombs, func(r syncService) string { return r.ID }, func(r syncService) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeDomains(a, b []syncDomain, tombs []syncTomb) ([]syncDomain, []syncTomb) {
	return mergeByID("domains", a, b, tombs, func(r syncDomain) string { return r.ID }, func(r syncDomain) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeTemplates(a, b []syncTemplate, tombs []syncTomb) ([]syncTemplate, []syncTomb) {
	return mergeByID("templates", a, b, tombs, func(r syncTemplate) string { return r.ID }, func(r syncTemplate) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeDestructions(a, b []syncDestruction, tombs []syncTomb) ([]syncDestruction, []syncTomb) {
	return mergeByID("destructions", a, b, tombs, func(r syncDestruction) string { return r.ID }, func(r syncDestruction) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeRuns(a, b []syncRun, tombs []syncTomb) ([]syncRun, []syncTomb) {
	return mergeByID("template_runs", a, b, tombs, func(r syncRun) string { return r.ID }, func(r syncRun) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeRollbacks(a, b []syncRollback, tombs []syncTomb) ([]syncRollback, []syncTomb) {
	return mergeByID("image_rollbacks", a, b, tombs, func(r syncRollback) string { return r.ApplicationID }, func(r syncRollback) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeAppSecrets(a, b []syncAppSecret, tombs []syncTomb) ([]syncAppSecret, []syncTomb) {
	return mergeByID("app_secrets", a, b, tombs, func(r syncAppSecret) string { return r.ID }, func(r syncAppSecret) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeUpdates(a, b []syncUpdate, tombs []syncTomb) ([]syncUpdate, []syncTomb) {
	return mergeByID("updates", a, b, tombs, func(r syncUpdate) string { return r.ref() }, func(r syncUpdate) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeUpdateJobs(a, b []syncUpdateJob, tombs []syncTomb) ([]syncUpdateJob, []syncTomb) {
	return mergeByID("update_jobs", a, b, tombs, func(r syncUpdateJob) string { return r.ID }, func(r syncUpdateJob) syncVersion { return syncVersion{r.Seq, r.Origin} })
}

func mergeByID[T any](table string, left, right []T, tombs []syncTomb, id func(T) string, ver func(T) syncVersion) ([]T, []syncTomb) {
	type cand struct {
		row T
		ver syncVersion
		ok  bool
	}
	side := func(rows []T) map[string]cand {
		out := map[string]cand{}
		for _, row := range rows {
			out[id(row)] = cand{row: row, ver: ver(row), ok: true}
		}
		return out
	}
	a := side(left)
	b := side(right)
	best := map[string]syncTomb{}
	var rest []syncTomb
	for _, tb := range tombs {
		if tb.Table != table {
			rest = append(rest, tb)
			continue
		}
		cur, ok := best[tb.ID]
		if !ok || (syncVersion{tb.Seq, tb.Origin}).after(syncVersion{cur.Seq, cur.Origin}) {
			best[tb.ID] = tb
		}
	}
	ids := map[string]struct{}{}
	for key := range a {
		ids[key] = struct{}{}
	}
	for key := range b {
		ids[key] = struct{}{}
	}
	for key := range best {
		ids[key] = struct{}{}
	}
	var keep []T
	for key := range ids {
		var row T
		var rv syncVersion
		have := false
		if c := a[key]; c.ok {
			row, rv, have = c.row, c.ver, true
		}
		if c := b[key]; c.ok && (!have || c.ver.after(rv)) {
			row, rv, have = c.row, c.ver, true
		}
		tb, hasTomb := best[key]
		tv := syncVersion{tb.Seq, tb.Origin}
		if have && (!hasTomb || !tv.after(rv)) {
			keep = append(keep, row)
			continue
		}
		if hasTomb {
			if len(tb.Payload) == 0 && have {
				tb.Payload, _ = json.Marshal(row)
			}
			rest = append(rest, tb)
		}
	}
	return keep, rest
}

type syncLink struct {
	childTable  string
	childID     string
	parentTable string
	parentID    string
	child       syncVersion
}

func reconcile(doc SyncDoc) SyncDoc {
	for pass := 0; pass < 12; pass++ {
		changed := false
		for _, tb := range append([]syncTomb{}, doc.Tombs...) {
			if rowAlive(doc, tb.Table, tb.ID) {
				continue
			}
			parent := syncVersion{tb.Seq, tb.Origin}
			var newest syncVersion
			newer := false
			var older []syncLink
			for _, lk := range docLinks(doc) {
				if lk.parentTable != tb.Table || lk.parentID != tb.ID || !rowAlive(doc, lk.childTable, lk.childID) {
					continue
				}
				if lk.child.after(parent) {
					newer = true
					if !newest.after(lk.child) {
						newest = lk.child
					}
				} else {
					older = append(older, lk)
				}
			}
			if newer {
				if resurrect(&doc, tb, newest) {
					changed = true
					continue
				}
				for _, lk := range docLinks(doc) {
					if lk.parentTable == tb.Table && lk.parentID == tb.ID && rowAlive(doc, lk.childTable, lk.childID) {
						if tombstoneRow(&doc, lk.childTable, lk.childID, parent) {
							changed = true
						}
					}
				}
				continue
			}
			for _, lk := range older {
				if tombstoneRow(&doc, lk.childTable, lk.childID, parent) {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return dropLostTombs(doc)
}

func resurrect(doc *SyncDoc, tb syncTomb, v syncVersion) bool {
	if len(tb.Payload) == 0 {
		return false
	}
	switch tb.Table {
	case "users":
		row, err := userFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Users = append(doc.Users, row)
	case "secrets":
		row, err := secretFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Secrets = append(doc.Secrets, row)
	case "clusters":
		row, err := clusterFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Clusters = append(doc.Clusters, row)
	case "servers":
		row, err := serverFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Servers = append(doc.Servers, row)
	case "server_keys":
		row, err := keyFromPayload(tb.Payload)
		if err != nil || row.ref() == "" || row.ref() == "/" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Keys = append(doc.Keys, row)
	case "applications":
		row, err := appFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Applications = append(doc.Applications, row)
	case "application_services":
		row, err := serviceFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Services = append(doc.Services, row)
	case "domains":
		row, err := domainFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Domains = append(doc.Domains, row)
	case "templates":
		row, err := templateFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Templates = append(doc.Templates, row)
	case "destructions":
		row, err := destructionFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Destructions = append(doc.Destructions, row)
	case "template_runs":
		row, err := runFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Runs = append(doc.Runs, row)
	case "image_rollbacks":
		row, err := rollbackFromPayload(tb.Payload)
		if err != nil || row.ApplicationID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Rollbacks = append(doc.Rollbacks, row)
	case "app_secrets":
		row, err := appSecretFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.AppSecrets = append(doc.AppSecrets, row)
	case "updates":
		row, err := updateFromPayload(tb.Payload)
		if err != nil || row.Kind == "" || row.TargetID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.Updates = append(doc.Updates, row)
	case "update_jobs":
		row, err := updateJobFromPayload(tb.Payload)
		if err != nil || row.ID == "" {
			return false
		}
		row.Seq, row.Origin = v.Seq, v.Origin
		doc.UpdateJobs = append(doc.UpdateJobs, row)
	default:
		return false
	}
	doc.Tombs = removeTomb(doc.Tombs, tb.Table, tb.ID)
	return true
}

func tombstoneRow(doc *SyncDoc, table, id string, v syncVersion) bool {
	raw, ok := takeRow(doc, table, id)
	if !ok {
		return false
	}
	putTomb(doc, syncTomb{Table: table, ID: id, Seq: v.Seq, Origin: v.Origin, Payload: raw})
	return true
}

func putTomb(doc *SyncDoc, tb syncTomb) {
	for i, cur := range doc.Tombs {
		if cur.Table != tb.Table || cur.ID != tb.ID {
			continue
		}
		if (syncVersion{cur.Seq, cur.Origin}).after(syncVersion{tb.Seq, tb.Origin}) {
			return
		}
		doc.Tombs[i] = tb
		return
	}
	doc.Tombs = append(doc.Tombs, tb)
}

func removeTomb(rows []syncTomb, table, id string) []syncTomb {
	out := rows[:0]
	for _, row := range rows {
		if row.Table == table && row.ID == id {
			continue
		}
		out = append(out, row)
	}
	return out
}

func dropLostTombs(doc SyncDoc) SyncDoc {
	alive := map[string]syncVersion{}
	for _, row := range doc.Users {
		alive["users/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Secrets {
		alive["secrets/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Clusters {
		alive["clusters/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Servers {
		alive["servers/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Keys {
		alive["server_keys/"+row.ref()] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Applications {
		alive["applications/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Services {
		alive["application_services/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Domains {
		alive["domains/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Templates {
		alive["templates/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Destructions {
		alive["destructions/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Runs {
		alive["template_runs/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Rollbacks {
		alive["image_rollbacks/"+row.ApplicationID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.AppSecrets {
		alive["app_secrets/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.Updates {
		alive["updates/"+row.ref()] = syncVersion{row.Seq, row.Origin}
	}
	for _, row := range doc.UpdateJobs {
		alive["update_jobs/"+row.ID] = syncVersion{row.Seq, row.Origin}
	}
	var keep []syncTomb
	for _, tb := range doc.Tombs {
		if ver, ok := alive[tb.Table+"/"+tb.ID]; ok && !(syncVersion{tb.Seq, tb.Origin}).after(ver) {
			continue
		}
		keep = append(keep, tb)
	}
	doc.Tombs = keep
	return doc
}

func rowAlive(doc SyncDoc, table, id string) bool {
	switch table {
	case "users":
		for _, row := range doc.Users {
			if row.ID == id {
				return true
			}
		}
	case "secrets":
		for _, row := range doc.Secrets {
			if row.ID == id {
				return true
			}
		}
	case "clusters":
		for _, row := range doc.Clusters {
			if row.ID == id {
				return true
			}
		}
	case "servers":
		for _, row := range doc.Servers {
			if row.ID == id {
				return true
			}
		}
	case "server_keys":
		for _, row := range doc.Keys {
			if row.ref() == id {
				return true
			}
		}
	case "applications":
		for _, row := range doc.Applications {
			if row.ID == id {
				return true
			}
		}
	case "application_services":
		for _, row := range doc.Services {
			if row.ID == id {
				return true
			}
		}
	case "domains":
		for _, row := range doc.Domains {
			if row.ID == id {
				return true
			}
		}
	case "templates":
		for _, row := range doc.Templates {
			if row.ID == id {
				return true
			}
		}
	case "destructions":
		for _, row := range doc.Destructions {
			if row.ID == id {
				return true
			}
		}
	case "template_runs":
		for _, row := range doc.Runs {
			if row.ID == id {
				return true
			}
		}
	case "image_rollbacks":
		for _, row := range doc.Rollbacks {
			if row.ApplicationID == id {
				return true
			}
		}
	case "app_secrets":
		for _, row := range doc.AppSecrets {
			if row.ID == id {
				return true
			}
		}
	case "updates":
		for _, row := range doc.Updates {
			if row.ref() == id {
				return true
			}
		}
	case "update_jobs":
		for _, row := range doc.UpdateJobs {
			if row.ID == id {
				return true
			}
		}
	}
	return false
}

func docLinks(doc SyncDoc) []syncLink {
	var out []syncLink
	for _, row := range doc.Secrets {
		out = append(out, syncLink{"secrets", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Clusters {
		out = append(out, syncLink{"clusters", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Servers {
		out = append(out, syncLink{"servers", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
		if row.ClusterID != nil && *row.ClusterID != "" {
			out = append(out, syncLink{"servers", row.ID, "clusters", *row.ClusterID, syncVersion{row.Seq, row.Origin}})
		}
	}
	for _, row := range doc.Keys {
		out = append(out, syncLink{"server_keys", row.ref(), "servers", row.ServerID, syncVersion{row.Seq, row.Origin}})
		out = append(out, syncLink{"server_keys", row.ref(), "secrets", row.SecretID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Applications {
		out = append(out, syncLink{"applications", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
		if row.ServerID != nil && *row.ServerID != "" {
			out = append(out, syncLink{"applications", row.ID, "servers", *row.ServerID, syncVersion{row.Seq, row.Origin}})
		}
		if row.ClusterID != nil && *row.ClusterID != "" {
			out = append(out, syncLink{"applications", row.ID, "clusters", *row.ClusterID, syncVersion{row.Seq, row.Origin}})
		}
	}
	for _, row := range doc.Services {
		out = append(out, syncLink{"application_services", row.ID, "applications", row.ApplicationID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Domains {
		out = append(out, syncLink{"domains", row.ID, "applications", row.ApplicationID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Templates {
		out = append(out, syncLink{"templates", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Destructions {
		out = append(out, syncLink{"destructions", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Runs {
		out = append(out, syncLink{"template_runs", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Rollbacks {
		out = append(out, syncLink{"image_rollbacks", row.ApplicationID, "applications", row.ApplicationID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.AppSecrets {
		out = append(out, syncLink{"app_secrets", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.Updates {
		out = append(out, syncLink{"updates", row.ref(), "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	for _, row := range doc.UpdateJobs {
		out = append(out, syncLink{"update_jobs", row.ID, "users", row.OwnerID, syncVersion{row.Seq, row.Origin}})
	}
	return out
}

func takeRow(doc *SyncDoc, table, id string) (json.RawMessage, bool) {
	switch table {
	case "users":
		return take(&doc.Users, id, func(r syncUser) string { return r.ID })
	case "secrets":
		return take(&doc.Secrets, id, func(r syncSecret) string { return r.ID })
	case "clusters":
		return take(&doc.Clusters, id, func(r syncCluster) string { return r.ID })
	case "servers":
		return take(&doc.Servers, id, func(r syncServer) string { return r.ID })
	case "server_keys":
		return take(&doc.Keys, id, func(r syncKey) string { return r.ref() })
	case "applications":
		return take(&doc.Applications, id, func(r syncApp) string { return r.ID })
	case "application_services":
		return take(&doc.Services, id, func(r syncService) string { return r.ID })
	case "domains":
		return take(&doc.Domains, id, func(r syncDomain) string { return r.ID })
	case "templates":
		return take(&doc.Templates, id, func(r syncTemplate) string { return r.ID })
	case "destructions":
		return take(&doc.Destructions, id, func(r syncDestruction) string { return r.ID })
	case "template_runs":
		return take(&doc.Runs, id, func(r syncRun) string { return r.ID })
	case "image_rollbacks":
		return take(&doc.Rollbacks, id, func(r syncRollback) string { return r.ApplicationID })
	case "app_secrets":
		return take(&doc.AppSecrets, id, func(r syncAppSecret) string { return r.ID })
	case "updates":
		return take(&doc.Updates, id, func(r syncUpdate) string { return r.ref() })
	case "update_jobs":
		return take(&doc.UpdateJobs, id, func(r syncUpdateJob) string { return r.ID })
	default:
		return nil, false
	}
}

func take[T any](rows *[]T, id string, key func(T) string) (json.RawMessage, bool) {
	for i, row := range *rows {
		if key(row) != id {
			continue
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return nil, false
		}
		*rows = append((*rows)[:i], (*rows)[i+1:]...)
		return raw, true
	}
	return nil, false
}

func (d SyncDoc) canon() SyncDoc {
	d.Users = canonUsers(d.Users)
	d.Secrets = canonSecrets(d.Secrets)
	d.Clusters = canonClusters(d.Clusters)
	d.Servers = canonServers(d.Servers)
	d.Keys = canonKeys(d.Keys)
	d.Applications = canonApps(d.Applications)
	d.Services = canonServices(d.Services)
	d.Domains = canonDomains(d.Domains)
	d.Templates = canonTemplates(d.Templates)
	d.Destructions = canonDestructions(d.Destructions)
	d.Runs = canonRuns(d.Runs)
	d.Rollbacks = canonRollbacks(d.Rollbacks)
	d.AppSecrets = canonAppSecrets(d.AppSecrets)
	d.Updates = canonUpdates(d.Updates)
	d.UpdateJobs = canonUpdateJobs(d.UpdateJobs)
	if d.Tombs == nil {
		d.Tombs = []syncTomb{}
	}
	for i := range d.Tombs {
		d.Tombs[i].Payload = canonPayload(d.Tombs[i].Table, d.Tombs[i].Payload)
	}
	sort.Slice(d.Tombs, func(i, j int) bool {
		if d.Tombs[i].Table != d.Tombs[j].Table {
			return d.Tombs[i].Table < d.Tombs[j].Table
		}
		return d.Tombs[i].ID < d.Tombs[j].ID
	})
	return d
}

func canonPayload(table string, raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`null`)
	}
	var out any
	var err error
	switch table {
	case "users":
		out, err = userFromPayload(raw)
	case "secrets":
		out, err = secretFromPayload(raw)
	case "clusters":
		out, err = clusterFromPayload(raw)
	case "servers":
		out, err = serverFromPayload(raw)
	case "server_keys":
		out, err = keyFromPayload(raw)
	case "applications":
		out, err = appFromPayload(raw)
	case "application_services":
		out, err = serviceFromPayload(raw)
	case "domains":
		out, err = domainFromPayload(raw)
	case "templates":
		out, err = templateFromPayload(raw)
	case "destructions":
		out, err = destructionFromPayload(raw)
	case "template_runs":
		out, err = runFromPayload(raw)
	case "image_rollbacks":
		out, err = rollbackFromPayload(raw)
	case "app_secrets":
		out, err = appSecretFromPayload(raw)
	case "updates":
		out, err = updateFromPayload(raw)
	case "update_jobs":
		out, err = updateJobFromPayload(raw)
	default:
		return raw
	}
	if err != nil {
		return raw
	}
	next, err := json.Marshal(out)
	if err != nil {
		return raw
	}
	return next
}

func maxSeq(doc SyncDoc) int64 {
	max := int64(0)
	bump := func(seq int64) {
		if seq > max {
			max = seq
		}
	}
	for _, row := range doc.Users {
		bump(row.Seq)
	}
	for _, row := range doc.Secrets {
		bump(row.Seq)
	}
	for _, row := range doc.Clusters {
		bump(row.Seq)
	}
	for _, row := range doc.Servers {
		bump(row.Seq)
	}
	for _, row := range doc.Keys {
		bump(row.Seq)
	}
	for _, row := range doc.Applications {
		bump(row.Seq)
	}
	for _, row := range doc.Services {
		bump(row.Seq)
	}
	for _, row := range doc.Domains {
		bump(row.Seq)
	}
	for _, row := range doc.Templates {
		bump(row.Seq)
	}
	for _, row := range doc.Destructions {
		bump(row.Seq)
	}
	for _, row := range doc.Runs {
		bump(row.Seq)
	}
	for _, row := range doc.Rollbacks {
		bump(row.Seq)
	}
	for _, row := range doc.AppSecrets {
		bump(row.Seq)
	}
	for _, row := range doc.Updates {
		bump(row.Seq)
	}
	for _, row := range doc.UpdateJobs {
		bump(row.Seq)
	}
	for _, tb := range doc.Tombs {
		bump(tb.Seq)
	}
	return max
}

func idsOf[T any](rows []T, id func(T) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, id(row))
	}
	return out
}

func utc(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

// canonSpec writes a service's spec the one way, and an empty one as nothing.
func canonSpec(raw json.RawMessage) json.RawMessage {
	s := readSpec(raw)
	if s.Empty() {
		return nil
	}
	return json.RawMessage(specJSON(s))
}

func canonJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(fallback)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(fallback)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(fallback)
	}
	return out
}

func filled(b []byte) []byte {
	if b == nil {
		return []byte{}
	}
	return b
}

func isSnake(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	_, ok := m["sync_seq"]
	return ok
}

type bag map[string]json.RawMessage

func bagOf(raw json.RawMessage) bag {
	var m bag
	_ = json.Unmarshal(raw, &m)
	return m
}

func (b bag) str(k string) string {
	if b[k] == nil || bytes.Equal(b[k], []byte("null")) {
		return ""
	}
	var s string
	_ = json.Unmarshal(b[k], &s)
	return s
}

func (b bag) strPtr(k string) *string {
	if b[k] == nil || bytes.Equal(b[k], []byte("null")) {
		return nil
	}
	s := b.str(k)
	return &s
}

func (b bag) boolean(k string) bool {
	var v bool
	if b[k] != nil {
		_ = json.Unmarshal(b[k], &v)
	}
	return v
}

func (b bag) num(k string) int {
	var v int
	if b[k] != nil {
		_ = json.Unmarshal(b[k], &v)
	}
	return v
}

func (b bag) i64(k string) int64 {
	var v int64
	if b[k] != nil {
		_ = json.Unmarshal(b[k], &v)
	}
	return v
}

func (b bag) stamp(k string) time.Time {
	if b[k] == nil || bytes.Equal(b[k], []byte("null")) {
		return time.Time{}
	}
	var t time.Time
	_ = json.Unmarshal(b[k], &t)
	return utc(t)
}

func (b bag) stampPtr(k string) *time.Time {
	if b[k] == nil || bytes.Equal(b[k], []byte("null")) {
		return nil
	}
	var t time.Time
	if err := json.Unmarshal(b[k], &t); err != nil || t.IsZero() {
		return nil
	}
	t = utc(t)
	return &t
}

func (b bag) blob(k string) []byte {
	if b[k] == nil || bytes.Equal(b[k], []byte("null")) {
		return nil
	}
	var s string
	if json.Unmarshal(b[k], &s) != nil {
		return nil
	}
	if len(s) >= 2 && s[0] == '\\' && s[1] == 'x' {
		buf, err := hex.DecodeString(s[2:])
		if err != nil {
			return nil
		}
		return buf
	}
	buf, err := decodeBase64(s)
	if err != nil {
		return []byte(s)
	}
	return buf
}

func (b bag) raw(k, fallback string) json.RawMessage {
	if b[k] == nil || bytes.Equal(b[k], []byte("null")) {
		return json.RawMessage(fallback)
	}
	return canonJSON(b[k], fallback)
}

func decodeBase64(s string) ([]byte, error) {
	if s == "" {
		return []byte{}, nil
	}
	return base64.StdEncoding.DecodeString(s)
}
