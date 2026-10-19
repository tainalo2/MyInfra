package api

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"myinfra/internal/agent"
	"myinfra/internal/deploy"
	"myinfra/internal/store"
)

// deliverAlone runs the agent script on each machine alone over SSH. The
// machine does not need to reach MyInfra: it only has to accept SSH.
func (a *API) deliverAlone(ctx context.Context) {
	servers, err := a.Store.AloneServers(ctx)
	if err != nil {
		return
	}
	for _, server := range servers {
		a.deliverMachine(ctx, server)
	}
}

func (a *API) deliverMachine(ctx context.Context, server store.AloneServer) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if deliverPaused(server.Address) {
		return
	}
	work, err := a.workloads(ctx, server.JoinToken)
	if err != nil {
		return
	}
	statuses, err := a.Store.ServerAppStatuses(ctx, server.ID)
	if err != nil {
		return
	}
	script, err := agent.Sync(a.PublicURL, server.JoinToken, work)
	if err != nil {
		return
	}
	key := "machine:" + server.ID
	settled := true
	for _, item := range work {
		if !item.Paused && statuses[item.ID] != "running" {
			settled = false
		}
	}
	if settled && a.appliedRecently(key, digest(script)) {
		return
	}
	keyIDs, err := a.Store.ServerKeyIDs(ctx, server.ID)
	if err != nil {
		return
	}
	privateKey := a.privateKey(ctx, server.OwnerID, keyIDs)
	if privateKey == "" {
		return
	}
	out, err := deploy.SSH(ctx, deploy.Host{Address: server.Address, PrivateKey: privateKey}, deploy.RunScript(script))
	if err != nil {
		slog.Info("machine", "server", server.ID, "err", err)
		if !appRejected(err) {
			pauseDeliver(server.Address)
		}
		return
	}
	if fresh, first := a.rememberApplied(key, digest(script)); fresh && !first {
		for _, item := range work {
			if !item.Paused {
				a.markStarting(item.ID)
			}
		}
	}
	reported := reportedStatuses(out)
	for _, item := range work {
		if item.Service != "" && !item.Published {
			continue
		}
		status, ok := reported[item.ID]
		if !ok {
			continue
		}
		if status == "failed" && !a.failedAgain(item.ID) {
			// A container caught while it is replaced is not a failure yet.
			status = "preparing"
		} else if status != "failed" {
			a.clearFailures(item.ID)
		}
		if status == "running" && !a.namesAnswer(ctx, item.ID) {
			status = "preparing"
		}
		if status == "preparing" {
			since := a.startedAt(item.ID)
			if since.IsZero() {
				a.markStarting(item.ID)
			} else if time.Since(since) > startupLimit {
				status = "failed"
			}
		}
		if status != statuses[item.ID] {
			_ = a.Store.SetRunStatus(ctx, item.ID, status)
			statuses[item.ID] = status
		}
	}
}

// reportedStatuses reads the "myinfra-status <id> <status>" lines of the agent.
func reportedStatuses(out string) map[string]string {
	found := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "myinfra-status" {
			continue
		}
		switch fields[2] {
		case "running", "failed", "paused", "preparing":
			if found[fields[1]] != "failed" {
				found[fields[1]] = fields[2]
			}
		}
	}
	return found
}

// failuresBeforeShown is how many deliveries in a row must see a container
// stopped before the application is shown as failed.
const failuresBeforeShown = 3

func (a *API) failedAgain(appID string) bool {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	if a.failures == nil {
		a.failures = map[string]int{}
	}
	a.failures[appID]++
	return a.failures[appID] >= failuresBeforeShown
}

func (a *API) clearFailures(appID string) {
	a.appliedMu.Lock()
	defer a.appliedMu.Unlock()
	delete(a.failures, appID)
}
