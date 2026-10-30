package api

import (
	"encoding/json"
	"net/http"

	"myinfra/internal/store"
)

func mcpCopyTools() []mcpTool {
	return []mcpTool{
		{
			Name: "get_copies", Title: "Copies de MyInfra", Role: store.RoleRead,
			Description: "Cette copie pilote-t-elle les machines, et quels pairs la suivent. La prise de main se fait avec take_lead ou hand_over.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/sync"},
			Run:         passThrough("/api/sync"),
		},
		{
			Name: "list_host_tools", Title: "Outils de l'hôte", Role: store.RoleRead,
			Description: "Dit si Ansible, OpenTofu et Helm sont installés sur la machine qui porte MyInfra.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/tools"},
			Run:         passThrough("/api/tools"),
		},
		{
			Name: "install_host_tool", Title: "Installer un outil de l'hôte", Role: store.RoleDeploy, Write: true,
			Description: "Installe Ansible, OpenTofu ou Helm sur la machine qui porte MyInfra, s'il n'y est pas déjà.",
			Schema:      object([]string{"name"}, map[string]any{"name": choice("Outil.", "ansible", "tofu", "helm")}),
			Routes:      []string{"POST /api/tools/{name}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name string }](raw)
				if err != nil {
					return "", err
				}
				text, err := c.callJSON(http.MethodPost, "/api/tools/"+args.Name, nil)
				if err != nil {
					return "", err
				}
				return args.Name + " est disponible.\n" + text, nil
			},
		},
		{
			Name: "add_sync_peer", Title: "Ajouter un pair", Role: store.RoleAdmin, Write: true,
			Description: "Ajoute l'adresse d'une autre copie de MyInfra à synchroniser. Réservé à un jeton administrateur.",
			Schema:      object([]string{"url"}, map[string]any{"url": text("Adresse de l'autre copie, par exemple https://secours.exemple.fr.")}),
			Routes:      []string{"POST /api/sync/peers"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ URL string }](raw)
				if err != nil {
					return "", err
				}
				text, err := c.callJSON(http.MethodPost, "/api/sync/peers", map[string]string{"url": args.URL})
				if err != nil {
					return "", err
				}
				return "Pair ajouté.\n" + text, nil
			},
		},
		{
			Name: "remove_sync_peer", Title: "Retirer un pair", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Retire un pair de la synchronisation. Deux appels : récapitulatif et code, puis retrait après l'accord. Réservé à un jeton administrateur.",
			Schema:      object([]string{"url"}, map[string]any{"url": text("Adresse du pair."), "confirmation": confirmationArg}),
			Routes:      []string{"DELETE /api/sync/peers"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ URL, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "Retrait du pair " + args.URL + ".\n", args.URL, nil
				}, func() (string, error) {
					text, err := c.callJSON(http.MethodDelete, "/api/sync/peers", map[string]string{"url": args.URL})
					if err != nil {
						return "", err
					}
					return "Pair retiré.\n" + text, nil
				})
			},
		},
		{
			Name: "take_lead", Title: "Prendre la main", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Cette copie, si elle suit une autre, devient celle qui pilote les machines. force est pour le cas où l'autre copie ne répond pas. La copie redémarre. Deux appels : récapitulatif et code, puis prise de main après l'accord explicite. Réservé à un jeton administrateur.",
			Schema:      object(nil, map[string]any{"force": flag("Prendre la main même si l'autre copie ne répond pas."), "confirmation": confirmationArg}),
			Routes:      []string{"POST /api/copies/lead"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Force        bool
					Confirmation string
				}](raw)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					text := "Cette copie va piloter les machines, et redémarrer.\n"
					if args.Force {
						text += "L'autre copie sera écartée même si elle ne répond pas.\n"
					}
					return text, fingerprintOf(args.Force), nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPost, "/api/copies/lead", map[string]bool{"force": args.Force})
				})
			},
		},
		{
			Name: "hand_over", Title: "Rendre la main", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Rend le pilotage des machines à l'autre copie, qui doit répondre. Cette copie redémarre en suiveuse. Deux appels : récapitulatif et code, puis passation après l'accord explicite. Réservé à un jeton administrateur.",
			Schema:      object(nil, map[string]any{"confirmation": confirmationArg}),
			Routes:      []string{"POST /api/copies/handover"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "L'autre copie va piloter les machines. Celle-ci redémarre et la suit.\n", "handover", nil
				}, func() (string, error) {
					return c.callJSON(http.MethodPost, "/api/copies/handover", map[string]any{})
				})
			},
		},
	}
}
