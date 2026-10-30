package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"myinfra/internal/store"
)

func mcpUpdateTools() []mcpTool {
	return []mcpTool{
		{
			Name: "check_updates", Title: "Chercher les mises à jour", Role: store.RoleRead, Write: true,
			Description: "Relance tout de suite la recherche des mises à jour. Le résultat arrive ensuite dans get_update_impact ; cette réponse dit seulement que la recherche est partie.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"POST /api/updates/check"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				return c.callJSON(http.MethodPost, "/api/updates/check", map[string]any{})
			},
		},
		{
			Name: "list_update_jobs", Title: "Mises à jour de machines", Role: store.RoleRead,
			Description: "Les mises à jour de machines et de clusters lancées, avec leur état.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/update-jobs"},
			Run:         passThrough("/api/update-jobs"),
		},
		{
			Name: "get_update_job", Title: "Suivre une mise à jour de machines", Role: store.RoleRead,
			Description: "Une mise à jour de machines ou de cluster, étape par étape.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant rendu au lancement.")}),
			Routes:      []string{"GET /api/update-jobs/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/update-jobs/"+url.PathEscape(args.ID), nil)
			},
		},
		{
			Name: "update_server", Title: "Mettre à jour une machine", Role: store.RoleDeploy, Write: true,
			Description: "Applique la mise à jour disponible d'une machine (paquets, et k3s si k3s est vrai). get_update_impact avec kind=server la décrit. Suivez-la avec get_update_job.",
			Schema:      object([]string{"server"}, map[string]any{"server": text("Nom de la machine."), "k3s": flag("Mettre aussi à jour k3s, quand une version est proposée.")}),
			Routes:      []string{"GET /api/servers", "POST /api/servers/{id}/update"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Server string
					K3s    bool
				}](raw)
				if err != nil {
					return "", err
				}
				row, err := c.server(args.Server)
				if err != nil {
					return "", err
				}
				text, err := c.callJSON(http.MethodPost, "/api/servers/"+url.PathEscape(row.ID)+"/update", map[string]bool{"k3s": args.K3s})
				if err != nil {
					return "", err
				}
				return "Mise à jour de " + row.Name + " lancée.\n" + text, nil
			},
		},
		{
			Name: "update_cluster", Title: "Mettre à jour un cluster", Role: store.RoleDeploy, Write: true,
			Description: "Applique la mise à jour des machines d'un cluster, l'une après l'autre. k3s met aussi à jour Kubernetes quand une version est proposée. get_update_impact avec kind=cluster décrit l'impact.",
			Schema:      object([]string{"cluster"}, map[string]any{"cluster": text("Nom du cluster."), "k3s": flag("Mettre aussi à jour k3s.")}),
			Routes:      []string{"GET /api/clusters", "POST /api/clusters/{id}/update"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Cluster string
					K3s     bool
				}](raw)
				if err != nil {
					return "", err
				}
				row, err := c.cluster(args.Cluster)
				if err != nil {
					return "", err
				}
				text, err := c.callJSON(http.MethodPost, "/api/clusters/"+url.PathEscape(row.ID)+"/update", map[string]bool{"k3s": args.K3s})
				if err != nil {
					return "", err
				}
				return "Mise à jour du cluster " + row.Name + " lancée.\n" + text, nil
			},
		},
		{
			Name: "update_myinfra", Title: "Mettre à jour MyInfra", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Remplace cette copie de MyInfra par la version disponible. Deux appels : le premier dit la version et rend un code ; le second, après l'accord explicite de l'utilisateur, lance la mise à jour. MyInfra peut redémarrer.",
			Schema:      object(nil, map[string]any{"confirmation": confirmationArg}),
			Routes:      []string{"GET /api/version", "POST /api/updates/myinfra"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					var view versionView
					if err := c.get("/api/version", &view); err != nil {
						return "", "", err
					}
					if !view.CanUpdate {
						reason := view.Reason
						if reason == "" {
							reason = "MyInfra est à jour."
						}
						return "", "", failf("%s", reason)
					}
					text := "Mise à jour de MyInfra : " + view.Current + " → " + view.Latest + ". Cette copie peut redémarrer.\n"
					return text, view.Latest, nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPost, "/api/updates/myinfra", map[string]any{})
				})
			},
		},
		{
			Name: "list_quiet_updates", Title: "Mises à jour sans interruption", Role: store.RoleRead,
			Description: "Les mises à jour qui passent sans interrompre aucune application (jamais une version majeure), et le réglage des correctifs de nuit.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/updates/quiet", "GET /api/updates/auto"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var quiet, auto any
				if err := c.get("/api/updates/quiet", &quiet); err != nil {
					return "", err
				}
				if err := c.get("/api/updates/auto", &auto); err != nil {
					return "", err
				}
				return asJSON(map[string]any{"quiet": quiet, "nightly": auto}), nil
			},
		},
		{
			Name: "apply_quiet_updates", Title: "Appliquer les mises à jour sans interruption", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Lance ensemble les mises à jour qui passent sans interruption : copies de la bibliothèque, applications, serveurs, et MyInfra en dernier quand il tourne en deux copies. Deux appels : le premier liste ce qui sera lancé et rend un code ; le second, après l'accord explicite de l'utilisateur, lance tout.",
			Schema:      object(nil, map[string]any{"confirmation": confirmationArg}),
			Routes:      []string{"GET /api/updates/quiet", "POST /api/updates/quiet"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					var view struct {
						Updates []quietItem `json:"updates"`
					}
					if err := c.get("/api/updates/quiet", &view); err != nil {
						return "", "", err
					}
					if len(view.Updates) == 0 {
						return "", "", failf("Aucune mise à jour ne passe sans interruption pour le moment.")
					}
					names := make([]string, 0, len(view.Updates))
					for _, item := range view.Updates {
						names = append(names, item.Name)
					}
					text := "Mises à jour lancées sans interruption : " + strings.Join(names, ", ") + ".\n"
					return text, strings.Join(names, "|"), nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPost, "/api/updates/quiet", map[string]any{})
				})
			},
		},
		{
			Name: "set_nightly_updates", Title: "Correctifs de nuit", Role: store.RoleDeploy, Write: true,
			Description: "Active ou coupe l'application chaque nuit des mises à jour qui passent sans interruption, à partir d'une heure (0 à 23). Les versions majeures et ce qui interrompt une application attendent toujours l'accord de l'utilisateur.",
			Schema:      object([]string{"enabled"}, map[string]any{"enabled": flag("true pour activer, false pour couper."), "hour": map[string]any{"type": "integer", "minimum": 0, "maximum": 23, "description": "Heure de départ, de 0 à 23."}}),
			Routes:      []string{"PUT /api/updates/auto"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Enabled bool
					Hour    *int
				}](raw)
				if err != nil {
					return "", err
				}
				body := map[string]any{"enabled": args.Enabled}
				if args.Hour != nil {
					body["hour"] = *args.Hour
				}
				return c.callJSON(http.MethodPut, "/api/updates/auto", body)
			},
		},
		{
			Name: "resume_update_job", Title: "Reprendre une mise à jour", Role: store.RoleDeploy, Write: true,
			Description: "Relance une mise à jour de machines qui a échoué, depuis l'étape en échec.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant de la mise à jour.")}),
			Routes:      []string{"POST /api/update-jobs/{id}/resume"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				text, err := c.callJSON(http.MethodPost, "/api/update-jobs/"+url.PathEscape(args.ID)+"/resume", map[string]any{})
				if err != nil {
					return "", err
				}
				return "Mise à jour reprise.\n" + text, nil
			},
		},
		{
			Name: "cancel_update_job", Title: "Annuler une mise à jour", Role: store.RoleDeploy, Write: true,
			Description: "Arrête une mise à jour de machines encore en cours. Ce qui est déjà appliqué sur une machine reste.",
			Schema:      object([]string{"id"}, map[string]any{"id": text("Identifiant de la mise à jour.")}),
			Routes:      []string{"POST /api/update-jobs/{id}/cancel"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ ID string }](raw)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/update-jobs/"+url.PathEscape(args.ID)+"/cancel", map[string]any{}, nil); err != nil {
					return "", err
				}
				return "Mise à jour " + args.ID + " annulée.", nil
			},
		},
		{
			Name: "update_library_copy", Title: "Mettre à jour une copie de la bibliothèque", Role: store.RoleDeploy, Write: true,
			Description: "Remplace un paquet ou un modèle copié depuis la bibliothèque par la version qu'elle propose. Les applications déjà installées ne changent pas.",
			Schema: object([]string{"kind", "name"}, map[string]any{
				"kind": choice("Ce qui est copié.", "package", "template"),
				"name": text("Nom du paquet ou du modèle."),
			}),
			Routes: []string{"GET /api/packages", "GET /api/templates", "POST /api/updates/library/{kind}/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Kind, Name string }](raw)
				if err != nil {
					return "", err
				}
				var id string
				switch args.Kind {
				case "package":
					row, err := c.savedPackage(args.Name)
					if err != nil {
						return "", err
					}
					id = row.ID
				case "template":
					row, err := c.template(args.Name)
					if err != nil {
						return "", err
					}
					id = row.ID
				default:
					return "", failf("kind vaut package ou template.")
				}
				if err := c.send(http.MethodPost, "/api/updates/library/"+url.PathEscape(args.Kind)+"/"+url.PathEscape(id), map[string]any{}, nil); err != nil {
					return "", err
				}
				return "« " + args.Name + " » est à la version de la bibliothèque.", nil
			},
		},
	}
}
