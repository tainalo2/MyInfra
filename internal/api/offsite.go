package api

import (
	"os"
	"strings"

	"myinfra/internal/manifest"
)

type offsiteStore struct {
	Endpoint, Bucket, Region, KeyID, Secret string
}

func offsiteStoreFromEnv() *offsiteStore {
	endpoint := strings.TrimSpace(os.Getenv("MYINFRA_OFFSITE_S3_ENDPOINT"))
	bucket := strings.TrimSpace(os.Getenv("MYINFRA_OFFSITE_S3_BUCKET"))
	keyID := strings.TrimSpace(os.Getenv("MYINFRA_OFFSITE_S3_ACCESS_KEY"))
	secret := strings.TrimSpace(os.Getenv("MYINFRA_OFFSITE_S3_SECRET_KEY"))
	if endpoint == "" || bucket == "" || keyID == "" || secret == "" {
		return nil
	}
	region := strings.TrimSpace(os.Getenv("MYINFRA_OFFSITE_S3_REGION"))
	if region == "" {
		region = "auto"
	}
	return &offsiteStore{Endpoint: endpoint, Bucket: bucket, Region: region, KeyID: keyID, Secret: secret}
}

func (s *offsiteStore) replica(path string) *manifest.S3Replica {
	return &manifest.S3Replica{
		Endpoint: s.Endpoint,
		Bucket:   s.Bucket,
		Region:   s.Region,
		Path:     path,
		KeyID:    s.KeyID,
		Secret:   s.Secret,
	}
}

// offsiteS3 reads an external object store from the environment. When set,
// Litestream keeps a second replica outside the cluster's Garage.
func offsiteS3(clusterID, appID string) *manifest.S3Replica {
	store := offsiteStoreFromEnv()
	if store == nil {
		return nil
	}
	return store.replica("clusters/" + clusterID + "/apps/" + appID)
}

// offsiteCNPG configures CloudNativePG backups for a database cluster.
func offsiteCNPG(clusterID, dbName string) *manifest.S3Replica {
	store := offsiteStoreFromEnv()
	if store == nil {
		return nil
	}
	return store.replica("clusters/" + clusterID + "/cnpg/" + dbName)
}

// cnpgBackupCron is the ScheduledBackup cron when offsite CNPG is enabled.
func cnpgBackupCron() string {
	s := strings.TrimSpace(os.Getenv("MYINFRA_CNPG_BACKUP_CRON"))
	if s == "" {
		return "0 4 * * *"
	}
	return s
}
