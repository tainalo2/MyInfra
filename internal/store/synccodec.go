package store

import (
	"encoding/json"
	"sort"
)

func userFromPayload(raw json.RawMessage) (syncUser, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncUser{ID: b.str("id"), PasswordHash: b.str("password_hash"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin")}, nil
	}
	var row syncUser
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	return row, err
}

func secretFromPayload(raw json.RawMessage) (syncSecret, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncSecret{
			ID: b.str("id"), OwnerID: b.str("owner_id"), Kind: b.str("kind"), Name: b.str("name"), Provider: b.str("provider"),
			Fingerprint: b.str("fingerprint"), PublicKey: b.str("public_key"), Hint: b.str("hint"), KeyID: b.str("key_id"),
			HasPrivate: b.boolean("has_private"), Ciphertext: b.blob("ciphertext"), Nonce: b.blob("nonce"), WrappedDEK: b.blob("wrapped_dek"),
			CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncSecret
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	return row, err
}

func appSecretFromPayload(raw json.RawMessage) (syncAppSecret, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncAppSecret{
			ID: b.str("id"), OwnerID: b.str("owner_id"), Name: b.str("name"),
			Ciphertext: b.blob("ciphertext"), Nonce: b.blob("nonce"), WrappedDEK: b.blob("wrapped_dek"),
			Apps: b.raw("apps", "[]"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncAppSecret
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	row.Apps = canonAppList(row.Apps)
	return row, err
}

func canonAppList(raw json.RawMessage) json.RawMessage {
	raw = canonJSON(raw, "[]")
	var ids []string
	if json.Unmarshal(raw, &ids) != nil {
		return json.RawMessage(`[]`)
	}
	if ids == nil {
		ids = []string{}
	}
	sort.Strings(ids)
	next, err := json.Marshal(ids)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return next
}

func clusterFromPayload(raw json.RawMessage) (syncCluster, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncCluster{
			ID: b.str("id"), OwnerID: b.str("owner_id"), Name: b.str("name"), OriginCluster: b.str("origin"),
			KubeCiphertext: b.blob("kube_ciphertext"), KubeNonce: b.blob("kube_nonce"), KubeWrapped: b.blob("kube_wrapped"),
			CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncCluster
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	return row, err
}

func serverFromPayload(raw json.RawMessage) (syncServer, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncServer{
			ID: b.str("id"), OwnerID: b.str("owner_id"), ClusterID: b.strPtr("cluster_id"), Name: b.str("name"),
			OriginServer: b.str("origin"), Status: b.str("status"), Address: b.str("address"), JoinToken: b.str("join_token"),
			ProviderID: b.str("provider_id"), ProviderServerID: b.str("provider_server_id"), Plan: b.raw("plan", "{}"), Deploy: b.raw("deploy", "[]"),
			ControlPlane: b.boolean("control_plane"), WGIndex: int(b.i64("wg_index")), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncServer
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	row.Plan = canonJSON(row.Plan, "{}")
	row.Deploy = canonJSON(row.Deploy, "[]")
	return row, err
}

func keyFromPayload(raw json.RawMessage) (syncKey, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncKey{ServerID: b.str("server_id"), SecretID: b.str("secret_id"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin")}, nil
	}
	var row syncKey
	err := json.Unmarshal(raw, &row)
	return row, err
}

func appFromPayload(raw json.RawMessage) (syncApp, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncApp{
			ID: b.str("id"), OwnerID: b.str("owner_id"), Name: b.str("name"), SourceKind: b.str("source_kind"), Source: b.str("source"),
			DestinationKind: b.str("destination_kind"), ServerID: b.strPtr("server_id"), ClusterID: b.strPtr("cluster_id"),
			Replicas: b.num("replicas"), Variables: b.raw("variables", "{}"), RunStatus: b.str("run_status"), Paused: b.boolean("paused"),
			Rollout: b.str("rollout"), RolloutDetail: b.str("rollout_detail"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncApp
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	row.Variables = canonJSON(row.Variables, "{}")
	return row, err
}

func serviceFromPayload(raw json.RawMessage) (syncService, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncService{
			ID: b.str("id"), ApplicationID: b.str("application_id"), Name: b.str("name"), Image: b.str("image"),
			Published: b.boolean("published"), Disk: b.boolean("disk"), Replicas: b.num("replicas"), Listen: b.num("listen"),
			DiskPath: b.str("disk_path"), DiskGi: b.num("disk_gi"), Args: b.str("args"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncService
	return row, json.Unmarshal(raw, &row)
}

func domainFromPayload(raw json.RawMessage) (syncDomain, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncDomain{
			ID: b.str("id"), ApplicationID: b.str("application_id"), Hostname: b.str("hostname"), Kind: b.str("kind"), Status: b.str("status"),
			Zone: b.str("zone"), SecretID: b.str("secret_id"), Detail: b.str("detail"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncDomain
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	return row, err
}

func templateFromPayload(raw json.RawMessage) (syncTemplate, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncTemplate{ID: b.str("id"), OwnerID: b.str("owner_id"), Name: b.str("name"), Body: b.raw("body", "{}"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin")}, nil
	}
	var row syncTemplate
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	row.Body = canonJSON(row.Body, "{}")
	return row, err
}

func destructionFromPayload(raw json.RawMessage) (syncDestruction, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncDestruction{
			ID: b.str("id"), OwnerID: b.str("owner_id"), Kind: b.str("kind"), TargetID: b.str("target_id"), Name: b.str("name"),
			Status: b.str("status"), Body: b.raw("body", "{}"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncDestruction
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	row.Body = canonJSON(row.Body, "{}")
	return row, err
}

func runFromPayload(raw json.RawMessage) (syncRun, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncRun{
			ID: b.str("id"), OwnerID: b.str("owner_id"), TemplateID: b.str("template_id"), Status: b.str("status"), ClusterID: b.str("cluster_id"),
			Body: b.raw("body", "{}"), CreatedAt: b.stamp("created_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncRun
	err := json.Unmarshal(raw, &row)
	row.CreatedAt = utc(row.CreatedAt)
	row.Body = canonJSON(row.Body, "{}")
	return row, err
}

func rollbackFromPayload(raw json.RawMessage) (syncRollback, error) {
	if isSnake(raw) {
		b := bagOf(raw)
		return syncRollback{
			ApplicationID: b.str("application_id"), Images: b.raw("images", "[]"), RunStatus: b.str("run_status"),
			StartedAt: b.stamp("started_at"), Seq: b.i64("sync_seq"), Origin: b.str("sync_origin"),
		}, nil
	}
	var row syncRollback
	err := json.Unmarshal(raw, &row)
	row.StartedAt = utc(row.StartedAt)
	row.Images = canonJSON(row.Images, "[]")
	return row, err
}

func canonUsers(rows []syncUser) []syncUser {
	if rows == nil {
		rows = []syncUser{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonSecrets(rows []syncSecret) []syncSecret {
	if rows == nil {
		rows = []syncSecret{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonClusters(rows []syncCluster) []syncCluster {
	if rows == nil {
		rows = []syncCluster{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		if rows[i].OriginCluster == "" {
			rows[i].OriginCluster = "created"
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonServers(rows []syncServer) []syncServer {
	if rows == nil {
		rows = []syncServer{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		rows[i].Plan = canonJSON(rows[i].Plan, "{}")
		rows[i].Deploy = canonJSON(rows[i].Deploy, "[]")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonKeys(rows []syncKey) []syncKey {
	if rows == nil {
		rows = []syncKey{}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ref() < rows[j].ref() })
	return rows
}

func canonApps(rows []syncApp) []syncApp {
	if rows == nil {
		rows = []syncApp{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		rows[i].Variables = canonJSON(rows[i].Variables, "{}")
		if rows[i].Replicas < 1 {
			rows[i].Replicas = 1
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonServices(rows []syncService) []syncService {
	if rows == nil {
		rows = []syncService{}
	}
	for i := range rows {
		if rows[i].Replicas < 1 {
			rows[i].Replicas = 1
		}
		if rows[i].Listen < 1 {
			rows[i].Listen = 80
		}
		if rows[i].Listen > 65535 {
			rows[i].Listen = 65535
		}
		if rows[i].DiskGi < 1 {
			rows[i].DiskGi = 1
		}
		if rows[i].DiskPath == "" {
			rows[i].DiskPath = "/data"
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonDomains(rows []syncDomain) []syncDomain {
	if rows == nil {
		rows = []syncDomain{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonTemplates(rows []syncTemplate) []syncTemplate {
	if rows == nil {
		rows = []syncTemplate{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		rows[i].Body = canonJSON(rows[i].Body, "{}")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonDestructions(rows []syncDestruction) []syncDestruction {
	if rows == nil {
		rows = []syncDestruction{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		rows[i].Body = canonJSON(rows[i].Body, "{}")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonRuns(rows []syncRun) []syncRun {
	if rows == nil {
		rows = []syncRun{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		rows[i].Body = canonJSON(rows[i].Body, "{}")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

func canonRollbacks(rows []syncRollback) []syncRollback {
	if rows == nil {
		rows = []syncRollback{}
	}
	for i := range rows {
		rows[i].StartedAt = utc(rows[i].StartedAt)
		rows[i].Images = canonJSON(rows[i].Images, "[]")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ApplicationID < rows[j].ApplicationID })
	return rows
}

func canonAppSecrets(rows []syncAppSecret) []syncAppSecret {
	if rows == nil {
		rows = []syncAppSecret{}
	}
	for i := range rows {
		rows[i].CreatedAt = utc(rows[i].CreatedAt)
		rows[i].Apps = canonAppList(rows[i].Apps)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}
