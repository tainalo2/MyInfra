package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"myinfra/internal/firewall"
	"myinfra/internal/spec"
	"myinfra/internal/store"
)

// checkExposed refuses the ports the services of an application would open
// on the machines of its destination when another application holds them.
// On a machine alone, a port has one container behind it: one copy.
func (a *API) checkExposed(ctx context.Context, kind, serverID, clusterID, appID string, services []store.Service) error {
	mine := map[string]string{}
	for _, svc := range services {
		exposed := spec.Exposed(svc.Spec)
		if len(exposed) == 0 {
			continue
		}
		if kind == "server" && svc.Replicas > 1 {
			return fmt.Errorf("Le service %s ouvre un port sur Internet : sur une machine seule, il tourne en une copie.", svc.Name)
		}
		for _, port := range exposed {
			key := publicKey(port)
			if other, taken := mine[key]; taken {
				return fmt.Errorf("Les services %s et %s ouvrent tous deux le port %s.", other, svc.Name, key)
			}
			mine[key] = svc.Name
		}
	}
	if len(mine) == 0 {
		return nil
	}
	held, err := a.Store.ExposedPorts(ctx, kind, serverID, clusterID)
	if err != nil {
		return err
	}
	for _, row := range held {
		if row.ApplicationID == appID {
			continue
		}
		if _, taken := mine[publicKey(row.Port)]; taken {
			return fmt.Errorf("Le port %s est déjà ouvert par l'application %s.", publicKey(row.Port), row.ApplicationName)
		}
	}
	return nil
}

// exposes tells whether services open a port to the Internet.
func exposes(services []store.Service) bool {
	for _, svc := range services {
		if len(spec.Exposed(svc.Spec)) > 0 {
			return true
		}
	}
	return false
}

// openFirewall lets the nodes of a cluster take the ports its applications
// publish, or close those they no longer do.
func (a *API) openFirewall(owner, clusterID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := a.reconcileFirewall(ctx, owner, clusterID); err != nil {
		slog.Info("firewall", "cluster", clusterID, "err", err)
	}
}

func publicKey(port spec.Port) string {
	return fmt.Sprintf("%d/%s", port.Expose, port.Proto())
}

func openPorts(rows []store.ExposedPort) []firewall.Port {
	out := make([]firewall.Port, 0, len(rows))
	for _, row := range rows {
		out = append(out, firewall.Port{Port: row.Port.Expose, Protocol: strings.ToLower(row.Port.Proto())})
	}
	return out
}
