package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"

	"myinfra/internal/store"
)

func mcpAccountTools() []mcpTool {
	return []mcpTool{
		accountList("list_dns_accounts", "Comptes DNS", "dns_token",
			"Les comptes DNS enregistrés : nom, provider, et l'identifiant public s'il y en a un. Le secret n'est jamais renvoyé. Un compte s'ajoute avec create_dns_account."),
		accountList("list_git_accounts", "Comptes Git", "git_token",
			"Les comptes Git enregistrés (GitHub, GitLab) : nom et provider. Le jeton n'est jamais renvoyé. Un compte s'ajoute avec create_git_account."),
		accountWriteTool("create_provider_account", "Enregistrer un compte provider", "provider_token", providerChoice,
			"Enregistre un compte de fournisseur cloud, après l'avoir vérifié. Le secret est chiffré et ne pourra plus être relu. Champs selon le provider : secret pour Hetzner, DigitalOcean, Vultr, Akamai, UpCloud, Gcore et IONOS ; access_key, secret et project pour Scaleway ; key et key_secret pour Infomaniak, Exoscale et Kamatera ; key, key_secret et consumer_key pour OVH ; key (adresse) et secret (jeton) pour Proxmox ; secret (adresse Keystone), key, key_secret et project pour OpenStack. Réservé à un jeton administrateur."),
		accountWriteTool("update_provider_account", "Remplacer un compte provider", "provider_token", nil,
			"Remplace les clés d'un compte provider déjà enregistré. Le type ne change pas. L'ancien secret ne se relit pas : il faut redonner toutes les clés, même celles qui ne changent pas. Réservé à un jeton administrateur."),
		accountDeleteTool("delete_provider_account", "Retirer un compte provider", "provider_token",
			"Retire un compte provider. Refusé tant qu'une machine achetée avec lui existe. Deux appels : récapitulatif et code, puis suppression après l'accord explicite de l'utilisateur. Le secret n'apparaît pas. Réservé à un jeton administrateur."),
		accountWriteTool("create_dns_account", "Enregistrer un compte DNS", "dns_token", dnsChoice,
			"Enregistre un compte DNS, après l'avoir vérifié. Cloudflare : secret est le jeton. OVH : key, key_secret et consumer_key. Le secret est chiffré et ne pourra plus être relu. Réservé à un jeton administrateur."),
		accountWriteTool("update_dns_account", "Remplacer un compte DNS", "dns_token", nil,
			"Remplace les clés d'un compte DNS déjà enregistré. Le type ne change pas. L'ancien secret ne se relit pas : il faut redonner toutes les clés. Réservé à un jeton administrateur."),
		accountDeleteTool("delete_dns_account", "Retirer un compte DNS", "dns_token",
			"Retire un compte DNS. MyInfra ne pourra plus écrire les noms de ce compte. Deux appels : récapitulatif et code, puis suppression après l'accord explicite de l'utilisateur. Le secret n'apparaît pas. Réservé à un jeton administrateur."),
		accountWriteTool("create_git_account", "Enregistrer un compte Git", "git_token", gitChoice,
			"Enregistre un compte GitHub ou GitLab, après l'avoir vérifié. secret est le jeton. Il est chiffré et ne pourra plus être relu. Réservé à un jeton administrateur."),
		accountWriteTool("update_git_account", "Remplacer un compte Git", "git_token", nil,
			"Remplace le jeton d'un compte Git déjà enregistré. L'ancien ne se relit pas. Réservé à un jeton administrateur."),
		accountDeleteTool("delete_git_account", "Retirer un compte Git", "git_token",
			"Retire un compte Git. Les dépôts privés de ce compte ne pourront plus être lus. Deux appels : récapitulatif et code, puis suppression après l'accord explicite de l'utilisateur. Le jeton n'apparaît pas. Réservé à un jeton administrateur."),
	}
}

var (
	providerChoice = choice("Fournisseur.", "hetzner", "scaleway", "ovh", "digitalocean", "infomaniak", "upcloud", "akamai", "vultr", "exoscale", "gcore", "kamatera", "ionos", "openstack", "proxmox")
	dnsChoice      = choice("Compte DNS.", "cloudflare", "ovh")
	gitChoice      = choice("Forge.", "github", "gitlab")
)

func accountList(name, title, kind, description string) mcpTool {
	return mcpTool{
		Name: name, Title: title, Role: store.RoleRead,
		Description: description,
		Schema:      object(nil, map[string]any{}),
		Routes:      []string{"GET /api/secrets"},
		Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
			return c.listAccounts(kind)
		},
	}
}

func accountWriteTool(name, title, kind string, provider map[string]any, description string) mcpTool {
	props := accountProps()
	required := []string{"name"}
	routes := []string{"GET /api/secrets", "PUT /api/secrets/{id}"}
	if provider != nil {
		props["provider"] = provider
		required = []string{"name", "provider"}
		routes = []string{"POST /api/secrets/tokens"}
	}
	return mcpTool{
		Name: name, Title: title, Role: store.RoleAdmin, Write: true,
		Description: description,
		Schema:      object(required, props),
		Routes:      routes,
		Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
			args, err := decodeArgs[accountWrite](raw)
			if err != nil {
				return "", args.redactErr(err)
			}
			id := ""
			if provider == nil {
				row, err := c.account(kind, args.Name)
				if err != nil {
					return "", args.redactErr(err)
				}
				id = row.ID
				args.Provider = row.Provider
			}
			text, err := c.writeAccount(kind, id, args)
			return args.redact(text), args.redactErr(err)
		},
	}
}

func accountDeleteTool(name, title, kind, description string) mcpTool {
	routes := []string{"GET /api/secrets", "DELETE /api/secrets/{id}"}
	if kind == "provider_token" {
		routes = []string{"GET /api/secrets", "GET /api/servers", "DELETE /api/secrets/{id}"}
	}
	return mcpTool{
		Name: name, Title: title, Role: store.RoleAdmin, Write: true, Destructive: true,
		Description: description,
		Schema: object([]string{"name"}, map[string]any{
			"name":         text("Nom du compte."),
			"confirmation": confirmationArg,
		}),
		Routes: routes,
		Run:    runDeleteAccount(kind),
	}
}

func accountProps() map[string]any {
	return map[string]any{
		"name":         text("Nom du compte, pour le reconnaître."),
		"secret":       text("Jeton ou clé secrète. Pour OpenStack, l'adresse Keystone. Pour Proxmox, le jeton API. Écrit une seule fois, jamais relu."),
		"access_key":   text("Scaleway : l'access key. Elle commence par SCW."),
		"project":      text("Scaleway : le projet. OpenStack : le projet, s'il y en a un."),
		"key":          text("OVH : application key. Infomaniak : identifiant. Exoscale : API key. Kamatera : client id. OpenStack : identifiant. Proxmox : adresse, par exemple https://pve.exemple:8006."),
		"key_secret":   text("OVH : application secret. Infomaniak, Exoscale et Kamatera : secret. OpenStack : secret. Jamais relu."),
		"consumer_key": text("OVH : consumer key. Jamais relue."),
	}
}

// accountWrite is a provider, DNS or Git login as an assistant sends it.
// The secret fields are stored and never copied into an answer.
type accountWrite struct {
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Secret      string `json:"secret"`
	AccessKey   string `json:"access_key"`
	Project     string `json:"project"`
	Key         string `json:"key"`
	KeySecret   string `json:"key_secret"`
	ConsumerKey string `json:"consumer_key"`
}

func (a accountWrite) secrets() []string {
	out := []string{a.Secret, a.KeySecret, a.ConsumerKey}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

func (a accountWrite) redact(text string) string {
	for _, secret := range a.secrets() {
		text = hideSecret(secret, text)
	}
	return text
}

func (a accountWrite) redactErr(err error) error {
	for _, secret := range a.secrets() {
		err = hideSecretErr(secret, err)
	}
	return err
}

func accountBrief(row secretView) map[string]string {
	out := map[string]string{"name": row.Name, "provider": row.Provider}
	if row.KeyID != "" {
		out["login"] = row.KeyID
	}
	return out
}

func (c *mcpCall) listAccounts(kind string) (string, error) {
	rows, err := c.secretsOf(kind)
	if err != nil {
		return "", err
	}
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, accountBrief(row))
	}
	return asJSON(out), nil
}

func (c *mcpCall) account(kind, name string) (secretView, error) {
	rows, err := c.secretsOf(kind)
	if err != nil {
		return secretView{}, err
	}
	what := map[string]string{"provider_token": "compte provider", "dns_token": "compte DNS", "git_token": "compte Git"}[kind]
	return pick(what, name, rows, func(row secretView) named { return named{row.ID, row.Name} })
}

func (c *mcpCall) writeAccount(kind, id string, args accountWrite) (string, error) {
	body := map[string]string{
		"name": args.Name, "kind": kind, "provider": args.Provider,
		"token": args.Secret, "keyId": args.AccessKey, "projectId": args.Project,
		"applicationKey": args.Key, "applicationSecret": args.KeySecret, "consumerKey": args.ConsumerKey,
	}
	method, path := http.MethodPost, "/api/secrets/tokens"
	if id != "" {
		method, path = http.MethodPut, "/api/secrets/"+url.PathEscape(id)
	}
	var saved secretView
	if err := c.send(method, path, body, &saved); err != nil {
		return "", err
	}
	where := saved.Provider
	if saved.KeyID != "" {
		where += ", " + saved.KeyID
	}
	verb := "enregistré"
	if id != "" {
		verb = "mis à jour"
	}
	return "Compte « " + saved.Name + " » " + verb + " (" + where + "). Le secret est chiffré et ne sera pas relu.", nil
}

func runDeleteAccount(kind string) func(*mcpCall, json.RawMessage) (string, error) {
	return func(c *mcpCall, raw json.RawMessage) (string, error) {
		args, err := decodeArgs[struct {
			Name         string `json:"name"`
			Confirmation string `json:"confirmation"`
		}](raw)
		if err != nil {
			return "", err
		}
		row, err := c.account(kind, args.Name)
		if err != nil {
			return "", c.spentCode(args.Confirmation, err)
		}
		recap := func() (string, string, error) {
			current, err := c.account(kind, args.Name)
			if err != nil {
				return "", "", err
			}
			using := 0
			if kind == "provider_token" {
				using, err = c.providerServers(current.ID)
				if err != nil {
					return "", "", err
				}
				if using > 0 {
					return "", "", failf("Ce provider a encore des serveurs. Supprimez-les avant.")
				}
			}
			return accountDeleteRecap(kind, current), current.ID + "\n" + current.Provider, nil
		}
		return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
			if err := c.send(http.MethodDelete, "/api/secrets/"+url.PathEscape(row.ID), nil, nil); err != nil {
				return "", err
			}
			return "Compte « " + row.Name + " » retiré.", nil
		})
	}
}

func accountDeleteRecap(kind string, row secretView) string {
	where := row.Provider
	if row.KeyID != "" {
		where += ", " + row.KeyID
	}
	switch kind {
	case "dns_token":
		return "Retrait du compte DNS « " + row.Name + " » (" + where + "). MyInfra ne pourra plus écrire les noms de ce compte.\n"
	case "git_token":
		return "Retrait du compte Git « " + row.Name + " » (" + where + "). Les dépôts privés de ce compte ne pourront plus être lus.\n"
	default:
		return "Retrait du compte provider « " + row.Name + " » (" + where + "). MyInfra ne pourra plus acheter de machines avec lui.\n"
	}
}

func (c *mcpCall) providerServers(id string) (int, error) {
	rows, err := c.serversList()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		if row.ProviderID == id {
			n++
		}
	}
	return n, nil
}
