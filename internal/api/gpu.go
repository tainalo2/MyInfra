package api

import (
	"context"
	"errors"
	"sort"
	"strings"

	"myinfra/internal/store"
)

// gpuAsk is how many cards the wanted services take. A stopped service,
// with no copy, holds none.
func gpuAsk(services []store.Service) int {
	total := 0
	for _, svc := range services {
		if svc.Replicas <= 0 || svc.Spec.Resources == nil || svc.Spec.Resources.GPU <= 0 {
			continue
		}
		total += svc.Replicas * svc.Spec.Resources.GPU
	}
	return total
}

// gpuConflict refuses a demand the free cards cannot cover. held maps an
// application name to the cards it already keeps.
func gpuConflict(supply int, held map[string]int, ask int) string {
	if ask <= 0 {
		return ""
	}
	used := 0
	names := make([]string, 0)
	for name, count := range held {
		if count <= 0 {
			continue
		}
		used += count
		names = append(names, name)
	}
	sort.Strings(names)
	free := supply - used
	if free < 0 {
		free = 0
	}
	if free >= ask {
		return ""
	}
	if supply <= 0 {
		return "Aucune machine ici n'a de GPU."
	}
	if len(names) == 0 {
		return "Il n'y a pas assez de GPU pour cette application."
	}
	return "Le GPU est déjà utilisé par " + frenchAnd(names) + "."
}

func frenchAnd(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " et " + names[len(names)-1]
}

// reserveGPU checks the cards of one machine or one cluster against the
// services about to run there. except is an application already counted in
// services, so its current hold is not counted twice.
func (a *API) reserveGPU(ctx context.Context, owner, kind, serverID, clusterID, except string, services []store.Service) error {
	ask := gpuAsk(services)
	if ask == 0 {
		return nil
	}
	supply, err := a.gpuSupply(ctx, owner, kind, serverID, clusterID)
	if err != nil {
		return err
	}
	held, err := a.gpuHeld(ctx, owner, kind, serverID, clusterID, except)
	if err != nil {
		return err
	}
	if msg := gpuConflict(supply, held, ask); msg != "" {
		return errors.New(msg)
	}
	return nil
}

func (a *API) gpuSupply(ctx context.Context, owner, kind, serverID, clusterID string) (int, error) {
	servers, err := a.Store.ListServers(ctx, owner)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, server := range servers {
		if server.Status != "linked" {
			continue
		}
		if kind == "server" && server.ID != serverID {
			continue
		}
		if kind == "cluster" && server.ClusterID != clusterID {
			continue
		}
		plan := planOf(server.Plan)
		if plan == nil {
			continue
		}
		total += plan.GPUs
	}
	return total, nil
}

func (a *API) gpuHeld(ctx context.Context, owner, kind, serverID, clusterID, except string) (map[string]int, error) {
	apps, err := a.Store.ListApplications(ctx, owner)
	if err != nil {
		return nil, err
	}
	held := map[string]int{}
	for _, item := range apps {
		if item.ID == except {
			continue
		}
		if kind == "server" && (item.DestinationKind != "server" || item.ServerID != serverID) {
			continue
		}
		if kind == "cluster" && item.ClusterID != clusterID {
			continue
		}
		full, err := a.Store.Application(ctx, owner, item.ID)
		if err != nil {
			return nil, err
		}
		if ask := gpuAsk(full.Services); ask > 0 {
			held[full.Name] += ask
		}
	}
	return held, nil
}
