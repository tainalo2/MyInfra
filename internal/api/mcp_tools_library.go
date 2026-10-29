package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"myinfra/internal/pack"
	"myinfra/internal/store"
)

func mcpLibraryTools() []mcpTool {
	return []mcpTool{
		{
			Name: "format_package", Title: "Mettre un paquet en YAML", Role: store.RoleRead,
			Description: "Écrit en YAML un paquet donné comme objet, et signale ce qui ne va pas. Rien n'est enregistré.",
			Schema:      object([]string{"package"}, map[string]any{"package": loose("Le paquet : type, version, name, services, inputs.")}),
			Routes:      []string{"POST /api/packages/format"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Package json.RawMessage `json:"package"`
				}](raw)
				if err != nil {
					return "", err
				}
				var pkg any
				if err := json.Unmarshal(args.Package, &pkg); err != nil {
					return "", failf("Le paquet est illisible.")
				}
				var out struct {
					YAML     string         `json:"yaml"`
					Problems []pack.Problem `json:"problems"`
				}
				if err := c.send(http.MethodPost, "/api/packages/format", map[string]any{"package": pkg}, &out); err != nil {
					return "", err
				}
				text := out.YAML
				if len(out.Problems) > 0 {
					text += "\nÀ corriger :\n" + problemsText(out.Problems)
				}
				return text, nil
			},
		},
		{
			Name: "export_package", Title: "Exporter un paquet", Role: store.RoleRead,
			Description: "Le YAML d'un paquet enregistré, prêt à être gardé ou réimporté.",
			Schema:      object([]string{"package"}, map[string]any{"package": text("Nom du paquet.")}),
			Routes:      []string{"GET /api/packages", "GET /api/packages/{id}/export"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Package string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.savedPackage(args.Package)
				if err != nil {
					return "", err
				}
				return c.callText(http.MethodGet, "/api/packages/"+url.PathEscape(row.ID)+"/export", nil)
			},
		},
		{
			Name: "import_package_url", Title: "Importer un paquet par URL", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre un paquet MyInfra publié à une adresse https. Un compose ou des manifests se convertissent d'abord avec convert_to_package.",
			Schema:      object([]string{"url"}, map[string]any{"url": text("Adresse https du fichier YAML ou JSON.")}),
			Routes:      []string{"POST /api/packages/import-url"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ URL string }](raw)
				if err != nil {
					return "", err
				}
				var view packageView
				if err := c.send(http.MethodPost, "/api/packages/import-url", map[string]string{"url": args.URL}, &view); err != nil {
					return "", err
				}
				return "Paquet « " + view.Name + " » importé.", nil
			},
		},
		{
			Name: "copy_catalogue_article", Title: "Copier un article du catalogue", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre un article du catalogue comme paquet, pour le modifier. La bibliothèque elle-même ne change pas. Les applications déjà installées non plus.",
			Schema:      object([]string{"article"}, map[string]any{"article": text("Article du catalogue, par exemple nextcloud.")}),
			Routes:      []string{"GET /api/catalogue", "POST /api/catalogue/{id}/copy"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Article string }](raw)
				if err != nil {
					return "", err
				}
				id, err := c.article(args.Article)
				if err != nil {
					return "", err
				}
				var view packageView
				if err := c.send(http.MethodPost, "/api/catalogue/"+url.PathEscape(id)+"/copy", map[string]any{}, &view); err != nil {
					return "", err
				}
				return "Article " + id + " copié comme paquet « " + view.Name + " ».", nil
			},
		},
		{
			Name: "list_article_versions", Title: "Versions d'un article", Role: store.RoleRead,
			Description: "Les étiquettes d'image publiées pour le service public d'un article du catalogue. install peut en prendre une avec version.",
			Schema:      object([]string{"article"}, map[string]any{"article": text("Article du catalogue.")}),
			Routes:      []string{"GET /api/catalogue", "GET /api/catalogue/{id}/versions"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Article string }](raw)
				if err != nil {
					return "", err
				}
				id, err := c.article(args.Article)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/catalogue/"+url.PathEscape(id)+"/versions", nil)
			},
		},
		{
			Name: "set_application_services", Title: "Changer les services", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Remplace les services d'une application installée depuis des images. Si le changement efface des données, le premier appel les nomme et rend un code ; le second, après l'accord, l'applique. Sinon il s'applique tout de suite. Le format d'un service est celui du guide myinfra://guide/packages.",
			Schema: object([]string{"application", "services"}, map[string]any{
				"application":  text("Nom de l'application."),
				"services":     list("Services voulus, dans l'ordre.", loose("Un service du paquet.")),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/applications", "PUT /api/applications/{id}/services"},
			Run:    runSetServices,
		},
		{
			Name: "replace_images", Title: "Changer les images", Role: store.RoleDeploy, Write: true,
			Description: "Remplace l'image de chaque service d'une application. Il faut nommer chaque service, même ceux dont l'image ne change pas.",
			Schema: object([]string{"application", "services"}, map[string]any{
				"application": text("Nom de l'application."),
				"services": list("Une entrée par service.", object([]string{"name", "image"}, map[string]any{
					"name":  text("Nom du service."),
					"image": text("Image, par exemple nginx:1.27."),
				})),
			}),
			Routes: []string{"GET /api/applications", "POST /api/applications/{id}/images"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Application string
					Services    []struct {
						Name  string `json:"name"`
						Image string `json:"image"`
					}
				}](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/applications/"+url.PathEscape(app.ID)+"/images", map[string]any{"services": args.Services}, nil); err != nil {
					return "", err
				}
				return "Images de " + app.Name + " remplacées.", nil
			},
		},
		{
			Name: "list_local_images", Title: "Images locales", Role: store.RoleRead,
			Description: "Les images que MyInfra garde et charge sur les machines (myinfra.local/nom:étiquette) : taille, ports, volumes, applications qui les utilisent. Déposer une archive se fait dans l'interface ; une adresse se récupère avec fetch_local_image.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/images"},
			Run:         passThrough("/api/images"),
		},
		{
			Name: "fetch_local_image", Title: "Récupérer une image", Role: store.RoleAdmin, Write: true,
			Description: "Télécharge une archive d'image à une adresse et la garde. Le nom sert dans les paquets sous myinfra.local/<nom>:<étiquette>. Réservé à un jeton administrateur.",
			Schema: object([]string{"url", "name"}, map[string]any{
				"url":  text("Adresse https de l'archive."),
				"name": text("Nom, en minuscules."),
				"tag":  text("Étiquette. Déduite de l'archive sinon."),
			}),
			Routes: []string{"POST /api/images/fetch"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ URL, Name, Tag string }](raw)
				if err != nil {
					return "", err
				}
				text, err := c.callJSON(http.MethodPost, "/api/images/fetch", map[string]string{"url": args.URL, "name": args.Name, "tag": args.Tag})
				if err != nil {
					return "", err
				}
				return "Image " + args.Name + " enregistrée.\n" + text, nil
			},
		},
		{
			Name: "delete_local_image", Title: "Supprimer une image locale", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Retire une image locale. Refusé tant qu'une application l'utilise. Deux appels : récapitulatif et code, puis suppression après l'accord. Réservé à un jeton administrateur.",
			Schema:      object([]string{"image"}, map[string]any{"image": text("Nom ou référence de l'image."), "confirmation": confirmationArg}),
			Routes:      []string{"GET /api/images", "DELETE /api/images/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Image, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.localImage(args.Image)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "Suppression de l'image " + row.Ref + ".\n", row.ID, nil
				}, func() (string, error) {
					if err := c.send(http.MethodDelete, "/api/images/"+url.PathEscape(row.ID), nil, nil); err != nil {
						return "", err
					}
					return "Image " + row.Ref + " supprimée.", nil
				})
			},
		},
		{
			Name: "create_ssh_key", Title: "Créer une clé SSH", Role: store.RoleAdmin, Write: true,
			Description: "Génère une clé SSH et en garde la clé privée. Seuls le nom, l'empreinte et la clé publique sont rendus : la clé privée ne se télécharge que dans l'interface. Réservé à un jeton administrateur.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom de la clé.")}),
			Routes:      []string{"POST /api/secrets/ssh-keys"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name string }](raw)
				if err != nil {
					return "", err
				}
				var view secretView
				if err := c.send(http.MethodPost, "/api/secrets/ssh-keys", map[string]string{"name": args.Name}, &view); err != nil {
					return "", err
				}
				return "Clé « " + view.Name + " » créée. Empreinte " + view.Fingerprint + ".\n" + view.PublicKey, nil
			},
		},
		{
			Name: "import_ssh_key", Title: "Importer une clé SSH", Role: store.RoleAdmin, Write: true,
			Description: "Enregistre une clé publique, et éventuellement sa clé privée pour que MyInfra s'en serve. La clé privée n'est pas répétée dans la réponse. Réservé à un jeton administrateur.",
			Schema: object([]string{"name", "public_key"}, map[string]any{
				"name":        text("Nom de la clé."),
				"public_key":  text("Ligne de clé publique."),
				"private_key": text("Clé privée, si MyInfra doit pouvoir s'en servir. Écrite une fois, jamais relue."),
			}),
			Routes: []string{"POST /api/secrets/ssh-keys/import"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Name       string `json:"name"`
					PublicKey  string `json:"public_key"`
					PrivateKey string `json:"private_key"`
				}](raw)
				if err != nil {
					return "", hideSecretErr(args.PrivateKey, err)
				}
				var view secretView
				err = c.send(http.MethodPost, "/api/secrets/ssh-keys/import", map[string]string{
					"name": args.Name, "publicKey": args.PublicKey, "privateKey": args.PrivateKey,
				}, &view)
				if err != nil {
					return "", hideSecretErr(args.PrivateKey, err)
				}
				text := "Clé « " + view.Name + " » importée. Empreinte " + view.Fingerprint + "."
				if view.HasPrivate {
					text += " MyInfra en garde la clé privée."
				}
				return hideSecret(args.PrivateKey, text), nil
			},
		},
		{
			Name: "delete_ssh_key", Title: "Supprimer une clé SSH", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Retire une clé SSH. Deux appels : récapitulatif et code, puis suppression après l'accord. La clé privée n'est pas montrée. Réservé à un jeton administrateur.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom de la clé."), "confirmation": confirmationArg}),
			Routes:      []string{"GET /api/secrets", "DELETE /api/secrets/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.sshKey(args.Name)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "Suppression de la clé SSH « " + row.Name + " » (" + row.Fingerprint + ").\n", row.ID, nil
				}, func() (string, error) {
					if err := c.send(http.MethodDelete, "/api/secrets/"+url.PathEscape(row.ID), nil, nil); err != nil {
						return "", err
					}
					return "Clé « " + row.Name + " » supprimée.", nil
				})
			},
		},
		{
			Name: "check_account", Title: "Vérifier un compte", Role: store.RoleRead,
			Description: "Revérifie un compte provider ou DNS auprès du service, sans en relire le secret. Dit s'il répond.",
			Schema: object([]string{"kind", "name"}, map[string]any{
				"kind": choice("Type de compte.", "provider", "dns"),
				"name": text("Nom du compte."),
			}),
			Routes: []string{"GET /api/secrets", "POST /api/secrets/{id}/check"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Kind, Name string }](raw)
				if err != nil {
					return "", err
				}
				kind := map[string]string{"provider": "provider_token", "dns": "dns_token"}[args.Kind]
				row, err := c.account(kind, args.Name)
				if err != nil {
					return "", err
				}
				var out struct {
					OK    bool   `json:"ok"`
					Error string `json:"error"`
				}
				if err := c.send(http.MethodPost, "/api/secrets/"+url.PathEscape(row.ID)+"/check", map[string]any{}, &out); err != nil {
					return "", err
				}
				if !out.OK {
					return "", failf("Le compte « %s » ne répond pas : %s", row.Name, out.Error)
				}
				return "Le compte « " + row.Name + " » répond.", nil
			},
		},
		{
			Name: "list_app_secrets", Title: "Secrets d'application", Role: store.RoleRead,
			Description: "Les secrets partagés entre applications : nom, nombre de clés, applications qui les reçoivent. Les valeurs ne sont jamais renvoyées.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/app-secrets"},
			Run:         passThrough("/api/app-secrets"),
		},
		{
			Name: "get_app_secret", Title: "Détail d'un secret d'application", Role: store.RoleRead,
			Description: "Les noms des clés d'un secret partagé et les applications qui le reçoivent. Les valeurs ne sont pas renvoyées.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom du secret.")}),
			Routes:      []string{"GET /api/app-secrets", "GET /api/app-secrets/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				row, err := c.appSecretFrom(raw)
				if err != nil {
					return "", err
				}
				return c.callJSON(http.MethodGet, "/api/app-secrets/"+url.PathEscape(row.ID), nil)
			},
		},
		{
			Name: "create_app_secret", Title: "Créer un secret d'application", Role: store.RoleAdmin, Write: true,
			Description: "Enregistre un secret partagé (plusieurs clés) et le donne à des applications. Les valeurs sont chiffrées et ne sont pas répétées. Réservé à un jeton administrateur.",
			Schema: object([]string{"name", "entries"}, map[string]any{
				"name":    text("Nom du secret."),
				"entries": textMap("Clés et valeurs. Les valeurs ne seront plus relues."),
				"apps":    list("Applications qui reçoivent ce secret, par leur nom.", text("")),
			}),
			Routes: []string{"GET /api/applications", "POST /api/app-secrets"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[appSecretWrite](raw)
				if err != nil {
					return "", args.redactErr(err)
				}
				ids, err := c.applicationIDs(args.Apps)
				if err != nil {
					return "", args.redactErr(err)
				}
				var view struct{ ID string }
				err = c.send(http.MethodPost, "/api/app-secrets", map[string]any{"name": args.Name, "entries": args.Entries, "apps": ids}, &view)
				if err != nil {
					return "", args.redactErr(err)
				}
				return args.redact("Secret « " + args.Name + " » enregistré."), nil
			},
		},
		{
			Name: "update_app_secret", Title: "Modifier un secret d'application", Role: store.RoleAdmin, Write: true,
			Description: "Ajoute, remplace ou retire des clés d'un secret partagé, ou change les applications qui le reçoivent. Une valeur absente n'est pas effacée : elle se retire avec remove. Les valeurs ne sont pas relues. Réservé à un jeton administrateur.",
			Schema: object([]string{"name"}, map[string]any{
				"name":   text("Nom du secret."),
				"set":    textMap("Clés à poser ou à remplacer."),
				"remove": list("Clés à retirer.", text("")),
				"apps":   list("Applications qui doivent le recevoir, si la liste change. Absente : elles ne changent pas.", text("")),
			}),
			Routes: []string{"GET /api/app-secrets", "GET /api/applications", "PUT /api/app-secrets/{id}"},
			Run:    runUpdateAppSecret,
		},
		{
			Name: "delete_app_secret", Title: "Supprimer un secret d'application", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Retire un secret partagé. Deux appels : récapitulatif et code, puis suppression après l'accord. Les valeurs n'apparaissent pas. Réservé à un jeton administrateur.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom du secret."), "confirmation": confirmationArg}),
			Routes:      []string{"GET /api/app-secrets", "DELETE /api/app-secrets/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Name, Confirmation string }](raw)
				if err != nil {
					return "", err
				}
				row, err := c.appSecret(args.Name)
				if err != nil {
					return "", err
				}
				return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
					return "Suppression du secret « " + row.Name + " ».\n", row.ID, nil
				}, func() (string, error) {
					if err := c.send(http.MethodDelete, "/api/app-secrets/"+url.PathEscape(row.ID), map[string]string{"confirmedName": row.Name}, nil); err != nil {
						return "", err
					}
					return "Secret « " + row.Name + " » supprimé.", nil
				})
			},
		},
	}
}

type appSecretWrite struct {
	Name    string            `json:"name"`
	Entries map[string]string `json:"entries"`
	Apps    []string          `json:"apps"`
}

func (a appSecretWrite) values() []string {
	var out []string
	for _, value := range a.Entries {
		out = append(out, value)
	}
	return out
}

func (a appSecretWrite) redact(text string) string {
	for _, value := range a.values() {
		text = hideSecret(value, text)
	}
	return text
}

func (a appSecretWrite) redactErr(err error) error {
	for _, value := range a.values() {
		err = hideSecretErr(value, err)
	}
	return err
}

func (c *mcpCall) applicationIDs(names []string) ([]string, error) {
	if len(names) == 0 {
		return []string{}, nil
	}
	var rows []named
	if err := c.get("/api/applications", &rows); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(names))
	for _, name := range names {
		row, err := pickNamed("application", name, rows)
		if err != nil {
			return nil, err
		}
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func (c *mcpCall) sshKey(name string) (secretView, error) {
	rows, err := c.secretsOf("ssh_key")
	if err != nil {
		return secretView{}, err
	}
	return pick("clé SSH", name, rows, func(row secretView) named { return named{row.ID, row.Name} })
}

func (c *mcpCall) localImage(name string) (localImageView, error) {
	var rows []localImageView
	if err := c.get("/api/images", &rows); err != nil {
		return localImageView{}, err
	}
	return pick("image", name, rows, func(row localImageView) named {
		if row.Name == name || row.Ref == name {
			return named{row.ID, name}
		}
		return named{row.ID, row.Name}
	})
}

func (c *mcpCall) appSecrets() ([]appSecretView, error) {
	var rows []appSecretView
	err := c.get("/api/app-secrets", &rows)
	return rows, err
}

func (c *mcpCall) appSecret(name string) (appSecretView, error) {
	rows, err := c.appSecrets()
	if err != nil {
		return appSecretView{}, err
	}
	return pick("secret", name, rows, func(row appSecretView) named { return named{row.ID, row.Name} })
}

func (c *mcpCall) appSecretFrom(raw json.RawMessage) (appSecretView, error) {
	args, err := decodeArgs[struct{ Name string }](raw)
	if err != nil {
		return appSecretView{}, err
	}
	return c.appSecret(args.Name)
}

func runUpdateAppSecret(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[struct {
		Name   string            `json:"name"`
		Set    map[string]string `json:"set"`
		Remove []string          `json:"remove"`
		Apps   []string          `json:"apps"`
	}](raw)
	redact := func(text string, err error) (string, error) {
		for _, value := range args.Set {
			text = hideSecret(value, text)
			err = hideSecretErr(value, err)
		}
		return text, err
	}
	if err != nil {
		_, err = redact("", err)
		return "", err
	}
	row, err := c.appSecret(args.Name)
	if err != nil {
		return "", err
	}
	body := map[string]any{}
	if args.Set != nil {
		body["set"] = args.Set
	}
	if args.Remove != nil {
		body["remove"] = args.Remove
	}
	if args.Apps != nil {
		ids, err := c.applicationIDs(args.Apps)
		if err != nil {
			_, err = redact("", err)
			return "", err
		}
		body["apps"] = ids
	}
	if err := c.send(http.MethodPut, "/api/app-secrets/"+url.PathEscape(row.ID), body, nil); err != nil {
		_, err = redact("", err)
		return "", err
	}
	text, _ := redact("Secret « "+row.Name+" » mis à jour.", nil)
	return text, nil
}

func runSetServices(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[struct {
		Application  string          `json:"application"`
		Services     json.RawMessage `json:"services"`
		Confirmation string          `json:"confirmation"`
	}](raw)
	if err != nil {
		return "", err
	}
	app, err := c.application(args.Application)
	if err != nil {
		return "", err
	}
	var services any
	if err := json.Unmarshal(args.Services, &services); err != nil {
		return "", failf("Les services sont illisibles.")
	}
	path := "/api/applications/" + url.PathEscape(app.ID) + "/services"
	apply := func(confirm bool) (int, []byte) {
		return c.do(http.MethodPut, path, map[string]any{"services": services, "confirm": confirm})
	}
	if args.Confirmation == "" {
		status, body := apply(false)
		if status < 400 {
			return "Services de " + app.Name + " mis à jour.", nil
		}
		if lost, ok := serviceLoss(body); ok {
			return c.confirmed(raw, "", func() (string, string, error) {
				return "Changer les services de " + app.Name + " efface : " + joined(lost) + ".\n", fingerprintOf(string(args.Services)), nil
			}, func() (string, error) { return "", nil })
		}
		return "", httpFailure(body, status)
	}
	status, body := apply(false)
	if status < 400 {
		return "Services de " + app.Name + " mis à jour.", nil
	}
	if _, ok := serviceLoss(body); !ok {
		return "", httpFailure(body, status)
	}
	return c.confirmed(raw, args.Confirmation, func() (string, string, error) {
		lost, _ := serviceLoss(body)
		return "Changer les services de " + app.Name + " efface : " + joined(lost) + ".\n", fingerprintOf(string(args.Services)), nil
	}, func() (string, error) {
		status, body := apply(true)
		if status >= 400 {
			return "", httpFailure(body, status)
		}
		return "Services de " + app.Name + " mis à jour.", nil
	})
}

func serviceLoss(raw []byte) ([]string, bool) {
	var body struct {
		Code string   `json:"code"`
		Lost []string `json:"lost"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Code != "services_data_loss" {
		return nil, false
	}
	return body.Lost, true
}

func httpFailure(raw []byte, status int) error {
	var refusal struct {
		Error    string          `json:"error"`
		Problems json.RawMessage `json:"problems"`
	}
	_ = json.Unmarshal(raw, &refusal)
	if refusal.Error == "" {
		refusal.Error = "MyInfra a refusé."
	}
	return &toolError{Message: refusal.Error, Problems: refusal.Problems, Status: status}
}

func joined(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ", "
		}
		out += item
	}
	return out
}
