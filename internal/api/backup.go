package api

import (
	"context"
	"errors"
	"log/slog"

	"myinfra/internal/deploy"
	"myinfra/internal/manifest"
	"myinfra/internal/store"
)

var errNoBackup = errors.New("MyInfra ne connaît pas les fichiers de données de cette image.")

// appParts is what an application adds to its manifest besides itself.
type appParts struct {
	Services  []manifest.Service
	Databases []manifest.Database
	Secrets   []manifest.SharedSecret
}

// clusterServices turns an application's services into what the manifest
// renders. A service backed up gets its bucket in the cluster's store; one in
// PostgreSQL gets its database. When the store is not there yet, a service
// still copying keeps its node disk; one already running from the store is
// left as it runs: ok is false.
func (a *API) clusterServices(ctx context.Context, host deploy.Host, owner, clusterID string, app store.ClusterApp) (appParts, bool) {
	parts := appParts{Services: make([]manifest.Service, 0, len(app.Services))}
	for _, svc := range app.Services {
		out := manifest.Service{
			Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk, Replicas: svc.Replicas,
			DiskPath: svc.DiskPath, DiskGi: svc.DiskGi, Listen: svc.Listen, Args: svc.Args, Spec: svc.Spec,
		}
		for _, volume := range svc.Spec.Volumes {
			// A shared disk chosen on a server keeps its services there.
			if volume.Disk != "" && volume.Node != "" {
				if server, err := a.Store.Server(ctx, owner, volume.Node); err == nil {
					out.Node = deploy.NodeName(server.Name)
				}
				break
			}
		}
		if svc.Published && app.ServiceHosts != nil {
			// Each public service gets its own names, none when it has none.
			out.Hosts = append([]string{}, app.ServiceHosts[svc.Name]...)
		}
		switch {
		case svc.Database != "":
			if err := a.serviceDatabase(ctx, host, owner, clusterID, app.ID, svc, &out, &parts); err != nil {
				slog.Info("base", "app", app.ID, "service", svc.Name, "err", err)
				return appParts{}, false
			}
		case svc.Backup != "" && svc.Disk:
			backup, err := a.serviceBackup(ctx, host, owner, clusterID, app.ID, svc)
			if err != nil {
				slog.Info("sauvegarde", "app", app.ID, "service", svc.Name, "err", err)
				if svc.Backup == "ready" {
					return appParts{}, false
				}
			} else {
				out.Backup = backup
			}
		}
		parts.Services = append(parts.Services, out)
	}
	return parts, true
}

func (a *API) serviceBackup(ctx context.Context, host deploy.Host, owner, clusterID, appID string, svc store.Service) (*manifest.Backup, error) {
	profile := profileOf(svc)
	if len(profile.SQLite) == 0 {
		return nil, errNoBackup
	}
	if err := a.ensureStorage(ctx, host, owner, clusterID); err != nil {
		return nil, err
	}
	bucket, keyID, secret, err := a.ensureBucket(ctx, host, clusterID, appID)
	if err != nil {
		return nil, err
	}
	b := &manifest.Backup{
		Databases: profile.SQLite, Env: profile.Env, Bucket: bucket, KeyID: keyID, Secret: secret,
		Copying: svc.Backup == "migrating",
	}
	if off := offsiteS3(clusterID, appID); off != nil {
		b.Offsite = off
	}
	return b, nil
}

// advanceBackups moves the services still copying to the store once their
// copy is there. The node disk stays, unused: it is the way back if the copy
// in the store were ever wrong.
func (a *API) advanceBackups(ctx context.Context, host deploy.Host, app store.ClusterApp) {
	for _, svc := range app.Services {
		switch svc.Backup {
		case "migrating":
			count, err := a.bucketObjects(ctx, host, storageBucket(app.ID))
			if err != nil || count == 0 {
				continue
			}
			// The copy exists: from now on the service starts from it, on any node.
			_ = a.Store.SetServiceBackup(ctx, app.ID, svc.Name, "ready")
		}
	}
}
