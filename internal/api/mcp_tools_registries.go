package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"myinfra/internal/store"
)

func mcpRegistryTools() []mcpTool {
	return []mcpTool{
		{
			Name: "list_registries", Title: "Lister les registres privés", Role: store.RoleRead,
			Description: "Les registres d'images privés enregistrés : nom, type et identifiant (identifiant@adresse). Le mot de passe ou jeton n'est jamais renvoyé, pas même en partie.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/secrets"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				rows, err := c.secretsOf(registryKind)
				if err != nil {
					return "", err
				}
				out := make([]map[string]string, 0, len(rows))
				for _, row := range rows {
					out = append(out, registryBrief(row))
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "create_registry", Title: "Enregistrer un registre privé", Role: store.RoleAdmin, Write: true,
			Description: "Enregistre l'identifiant d'un registre d'images privé, après l'avoir vérifié auprès du registre. Le mot de passe ou jeton est chiffré et ne pourra plus être relu : il faut le redonner pour le remplacer. Réservé à un jeton administrateur.",
			Schema: object([]string{"name", "registry", "username", "secret"}, map[string]any{
				"name":     text("Nom du compte, pour le reconnaître."),
				"registry": choice("Registre.", "registry-dockerhub", "registry-ghcr", "registry-gitlab", "registry-quay", "registry-custom"),
				"address":  text("Adresse, seulement pour registry-custom, par exemple registry.exemple.fr. Un préfixe https:// est accepté."),
				"username": text("Identifiant du registre."),
				"secret":   text("Mot de passe ou jeton, de préférence limité à la lecture des images. Écrit une seule fois, jamais relu."),
			}),
			Routes: []string{"POST /api/secrets/tokens"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[registryWrite](raw)
				if err != nil {
					return "", err
				}
				text, err := c.writeRegistry("", args)
				return hideSecret(args.Secret, text), hideSecretErr(args.Secret, err)
			},
		},
		{
			Name: "update_registry", Title: "Remplacer un identifiant de registre", Role: store.RoleAdmin, Write: true,
			Description: "Remplace l'identifiant ou le jeton d'un registre déjà enregistré. L'ancien jeton ne se relit pas : le nouveau est obligatoire. Sans identifiant, celui déjà enregistré est gardé. Réservé à un jeton administrateur.",
			Schema: object([]string{"name", "secret"}, map[string]any{
				"name":     text("Nom du compte de registre."),
				"username": text("Nouvel identifiant. Vide : garder l'identifiant déjà enregistré."),
				"address":  text("Nouvelle adresse, seulement pour un registre custom. Vide : garder l'adresse déjà enregistrée."),
				"secret":   text("Nouveau mot de passe ou jeton. L'ancien ne se relit pas, il faut donc le redonner même s'il ne change pas."),
			}),
			Routes: []string{"GET /api/secrets", "PUT /api/secrets/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[registryWrite](raw)
				if err != nil {
					return "", err
				}
				row, err := c.registry(args.Name)
				if err != nil {
					return "", hideSecretErr(args.Secret, err)
				}
				text, err := c.writeRegistry(row.ID, args)
				return hideSecret(args.Secret, text), hideSecretErr(args.Secret, err)
			},
		},
		{
			Name: "delete_registry", Title: "Retirer un registre privé", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Retire un registre d'images privé. Les images privées de ce registre ne pourront plus être lues tant qu'un identifiant n'est pas remis. Deux appels : récapitulatif et code, puis suppression après l'accord explicite de l'utilisateur. Le jeton n'apparaît pas. Réservé à un jeton administrateur.",
			Schema: object([]string{"name"}, map[string]any{
				"name":         text("Nom du compte de registre."),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/secrets", "DELETE /api/secrets/{id}"},
			Run:    runDeleteRegistry,
		},
	}
}

// registryWrite is a registry login as an assistant sends it. Secret is
// accepted and stored; it is never copied into an answer.
type registryWrite struct {
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Address  string `json:"address"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

func registryBrief(row secretView) map[string]string {
	return map[string]string{"name": row.Name, "registry": row.Provider, "login": row.KeyID}
}

func (c *mcpCall) registry(name string) (secretView, error) {
	rows, err := c.secretsOf(registryKind)
	if err != nil {
		return secretView{}, err
	}
	return pick("registre", name, rows, func(row secretView) named { return named{row.ID, row.Name} })
}

// writeRegistry creates or replaces a login. id empty creates it. The answer
// names the account and the login, never the secret.
func (c *mcpCall) writeRegistry(id string, args registryWrite) (string, error) {
	user := strings.TrimSpace(args.Username)
	address := strings.TrimSpace(args.Address)
	provider := args.Registry
	var row secretView
	if id != "" {
		found, err := c.registry(args.Name)
		if err != nil {
			return "", err
		}
		row = found
		provider = row.Provider
		knownUser, knownHost := registryLoginParts(row.KeyID)
		if user == "" {
			user = knownUser
		}
		if address == "" {
			address = knownHost
		}
	}
	if user == "" {
		return "", failf("Indiquez l'identifiant du registre.")
	}
	body := map[string]string{
		"name": args.Name, "kind": registryKind, "provider": provider,
		"token": args.Secret, "keyId": user, "applicationKey": address,
	}
	method, path := http.MethodPost, "/api/secrets/tokens"
	if id != "" {
		method, path = http.MethodPut, "/api/secrets/"+url.PathEscape(id)
	}
	var saved secretView
	if err := c.send(method, path, body, &saved); err != nil {
		return "", err
	}
	brief := registryBrief(saved)
	if id == "" {
		return "Registre « " + saved.Name + " » enregistré (" + brief["login"] + "). Le jeton est chiffré et ne sera pas relu.", nil
	}
	return "Registre « " + saved.Name + " » mis à jour (" + brief["login"] + "). Le jeton est chiffré et ne sera pas relu.", nil
}

func runDeleteRegistry(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[struct {
		Name         string `json:"name"`
		Confirmation string `json:"confirmation"`
	}](raw)
	if err != nil {
		return "", err
	}
	row, err := c.registry(args.Name)
	if err != nil {
		return "", c.spentCode(args.Confirmation, err)
	}
	recap := func() (string, string, error) {
		current, err := c.registry(args.Name)
		if err != nil {
			return "", "", err
		}
		login := current.KeyID
		if login == "" {
			login = current.Provider
		}
		return "Retrait du registre privé « " + current.Name + " » (" + login + "). Les images privées de ce registre ne pourront plus être lues tant qu'un identifiant n'est pas remis.\n",
			current.ID + "\n" + login, nil
	}
	return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
		if err := c.send(http.MethodDelete, "/api/secrets/"+url.PathEscape(row.ID), nil, nil); err != nil {
			return "", err
		}
		return "Registre « " + row.Name + " » retiré.", nil
	})
}

// hideSecret drops a registry token from a text an assistant will read.
func hideSecret(secret, text string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "••••")
}

func hideSecretErr(secret string, err error) error {
	secret = strings.TrimSpace(secret)
	if err == nil || secret == "" {
		return err
	}
	var fail *toolError
	if errors.As(err, &fail) {
		fail.Message = hideSecret(secret, fail.Message)
		if len(fail.Problems) > 0 {
			fail.Problems = json.RawMessage(hideSecret(secret, string(fail.Problems)))
		}
		return fail
	}
	if strings.Contains(err.Error(), secret) {
		return failf("%s", hideSecret(secret, err.Error()))
	}
	return err
}
