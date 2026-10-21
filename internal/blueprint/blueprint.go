package blueprint

import "strings"

type Body struct {
	ClusterName  string   `json:"clusterName,omitempty"`
	Servers      []Server `json:"servers,omitempty"`
	Applications []App    `json:"applications,omitempty"`
}

type Server struct {
	Name         string   `json:"name"`
	ProviderID   string   `json:"providerId,omitempty"`
	Provider     string   `json:"provider,omitempty"`
	ProviderName string   `json:"providerName,omitempty"`
	Location     string   `json:"location,omitempty"`
	ProjectID    string   `json:"projectId,omitempty"`
	OfferID      string   `json:"offerId,omitempty"`
	Period       string   `json:"period,omitempty"`
	StorageID    string   `json:"storageId,omitempty"`
	StorageGB    int      `json:"storageGb,omitempty"`
	KeyIDs       []string `json:"keyIds,omitempty"`
	KeyNames     []string `json:"keyNames,omitempty"`
	ControlPlane bool     `json:"controlPlane,omitempty"`
}

type App struct {
	Name       string `json:"name"`
	SourceKind string `json:"sourceKind"`
	Source     string `json:"source"`
	// Catalog is the catalogue article the app comes from: its variables,
	// passwords included, are only drawn when the template runs.
	Catalog   string            `json:"catalog,omitempty"`
	Replicas  int               `json:"replicas,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
	Services  []Service         `json:"services,omitempty"`
	Names     []Name            `json:"names,omitempty"`
}

type Service struct {
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Published bool     `json:"published"`
	Disk      bool     `json:"disk"`
	Replicas  int      `json:"replicas,omitempty"`
	Listen    int      `json:"listen,omitempty"`
	DiskPath  string   `json:"diskPath,omitempty"`
	DiskGi    int      `json:"diskGi,omitempty"`
	Args      []string `json:"args,omitempty"`
}

type Name struct {
	Subdomain string `json:"subdomain"`
	Zone      string `json:"zone"`
}

type Step struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	ResourceID string `json:"resourceId,omitempty"`
}

type Run struct {
	ClusterName     string   `json:"clusterName,omitempty"`
	TargetClusterID string   `json:"targetClusterId,omitempty"`
	Servers         []Server `json:"servers,omitempty"`
	Applications    []App    `json:"applications,omitempty"`
	Steps           []Step   `json:"steps"`
}

type Decision struct {
	Kind   string
	Index  int
	Detail string
}

func Validate(body Body) string {
	if message := ValidateShape(body); message != "" {
		return message
	}
	return CheckControlPlanes(body)
}

// ValidateShape checks a template without judging its control planes, for a
// template that describes a cluster as it already is.
func ValidateShape(body Body) string {
	if len(body.Servers) == 0 && len(body.Applications) == 0 {
		return "Ajoutez un serveur ou une application."
	}
	if strings.TrimSpace(body.ClusterName) != "" && len(body.Servers) == 0 {
		return "Un cluster nouveau a au moins un serveur."
	}
	return ""
}

// CheckControlPlanes refuses an even number of control planes.
func CheckControlPlanes(body Body) string {
	planes := 0
	for _, server := range body.Servers {
		if server.ControlPlane {
			planes++
		}
	}
	// etcd needs a majority: with two control planes, losing one stops the cluster.
	if planes%2 == 0 && planes > 0 {
		return "Le nombre de control planes doit être impair (1, 3 ou 5) : avec un nombre pair, la perte d'une machine arrête le cluster."
	}
	return ""
}

func IsControlPlane(run Run, serverIndex int) bool {
	if serverIndex < 0 || serverIndex >= len(run.Servers) {
		return false
	}
	for _, item := range run.Servers {
		if item.ControlPlane {
			return run.Servers[serverIndex].ControlPlane
		}
	}
	return strings.TrimSpace(run.ClusterName) != "" && serverIndex == 0
}

func serverIndex(run Run, stepIndex int) int {
	n := 0
	for i, step := range run.Steps {
		if step.Kind != "server" {
			continue
		}
		if i == stepIndex {
			return n
		}
		n++
	}
	return -1
}

func NeedsCluster(body Body) bool {
	return strings.TrimSpace(body.ClusterName) == "" && (len(body.Servers) > 0 || len(body.Applications) > 0)
}

func ServerReady(server Server) bool {
	if strings.TrimSpace(server.ProviderID) == "" || strings.TrimSpace(server.OfferID) == "" || strings.TrimSpace(server.Location) == "" || strings.TrimSpace(server.Period) == "" || len(server.KeyIDs) == 0 {
		return false
	}
	return len(server.KeyNames) <= len(server.KeyIDs)
}

func Gaps(body Body) []string {
	var gaps []string
	if NeedsCluster(body) {
		gaps = append(gaps, "cluster")
	}
	for _, server := range body.Servers {
		if !ServerReady(server) {
			gaps = append(gaps, "server")
			break
		}
	}
	for _, app := range body.Applications {
		for _, name := range app.Names {
			if strings.TrimSpace(name.Subdomain) != "" && strings.TrimSpace(name.Zone) == "" {
				gaps = append(gaps, "zone")
				return gaps
			}
		}
	}
	return gaps
}

func Build(body Body, targetClusterID string) Run {
	run := Run{
		ClusterName:     strings.TrimSpace(body.ClusterName),
		TargetClusterID: strings.TrimSpace(targetClusterID),
		Servers:         body.Servers,
		Applications:    body.Applications,
	}
	if run.ClusterName != "" {
		run.Steps = append(run.Steps, Step{Kind: "cluster", Name: run.ClusterName, Status: "pending"})
	} else if run.TargetClusterID != "" {
		run.Steps = append(run.Steps, Step{Kind: "cluster", Name: run.TargetClusterID, Status: "done", ResourceID: run.TargetClusterID})
	}
	for _, server := range run.Servers {
		run.Steps = append(run.Steps, Step{Kind: "server", Name: server.Name, Status: "pending"})
	}
	for _, app := range run.Applications {
		run.Steps = append(run.Steps, Step{Kind: "application", Name: app.Name, Status: "pending"})
	}
	if run.Steps == nil {
		run.Steps = []Step{}
	}
	return run
}

// Next picks what the run does now. Only the cluster stops everything: a
// server that fails leaves the others, and the applications start once every
// server has settled and a control plane is linked. A failed step then makes
// the run end as failed, after all the rest is done, so that resuming it only
// starts again what failed.
func Next(run Run, leader bool, status map[string]string) Decision {
	failed := -1
	for i, step := range run.Steps {
		if step.Status != "failed" {
			continue
		}
		if step.Kind == "cluster" {
			return Decision{Kind: "failed", Index: i, Detail: step.Detail}
		}
		if failed < 0 {
			failed = i
		}
	}
	for i, step := range run.Steps {
		if step.Kind == "cluster" && step.Status != "done" {
			return Decision{Kind: "cluster", Index: i}
		}
	}
	pending := false
	serverFailed := false
	for i, step := range run.Steps {
		if step.Kind != "server" || step.Status == "done" {
			continue
		}
		if step.Status == "failed" || (step.ResourceID != "" && status[step.ResourceID] == "failed") {
			serverFailed = true
			if failed < 0 {
				failed = i
			}
			continue
		}
		if step.ResourceID != "" {
			pending = true
			continue
		}
		return Decision{Kind: "server", Index: i}
	}
	if pending {
		return Decision{Kind: "wait"}
	}
	if serverFailed && !leader {
		// No control plane answers: the applications would have nowhere to run.
		return Decision{Kind: "failed", Index: failed, Detail: run.Steps[failed].Detail}
	}
	starting := false
	for i, step := range run.Steps {
		if step.Kind != "application" || step.Status == "done" || step.Status == "failed" {
			continue
		}
		if step.ResourceID == "" {
			return Decision{Kind: "application", Index: i}
		}
		starting = true
	}
	if starting {
		return Decision{Kind: "wait"}
	}
	if failed >= 0 {
		return Decision{Kind: "failed", Index: failed, Detail: run.Steps[failed].Detail}
	}
	return Decision{Kind: "done"}
}

type Account struct {
	ID       string
	Name     string
	Provider string
}

type Document struct {
	Type         string         `json:"type"`
	Version      int            `json:"version"`
	Name         string         `json:"name"`
	ClusterName  string         `json:"clusterName,omitempty"`
	Servers      []ExportServer `json:"servers,omitempty"`
	Applications []App          `json:"applications,omitempty"`
}

type ExportServer struct {
	Name            string   `json:"name"`
	Provider        string   `json:"provider,omitempty"`
	ProviderAccount string   `json:"providerAccount,omitempty"`
	Location        string   `json:"location,omitempty"`
	OfferID         string   `json:"offerId,omitempty"`
	Period          string   `json:"period,omitempty"`
	StorageID       string   `json:"storageId,omitempty"`
	StorageGB       int      `json:"storageGb,omitempty"`
	Keys            []string `json:"keys,omitempty"`
	ControlPlane    *bool    `json:"controlPlane,omitempty"`
}

func Export(name string, body Body) Document {
	doc := Document{Type: "myinfra-template", Version: 1, Name: name, ClusterName: body.ClusterName, Applications: body.Applications}
	for _, server := range body.Servers {
		keys := server.KeyNames
		if keys == nil {
			keys = []string{}
		}
		controlPlane := server.ControlPlane
		doc.Servers = append(doc.Servers, ExportServer{
			Name: server.Name, Provider: server.Provider, ProviderAccount: server.ProviderName,
			Location: server.Location, OfferID: server.OfferID, Period: server.Period,
			StorageID: server.StorageID, StorageGB: server.StorageGB, Keys: keys,
			ControlPlane: &controlPlane,
		})
	}
	if doc.Servers == nil {
		doc.Servers = []ExportServer{}
	}
	if doc.Applications == nil {
		doc.Applications = []App{}
	}
	return doc
}

func Bind(doc Document, providers, keys []Account, zones []string) (Body, string) {
	if doc.Type != "myinfra-template" || doc.Version != 1 {
		return Body{}, "Ce fichier n'est pas un template MyInfra."
	}
	known := map[string]bool{}
	for _, zone := range zones {
		known[strings.ToLower(zone)] = true
	}
	body := Body{ClusterName: strings.TrimSpace(doc.ClusterName)}
	explicitRole := false
	for _, server := range doc.Servers {
		if server.ControlPlane != nil {
			explicitRole = true
			break
		}
	}
	for i, server := range doc.Servers {
		controlPlane := false
		if server.ControlPlane != nil {
			controlPlane = *server.ControlPlane
		} else if !explicitRole && i == 0 && body.ClusterName != "" {
			controlPlane = true
		}
		item := Server{
			Name: server.Name, Provider: server.Provider, ProviderName: server.ProviderAccount,
			Location: server.Location, OfferID: server.OfferID, Period: server.Period,
			StorageID: server.StorageID, StorageGB: server.StorageGB, KeyNames: server.Keys,
			ControlPlane: controlPlane,
		}
		for _, account := range providers {
			if account.Provider == server.Provider && account.Name == server.ProviderAccount {
				item.ProviderID = account.ID
				break
			}
		}
		for _, keyName := range server.Keys {
			for _, key := range keys {
				if key.Name == keyName {
					item.KeyIDs = append(item.KeyIDs, key.ID)
					break
				}
			}
		}
		body.Servers = append(body.Servers, item)
	}
	for _, app := range doc.Applications {
		next := app
		for i, name := range next.Names {
			if !known[strings.ToLower(strings.TrimSpace(name.Zone))] {
				next.Names[i].Zone = ""
			}
		}
		body.Applications = append(body.Applications, next)
	}
	return body, ""
}
