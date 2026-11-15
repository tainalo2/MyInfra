package assistant

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// modelTTL is how long a provider's list of models is kept: the status is
// read by every page that shows the pane, and the list rarely changes.
const modelTTL = 10 * time.Minute

type modelCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	list map[string][]ModelCapability
}

// modelsFor lists the models the provider's credential may use, from its
// API when it can say so, else the adapter's own list.
func (s *Service) modelsFor(ctx context.Context, name string) []ModelCapability {
	prov, ok := s.Providers[name]
	if !ok {
		return nil
	}
	lister, ok := prov.(ModelLister)
	if !ok {
		return prov.ListModels()
	}
	s.models.mu.Lock()
	cached, hit := s.models.list[name]
	fresh := hit && time.Since(s.models.at[name]) < modelTTL
	s.models.mu.Unlock()
	if fresh {
		return cached
	}
	key, err := s.credentialFor(ctx, name)
	if err != nil || key == "" {
		return prov.ListModels()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	list, err := lister.FetchModels(ctx, key)
	if err != nil {
		slog.Warn("liste des modèles indisponible, liste par défaut", "provider", name, "err", err)
		return prov.ListModels()
	}
	s.models.mu.Lock()
	if s.models.list == nil {
		s.models.list, s.models.at = map[string][]ModelCapability{}, map[string]time.Time{}
	}
	s.models.list[name], s.models.at[name] = list, time.Now()
	s.models.mu.Unlock()
	return list
}

// forgetModels drops the lists: a new key or sign-in may open other models.
func (s *Service) forgetModels() {
	s.models.mu.Lock()
	s.models.list, s.models.at = nil, nil
	s.models.mu.Unlock()
}
