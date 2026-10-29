package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"myinfra/internal/store"
)

func mcpMachineReadTools() []mcpTool {
	return []mcpTool{
		{
			Name: "get_server_runtime", Title: "Charge d'une machine", Role: store.RoleRead,
			Description: "La charge mesurée d'une machine de cluster : processeur, mémoire, disque, et les pods qui y tournent. Une machine seule ou pas encore reliée n'a pas de mesure.",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine.")}),
			Routes:      []string{"GET /api/servers", "GET /api/servers/{id}/runtime"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				row, err := c.namedServer(raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/servers/"+url.PathEscape(row.ID)+"/runtime", nil)
			},
		},
		{
			Name: "check_server", Title: "Sonder une machine", Role: store.RoleRead, Write: true,
			Description: "Interroge tout de suite une machine (SSH) et, si elle a été achetée, son provider. Le résultat est la fiche de la machine après la sonde.",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine.")}),
			Routes:      []string{"GET /api/servers", "POST /api/servers/{id}/check"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				row, err := c.namedServer(raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodPost, "/api/servers/"+url.PathEscape(row.ID)+"/check", map[string]any{})
			},
		},
		{
			Name: "get_server_tofu", Title: "Fichiers OpenTofu d'une machine", Role: store.RoleRead,
			Description: "Les fichiers OpenTofu de la machine achetée, tels que MyInfra les tient. Rien pour une machine ajoutée à la main.",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine.")}),
			Routes:      []string{"GET /api/servers", "GET /api/servers/{id}/tofu"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				row, err := c.namedServer(raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/servers/"+url.PathEscape(row.ID)+"/tofu", nil)
			},
		},
		{
			Name: "get_dns_tofu", Title: "Fichiers OpenTofu d'un nom", Role: store.RoleRead,
			Description: "Les fichiers OpenTofu d'un nom DNS que MyInfra a écrit.",
			Schema:      object([]string{"host"}, map[string]any{"host": text("Nom complet, par exemple cloud.mondomaine.fr.")}),
			Routes:      []string{"GET /api/dns/{host}/tofu"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Host string }](raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/dns/"+url.PathEscape(strings.TrimSpace(args.Host))+"/tofu", nil)
			},
		},
		{
			Name: "list_argo_apps", Title: "Applications Argo d'un cluster", Role: store.RoleRead,
			Description: "Les applications Argo CD d'un cluster importé. Un cluster créé par MyInfra n'en a pas : ses applications se lisent avec list_applications.",
			Schema:      object([]string{"cluster"}, map[string]any{"cluster": text("Nom du cluster.")}),
			Routes:      []string{"GET /api/clusters", "GET /api/clusters/{id}/argo"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Cluster string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.cluster(args.Cluster)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/clusters/"+url.PathEscape(row.ID)+"/argo", nil)
			},
		},
		{
			Name: "list_backups", Title: "Sauvegardes d'une application", Role: store.RoleRead,
			Description: "Les sources de sauvegarde de chaque service d'une application (stockage du cluster, S3, disque, PostgreSQL, machine). Le fichier lui-même se télécharge et se restaure depuis l'interface : le MCP ne transporte pas ce fichier.",
			Schema:      object([]string{"application"}, map[string]any{"application": text("Nom de l'application.")}),
			Routes:      []string{"GET /api/applications", "GET /api/applications/{id}/backups"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application string }](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/applications/"+url.PathEscape(app.ID)+"/backups", nil)
			},
		},
	}
}

func mcpMachineWriteTools() []mcpTool {
	server := text("Nom de la machine.")
	return []mcpTool{
		{
			Name: "set_server_place", Title: "Lieu d'une machine", Role: store.RoleDeploy, Write: true,
			Description: "Indique la ville d'une machine ajoutée à la main. Le lieu d'une machine achetée vient de son offre et ne se change pas.",
			Schema:      object([]string{"server", "place"}, map[string]any{"server": server, "place": text("Ville, par exemple Paris. Vide pour l'effacer.")}),
			Routes:      []string{"GET /api/servers", "PUT /api/servers/{id}/place"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Server, Place string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPut, "/api/servers/"+url.PathEscape(row.ID)+"/place", map[string]string{"place": args.Place}, nil); err != nil {
					return "", err
				}
				return "Lieu de " + row.Name + " enregistré.", nil
			},
		},
		{
			Name: "set_server_hosting", Title: "Hébergement d'une machine", Role: store.RoleDeploy, Write: true,
			Description: "Indique qui loue une machine ajoutée à la main, et à quel prix. Une machine achetée garde le provider et le prix de son offre.",
			Schema: object([]string{"server"}, map[string]any{
				"server":   server,
				"provider": text("Provider connu (hetzner, ovh, scaleway…) ou other."),
				"name":     text("Nom du provider quand provider vaut other."),
				"price":    text("Prix, par exemple 4.99. Vide pour n'en garder aucun."),
				"period":   choice("Période du prix.", "month", "hour"),
				"currency": choice("Devise.", "EUR", "USD"),
			}),
			Routes: []string{"GET /api/servers", "PUT /api/servers/{id}/hosting"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Server, Provider, Name, Price, Period, Currency string
				}](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				body := map[string]string{"provider": args.Provider, "name": args.Name, "price": args.Price, "period": args.Period, "currency": args.Currency}
				if err := c.send(http.MethodPut, "/api/servers/"+url.PathEscape(row.ID)+"/hosting", body, nil); err != nil {
					return "", err
				}
				return "Hébergement de " + row.Name + " enregistré.", nil
			},
		},
		{
			Name: "set_server_ssh_user", Title: "Utilisateur SSH d'une machine", Role: store.RoleDeploy, Write: true,
			Description: "Change l'utilisateur de la première session SSH d'une machine ajoutée à la main. root par défaut. Une machine achetée s'ouvre toujours en root.",
			Schema:      object([]string{"server", "ssh_user"}, map[string]any{"server": server, "ssh_user": text("Utilisateur, par exemple debian. root pour revenir à root.")}),
			Routes:      []string{"GET /api/servers", "PUT /api/servers/{id}/ssh-user"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Server  string `json:"server"`
					SSHUser string `json:"ssh_user"`
				}](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPut, "/api/servers/"+url.PathEscape(row.ID)+"/ssh-user", map[string]string{"sshUser": args.SSHUser}, nil); err != nil {
					return "", err
				}
				return "Utilisateur SSH de " + row.Name + " enregistré.", nil
			},
		},
		{
			Name: "save_server_tofu", Title: "Appliquer l'OpenTofu d'une machine", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Remplace les fichiers OpenTofu d'une machine achetée et les applique chez le provider. Deux appels : le premier décrit les fichiers et rend un code ; le second, avec le code, après l'accord de l'utilisateur, applique.",
			Schema: object([]string{"server", "files"}, map[string]any{
				"server":       server,
				"files":        textMap("Fichiers par nom, par exemple main.tf, et leur texte."),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/servers", "GET /api/servers/{id}/tofu", "PUT /api/servers/{id}/tofu"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Server       string
					Files        map[string]string
					Confirmation string
				}](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				path := "/api/servers/" + url.PathEscape(row.ID) + "/tofu"
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					if err := c.get(path, &struct{}{}); err != nil {
						return "", "", err
					}
					names := make([]string, 0, len(args.Files))
					for name := range args.Files {
						names = append(names, name)
					}
					text := "Application OpenTofu de « " + row.Name + " » chez son provider. Fichiers : " + strings.Join(names, ", ") + ".\n"
					return text, fingerprintOf(args.Files), nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPut, path, map[string]any{"files": args.Files})
				})
			},
		},
		{
			Name: "save_dns_tofu", Title: "Appliquer l'OpenTofu d'un nom", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Remplace les fichiers OpenTofu d'un nom DNS et les applique. Deux appels : récapitulatif et code, puis application après l'accord de l'utilisateur. Réservé à un jeton administrateur.",
			Schema: object([]string{"host", "files"}, map[string]any{
				"host":         text("Nom complet."),
				"files":        textMap("Fichiers par nom et leur texte."),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/dns/{host}/tofu", "PUT /api/dns/{host}/tofu"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Host         string
					Files        map[string]string
					Confirmation string
				}](raw)
				if err != nil {
					return "", err
				}
				path := "/api/dns/" + url.PathEscape(strings.TrimSpace(args.Host)) + "/tofu"
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					if err := c.get(path, &struct{}{}); err != nil {
						return "", "", err
					}
					return "Application OpenTofu du nom " + args.Host + ".\n", fingerprintOf(args.Files), nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPut, path, map[string]any{"files": args.Files})
				})
			},
		},
		{
			Name: "attach_server", Title: "Rattacher une machine à un cluster", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Enregistre qu'une machine déjà dans MyInfra appartient à un cluster, ou la détache si cluster est vide. Cela ne réinstalle rien : WireGuard et Kubernetes se mettent en place en ajoutant la machine (add_existing_server) ou en l'achetant (order_servers). Refusé tant qu'une application tourne sur la machine. Deux appels : récapitulatif et code, puis rattachement après l'accord.",
			Schema: object([]string{"server"}, map[string]any{
				"server":       server,
				"cluster":      text("Cluster. Vide pour détacher la machine."),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/servers", "GET /api/clusters", "POST /api/servers/{id}/cluster"},
			Run:    runAttachServer,
		},
		{
			Name: "import_cluster", Title: "Importer un cluster", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre un cluster déjà existant à partir de son kubeconfig. Le kubeconfig est chiffré et n'est pas répété dans la réponse. Ce cluster n'est pas géré comme un cluster créé par MyInfra : ses applications Argo se lisent avec list_argo_apps.",
			Schema: object([]string{"name", "kubeconfig"}, map[string]any{
				"name":       text("Nom du cluster dans MyInfra."),
				"kubeconfig": text("Texte du kubeconfig. Écrit une fois, jamais relu."),
			}),
			Routes: []string{"POST /api/clusters/import"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name, Kubeconfig string }](raw)
				if err != nil {
					return "", hideSecretErr(args.Kubeconfig, err)
				}
				var view clusterView
				err = c.send(http.MethodPost, "/api/clusters/import", map[string]string{"name": args.Name, "kubeconfig": args.Kubeconfig}, &view)
				if err != nil {
					return "", hideSecretErr(args.Kubeconfig, err)
				}
				return hideSecret(args.Kubeconfig, "Cluster « "+view.Name+" » importé."), nil
			},
		},
		{
			Name: "cancel_deployment", Title: "Annuler un déploiement", Role: store.RoleDeploy, Write: true,
			Description: "Arrête un déploiement en cours (machine, application ou lancement de modèle). L'identifiant et le type viennent de list_deployments. Le transfert de MyInfra ne s'annule pas par ici.",
			Schema: object([]string{"kind", "id"}, map[string]any{
				"kind": choice("Type du déploiement.", "server", "application", "template"),
				"id":   text("Identifiant rendu par list_deployments."),
			}),
			Routes: []string{"POST /api/deployments/cancel"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Kind, ID string }](raw)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/deployments/cancel", map[string]string{"kind": args.Kind, "id": args.ID}, nil); err != nil {
					return "", err
				}
				return "Déploiement " + args.Kind + " " + args.ID + " annulé.", nil
			},
		},
	}
}

func (c *mcpCall) namedServer(raw json.RawMessage) (serverView, error) {
	args, err := decodeArgs[struct{ Server string }](raw)
	if err != nil {
		return serverView{}, err
	}
	return c.server(args.Server)
}

func runAttachServer(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[struct {
		Server, Cluster, Confirmation string
	}](raw)
	if err != nil {
		return "", err
	}
	row, err := c.server(args.Server)
	if err != nil {
		return "", err
	}
	var clusterID, label string
	if strings.TrimSpace(args.Cluster) == "" {
		label = "Détachement de « " + row.Name + " » de son cluster. La machine n'est pas réinstallée.\n"
	} else {
		cluster, err := c.cluster(args.Cluster)
		if err != nil {
			return "", err
		}
		clusterID = cluster.ID
		label = "Rattachement de « " + row.Name + " » au cluster " + cluster.Name + ". La machine n'est pas réinstallée.\n"
	}
	return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
		return label, fingerprintOf([]string{row.ID, clusterID}), nil
	}, func() (string, error) {
		if err := c.send(http.MethodPost, "/api/servers/"+url.PathEscape(row.ID)+"/cluster", map[string]string{"clusterId": clusterID}, nil); err != nil {
			return "", err
		}
		if clusterID == "" {
			return row.Name + " n'est plus rattachée à un cluster.", nil
		}
		return row.Name + " est rattachée au cluster.", nil
	})
}
