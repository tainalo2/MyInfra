package api

import (
	"context"
	"fmt"
	"strings"
)

// snapshotMax bounds each list the assistant is given: enough to answer,
// not a whole inventory in every turn.
const snapshotMax = 40

// assistantSnapshot tells the assistant what the account runs now: its
// servers, clusters and applications, with their state. Only what the
// account owns, read the same way as the overview.
func (a *API) assistantSnapshot(ctx context.Context) string {
	owner := ownerID(ctx)
	var b strings.Builder
	if servers, err := a.Store.ListServers(ctx, owner); err == nil {
		fmt.Fprintf(&b, "Servers (%d):\n", len(servers))
		for i, s := range servers {
			if i == snapshotMax {
				fmt.Fprintf(&b, "- and %d more\n", len(servers)-i)
				break
			}
			parts := []string{s.Status}
			if s.Provider != "" {
				parts = append(parts, "provider "+s.Provider)
			}
			if s.ClusterName != "" {
				parts = append(parts, "cluster "+s.ClusterName)
			}
			if s.Address != "" {
				parts = append(parts, "address "+s.Address)
			}
			fmt.Fprintf(&b, "- %s: %s\n", s.Name, strings.Join(parts, ", "))
		}
	}
	if clusters, err := a.Store.ListClusters(ctx, owner); err == nil {
		names := make([]string, 0, len(clusters))
		for _, c := range clusters {
			names = append(names, c.Name)
		}
		fmt.Fprintf(&b, "Clusters (%d): %s\n", len(clusters), strings.Join(names, ", "))
	}
	if apps, err := a.Store.ListApplications(ctx, owner); err == nil {
		fmt.Fprintf(&b, "Applications (%d):\n", len(apps))
		for i, app := range apps {
			if i == snapshotMax {
				fmt.Fprintf(&b, "- and %d more\n", len(apps)-i)
				break
			}
			state := app.RunStatus
			if app.Paused {
				state = "paused"
			}
			if state == "" {
				state = "unknown"
			}
			parts := []string{state}
			if app.ClusterName != "" {
				parts = append(parts, "on cluster "+app.ClusterName)
			} else if app.ServerName != "" {
				parts = append(parts, "on server "+app.ServerName)
			}
			hosts := []string{}
			for _, d := range app.Domains {
				hosts = append(hosts, d.Hostname)
			}
			if len(hosts) > 0 {
				parts = append(parts, "addresses "+strings.Join(hosts, " "))
			}
			fmt.Fprintf(&b, "- %s: %s\n", app.Name, strings.Join(parts, ", "))
		}
	}
	return b.String()
}
