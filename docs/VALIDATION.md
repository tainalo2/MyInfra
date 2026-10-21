# Validation des intégrations (phase 3)

Ce document distingue ce qui a été **essayé en conditions réelles** de ce qui est couvert par des **tests de contrat** (faux serveurs HTTP, OpenTofu `validate`, échantillons catalogue).

## Providers (machines)

| Provider | Catalogue (échantillon) | OpenTofu `validate` | `Exists` (contrat HTTP) | E2E réel |
|----------|-------------------------|----------------------|-------------------------|----------|
| Scaleway | — | oui | oui | oui |
| Hetzner | — | oui | oui | oui |
| DigitalOcean | — | oui | oui | oui |
| Vultr | oui | oui | oui | oui |
| OVH | — | oui | (API signée) | oui |
| Exoscale | oui | oui | oui | oui |
| UpCloud | oui | oui | oui | contrat |
| Akamai (Linode) | oui | oui | oui | contrat |
| Gcore | oui | oui | oui | contrat |
| Ionos | oui | oui | oui | contrat |
| Kamatera | oui | oui | non (`ErrNoCheck`) | contrat |
| Infomaniak | — | oui | oui (OpenStack) | contrat |
| OpenStack | — | oui | non | contrat |
| Proxmox | — | oui | non | contrat |

Les tests Go concernés : `internal/catalog/providers_test.go`, `internal/provision/tofu_json_test.go`, `internal/provision/exists_test.go`, `internal/provision/exists_providers_test.go`.

## DNS

| Fournisseur | Zones / écriture | Liste des enregistrements | E2E réel |
|-------------|------------------|---------------------------|----------|
| OVH | `dns_test.go` | `records_test.go` | oui |
| Cloudflare | `dns_test.go` | `records_test.go` | contrat |

Scaleway et Hetzner DNS sont exposés dans l’interface ; la validation produit suit la même approche que OVH/Cloudflare (à étendre si besoin).

## Registres privés (pull d’images)

| Étape | Couverture |
|-------|------------|
| Vérification du login (`registry.Check`, bearer/basic) | `internal/registry/registry_test.go` |
| Liste des tags avec identifiants | `TestPrivateRegistryBearerUsesTheLogin` |
| Secret `imagePullSecrets` au déploiement cluster | `internal/manifest/manifest_test.go` (`TestRenderGivesPrivateRegistryLoginToPods`) |
| `docker login` sur machine seule | `internal/api/registries.go` (`agentLogin`) |

Un essai **réel** sur votre registre (GHCR, Docker Hub privé, registre custom) reste recommandé une fois par installation : compte **Registres** → image privée dans une application → déploiement.

## Copies MyInfra et synchronisation

Voir [SECURITY_COPIES.md](SECURITY_COPIES.md). Tests : `internal/api/lead_db_test.go`, `internal/api/sync_auth_test.go`, `internal/api/guard_test.go`.
