package provision

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"myinfra/internal/catalog"
)

// errMachineGone is the answer of a provider for a machine it no longer has.
var errMachineGone = errors.New("machine absente")

// ErrNoCheck says the provider offers no way, here, to look a machine up.
var ErrNoCheck = errors.New("vérification indisponible pour ce provider")

// Exists asks the provider whether a machine is still there. It answers
// false only when the provider says it no longer knows it; any other
// trouble — a refused key, a provider down — is an error, never a removal.
func (c Client) Exists(ctx context.Context, provider string, creds catalog.Creds, id string) (bool, error) {
	if strings.TrimSpace(id) == "" {
		return false, ErrNoCheck
	}
	var (
		url    string
		header map[string]string
	)
	switch provider {
	case "hetzner":
		url, header = c.base(c.HetznerBase, "https://api.hetzner.cloud")+"/v1/servers/"+id, bearer(creds.Token)
	case "digitalocean":
		url, header = c.base(c.DigitalOceanBase, "https://api.digitalocean.com")+"/v2/droplets/"+id, bearer(creds.Token)
	case "scaleway":
		zone, serverID := splitScaleway(id)
		url, header = c.base(c.ScalewayBase, "https://api.scaleway.com")+"/instance/v1/zones/"+zone+"/servers/"+serverID, scalewayAuth(creds.Token)
	case "vultr":
		url, header = c.base(c.VultrBase, "https://api.vultr.com")+"/v2/instances/"+id, bearer(creds.Token)
	case "akamai":
		url, header = c.base(c.AkamaiBase, "https://api.linode.com")+"/v4/linode/instances/"+id, bearer(creds.Token)
	case "upcloud":
		url, header = c.base(c.UpCloudBase, "https://api.upcloud.com")+"/1.3/server/"+id, bearer(creds.Token)
	case "ionos":
		url, header = c.base(c.IonosBase, "https://api.ionos.com")+"/cloudapi/v6/datacenters/"+id, bearer(creds.Token)
	case "gcore":
		parts := strings.Split(id, ":")
		if len(parts) != 3 {
			return false, ErrNoCheck
		}
		url, header = c.base(c.GcoreBase, "https://api.gcore.com")+"/cloud/v1/instances/"+parts[1]+"/"+parts[0]+"/"+parts[2], gcoreHeader(creds.Token)
	case "exoscale":
		zone, instance, ok := strings.Cut(id, ":")
		if !ok || instance == "" {
			return false, ErrNoCheck
		}
		status, body, err := c.exoscaleCall(ctx, creds, zone, http.MethodGet, "/v2/instance/"+instance, nil)
		return presence(status, body, err)
	case "ovh":
		project, instance, ok := strings.Cut(id, "/")
		if !ok {
			return false, ErrNoCheck
		}
		base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
		_, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/instance/"+instance, nil)
		if errors.Is(err, errMachineGone) {
			return false, nil
		}
		return err == nil, err
	case "infomaniak":
		region, serverID, ok := strings.Cut(id, "/")
		if !ok {
			return false, ErrNoCheck
		}
		base := c.base(c.InfomaniakBase, "https://api.pub1.infomaniak.cloud")
		token, endpoints, err := infomaniakToken(ctx, c, base, creds.ApplicationKey, creds.ApplicationSecret)
		if err != nil {
			return false, err
		}
		compute := endpoint(endpoints, "compute", region)
		if compute == "" {
			return false, fmt.Errorf("Le provider ne répond pas.")
		}
		url, header = strings.TrimRight(compute, "/")+"/servers/"+serverID, map[string]string{"X-Auth-Token": token}
	default:
		return false, ErrNoCheck
	}
	status, body, err := c.call(ctx, http.MethodGet, url, header, nil)
	return presence(status, body, err)
}

func presence(status int, body []byte, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	switch {
	case status == http.StatusNotFound:
		return false, nil
	case status < 300:
		return true, nil
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return false, fmt.Errorf("Le provider refuse la clé API (%d).", status)
	default:
		_ = body
		return false, fmt.Errorf("Le provider répond %d.", status)
	}
}
