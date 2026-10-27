package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"myinfra/internal/store"
)

func mcpInfraTools() []mcpTool {
	tools := []mcpTool{
		{
			Name: "get_costs", Title: "Coût de l'infrastructure", Role: store.RoleRead,
			Description: "Le coût théorique mensuel des machines, par devise et machine par machine, et le budget mensuel s'il est fixé.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/costs"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var view costsView
				if err := c.get("/api/costs", &view); err != nil {
					return "", err
				}
				out := map[string]any{"monthly": amountsOf(view.Monthly).String(), "servers": view.Servers, "note": view.Note}
				if view.Budget > 0 {
					out["budget"] = view.Budget
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "create_cluster", Title: "Créer un cluster", Role: store.RoleDeploy, Write: true,
			Description: "Crée un cluster vide, sans machine. Pour un cluster avec ses machines, utilisez plutôt order_servers avec new_cluster : il achète et relie les machines en une commande.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom du cluster, par exemple SW.")}),
			Routes:      []string{"POST /api/clusters"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name string }](raw)
				if err != nil {
					return "", err
				}
				var out clusterView
				if err := c.send(http.MethodPost, "/api/clusters", map[string]string{"name": args.Name}, &out); err != nil {
					return "", err
				}
				return "Cluster « " + out.Name + " » créé, sans machine. Ajoutez-en avec order_servers (cluster=" + out.Name + ").", nil
			},
		},
		{
			Name: "get_delete_impact", Title: "Ce qu'une suppression emporterait", Role: store.RoleRead,
			Description: "Dit, sans rien lancer, ce que supprimer une machine, un cluster ou une application détruirait : machines chez le provider, applications, noms DNS, étapes, économie mensuelle.",
			Schema: object([]string{"kind", "name"}, map[string]any{
				"kind": choice("Ce qui serait supprimé.", "server", "cluster", "application"),
				"name": text("Son nom."),
			}),
			Routes: []string{"GET /api/servers", "GET /api/clusters", "GET /api/applications", "GET /api/servers/{id}/delete/impact", "GET /api/clusters/{id}/delete/impact", "GET /api/applications/{id}/delete/impact"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Kind, Name string }](raw)
				if err != nil {
					return "", err
				}
				target, err := c.deletionTarget(args.Kind, args.Name)
				if err != nil {
					return "", err
				}
				impact, err := c.impact(args.Kind, target.ID)
				if err != nil {
					return "", err
				}
				return impact.recap(), nil
			},
		},
		deletionTool("server", "delete_server", "Supprimer une machine",
			"Supprime une machine : achetée par MyInfra, elle est détruite chez son provider avec ses disques ; ajoutée à la main, elle est seulement oubliée. Ses applications et leurs noms DNS partent avec elle."),
		deletionTool("cluster", "delete_cluster", "Supprimer un cluster",
			"Supprime un cluster : ses machines achetées sont détruites chez leur provider, ses applications et leurs noms DNS retirés. Refusé pour le cluster qui héberge MyInfra."),
		deletionTool("application", "delete_application", "Supprimer une application",
			"Supprime une application, ses disques et ses noms DNS. Ses sauvegardes hors site sont gardées. Refusé pour MyInfra elle-même."),
		{
			Name: "get_destruction", Title: "Suivre une suppression", Role: store.RoleRead,
			Description: "L'avancement d'une suppression lancée : chaque étape et son état. Sans identifiant, les suppressions en cours.",
			Schema:      object(nil, map[string]any{"id": text("Identifiant rendu par l'outil de suppression.")}),
			Routes:      []string{"GET /api/destructions", "GET /api/destructions/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				path := "/api/destructions"
				if args.ID != "" {
					path += "/" + url.PathEscape(args.ID)
				}
				var out any
				if err := c.get(path, &out); err != nil {
					return "", err
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "resume_destruction", Title: "Reprendre une suppression", Role: store.RoleDeploy, Write: true,
			Description: "Relance une suppression en échec à partir de l'étape qui a échoué, après en avoir corrigé la cause.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant de la suppression.")}),
			Routes:      []string{"POST /api/destructions/{id}/resume"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				var out destructionView
				if err := c.send(http.MethodPost, "/api/destructions/"+url.PathEscape(args.ID)+"/resume", map[string]any{}, &out); err != nil {
					return "", err
				}
				return "Suppression relancée.\n" + asJSON(out), nil
			},
		},
		{
			Name: "retry_server_deploy", Title: "Relancer l'installation d'une machine", Role: store.RoleDeploy, Write: true,
			Description: "Relance l'installation d'une machine dont une étape a échoué (get_server montre les étapes).",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine.")}),
			Routes:      []string{"GET /api/servers", "POST /api/servers/{id}/deploy"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Server string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/servers/"+url.PathEscape(row.ID)+"/deploy", map[string]any{}, nil); err != nil {
					return "", err
				}
				return "Installation de " + row.Name + " relancée : suivez-la avec get_server.", nil
			},
		},
		{
			Name: "add_existing_server", Title: "Ajouter un serveur existant", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Relie une machine que l'utilisateur a déjà, par son adresse IP : MyInfra s'y connecte lui-même en root par SSH, puis installe Docker et le proxy, ou WireGuard et Kubernetes dans un cluster. La clé publique d'une des clés choisies doit déjà être acceptée par la machine pour root, ou pour ssh_user quand root est refusé, et MyInfra doit garder sa clé privée. La commande à coller sur la machine, l'autre façon de la relier, ne se donne que dans l'interface. Deux appels : le premier rend le récapitulatif et un code ; le second, avec le code, après l'accord explicite de l'utilisateur, lance l'installation.",
			Schema: object([]string{"name", "address"}, map[string]any{
				"name":          text("Nom de la machine dans MyInfra."),
				"address":       text("Adresse IP de la machine, que MyInfra joint en SSH (port 22)."),
				"ssh_user":      text("Utilisateur de la première session, root par défaut. Pour une machine qui refuse root (images Debian : « Please login as the user debian »), cet utilisateur prépare l'accès root par sudo sans mot de passe."),
				"cluster":       text("Cluster existant où relier la machine. Sans cluster, elle tourne seule avec Docker."),
				"control_plane": flag("La machine pilote le cluster. Toujours vrai pour la première machine d'un cluster."),
				"ssh_keys":      list("Clés SSH par leur nom. Par défaut toutes ; au moins une doit avoir sa clé privée dans MyInfra.", text("")),
				"confirmation":  confirmationArg,
			}),
			Routes: []string{"GET /api/secrets", "GET /api/clusters", "GET /api/servers", "POST /api/servers"},
			Run:    runAddExisting,
		},
	}
	return append(tools, mcpOrderTools()...)
}

type addExistingArgs struct {
	Name         string   `json:"name"`
	Address      string   `json:"address"`
	SSHUser      string   `json:"ssh_user"`
	Cluster      string   `json:"cluster"`
	ControlPlane bool     `json:"control_plane"`
	SSHKeys      []string `json:"ssh_keys"`
	Confirmation string   `json:"confirmation"`
}

// existingPlan is a machine added by hand, made ready: what the recap shows
// and what the creation sends.
type existingPlan struct {
	name         string
	address      string
	sshUser      string
	keys         []secretView
	cluster      *named
	controlPlane bool
}

func runAddExisting(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[addExistingArgs](raw)
	if err != nil {
		return "", err
	}
	var plan existingPlan
	recap := func() (string, string, error) {
		var err error
		plan, err = c.planExisting(args)
		if err != nil {
			return "", "", err
		}
		return plan.recap(), fingerprintOf([]any{plan.address, plan.sshUser, plan.cluster, plan.controlPlane, plan.keys}), nil
	}
	return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
		body := map[string]any{"name": plan.name, "providerId": "manual", "address": plan.address, "sshUser": plan.sshUser, "controlPlane": plan.controlPlane}
		keyIDs := make([]string, 0, len(plan.keys))
		for _, key := range plan.keys {
			keyIDs = append(keyIDs, key.ID)
		}
		body["keyIds"] = keyIDs
		if plan.cluster != nil {
			body["clusterId"] = plan.cluster.ID
		}
		var row serverView
		if err := c.send(http.MethodPost, "/api/servers", body, &row); err != nil {
			return "", err
		}
		return "Machine " + row.Name + " ajoutée (" + row.Address + ") : l'installation passe par SSH, suivez-la avec get_server.", nil
	})
}

func (c *mcpCall) planExisting(args addExistingArgs) (existingPlan, error) {
	var plan existingPlan
	name, ok := cleanName(args.Name)
	if !ok {
		return plan, failf("Donnez un nom à la machine.")
	}
	plan.name = name
	address, problem := directAddress(args.Address)
	if problem != "" || address == "" {
		return plan, failf("Donnez l'adresse IP de la machine, par exemple 203.0.113.10.")
	}
	plan.address = address
	user, problem := cleanSSHUser(args.SSHUser)
	if problem != "" {
		return plan, failf("%s", problem)
	}
	plan.sshUser = user
	keys, err := c.secretsOf("ssh_key")
	if err != nil {
		return plan, err
	}
	if len(args.SSHKeys) == 0 {
		plan.keys = keys
	} else {
		for _, wanted := range args.SSHKeys {
			key, err := pick("clé SSH", wanted, keys, func(s secretView) named { return named{s.ID, s.Name} })
			if err != nil {
				return plan, err
			}
			plan.keys = append(plan.keys, key)
		}
	}
	private := false
	for _, key := range plan.keys {
		private = private || key.HasPrivate
	}
	if !private {
		return plan, failf("Aucune des clés choisies n'a sa clé privée dans MyInfra : il ne pourrait pas ouvrir la session SSH. Ajoutez la clé privée dans Réglages › Clés SSH.")
	}
	var servers []serverView
	if err := c.get("/api/servers", &servers); err != nil {
		return plan, err
	}
	if args.Cluster != "" {
		cluster, err := c.cluster(args.Cluster)
		if err != nil {
			return plan, err
		}
		plan.cluster = &cluster
		plan.controlPlane = args.ControlPlane
	}
	hasPlane := false
	for _, row := range servers {
		if strings.EqualFold(row.Name, name) {
			return plan, failf("Une machine s'appelle déjà « %s ».", row.Name)
		}
		if row.Address == address {
			return plan, failf("La machine « %s » a déjà l'adresse %s.", row.Name, address)
		}
		if plan.cluster != nil && row.ClusterID == plan.cluster.ID && row.ControlPlane {
			hasPlane = true
		}
	}
	if plan.cluster != nil && !hasPlane {
		plan.controlPlane = true
	}
	return plan, nil
}

func (p existingPlan) recap() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ajout de la machine existante « %s » (%s).\n", p.name, p.address)
	names := make([]string, 0, len(p.keys))
	for _, key := range p.keys {
		names = append(names, key.Name)
	}
	if p.sshUser != "root" {
		fmt.Fprintf(&b, "MyInfra s'y connecte en %s par SSH avec : %s, prépare l'accès root par sudo, puis continue en root.\n", p.sshUser, strings.Join(names, ", "))
	} else {
		fmt.Fprintf(&b, "MyInfra s'y connecte en root par SSH avec : %s.\n", strings.Join(names, ", "))
	}
	switch {
	case p.cluster == nil:
		b.WriteString("Elle tourne seule : Docker et le proxy y sont installés.\n")
	case p.controlPlane:
		fmt.Fprintf(&b, "Elle rejoint le cluster %s et le pilote : WireGuard et Kubernetes y sont installés.\n", p.cluster.Name)
	default:
		fmt.Fprintf(&b, "Elle rejoint le cluster %s : WireGuard et Kubernetes y sont installés.\n", p.cluster.Name)
	}
	b.WriteString("Le pare-feu, la configuration SSH et les ports 80 et 443 de la machine sont pris par MyInfra ; une application peut ouvrir d'autres ports, TCP ou UDP, avec expose. Rien n'est acheté.\n")
	return b.String()
}

func (c *mcpCall) deletionTarget(kind, name string) (named, error) {
	switch kind {
	case "server":
		row, err := c.server(name)
		return named{row.ID, row.Name}, err
	case "cluster":
		return c.cluster(name)
	default:
		return c.application(name)
	}
}

var impactPath = map[string]string{"server": "/api/servers/", "cluster": "/api/clusters/", "application": "/api/applications/"}

func (c *mcpCall) impact(kind, id string) (deleteImpactView, error) {
	var out deleteImpactView
	err := c.get(impactPath[kind]+url.PathEscape(id)+"/delete/impact", &out)
	return out, err
}

// deletionTool deletes one thing in two calls: the impact, then the deletion
// with the name the API asks to type, as a person types it in the interface.
func deletionTool(kind, name, title, description string) mcpTool {
	label := map[string]string{"server": "Nom de la machine.", "cluster": "Nom du cluster.", "application": "Nom de l'application."}[kind]
	list := map[string]string{"server": "GET /api/servers", "cluster": "GET /api/clusters", "application": "GET /api/applications"}[kind]
	return mcpTool{
		Name: name, Title: title, Role: store.RoleDeploy, Write: true, Destructive: true,
		Description: description + " Deux appels : le premier rend ce qui sera détruit et un code ; le second, avec le code, après l'accord de l'utilisateur, lance la suppression.",
		Schema:      object([]string{"name"}, map[string]any{"name": text(label), "confirmation": confirmationArg}),
		Routes:      []string{list, "GET " + impactPath[kind] + "{id}/delete/impact", "DELETE " + impactPath[kind] + "{id}"},
		Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
			args, err := decodeArgs[struct{ Name, Confirmation string }](raw)
			if err != nil {
				return "", err
			}
			target, err := c.deletionTarget(kind, args.Name)
			if err != nil {
				// A code sent again once the deletion ran finds its target gone:
				// say the code is spent rather than the name unknown.
				return "", c.spentCode(args.Confirmation, err)
			}
			if target.ID == selfAppID {
				return "", failf("MyInfra ne se supprime pas par l'assistant : faites-le depuis l'interface, avec la phrase de confirmation.")
			}
			var impact deleteImpactView
			recap := func() (string, string, error) {
				var err error
				impact, err = c.impact(kind, target.ID)
				if err != nil {
					return "", "", err
				}
				if impact.HostsMyInfra {
					return "", "", failf("%s héberge MyInfra : sa suppression se fait depuis l'interface, avec la phrase « %s ».", impact.Name, selfDestroyPhrase)
				}
				return impact.recap(), fingerprintOf([]any{impact.Machines, impact.Applications, impact.Domains, impact.Steps}), nil
			}
			return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
				var out destructionView
				if err := c.send(http.MethodDelete, impactPath[kind]+url.PathEscape(target.ID), map[string]string{"name": target.Name}, &out); err != nil {
					return "", err
				}
				return "Suppression de « " + target.Name + " » lancée (identifiant " + out.ID + "). Suivez-la avec get_destruction.\n" + asJSON(out), nil
			})
		},
	}
}
