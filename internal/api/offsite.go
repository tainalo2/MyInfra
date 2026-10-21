package api

import (
	"os"
	"strings"

	"myinfra/internal/manifest"
)

// offsiteS3 reads an external object store from the environment. When set,
// Litestream keeps a second replica outside the cluster's Garage.
func offsiteS3(clusterID, appID string) *manifest.S3Replica {
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
	prefix := "clusters/" + clusterID + "/apps/" + appID
	return &manifest.S3Replica{
		Endpoint: endpoint,
		Bucket:   bucket,
		Region:   region,
		Path:     prefix,
		KeyID:    keyID,
		Secret:   secret,
	}
}
