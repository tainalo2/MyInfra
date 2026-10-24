package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"myinfra/internal/store"
)

// destination is where an application goes: a cluster or a machine alone.
type destination struct {
	kind, id, name string
}

func (c *mcpCall) destination(cluster, server string) (destination, error) {
	switch {
	case cluster != "" && server != "":
		return destination{}, failf("Choisissez un cluster ou une machine, pas les deux.")
	case cluster != "":
		row, err := c.cluster(cluster)
		return destination{"cluster", row.ID, row.Name}, err
	case server != "":
		row, err := c.server(server)
		if err == nil && row.ClusterID != "" {
			return destination{}, failf("La machine %s appartient au cluster %s : installez sur le cluster.", row.Name, row.ClusterName)
		}
		return destination{"server", row.ID, row.Name}, err
	}
	return destination{}, failf("Précisez le cluster ou la machine où installer.")
}

func (d destination) body(body map[string]any) map[string]any {
	body["destinationKind"] = d.kind
	if d.kind == "cluster" {
		body["clusterId"] = d.id
	} else {
		body["serverId"] = d.id
	}
	return body
}

// updateFromReleases moves the services that have a newer version to it, as
// the releases of the application suggest; with none, the refusal stands.
func (c *mcpCall) updateFromReleases(app named, base string, refusal error) (string, error) {
	var releases struct {
		Services []struct{ Name, Image, Update string }
	}
	if err := c.get(base+"/releases", &releases); err != nil {
		return "", refusal
	}
	type image struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}
	body := make([]image, 0, len(releases.Services))
	var moved []string
	for _, svc := range releases.Services {
		next := svc.Image
		if svc.Update != "" && svc.Update != svc.Image {
			next = svc.Update
			moved = append(moved, svc.Name+" : "+svc.Image+" → "+svc.Update)
		}
		body = append(body, image{svc.Name, next})
	}
	if len(moved) == 0 {
		return "", refusal
	}
	var out any
	if err := c.send(http.MethodPost, base+"/images", map[string]any{"services": body}, &out); err != nil {
		return "", err
	}
	return "Mise à jour de " + app.Name + " lancée.\n - " + strings.Join(moved, "\n - ") + "\n" + asJSON(out), nil
}

// domainNames splits full names into subdomain and zone, by the zones of the
// DNS accounts.
func (c *mcpCall) domainNames(domains []string) ([]nameBody, error) {
	var out []nameBody
	for _, domain := range domains {
		domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
		zone, err := c.zoneOf(domain)
		if err != nil {
			return nil, err
		}
		if domain == zone {
			return nil, failf("Une application a un sous-domaine (cloud.%s), pas la racine %s.", zone, zone)
		}
		out = append(out, nameBody{Subdomain: strings.TrimSuffix(domain, "."+zone), Zone: zone})
	}
	return out, nil
}

// dnsConflicts lists, for the names an application is about to take, the
// records that are there and that MyInfra did not write: taking the name
// replaces them.
func (c *mcpCall) dnsConflicts(names []nameBody, own map[string]bool) ([]string, error) {
	var apps []appView
	if err := c.get("/api/applications", &apps); err != nil {
		return nil, err
	}
	taken := map[string]string{}
	for _, app := range apps {
		taken[strings.ToLower(app.Hostname)] = app.Name
		for _, d := range app.Domains {
			taken[strings.ToLower(d.Hostname)] = app.Name
		}
	}
	var out []string
	for _, n := range names {
		host := n.Subdomain + "." + n.Zone
		if own[host] {
			continue
		}
		if app, ok := taken[host]; ok {
			return nil, failf("%s sert déjà à l'application %s.", host, app)
		}
		var rows []map[string]any
		if err := c.get("/api/dns/zones/"+url.PathEscape(n.Zone)+"/records?name="+url.QueryEscape(host), &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			kind := fmt.Sprint(row["type"])
			if kind != "A" && kind != "AAAA" && kind != "CNAME" {
				continue
			}
			if used, ok := row["usedBy"]; ok {
				return nil, failf("%s sert déjà à l'application %v.", host, used)
			}
			out = append(out, fmt.Sprintf("%s %s %v", host, kind, row["content"]))
		}
	}
	return out, nil
}

type redundancyView struct {
	Plan         redundancy `json:"plan"`
	Nodes        int        `json:"nodes"`
	StorageNodes int        `json:"storageNodes"`
}

// redundancyReport tells, service by service, what copies really give.
func redundancyReport(v redundancyView, copies int) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "Redondance demandée : %d copies ; le cluster a %d nœud(s) lié(s).\n", copies, v.Nodes)
	line := func(label string, names []string) {
		if len(names) > 0 {
			b.WriteString(" - " + label + " : " + strings.Join(names, ", ") + "\n")
		}
	}
	line(fmt.Sprintf("répliqués en %d copies", copies), v.Plan.Scaled)
	line("une copie qui redémarre ailleurs si son nœud tombe, fichiers répliqués", v.Plan.Failover)
	line("base déplacée vers un PostgreSQL répliqué, puis copies", v.Plan.Database)
	line("RESTENT À UNE SEULE COPIE (fichiers sur disque que MyInfra ne sait pas répliquer)", v.Plan.Alone)
	short := len(v.Plan.Alone) > 0
	if short {
		b.WriteString("L'application ne sera donc pas entièrement redondante : la perte du nœud qui porte ces services les arrête jusqu'à son retour.\n")
	}
	if (len(v.Plan.Failover)+len(v.Plan.Database) > 0) && v.Nodes < v.StorageNodes {
		fmt.Fprintf(&b, "La bascule et la base répliquée demandent %d nœuds au moins.\n", v.StorageNodes)
	}
	return b.String(), short
}

func randomSecret() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// databaseFor gives the service and the variables of the database asked for
// an application built from Git; the password is drawn here, never by the
// assistant.
func databaseFor(kind, app string) (serviceBody, map[string]string) {
	password := randomSecret()
	db := strings.ReplaceAll(slug(app), "-", "_")
	if db == "" {
		db = "app"
	}
	switch kind {
	case "mariadb":
		return serviceBody{Name: "db", Image: "mariadb:11", Disk: true, DiskPath: "/var/lib/mysql", DiskGi: 10, Replicas: 1},
			map[string]string{
				"MARIADB_ROOT_PASSWORD": randomSecret(), "MARIADB_DATABASE": db, "MARIADB_USER": db, "MARIADB_PASSWORD": password,
				"DB_HOST": "db", "DB_PORT": "3306", "DB_NAME": db, "DB_USER": db, "DB_PASSWORD": password,
				"DATABASE_URL": "mysql://" + db + ":" + password + "@db:3306/" + db,
			}
	case "postgres":
		return serviceBody{Name: "db", Image: "postgres:16", Disk: true, DiskPath: "/var/lib/postgresql/data", DiskGi: 10, Replicas: 1},
			map[string]string{
				"POSTGRES_DB": db, "POSTGRES_USER": db, "POSTGRES_PASSWORD": password,
				"DB_HOST": "db", "DB_PORT": "5432", "DB_NAME": db, "DB_USER": db, "DB_PASSWORD": password,
				"DATABASE_URL": "postgres://" + db + ":" + password + "@db:5432/" + db,
			}
	}
	return serviceBody{}, nil
}

func mcpAppTools() []mcpTool {
	appArg := text("Nom de l'application.")
	return []mcpTool{
		{
			Name: "install", Title: "Installer une application", Role: store.RoleDeploy, Write: true,
			Description: "Installe un article du catalogue ou un paquet enregistré sur un cluster ou une machine, avec ses domaines, les réponses à ses questions et un nombre de copies. Avec copies > 1, le résultat dit service par service ce qui est vraiment redondé ; si une partie reste à une copie, ou si un domaine porte déjà des enregistrements DNS étrangers à MyInfra, le premier appel rend un récapitulatif et un code à renvoyer après l'accord de l'utilisateur.",
			Schema: object(nil, map[string]any{
				"article":      text("Article du catalogue (search_catalogue), par exemple nextcloud."),
				"package":      text("Paquet enregistré (list_packages)."),
				"name":         text("Nom de l'application ; celui de l'article ou du paquet par défaut."),
				"cluster":      text("Cluster où installer."),
				"server":       text("Machine seule où installer."),
				"domains":      list("Noms complets de l'application, par exemple cloud.moydomain.name ; le premier est l'adresse principale.", text("")),
				"inputs":       textMap("Réponses aux questions de l'article ou du paquet, par clé."),
				"copies":       whole("Copies voulues, sur un cluster (1 par défaut).", 1, 10),
				"version":      text("Version (étiquette d'image) du service public, sinon celle de l'article."),
				"confirmation": confirmationArg,
			}),
			Routes: []string{
				"GET /api/catalogue", "GET /api/packages", "GET /api/clusters", "GET /api/servers", "GET /api/dns/zones",
				"GET /api/applications", "GET /api/dns/zones/{zone}/records", "POST /api/redundancy/preview", "POST /api/catalogue/{id}", "POST /api/packages/{id}/install",
			},
			Run: runInstall,
		},
		{
			Name: "deploy_git_app", Title: "Déployer depuis un dépôt Git", Role: store.RoleDeploy, Write: true,
			Description: "Construit l'image d'un dépôt Git (son Dockerfile) sur la destination et la déploie, exposée sur son port, avec au besoin une base MariaDB ou PostgreSQL à côté (mot de passe tiré par MyInfra). Les variables de connexion posées sont rendues : adaptez-les au besoin avec set_variables. save_application_as_package en fait ensuite un paquet. La construction peut prendre plusieurs minutes.",
			Schema: object([]string{"name", "repository"}, map[string]any{
				"name":         text("Nom de l'application."),
				"repository":   text("Dépôt : https://github.com/org/repo, avec @branche et :chemin/Dockerfile au besoin (https://github.com/org/repo@main:docker/Dockerfile)."),
				"port":         whole("Port où l'application répond dans son conteneur.", 1, 65535),
				"database":     choice("Base à mettre à côté.", "none", "mariadb", "postgres"),
				"cluster":      text("Cluster où déployer."),
				"server":       text("Machine seule où déployer."),
				"domains":      list("Noms complets de l'application.", text("")),
				"variables":    textMap("Variables d'environnement en plus."),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/clusters", "GET /api/servers", "GET /api/applications", "GET /api/dns/zones", "GET /api/dns/zones/{zone}/records", "POST /api/applications"},
			Run:    runGitApp,
		},
		{
			Name: "save_application_as_package", Title: "Garder une application comme paquet", Role: store.RoleDeploy, Write: true,
			Description: "Enregistre les services d'une application qui tourne comme paquet, pour l'installer ailleurs.",
			Schema:      object([]string{"application"}, map[string]any{"application": appArg}),
			Routes:      []string{"GET /api/applications", "POST /api/applications/{id}/package"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application string }](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				var view packageView
				if err := c.send(http.MethodPost, "/api/applications/"+url.PathEscape(app.ID)+"/package", map[string]any{}, &view); err != nil {
					return "", err
				}
				return "Paquet « " + view.Name + " » enregistré depuis " + app.Name + ".", nil
			},
		},
		{
			Name: "list_applications", Title: "Lister les applications", Role: store.RoleRead,
			Description: "Les applications : destination, adresse, état.",
			Schema:      object(nil, map[string]any{}),
			Routes:      []string{"GET /api/applications"},
			Run: func(c *mcpCall, _ json.RawMessage) (string, error) {
				var rows []appView
				if err := c.get("/api/applications", &rows); err != nil {
					return "", err
				}
				out := []map[string]any{}
				for _, row := range rows {
					out = append(out, appBrief(row))
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "get_application", Title: "Détail d'une application", Role: store.RoleRead,
			Description: "Une application en détail : services, domaines et leur état DNS, variables (secrets masqués), pods et leur état, versions déployées. Le point de départ pour comprendre pourquoi elle ne démarre pas.",
			Schema:      object([]string{"application"}, map[string]any{"application": appArg}),
			Routes:      []string{"GET /api/applications", "GET /api/applications/{id}", "GET /api/applications/{id}/runtime", "GET /api/applications/{id}/releases"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application string }](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				base := "/api/applications/" + url.PathEscape(app.ID)
				var view appView
				if err := c.get(base, &view); err != nil {
					return "", err
				}
				view.Logo = ""
				out := map[string]any{"application": view}
				var runtime, releases any
				if err := c.get(base+"/runtime", &runtime); err == nil {
					out["runtime"] = runtime
				} else {
					out["runtime"] = "illisible : " + err.Error()
				}
				if err := c.get(base+"/releases", &releases); err == nil {
					out["releases"] = releases
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "preview_redundancy", Title: "Ce que des copies donneraient", Role: store.RoleRead,
			Description: "Dit, service par service, ce que des copies donneraient à une application installée, ou à un article ou un paquet avant de l'installer sur un cluster : répliqué, bascule, base répliquée, ou reste à une copie.",
			Schema: object(nil, map[string]any{
				"application": text("Application installée."),
				"article":     text("Article du catalogue."),
				"package":     text("Paquet enregistré."),
				"cluster":     text("Cluster visé, pour un article ou un paquet."),
				"copies":      whole("Copies envisagées (2 par défaut).", 2, 10),
			}),
			Routes: []string{"GET /api/applications", "GET /api/catalogue", "GET /api/packages", "GET /api/clusters", "GET /api/applications/{id}/redundancy", "POST /api/redundancy/preview"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Application, Article, Package, Cluster string
					Copies                                 int
				}](raw)
				if err != nil {
					return "", err
				}
				copies := max(args.Copies, 2)
				var view redundancyView
				if args.Application != "" {
					app, err := c.application(args.Application)
					if err != nil {
						return "", err
					}
					if err := c.get("/api/applications/"+url.PathEscape(app.ID)+"/redundancy", &view); err != nil {
						return "", err
					}
				} else {
					body, err := c.previewBody(args.Article, args.Package, args.Cluster)
					if err != nil {
						return "", err
					}
					if err := c.send(http.MethodPost, "/api/redundancy/preview", body, &view); err != nil {
						return "", err
					}
				}
				text, _ := redundancyReport(view, copies)
				return text, nil
			},
		},
		{
			Name: "set_redundancy", Title: "Changer le nombre de copies", Role: store.RoleDeploy, Write: true,
			Description: "Change le nombre de copies d'une application sur un cluster. Le résultat dit ce qui est vraiment redondé.",
			Schema:      object([]string{"application", "copies"}, map[string]any{"application": appArg, "copies": whole("Copies voulues.", 1, 10)}),
			Routes:      []string{"GET /api/applications", "GET /api/applications/{id}/redundancy", "POST /api/applications/{id}/redundancy"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Application string
					Copies      int
				}](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				base := "/api/applications/" + url.PathEscape(app.ID) + "/redundancy"
				if err := c.send(http.MethodPost, base, map[string]int{"copies": args.Copies}, nil); err != nil {
					return "", err
				}
				var view redundancyView
				_ = c.get(base, &view)
				text, _ := redundancyReport(view, args.Copies)
				return "Copies de " + app.Name + " réglées à " + fmt.Sprint(args.Copies) + ".\n" + text, nil
			},
		},
		{
			Name: "set_domains", Title: "Changer les domaines d'une application", Role: store.RoleDeploy, Write: true,
			Description: "Donne à une application exactement ces noms de domaine (la liste remplace l'actuelle ; vide, il ne reste que l'adresse par défaut). MyInfra écrit le DNS et le certificat. Si un nouveau nom porte déjà des enregistrements étrangers à MyInfra, le premier appel rend le récapitulatif et un code.",
			Schema: object([]string{"application", "domains"}, map[string]any{
				"application":  appArg,
				"domains":      list("Noms complets voulus.", text("")),
				"confirmation": confirmationArg,
			}),
			Routes: []string{"GET /api/applications", "GET /api/applications/{id}", "GET /api/dns/zones", "GET /api/dns/zones/{zone}/records", "PUT /api/applications/{id}/names"},
			Run:    runSetDomains,
		},
		{
			Name: "set_variables", Title: "Changer des variables", Role: store.RoleDeploy, Write: true,
			Description: "Change ou ajoute des variables d'environnement d'une application (communes à ses services) ; une valeur vide retire la variable. Les secrets sont chiffrés. L'application redémarre avec elles.",
			Schema: object([]string{"application", "variables"}, map[string]any{
				"application": appArg,
				"variables":   textMap("Variables à poser, par nom."),
			}),
			Routes: []string{"GET /api/applications", "GET /api/applications/{id}", "PUT /api/applications/{id}/variables"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Application string
					Variables   map[string]string
				}](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				var view appView
				if err := c.get("/api/applications/"+url.PathEscape(app.ID), &view); err != nil {
					return "", err
				}
				// Masked values are sent back as they are: the API keeps the
				// value it holds for them.
				next := map[string]string{}
				for k, v := range view.Variables {
					next[k] = v
				}
				for k, v := range args.Variables {
					if v == "" {
						delete(next, k)
					} else {
						next[k] = v
					}
				}
				if err := c.send(http.MethodPut, "/api/applications/"+url.PathEscape(app.ID)+"/variables", map[string]any{"variables": next}, nil); err != nil {
					return "", err
				}
				return "Variables de " + app.Name + " mises à jour ; l'application redémarre.", nil
			},
		},
		{
			Name: "pause_application", Title: "Mettre en pause ou reprendre", Role: store.RoleDeploy, Write: true,
			Description: "Arrête une application sans rien effacer, ou la reprend.",
			Schema:      object([]string{"application", "paused"}, map[string]any{"application": appArg, "paused": flag("true pour arrêter, false pour reprendre.")}),
			Routes:      []string{"GET /api/applications", "POST /api/applications/{id}/pause"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct {
					Application string
					Paused      bool
				}](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/applications/"+url.PathEscape(app.ID)+"/pause", map[string]bool{"paused": args.Paused}, nil); err != nil {
					return "", err
				}
				if args.Paused {
					return app.Name + " est en pause.", nil
				}
				return app.Name + " reprend.", nil
			},
		},
		{
			Name: "get_update_impact", Title: "Impact d'une mise à jour", Role: store.RoleRead,
			Description: "Les mises à jour disponibles, et pour une application ce que sa mise à jour changerait (coupure, versions).",
			Schema:      object(nil, map[string]any{"application": text("Application ; sans elle, toutes les mises à jour disponibles.")}),
			Routes:      []string{"GET /api/updates", "GET /api/applications", "GET /api/applications/{id}/update/impact"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application string }](raw)
				if err != nil {
					return "", err
				}
				if args.Application == "" {
					return passThrough("/api/updates")(c, raw)
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				return passThrough("/api/applications/"+url.PathEscape(app.ID)+"/update/impact")(c, raw)
			},
		},
		{
			Name: "update_application", Title: "Mettre à jour une application", Role: store.RoleDeploy, Write: true,
			Description: "Applique la mise à jour disponible d'une application (get_update_impact la décrit). Une version qui ne démarre pas laisse l'ancienne en place.",
			Schema:      object([]string{"application"}, map[string]any{"application": appArg}),
			Routes:      []string{"GET /api/applications", "POST /api/applications/{id}/update", "GET /api/applications/{id}/releases", "POST /api/applications/{id}/images"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application string }](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				base := "/api/applications/" + url.PathEscape(app.ID)
				var out any
				err = c.send(http.MethodPost, base+"/update", map[string]any{}, &out)
				var refused *toolError
				if errors.As(err, &refused) && errorCodes[refused.Message] == "update_none" {
					// The periodic search has not run since the install: the
					// versions read now say what get_application shows.
					return c.updateFromReleases(app, base, err)
				}
				if err != nil {
					return "", err
				}
				return "Mise à jour de " + app.Name + " lancée.\n" + asJSON(out), nil
			},
		},
		{
			Name: "get_pod_logs", Title: "Journaux d'un pod", Role: store.RoleRead,
			Description: "Les dernières lignes du journal d'un pod ou d'un conteneur d'une application (get_application liste ses pods).",
			Schema:      object([]string{"application", "pod"}, map[string]any{"application": appArg, "pod": text("Nom du pod ou du conteneur.")}),
			Routes:      []string{"GET /api/applications", "GET /api/applications/{id}/pods/{pod}/logs"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application, Pod string }](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				var out struct {
					Text string `json:"text"`
				}
				if err := c.get("/api/applications/"+url.PathEscape(app.ID)+"/pods/"+url.PathEscape(args.Pod)+"/logs", &out); err != nil {
					return "", err
				}
				lines := strings.Split(out.Text, "\n")
				if len(lines) > 200 {
					lines = lines[len(lines)-200:]
				}
				return strings.Join(lines, "\n"), nil
			},
		},
		{
			Name: "restart_pod", Title: "Redémarrer un pod", Role: store.RoleDeploy, Write: true,
			Description: "Redémarre un pod ou un conteneur d'une application, sans toucher à ses données.",
			Schema:      object([]string{"application", "pod"}, map[string]any{"application": appArg, "pod": text("Nom du pod ou du conteneur.")}),
			Routes:      []string{"GET /api/applications", "POST /api/applications/{id}/pods/{pod}/reset"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Application, Pod string }](raw)
				if err != nil {
					return "", err
				}
				app, err := c.application(args.Application)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/applications/"+url.PathEscape(app.ID)+"/pods/"+url.PathEscape(args.Pod)+"/reset", map[string]any{}, nil); err != nil {
					return "", err
				}
				return args.Pod + " redémarre.", nil
			},
		},
	}
}

func (c *mcpCall) previewBody(article, pkg, cluster string) (map[string]any, error) {
	body := map[string]any{}
	switch {
	case article != "":
		id, err := c.article(article)
		if err != nil {
			return nil, err
		}
		body["article"] = id
	case pkg != "":
		row, err := c.savedPackage(pkg)
		if err != nil {
			return nil, err
		}
		body["package"] = row.ID
	default:
		return nil, failf("Précisez une application, un article ou un paquet.")
	}
	if cluster != "" {
		row, err := c.cluster(cluster)
		if err != nil {
			return nil, err
		}
		body["clusterId"] = row.ID
	}
	return body, nil
}

type installArgs struct {
	Article      string            `json:"article"`
	Package      string            `json:"package"`
	Name         string            `json:"name"`
	Cluster      string            `json:"cluster"`
	Server       string            `json:"server"`
	Domains      []string          `json:"domains"`
	Inputs       map[string]string `json:"inputs"`
	Copies       int               `json:"copies"`
	Version      string            `json:"version"`
	Confirmation string            `json:"confirmation"`
}

func runInstall(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[installArgs](raw)
	if err != nil {
		return "", err
	}
	if (args.Article == "") == (args.Package == "") {
		return "", failf("Précisez un article du catalogue ou un paquet enregistré (l'un des deux).")
	}
	path, label := "", ""
	if args.Article != "" {
		id, err := c.article(args.Article)
		if err != nil {
			return "", err
		}
		path, label = "/api/catalogue/"+url.PathEscape(id), id
	} else {
		row, err := c.savedPackage(args.Package)
		if err != nil {
			return "", err
		}
		path, label = "/api/packages/"+url.PathEscape(row.ID)+"/install", row.Name
	}
	dest, err := c.destination(args.Cluster, args.Server)
	if err != nil {
		return "", err
	}
	copies := max(args.Copies, 1)
	if copies > 1 && dest.kind != "cluster" {
		return "", failf("Des copies se répartissent sur un cluster : une machine seule n'en porte qu'une.")
	}
	names, err := c.domainNames(args.Domains)
	if err != nil {
		return "", err
	}
	name := firstNonEmpty(args.Name, label)
	body := dest.body(map[string]any{"name": name, "names": names, "inputs": args.Inputs, "copies": copies, "tag": args.Version})
	plan := func() (string, []string, bool, error) {
		conflicts, err := c.dnsConflicts(names, nil)
		if err != nil {
			return "", nil, false, err
		}
		report, short := "", false
		if copies > 1 {
			preview, err := c.previewBody(args.Article, args.Package, dest.name)
			if err != nil {
				return "", nil, false, err
			}
			var view redundancyView
			if err := c.send(http.MethodPost, "/api/redundancy/preview", preview, &view); err != nil {
				return "", nil, false, err
			}
			report, short = redundancyReport(view, copies)
		}
		return report, conflicts, short, nil
	}
	report, conflicts, short, err := plan()
	if err != nil {
		return "", err
	}
	act := func() (string, error) {
		var app appView
		if err := c.send(http.MethodPost, path, body, &app); err != nil {
			return "", err
		}
		out := fmt.Sprintf("%s installée sur %s %s, à l'adresse https://%s. Le déploiement se poursuit : suivez-le avec get_application.\n", app.Name, map[string]string{"cluster": "le cluster", "server": "la machine"}[dest.kind], dest.name, app.Hostname)
		return out + report, nil
	}
	if len(conflicts) == 0 && !short {
		return act()
	}
	recap := func() (string, string, error) {
		report, conflicts, _, err := plan()
		if err != nil {
			return "", "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Installation de %s (%s) sur %s %s\n", name, label, dest.kind, dest.name)
		if len(args.Domains) > 0 {
			b.WriteString("Domaines : " + strings.Join(args.Domains, ", ") + "\n")
		}
		b.WriteString(report)
		if len(conflicts) > 0 {
			b.WriteString("\nCes enregistrements DNS, que MyInfra n'a pas écrits, seront remplacés par les adresses de l'application :\n")
			for _, line := range conflicts {
				b.WriteString("  " + line + "\n")
			}
		}
		return b.String(), fingerprintOf(conflicts), nil
	}
	return c.confirmed(raw, args.Confirmation, recap, act)
}

type gitArgs struct {
	Name         string            `json:"name"`
	Repository   string            `json:"repository"`
	Port         int               `json:"port"`
	Database     string            `json:"database"`
	Cluster      string            `json:"cluster"`
	Server       string            `json:"server"`
	Domains      []string          `json:"domains"`
	Variables    map[string]string `json:"variables"`
	Confirmation string            `json:"confirmation"`
}

func runGitApp(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[gitArgs](raw)
	if err != nil {
		return "", err
	}
	dest, err := c.destination(args.Cluster, args.Server)
	if err != nil {
		return "", err
	}
	names, err := c.domainNames(args.Domains)
	if err != nil {
		return "", err
	}
	service := slug(args.Name)
	if service == "" {
		service = "app"
	}
	services := []serviceBody{{Name: service, Published: true, Listen: args.Port, Replicas: 1}}
	vars := map[string]string{}
	for k, v := range args.Variables {
		vars[k] = v
	}
	var dbVars map[string]string
	if args.Database != "" && args.Database != "none" {
		var db serviceBody
		db, dbVars = databaseFor(args.Database, args.Name)
		services = append(services, db)
		for k, v := range dbVars {
			if _, set := vars[k]; !set {
				vars[k] = v
			}
		}
	}
	act := func() (string, error) {
		body := dest.body(map[string]any{
			"name": args.Name, "names": names, "sourceKind": "git", "source": args.Repository,
			"services": services, "variables": vars,
		})
		var app appView
		if err := c.send(http.MethodPost, "/api/applications", body, &app); err != nil {
			return "", err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s construite depuis %s et déployée sur %s, à l'adresse https://%s (port %d dans le conteneur).\n", app.Name, args.Repository, dest.name, app.Hostname, args.Port)
		if dbVars != nil {
			var keys []string
			for k := range dbVars {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(&b, "Base %s ajoutée (service « db », disque de 10 Go). Variables posées pour l'application, mots de passe chiffrés : %s. Si l'application attend d'autres noms, posez-les avec set_variables.\n", args.Database, strings.Join(keys, ", "))
		}
		b.WriteString("Pour en faire un paquet réutilisable : save_application_as_package.")
		return b.String(), nil
	}
	conflicts, err := c.dnsConflicts(names, nil)
	if err != nil {
		return "", err
	}
	if len(conflicts) == 0 {
		return act()
	}
	recap := func() (string, string, error) {
		conflicts, err := c.dnsConflicts(names, nil)
		if err != nil {
			return "", "", err
		}
		return "Déploiement de " + args.Name + " depuis " + args.Repository + "\nCes enregistrements DNS, que MyInfra n'a pas écrits, seront remplacés par les adresses de l'application :\n  " + strings.Join(conflicts, "\n  ") + "\n", fingerprintOf(conflicts), nil
	}
	return c.confirmed(raw, args.Confirmation, recap, act)
}

func runSetDomains(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[struct {
		Application  string   `json:"application"`
		Domains      []string `json:"domains"`
		Confirmation string   `json:"confirmation"`
	}](raw)
	if err != nil {
		return "", err
	}
	app, err := c.application(args.Application)
	if err != nil {
		return "", err
	}
	var view appView
	if err := c.get("/api/applications/"+url.PathEscape(app.ID), &view); err != nil {
		return "", err
	}
	own := map[string]bool{}
	for _, d := range view.Domains {
		own[strings.ToLower(d.Hostname)] = true
	}
	names, err := c.domainNames(args.Domains)
	if err != nil {
		return "", err
	}
	act := func() (string, error) {
		if err := c.send(http.MethodPut, "/api/applications/"+url.PathEscape(app.ID)+"/names", map[string]any{"names": names}, nil); err != nil {
			return "", err
		}
		if len(args.Domains) == 0 {
			return app.Name + " n'a plus que son adresse par défaut.", nil
		}
		return "Domaines de " + app.Name + " : " + strings.Join(args.Domains, ", ") + ". MyInfra écrit le DNS et demande les certificats.", nil
	}
	conflicts, err := c.dnsConflicts(names, own)
	if err != nil {
		return "", err
	}
	if len(conflicts) == 0 {
		return act()
	}
	recap := func() (string, string, error) {
		conflicts, err := c.dnsConflicts(names, own)
		if err != nil {
			return "", "", err
		}
		return "Domaines de " + app.Name + " : " + strings.Join(args.Domains, ", ") + "\nCes enregistrements DNS, que MyInfra n'a pas écrits, seront remplacés par les adresses de l'application :\n  " + strings.Join(conflicts, "\n  ") + "\n", fingerprintOf(conflicts), nil
	}
	return c.confirmed(raw, args.Confirmation, recap, act)
}
