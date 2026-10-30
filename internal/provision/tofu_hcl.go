package provision

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"myinfra/internal/catalog"
)

func literal(value string) string {
	return strings.ReplaceAll(value, "${", "$${")
}

// withTags marks a machine with the server it belongs to, so that a machine
// left behind can be recognised in the provider's console.
func withTags(resource map[string]any, field, key string) map[string]any {
	if key == "" || !purchaseKey.MatchString(key) {
		return resource
	}
	if field == "labels" {
		resource["labels"] = map[string]any{"myinfra-server": key}
	} else {
		resource["tags"] = []string{"myinfra", "myinfra-server-" + key}
	}
	return resource
}

func renderStack(provider string, creds catalog.Creds, spec Spec, c Client) (string, []string, error) {
	body, env, _, err := hcl(provider, creds, spec, "", c)
	return body, env, err
}

func renderImport(provider string, creds catalog.Creds, id string, c Client) (string, []string, string, error) {
	return hcl(provider, creds, Spec{Name: "machine", OfferID: "imported", Location: "imported"}, id, c)
}

func hcl(provider string, creds catalog.Creds, spec Spec, importID string, c Client) (string, []string, string, error) {
	spec.Name = literal(spec.Name)
	spec.OfferID = literal(spec.OfferID)
	spec.Location = literal(spec.Location)
	spec.UserData = literal(spec.UserData)
	spec.ProjectID = literal(spec.ProjectID)
	switch provider {
	case "hetzner":
		endpoint := ""
		if c.HetznerBase != "" {
			endpoint = c.HetznerBase + "/v1"
		}
		body, err := stackJSON("hcloud", "hetznercloud/hcloud", "~> 1.52", []string{"token", "endpoint"}, map[string]any{
			"token": "${var.token}", "endpoint": `${var.endpoint != "" ? var.endpoint : null}`,
		}, map[string]any{
			"hcloud_server": map[string]any{"machine": withTags(map[string]any{
				"name": spec.Name, "server_type": spec.OfferID, "image": "debian-13", "location": spec.Location, "user_data": spec.UserData,
				"public_net": map[string]any{"ipv4_enabled": true, "ipv6_enabled": false},
			}, "labels", spec.Key)},
		}, map[string]string{"id": "${hcloud_server.machine.id}", "ip": "${hcloud_server.machine.ipv4_address}"})
		return body, []string{"TF_VAR_token=" + creds.Token, "TF_VAR_endpoint=" + endpoint}, "hcloud_server.machine", err
	case "scaleway":
		body, err := stackJSON("scaleway", "scaleway/scaleway", "~> 2.50", []string{"token", "endpoint", "project_id"}, map[string]any{
			"access_key": "SCW00000000000000000", "secret_key": "${var.token}", "project_id": "${var.project_id}",
			"zone": spec.Location, "api_url": `${var.endpoint != "" ? var.endpoint : null}`,
		}, map[string]any{
			"scaleway_instance_ip": map[string]any{"machine": map[string]any{}},
			"scaleway_instance_server": map[string]any{"machine": withTags(map[string]any{
				"name": spec.Name, "type": spec.OfferID, "image": "debian_trixie",
				"ip_id":     "${scaleway_instance_ip.machine.id}",
				"user_data": map[string]any{"cloud-init": spec.UserData},
			}, "tags", spec.Key)},
		}, map[string]string{"id": "${scaleway_instance_server.machine.id}", "ip": "${scaleway_instance_server.machine.public_ips[0].address}"})
		return body, []string{"TF_VAR_token=" + creds.Token, "TF_VAR_endpoint=" + c.ScalewayBase, "TF_VAR_project_id=" + creds.ProjectID}, "scaleway_instance_server.machine", err
	case "digitalocean":
		body, err := stackJSON("digitalocean", "digitalocean/digitalocean", "~> 2.50", []string{"token"}, map[string]any{"token": "${var.token}"}, map[string]any{
			"digitalocean_droplet": map[string]any{"machine": map[string]any{
				"name": spec.Name, "size": spec.OfferID, "image": digitalOceanImage(spec.GPUs), "region": spec.Location, "user_data": spec.UserData,
			}},
		}, map[string]string{"id": "${digitalocean_droplet.machine.id}", "ip": "${digitalocean_droplet.machine.ipv4_address}"})
		return body, []string{"TF_VAR_token=" + creds.Token}, "digitalocean_droplet.machine", err
	case "ovh":
		project := spec.ProjectID
		if project == "" {
			project = creds.ProjectID
		}
		machine := map[string]any{
			"service_name": "${var.project_id}", "region": spec.Location, "name": spec.Name, "billing_period": "hourly",
			"flavor":    map[string]any{"flavor_id": spec.OfferID},
			"boot_from": map[string]any{"image_id": spec.ImageID},
			"network":   map[string]any{"public": true},
			"user_data": spec.UserData,
		}
		// OVH wants one key at creation; the others come with user_data.
		keyName := spec.KeyName
		if keyName == "" {
			keyName = "myinfra"
		}
		machine["ssh_key"] = map[string]any{"name": literal(keyName)}
		body, err := stackJSON("ovh", "ovh/ovh", "~> 2.0", []string{"application_key", "application_secret", "consumer_key", "project_id"}, map[string]any{
			"endpoint": "ovh-eu", "application_key": "${var.application_key}", "application_secret": "${var.application_secret}", "consumer_key": "${var.consumer_key}",
		}, map[string]any{
			"ovh_cloud_project_instance": map[string]any{"machine": machine},
		}, map[string]string{
			// project/instance, the form destroyOVH reads.
			"id": project + "/${ovh_cloud_project_instance.machine.id}",
			"ip": "${[for a in ovh_cloud_project_instance.machine.addresses : a.ip if a.version == 4][0]}",
		})
		env := []string{
			"TF_VAR_application_key=" + creds.ApplicationKey, "TF_VAR_application_secret=" + creds.ApplicationSecret,
			"TF_VAR_consumer_key=" + creds.ConsumerKey, "TF_VAR_project_id=" + project,
		}
		return body, env, "ovh_cloud_project_instance.machine", err
	case "upcloud":
		template := map[string]any{"storage": "Debian GNU/Linux 13 (Trixie)", "size": 25}
		if spec.ImageID != "" {
			template["storage"] = spec.ImageID
		}
		if spec.RootGB > 0 {
			template["size"] = spec.RootGB
		}
		if spec.DiskTier != "" {
			template["tier"] = spec.DiskTier
		}
		// The API token goes in token: username and password are for an account login.
		body, err := stackJSON("upcloud", "UpCloudLtd/upcloud", "~> 5.0", []string{"token"}, map[string]any{"token": "${var.token}"}, map[string]any{
			"upcloud_server": map[string]any{"machine": map[string]any{
				"hostname": machineLabel(spec.Name), "title": spec.Name, "zone": spec.Location, "plan": spec.OfferID,
				"template":          template,
				"network_interface": map[string]any{"type": "public"},
				// cloud-init reads user_data from the metadata service.
				"metadata":  true,
				"user_data": spec.UserData,
			}},
		}, map[string]string{"id": "${upcloud_server.machine.id}", "ip": "${upcloud_server.machine.network_interface[0].ip_address}"})
		return body, []string{"TF_VAR_token=" + creds.Token}, "upcloud_server.machine", err
	case "vultr":
		body, err := stackJSON("vultr", "vultr/vultr", "~> 2.20", []string{"token"}, map[string]any{"api_key": "${var.token}"}, map[string]any{
			"vultr_instance": map[string]any{"machine": map[string]any{
				"label": spec.Name, "plan": spec.OfferID, "region": spec.Location, "os_id": 2136, "user_data": spec.UserData,
			}},
		}, map[string]string{"id": "${vultr_instance.machine.id}", "ip": "${vultr_instance.machine.main_ip}"})
		return body, []string{"TF_VAR_token=" + creds.Token}, "vultr_instance.machine", err
	case "akamai":
		body, err := stackJSON("linode", "linode/linode", "~> 2.30", []string{"token"}, map[string]any{"token": "${var.token}"}, map[string]any{
			"linode_instance": map[string]any{"machine": map[string]any{
				"label": spec.Name, "type": spec.OfferID, "region": spec.Location, "image": "linode/debian13", "user_data": spec.UserData,
			}},
		}, map[string]string{"id": "${linode_instance.machine.id}", "ip": "${linode_instance.machine.ip_address}"})
		return body, []string{"TF_VAR_token=" + creds.Token}, "linode_instance.machine", err
	case "exoscale":
		templateID := spec.ImageID
		if templateID == "" {
			templateID = "debian-13"
		}
		disk := spec.RootGB
		if disk < 10 {
			disk = 10
		}
		machine := map[string]any{
			"zone": spec.Location, "name": spec.Name, "type": spec.OfferID, "template_id": templateID, "disk_size": disk, "user_data": spec.UserData,
		}
		if spec.GroupID != "" {
			machine["security_group_ids"] = []string{spec.GroupID}
		}
		body, err := stackJSON("exoscale", "exoscale/exoscale", "~> 0.64", []string{"application_key", "application_secret"}, map[string]any{
			"key": "${var.application_key}", "secret": "${var.application_secret}",
		}, map[string]any{
			"exoscale_compute_instance": map[string]any{"machine": machine},
		}, map[string]string{
			// zone:instance, the form destroyExoscale reads.
			"id": "${exoscale_compute_instance.machine.zone}:${exoscale_compute_instance.machine.id}",
			"ip": "${exoscale_compute_instance.machine.public_ip_address}",
		})
		return body, []string{"TF_VAR_application_key=" + creds.ApplicationKey, "TF_VAR_application_secret=" + creds.ApplicationSecret}, "exoscale_compute_instance.machine", err
	case "gcore":
		body, err := stackJSON("gcore", "G-Core/gcore", "~> 0.32", []string{"token"}, map[string]any{"api_key": "${var.token}"}, map[string]any{
			"gcore_instance": map[string]any{"machine": map[string]any{"name": spec.Name, "flavor_id": spec.OfferID, "region_id": spec.Location}},
		}, map[string]string{"id": "${gcore_instance.machine.id}", "ip": "${gcore_instance.machine.addresses[0].addr}"})
		return body, []string{"TF_VAR_token=" + creds.Token}, "gcore_instance.machine", err
	case "kamatera":
		body, err := stackJSON("kamatera", "Kamatera/kamatera", "~> 0.9", []string{"application_key", "application_secret"}, map[string]any{
			"api_client_id": "${var.application_key}", "api_secret": "${var.application_secret}",
		}, map[string]any{
			"kamatera_server": map[string]any{"machine": map[string]any{
				"name": spec.Name, "datacenter_id": spec.Location, "image_id": spec.OfferID, "cpu_cores": 1, "ram_mb": 1024,
			}},
		}, map[string]string{"id": "${kamatera_server.machine.id}", "ip": "${kamatera_server.machine.public_ips[0]}"})
		return body, []string{"TF_VAR_application_key=" + creds.ApplicationKey, "TF_VAR_application_secret=" + creds.ApplicationSecret}, "kamatera_server.machine", err
	case "ionos":
		cores, ram, err := ionosOffer(spec.OfferID)
		if err != nil && importID == "" {
			return "", nil, "", err
		}
		if cores == 0 {
			cores, ram = 1, 1
		}
		body, err := stackJSON("ionoscloud", "ionos-cloud/ionoscloud", "~> 6.7", []string{"token"}, map[string]any{"token": "${var.token}"}, map[string]any{
			"ionoscloud_datacenter": map[string]any{"machine": map[string]any{"name": spec.Name, "location": spec.Location}},
			"ionoscloud_server": map[string]any{"machine": map[string]any{
				"datacenter_id": "${ionoscloud_datacenter.machine.id}", "name": spec.Name, "cores": cores, "ram": ram,
			}},
		}, map[string]string{"id": "${ionoscloud_server.machine.id}", "ip": "${ionoscloud_server.machine.primary_ip}"})
		return body, []string{"TF_VAR_token=" + creds.Token}, "ionoscloud_server.machine", err
	case "openstack":
		body, err := openStackJSON(creds.Token, spec)
		env := []string{"TF_VAR_application_key=" + creds.ApplicationKey, "TF_VAR_application_secret=" + creds.ApplicationSecret, "TF_VAR_project_id=" + creds.ProjectID}
		return body, env, "openstack_compute_instance_v2.machine", err
	case "infomaniak":
		auth := "https://api.pub1.infomaniak.cloud/identity"
		if c.InfomaniakBase != "" {
			auth = strings.TrimRight(c.InfomaniakBase, "/") + "/identity"
		}
		body, err := openStackJSON(auth, spec)
		env := []string{"TF_VAR_application_key=" + creds.ApplicationKey, "TF_VAR_application_secret=" + creds.ApplicationSecret, "TF_VAR_project_id=" + creds.ProjectID}
		return body, env, "openstack_compute_instance_v2.machine", err
	case "proxmox":
		vmID := any(spec.OfferID)
		if n, err := strconv.Atoi(spec.OfferID); err == nil {
			vmID = n
		}
		body, err := stackJSON("proxmox", "bpg/proxmox", "~> 0.70", []string{"token", "endpoint"}, map[string]any{
			"endpoint": "${var.endpoint}", "api_token": "${var.token}", "insecure": true,
		}, map[string]any{
			"proxmox_virtual_environment_vm": map[string]any{"machine": map[string]any{
				"node_name": spec.Location, "name": spec.Name, "clone": map[string]any{"vm_id": vmID},
			}},
		}, map[string]string{"id": "${tostring(proxmox_virtual_environment_vm.machine.vm_id)}", "ip": "${proxmox_virtual_environment_vm.machine.ipv4_addresses[0][0]}"})
		return body, []string{"TF_VAR_token=" + creds.Token, "TF_VAR_endpoint=" + creds.ApplicationKey}, "proxmox_virtual_environment_vm.machine", err
	default:
		return "", nil, "", fmt.Errorf("Provider inconnu.")
	}
}

func openStackJSON(auth string, spec Spec) (string, error) {
	return stackJSON("openstack", "terraform-provider-openstack/openstack", "~> 3.0", []string{"application_key", "application_secret", "project_id"}, map[string]any{
		"auth_url": auth, "tenant_name": "${var.project_id}",
		"application_credential_id": "${var.application_key}", "application_credential_secret": "${var.application_secret}",
	}, map[string]any{
		"openstack_compute_instance_v2": map[string]any{"machine": map[string]any{
			"name": spec.Name, "flavor_id": spec.OfferID, "region": spec.Location, "user_data": spec.UserData,
			"network": map[string]any{"uuid": "auto"},
		}},
	}, map[string]string{"id": "${openstack_compute_instance_v2.machine.id}", "ip": "${openstack_compute_instance_v2.machine.access_ip_v4}"})
}

func stackJSON(name, source, version string, variables []string, provider map[string]any, resources map[string]any, outputs map[string]string) (string, error) {
	vars := map[string]any{}
	for _, variable := range variables {
		vars[variable] = map[string]any{"type": "string", "sensitive": true, "default": ""}
	}
	out := map[string]any{}
	for key, value := range outputs {
		out[key] = map[string]any{"value": value}
	}
	doc := map[string]any{
		"terraform": map[string]any{"required_providers": map[string]any{name: map[string]any{"source": source, "version": version}}},
		"variable":  vars,
		"provider":  map[string]any{name: provider},
		"resource":  resources,
		"output":    out,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}
