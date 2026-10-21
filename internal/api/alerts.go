package api

import (
	"context"
	"fmt"
	"sync"

	"myinfra/internal/notify"
	"myinfra/internal/store"
)

// alertMu guards alertLast: last state we already notified for a key.
type alertState struct {
	mu   sync.Mutex
	last map[string]string
}

func (a *API) alertCache() *alertState {
	if a.alerts == nil {
		a.alerts = &alertState{last: map[string]string{}}
	}
	return a.alerts
}

// emitReachAlerts notifies optional channels when reachability worsens.
func (a *API) emitReachAlerts(ctx context.Context, servers []store.WatchedServer) {
	if !notify.Enabled() || a.Replica {
		return
	}
	cache := a.alertCache()
	for _, srv := range servers {
		if !watched(srv) {
			continue
		}
		state, _ := a.reachOf(srv.ID)
		switch state {
		case "lost":
			cache.transition(ctx, "server:"+srv.ID, state,
				"Serveur injoignable",
				fmt.Sprintf("%s ne répond plus (SSH).", srv.Name))
		case "gone":
			cache.transition(ctx, "server:"+srv.ID+":gone", state,
				"Machine absente chez le provider",
				fmt.Sprintf("%s n'existe plus chez le provider : retirez-le de MyInfra.", srv.Name))
		default:
			cache.clear("server:" + srv.ID)
			cache.clear("server:" + srv.ID + ":gone")
		}
	}
	clusters := map[string]bool{}
	for _, srv := range servers {
		if srv.ClusterID != "" && watched(srv) {
			clusters[srv.ClusterID] = true
		}
	}
	for cid := range clusters {
		if a.clusterLost(cid) {
			cache.transition(ctx, "cluster:"+cid, "lost",
				"Cluster sans serveur",
				fmt.Sprintf("Aucun serveur du cluster %s ne répond.", cid))
		} else {
			cache.clear("cluster:" + cid)
		}
	}
	a.reach.mu.Lock()
	for id, fails := range a.reach.apps {
		if fails >= lostAfter {
			host := a.reach.hosts[id]
			cache.transition(ctx, "app:"+id, "silent",
				"Application injoignable",
				fmt.Sprintf("L'adresse %s ne répond plus.", host))
		} else {
			cache.clear("app:" + id)
		}
	}
	a.reach.mu.Unlock()
}

func (s *alertState) transition(ctx context.Context, key, state, title, body string) {
	s.mu.Lock()
	if s.last[key] == state {
		s.mu.Unlock()
		return
	}
	s.last[key] = state
	s.mu.Unlock()
	notify.Send(ctx, title, body)
}

func (s *alertState) clear(key string) {
	s.mu.Lock()
	delete(s.last, key)
	s.mu.Unlock()
}
