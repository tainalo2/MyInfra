package market

var contact = Input{Key: "email", Label: "Adresse de contact"}

func pub(name, image string, listen int, path string, gi int, args ...string) Service {
	svc := Service{Name: name, Image: image, Published: true, Listen: listen, Replicas: 1, Args: args}
	if path != "" {
		svc.Disk = true
		svc.DiskPath = path
		svc.DiskGi = gi
	}
	return svc
}

func side(name, image string, listen int, path string, gi int, args ...string) Service {
	svc := pub(name, image, listen, path, gi, args...)
	svc.Published = false
	return svc
}

func pg() Service {
	return side("postgres", "postgres:16", 5432, "/var/lib/postgresql/data", 8)
}

func maria() Service {
	return side("mariadb", "mariadb:11", 3306, "/var/lib/mysql", 8)
}

func mysql() Service {
	return side("mysql", "mysql:8.4", 3306, "/var/lib/mysql", 8)
}

func redis() Service {
	return side("redis", "redis:7-alpine", 6379, "", 0)
}

func mongo() Service {
	return side("mongo", "mongo:7", 27017, "/data/db", 8)
}

func one(id, name, summary, category, image string, listen int, path string, gi int, env map[string]string, inputs ...Input) Article {
	return Article{
		ID: id, Name: name, Summary: summary, Category: category, Inputs: inputs,
		Services: []Service{pub(id, image, listen, path, gi)}, Env: env,
	}
}

func withArgs(article Article, args ...string) Article {
	article.Services[0].Args = args
	return article
}

func stack(id, name, summary, category string, env map[string]string, services []Service, inputs ...Input) Article {
	return Article{ID: id, Name: name, Summary: summary, Category: category, Inputs: inputs, Services: services, Env: env}
}

func pgEnv(user, database string) map[string]string {
	return map[string]string{
		"POSTGRES_USER": user, "POSTGRES_PASSWORD": "{{secret:DB}}", "POSTGRES_DB": database,
	}
}

func merge(parts ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, part := range parts {
		for key, value := range part {
			out[key] = value
		}
	}
	return out
}

var articles = []Article{
	stack("wordpress", "WordPress", "Un site et son espace d'écriture.", Sites, merge(map[string]string{
		"WORDPRESS_DB_HOST": "mariadb", "WORDPRESS_DB_USER": "wordpress", "WORDPRESS_DB_PASSWORD": "{{secret:DB}}", "WORDPRESS_DB_NAME": "wordpress",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "wordpress", "MARIADB_USER": "wordpress", "MARIADB_PASSWORD": "{{secret:DB}}",
	}), []Service{pub("web", "wordpress:6-apache", 80, "/var/www/html", 8), maria()}),
	stack("ghost", "Ghost", "Une publication en ligne.", Sites, merge(map[string]string{
		"database__client": "mysql", "database__connection__host": "mysql", "database__connection__user": "ghost",
		"database__connection__password": "{{secret:DB}}", "database__connection__database": "ghost", "url": "{{url}}",
		"MYSQL_ROOT_PASSWORD": "{{secret:ROOT}}", "MYSQL_DATABASE": "ghost", "MYSQL_USER": "ghost", "MYSQL_PASSWORD": "{{secret:DB}}",
	}), []Service{pub("web", "ghost:5-alpine", 2368, "/var/lib/ghost/content", 8), mysql()}),
	stack("wikijs", "Wiki.js", "Un wiki partagé.", Sites, merge(pgEnv("wiki", "wiki"), map[string]string{
		"DB_TYPE": "postgres", "DB_HOST": "postgres", "DB_PORT": "5432", "DB_USER": "wiki", "DB_PASS": "{{secret:DB}}", "DB_NAME": "wiki",
	}), []Service{pub("web", "requarks/wiki:2", 3000, "", 0), pg()}),
	stack("bookstack", "BookStack", "Des notices rangées en étagères.", Sites, map[string]string{
		"APP_URL": "{{url}}", "DB_HOST": "mariadb", "DB_PORT": "3306", "DB_DATABASE": "bookstack", "DB_USERNAME": "bookstack", "DB_PASSWORD": "{{secret:DB}}",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "bookstack", "MARIADB_USER": "bookstack", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "linuxserver/bookstack:25.07.2", 80, "/config", 4), maria()}),
	one("dokuwiki", "DokuWiki", "Un wiki de fichiers, sans base à part.", Sites, "dokuwiki/dokuwiki:2024-02-06b", 80, "/storage", 2, nil),
	one("grav", "Grav", "Un site de pages en fichiers.", Sites, "linuxserver/grav:1.7.48", 80, "/config", 2, nil),
	one("freshrss", "FreshRSS", "Un lecteur de flux.", Sites, "freshrss/freshrss:1.26.1", 80, "/var/www/FreshRSS/data", 2, nil),
	stack("miniflux", "Miniflux", "Un lecteur de flux dépouillé.", Sites, merge(pgEnv("miniflux", "miniflux"), map[string]string{
		"DATABASE_URL": "postgres://miniflux:{{secret:DB}}@postgres/miniflux?sslmode=disable", "RUN_MIGRATIONS": "1", "CREATE_ADMIN": "1",
		"ADMIN_USERNAME": "admin", "ADMIN_PASSWORD": "{{secret:ADMIN}}",
	}), []Service{pub("web", "miniflux/miniflux:2.2.6", 8080, "", 0), pg()}),
	one("linkding", "Linkding", "Des liens enregistrés.", Sites, "sissbruecker/linkding:1.39.1", 9090, "/etc/linkding/data", 2, map[string]string{
		"LD_SUPERUSER_NAME": "admin", "LD_SUPERUSER_PASSWORD": "{{secret:ADMIN}}",
	}),
	stack("wallabag", "Wallabag", "Des pages mises de côté pour plus tard.", Sites, map[string]string{
		"SYMFONY__ENV__DATABASE_DRIVER": "pdo_mysql", "SYMFONY__ENV__DATABASE_HOST": "mariadb", "SYMFONY__ENV__DATABASE_PORT": "3306",
		"SYMFONY__ENV__DATABASE_NAME": "wallabag", "SYMFONY__ENV__DATABASE_USER": "wallabag", "SYMFONY__ENV__DATABASE_PASSWORD": "{{secret:DB}}",
		"SYMFONY__ENV__DOMAIN_NAME": "{{url}}", "SYMFONY__ENV__SECRET": "{{secret:SECRET}}",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "wallabag", "MARIADB_USER": "wallabag", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "wallabag/wallabag:2.6.13", 80, "/var/www/wallabag/data", 4), maria()}),
	stack("shlink", "Shlink", "Des liens courts.", Sites, map[string]string{
		"DEFAULT_DOMAIN": "{{host}}", "IS_HTTPS_ENABLED": "true", "DB_DRIVER": "maria", "DB_HOST": "mariadb", "DB_PORT": "3306",
		"DB_NAME": "shlink", "DB_USER": "shlink", "DB_PASSWORD": "{{secret:DB}}", "INITIAL_API_KEY": "{{secret:KEY}}",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "shlink", "MARIADB_USER": "shlink", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "shlinkio/shlink:4.4.6", 8080, "", 0), maria()}),
	stack("umami", "Umami", "Une mesure d'audience sobre.", Sites, merge(pgEnv("umami", "umami"), map[string]string{
		"DATABASE_URL": "postgresql://umami:{{secret:DB}}@postgres:5432/umami", "APP_SECRET": "{{secret:SECRET}}",
	}), []Service{pub("web", "umamisoftware/umami:postgresql-v2.17.0", 3000, "", 0), pg()}),
	stack("matomo", "Matomo", "Une mesure d'audience complète.", Sites, map[string]string{
		"MATOMO_DATABASE_HOST": "mariadb", "MATOMO_DATABASE_ADAPTER": "mysql", "MATOMO_DATABASE_TABLES_PREFIX": "matomo_",
		"MATOMO_DATABASE_USERNAME": "matomo", "MATOMO_DATABASE_PASSWORD": "{{secret:DB}}", "MATOMO_DATABASE_DBNAME": "matomo",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "matomo", "MARIADB_USER": "matomo", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "matomo:5-apache", 80, "/var/www/html", 8), maria()}),

	stack("nextcloud", "Nextcloud", "Fichiers, agenda et contacts partagés.", Collaboration, map[string]string{
		"MYSQL_HOST": "mariadb", "MYSQL_DATABASE": "nextcloud", "MYSQL_USER": "nextcloud", "MYSQL_PASSWORD": "{{secret:DB}}",
		"NEXTCLOUD_ADMIN_USER": "admin", "NEXTCLOUD_ADMIN_PASSWORD": "{{secret:ADMIN}}", "NEXTCLOUD_TRUSTED_DOMAINS": "{{host}}",
		"REDIS_HOST": "redis", "MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "nextcloud", "MARIADB_USER": "nextcloud", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "nextcloud:31-apache", 80, "/var/www/html", 32), maria(), redis()}),
	one("cryptpad", "CryptPad", "Des documents chiffrés dans le navigateur.", Collaboration, "cryptpad/cryptpad:version-2024.12.0", 3000, "/cryptpad/data", 8, map[string]string{}),
	stack("mattermost", "Mattermost", "Une discussion d'équipe.", Collaboration, merge(pgEnv("mattermost", "mattermost"), map[string]string{
		"MM_SQLSETTINGS_DRIVERNAME": "postgres", "MM_SQLSETTINGS_DATASOURCE": "postgres://mattermost:{{secret:DB}}@postgres:5432/mattermost?sslmode=disable",
		"MM_SERVICESETTINGS_SITEURL": "{{url}}",
	}), []Service{pub("web", "mattermost/mattermost-team-edition:10.5", 8065, "/mattermost/data", 8), pg()}),
	one("vikunja", "Vikunja", "Des listes de tâches.", Collaboration, "vikunja/vikunja:0.24.6", 3456, "/app/vikunja/files", 2, map[string]string{
		"VIKUNJA_DATABASE_TYPE": "sqlite", "VIKUNJA_DATABASE_PATH": "/app/vikunja/files/vikunja.db", "VIKUNJA_SERVICE_PUBLICURL": "{{url}}",
	}),
	stack("nocodb", "NocoDB", "Une base sous forme de tableau.", Collaboration, merge(pgEnv("nocodb", "nocodb"), map[string]string{
		"NC_DB": "pg://postgres:5432?u=nocodb&p={{secret:DB}}&d=nocodb",
	}), []Service{pub("web", "nocodb/nocodb:0.263.8", 8080, "/usr/app/data", 4), pg()}),
	one("baserow", "Baserow", "Un tableur partagé.", Collaboration, "baserow/baserow:1.33.3", 80, "/baserow/data", 8, map[string]string{"BASEROW_PUBLIC_URL": "{{url}}"}),
	one("memos", "Memos", "Des notes courtes.", Collaboration, "neosmemo/memos:0.24.4", 5230, "/var/opt/memos", 2, nil),
	one("trilium", "Trilium", "Des notes reliées entre elles.", Collaboration, "triliumnext/notes:v0.95.0", 8080, "/home/node/trilium-data", 4, nil),
	stack("affine", "AFFiNE", "Notes, tableaux et dessins au même endroit.", Collaboration, merge(pgEnv("affine", "affine"), map[string]string{
		"DATABASE_URL": "postgresql://affine:{{secret:DB}}@postgres:5432/affine", "REDIS_SERVER_HOST": "redis", "AFFINE_SERVER_HTTPS": "true", "AFFINE_SERVER_HOST": "{{host}}",
	}), []Service{pub("web", "ghcr.io/toeverything/affine:0.22.3", 3010, "/root/.affine/storage", 8), pg(), redis()}),
	stack("docmost", "Docmost", "Un espace de pages d'équipe.", Collaboration, merge(pgEnv("docmost", "docmost"), map[string]string{
		"DATABASE_URL": "postgresql://docmost:{{secret:DB}}@postgres:5432/docmost", "REDIS_URL": "redis://redis:6379", "APP_SECRET": "{{secret:SECRET}}", "APP_URL": "{{url}}",
	}), []Service{pub("web", "docmost/docmost:0.21.0", 3000, "/app/data", 8), pg(), redis()}),
	stack("outline", "Outline", "Une base de connaissances.", Collaboration, merge(pgEnv("outline", "outline"), map[string]string{
		"DATABASE_URL": "postgres://outline:{{secret:DB}}@postgres:5432/outline", "REDIS_URL": "redis://redis:6379",
		"SECRET_KEY": "{{secret:SECRET}}", "UTILS_SECRET": "{{secret:UTILS}}", "URL": "{{url}}",
		"FILE_STORAGE": "local", "FILE_STORAGE_LOCAL_ROOT_DIR": "/var/lib/outline/data",
	}), []Service{pub("web", "outlinewiki/outline:0.82.0", 3000, "/var/lib/outline/data", 8), pg(), redis()}),
	stack("wekan", "WeKan", "Un tableau de cartes.", Collaboration, map[string]string{
		"MONGO_URL": "mongodb://wekan:{{secret:DB}}@mongo:27017/wekan?authSource=admin", "ROOT_URL": "{{url}}",
		"MONGO_INITDB_ROOT_USERNAME": "wekan", "MONGO_INITDB_ROOT_PASSWORD": "{{secret:DB}}", "MONGO_INITDB_DATABASE": "wekan",
	}, []Service{pub("web", "wekanteam/wekan:v7.72", 8080, "/data", 4), mongo()}),
	one("excalidraw", "Excalidraw", "Un tableau blanc.", Collaboration, "excalidraw/excalidraw:v0.18.0", 80, "", 0, nil),
	one("etherpad", "Etherpad", "Un texte écrit à plusieurs.", Collaboration, "etherpad/etherpad:2.2.7", 9001, "/opt/etherpad-lite/var", 2, map[string]string{
		"ADMIN_PASSWORD": "{{secret:ADMIN}}", "TRUST_PROXY": "true",
	}),
	stack("hedgedoc", "HedgeDoc", "Des notes en markdown.", Collaboration, merge(pgEnv("hedgedoc", "hedgedoc"), map[string]string{
		"CMD_DB_URL": "postgres://hedgedoc:{{secret:DB}}@postgres:5432/hedgedoc", "CMD_DOMAIN": "{{host}}", "CMD_URL_ADDPORT": "false", "CMD_PROTOCOL_USESSL": "true",
	}), []Service{pub("web", "quay.io/hedgedoc/hedgedoc:1.10.3", 3000, "", 0), pg()}),
	stack("penpot", "Penpot", "Un atelier de maquettes.", Collaboration, merge(pgEnv("penpot", "penpot"), map[string]string{
		"PENPOT_FLAGS":      "disable-email-verification enable-registration disable-secure-session-cookies",
		"PENPOT_PUBLIC_URI": "{{url}}", "PENPOT_DATABASE_URI": "postgresql://penpot:{{secret:DB}}@postgres/penpot",
		"PENPOT_REDIS_URI": "redis://redis:6379/0", "PENPOT_SECRET_KEY": "{{secret:SECRET}}",
	}), []Service{
		pub("frontend", "penpotapp/frontend:2.6.2", 8080, "", 0),
		side("backend", "penpotapp/backend:2.6.2", 6060, "/opt/data", 8),
		side("exporter", "penpotapp/exporter:2.6.2", 6061, "", 0),
		pg(), redis(),
	}),
	stack("calcom", "Cal.com", "Des rendez-vous à réserver.", Collaboration, merge(pgEnv("cal", "cal"), map[string]string{
		"DATABASE_URL": "postgresql://cal:{{secret:DB}}@postgres:5432/cal", "NEXTAUTH_SECRET": "{{secret:SECRET}}",
		"CALENDSO_ENCRYPTION_KEY": "{{secret:ENC}}", "NEXT_PUBLIC_WEBAPP_URL": "{{url}}", "EMAIL_FROM": "{{input:email}}",
	}), []Service{pub("web", "calcom/cal.com:v5.1.4", 3000, "", 0), pg()}, contact),

	one("forgejo", "Forgejo", "Des dépôts Git et leurs demandes.", Code, "codeberg.org/forgejo/forgejo:7", 3000, "/data", 8, map[string]string{
		"USER_UID": "1000", "USER_GID": "1000", "FORGEJO__server__DOMAIN": "{{host}}", "FORGEJO__server__ROOT_URL": "{{url}}/",
		"FORGEJO__server__HTTP_PORT": "3000", "FORGEJO__database__DB_TYPE": "sqlite3", "FORGEJO__database__PATH": "/data/forgejo.db",
	}),
	one("code-server", "code-server", "Un éditeur dans le navigateur.", Code, "codercom/code-server:4.96.4", 8080, "/home/coder/project", 8, map[string]string{"PASSWORD": "{{secret:PASSWORD}}"}),
	one("verdaccio", "Verdaccio", "Un registre de paquets privé.", Code, "verdaccio/verdaccio:6.0.5", 4873, "/verdaccio/storage", 8, nil),
	one("meilisearch", "Meilisearch", "Un moteur de recherche.", Code, "getmeili/meilisearch:v1.13.3", 7700, "/meili_data", 8, map[string]string{
		"MEILI_ENV": "production", "MEILI_MASTER_KEY": "{{secret:KEY}}", "MEILI_NO_ANALYTICS": "true",
	}),
	one("typesense", "Typesense", "Un moteur de recherche.", Code, "typesense/typesense:27.1", 8108, "/data", 8, map[string]string{
		"TYPESENSE_DATA_DIR": "/data", "TYPESENSE_API_KEY": "{{secret:KEY}}", "TYPESENSE_ENABLE_CORS": "true",
	}),
	withArgs(one("pocketbase", "PocketBase", "Une petite base et son interface.", Code, "ghcr.io/pocketbase/pocketbase:0.28.4", 8090, "/pb_data", 4, nil), "serve", "--http=0.0.0.0:8090"),
	one("registry", "Registre d'images", "Un dépôt d'images de conteneurs.", Code, "registry:2", 5000, "/var/lib/registry", 16, map[string]string{
		"REGISTRY_STORAGE_FILESYSTEM_ROOTDIRECTORY": "/var/lib/registry",
	}),

	one("vaultwarden", "Vaultwarden", "Un coffre de mots de passe.", Identity, "vaultwarden/server:1.34.1", 80, "/data", 2, map[string]string{
		"DOMAIN": "{{url}}", "SIGNUPS_ALLOWED": "true", "ADMIN_TOKEN": "{{secret:ADMIN}}",
	}),
	stack("authentik", "Authentik", "Une porte d'identité pour les autres services.", Identity, merge(pgEnv("authentik", "authentik"), map[string]string{
		"AUTHENTIK_SECRET_KEY": "{{secret:SECRET}}", "AUTHENTIK_ERROR_REPORTING__ENABLED": "false",
		"AUTHENTIK_POSTGRESQL__HOST": "postgres", "AUTHENTIK_POSTGRESQL__USER": "authentik", "AUTHENTIK_POSTGRESQL__NAME": "authentik", "AUTHENTIK_POSTGRESQL__PASSWORD": "{{secret:DB}}",
		"AUTHENTIK_REDIS__HOST": "redis",
	}), []Service{
		pub("server", "authentik/server:2025.8.4", 9000, "/data", 2),
		side("worker", "authentik/server:2025.8.4", 9000, "", 0, "worker"),
		pg(), redis(),
	}),
	stack("keycloak", "Keycloak", "Un service d'identité.", Identity, merge(pgEnv("keycloak", "keycloak"), map[string]string{
		"KC_BOOTSTRAP_ADMIN_USERNAME": "admin", "KC_BOOTSTRAP_ADMIN_PASSWORD": "{{secret:ADMIN}}",
		"KC_DB": "postgres", "KC_DB_URL": "jdbc:postgresql://postgres:5432/keycloak", "KC_DB_USERNAME": "keycloak", "KC_DB_PASSWORD": "{{secret:DB}}",
		"KC_PROXY_HEADERS": "xforwarded", "KC_HTTP_ENABLED": "true", "KC_HOSTNAME": "{{host}}",
	}), []Service{pub("web", "keycloak/keycloak:26.3", 8080, "", 0), pg()}),
	one("lldap", "LLDAP", "Un annuaire léger et son écran.", Identity, "lldap/lldap:v0.6.1", 17170, "/data", 1, map[string]string{
		"LLDAP_JWT_SECRET": "{{secret:JWT}}", "LLDAP_LDAP_USER_PASS": "{{secret:ADMIN}}", "LLDAP_LDAP_BASE_DN": "dc=myinfra,dc=local",
	}),

	one("filebrowser", "File Browser", "Les fichiers d'un dossier, dans le navigateur.", Files, "filebrowser/filebrowser:v2.32.0", 80, "/srv", 16, nil),
	stack("directus", "Directus", "Une base de contenu.", Files, merge(pgEnv("directus", "directus"), map[string]string{
		"DB_CLIENT": "pg", "DB_HOST": "postgres", "DB_PORT": "5432", "DB_DATABASE": "directus", "DB_USER": "directus", "DB_PASSWORD": "{{secret:DB}}",
		"KEY": "{{secret:KEY}}", "SECRET": "{{secret:SECRET}}", "ADMIN_EMAIL": "{{input:email}}", "ADMIN_PASSWORD": "{{secret:ADMIN}}",
		"REDIS": "redis://redis:6379", "PUBLIC_URL": "{{url}}",
	}), []Service{pub("web", "directus/directus:11.9.3", 8055, "/directus/uploads", 8), pg(), redis()}, contact),
	stack("metabase", "Metabase", "Des questions sur une base de données.", Files, merge(pgEnv("metabase", "metabase"), map[string]string{
		"MB_DB_TYPE": "postgres", "MB_DB_HOST": "postgres", "MB_DB_PORT": "5432", "MB_DB_DBNAME": "metabase", "MB_DB_USER": "metabase", "MB_DB_PASS": "{{secret:DB}}",
	}), []Service{pub("web", "metabase/metabase:v0.54.9", 3000, "", 0), pg()}),
	one("grafana", "Grafana", "Des tableaux de bord.", Files, "grafana/grafana:11.6.1", 3000, "/var/lib/grafana", 4, map[string]string{
		"GF_SECURITY_ADMIN_USER": "admin", "GF_SECURITY_ADMIN_PASSWORD": "{{secret:ADMIN}}", "GF_SERVER_ROOT_URL": "{{url}}",
	}),
	one("appsmith", "Appsmith", "Des écrans posés sur des données.", Files, "appsmith/appsmith-ce:v1.65", 80, "/appsmith-stacks", 8, nil),

	stack("n8n", "n8n", "Des automatisations entre services.", Automation, merge(pgEnv("n8n", "n8n"), map[string]string{
		"DB_TYPE": "postgresdb", "DB_POSTGRESDB_HOST": "postgres", "DB_POSTGRESDB_PORT": "5432", "DB_POSTGRESDB_DATABASE": "n8n",
		"DB_POSTGRESDB_USER": "n8n", "DB_POSTGRESDB_PASSWORD": "{{secret:DB}}", "N8N_HOST": "{{host}}", "N8N_PORT": "5678",
		"N8N_PROTOCOL": "https", "WEBHOOK_URL": "{{url}}/", "N8N_ENCRYPTION_KEY": "{{secret:KEY}}",
	}), []Service{pub("web", "n8nio/n8n:1.109.1", 5678, "/home/node/.n8n", 4), pg()}),
	stack("activepieces", "Activepieces", "Des automatisations entre services.", Automation, merge(pgEnv("activepieces", "activepieces"), map[string]string{
		"AP_POSTGRES_DATABASE": "activepieces", "AP_POSTGRES_HOST": "postgres", "AP_POSTGRES_PORT": "5432",
		"AP_POSTGRES_USERNAME": "activepieces", "AP_POSTGRES_PASSWORD": "{{secret:DB}}", "AP_REDIS_HOST": "redis", "AP_REDIS_PORT": "6379",
		"AP_ENCRYPTION_KEY": "{{secret:KEY}}", "AP_FRONTEND_URL": "{{url}}", "AP_WEBHOOK_URL": "{{url}}",
	}), []Service{pub("web", "activepieces/activepieces:0.48.2", 80, "", 0), pg(), redis()}),
	one("uptime-kuma", "Uptime Kuma", "La surveillance de sites.", Automation, "louislam/uptime-kuma:1.23.16", 3001, "/app/data", 2, nil),
	one("changedetection", "Changedetection", "L'alerte quand une page change.", Automation, "dgtlmoon/changedetection.io:0.50.3", 5000, "/datastore", 2, nil),
	stack("huginn", "Huginn", "Des agents qui surveillent et agissent.", Automation, map[string]string{
		"DATABASE_ADAPTER": "mysql2", "DATABASE_HOST": "mysql", "DATABASE_PORT": "3306", "DATABASE_NAME": "huginn",
		"DATABASE_USERNAME": "huginn", "DATABASE_PASSWORD": "{{secret:DB}}", "APP_SECRET_TOKEN": "{{secret:SECRET}}",
		"MYSQL_ROOT_PASSWORD": "{{secret:ROOT}}", "MYSQL_DATABASE": "huginn", "MYSQL_USER": "huginn", "MYSQL_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "huginn/huginn:latest", 3000, "", 0), mysql()}),
	stack("listmonk", "Listmonk", "Des listes de diffusion.", Automation, merge(pgEnv("listmonk", "listmonk"), map[string]string{
		"LISTMONK_app__address": "{{url}}", "LISTMONK_db__host": "postgres", "LISTMONK_db__port": "5432", "LISTMONK_db__user": "listmonk",
		"LISTMONK_db__password": "{{secret:DB}}", "LISTMONK_db__database": "listmonk", "LISTMONK_db__ssl_mode": "disable",
		"LISTMONK_ADMIN_USER": "{{input:email}}", "LISTMONK_ADMIN_PASSWORD": "{{secret:ADMIN}}",
	}), []Service{pub("web", "listmonk/listmonk:v5.0.2", 9000, "", 0), pg()}, contact),
	stack("healthchecks", "Healthchecks", "La confirmation que des tâches ont bien tourné.", Automation, merge(pgEnv("healthchecks", "healthchecks"), map[string]string{
		"DB": "postgres", "DB_HOST": "postgres", "DB_PORT": "5432", "DB_NAME": "healthchecks", "DB_USER": "healthchecks", "DB_PASSWORD": "{{secret:DB}}",
		"SECRET_KEY": "{{secret:SECRET}}", "SITE_ROOT": "{{url}}",
	}), []Service{pub("web", "healthchecks/healthchecks:version-3.6", 8000, "", 0), pg()}),

	stack("chatwoot", "Chatwoot", "Une boîte de conversation avec les visiteurs.", Exchange, merge(pgEnv("chatwoot", "chatwoot"), map[string]string{
		"RAILS_ENV": "production", "SECRET_KEY_BASE": "{{secret:SECRET}}", "FRONTEND_URL": "{{url}}", "NODE_ENV": "production",
		"POSTGRES_HOST": "postgres", "POSTGRES_PORT": "5432", "POSTGRES_USERNAME": "chatwoot", "POSTGRES_PASSWORD": "{{secret:DB}}", "POSTGRES_DATABASE": "chatwoot",
		"REDIS_URL": "redis://redis:6379", "MAILER_SENDER_EMAIL": "{{input:email}}",
	}), []Service{pub("web", "chatwoot/chatwoot:v4.4.0", 3000, "/app/storage", 8), pg(), redis()}, contact),
	stack("freescout", "FreeScout", "Une boîte mail d'assistance.", Exchange, map[string]string{
		"DB_HOST": "mariadb", "DB_PORT": "3306", "DB_NAME": "freescout", "DB_USER": "freescout", "DB_PASS": "{{secret:DB}}",
		"ADMIN_EMAIL": "{{input:email}}", "ADMIN_PASS": "{{secret:ADMIN}}", "APP_URL": "{{url}}", "SITE_URL": "{{url}}",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "freescout", "MARIADB_USER": "freescout", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "tiredofit/freescout:1.17.5", 80, "/data", 4), maria()}, contact),
	stack("formbricks", "Formbricks", "Des formulaires et des enquêtes.", Exchange, merge(pgEnv("formbricks", "formbricks"), map[string]string{
		"DATABASE_URL": "postgresql://formbricks:{{secret:DB}}@postgres:5432/formbricks", "NEXTAUTH_SECRET": "{{secret:SECRET}}",
		"ENCRYPTION_KEY": "{{secret:ENC}}", "WEBAPP_URL": "{{url}}", "NEXTAUTH_URL": "{{url}}", "MAIL_FROM": "{{input:email}}",
	}), []Service{pub("web", "ghcr.io/formbricks/formbricks:3.3.3", 3000, "", 0), pg()}, contact),
	stack("docuseal", "DocuSeal", "Des documents à signer.", Exchange, merge(pgEnv("docuseal", "docuseal"), map[string]string{
		"DATABASE_URL": "postgresql://docuseal:{{secret:DB}}@postgres:5432/docuseal", "SECRET_KEY_BASE": "{{secret:SECRET}}",
	}), []Service{pub("web", "docuseal/docuseal:1.9.4", 3000, "/data", 8), pg()}),

	one("silverbullet", "SilverBullet", "Des notes en markdown.", Notes, "zefhemel/silverbullet:0.10.4", 3000, "/space", 2, nil),
	stack("joplin", "Joplin Server", "La synchronisation des notes Joplin.", Notes, merge(pgEnv("joplin", "joplin"), map[string]string{
		"APP_BASE_URL": "{{url}}", "APP_PORT": "22300", "DB_CLIENT": "pg", "POSTGRES_HOST": "postgres", "POSTGRES_PORT": "5432",
		"POSTGRES_DATABASE": "joplin", "POSTGRES_USER": "joplin", "POSTGRES_PASSWORD": "{{secret:DB}}",
	}), []Service{pub("web", "joplin/server:3.3.3", 22300, "", 0), pg()}),
	stack("linkwarden", "Linkwarden", "Des liens classés et conservés.", Notes, merge(pgEnv("linkwarden", "linkwarden"), map[string]string{
		"DATABASE_URL": "postgresql://linkwarden:{{secret:DB}}@postgres:5432/linkwarden", "NEXTAUTH_SECRET": "{{secret:SECRET}}", "NEXTAUTH_URL": "{{url}}",
	}), []Service{pub("web", "ghcr.io/linkwarden/linkwarden:v2.10.2", 3000, "/data", 8), pg()}),
	one("shiori", "Shiori", "Des pages mises de côté.", Notes, "ghcr.io/go-shiori/shiori:v1.7.4", 8080, "/shiori", 2, map[string]string{"SHIORI_DIR": "/shiori"}),

	stack("firefly", "Firefly III", "Un suivi des comptes et des dépenses.", Accounts, merge(pgEnv("firefly", "firefly"), map[string]string{
		"APP_KEY": "base64:{{encoded:KEY}}", "APP_URL": "{{url}}", "DB_CONNECTION": "pgsql", "DB_HOST": "postgres", "DB_PORT": "5432",
		"DB_DATABASE": "firefly", "DB_USERNAME": "firefly", "DB_PASSWORD": "{{secret:DB}}", "TRUSTED_PROXIES": "*",
	}), []Service{pub("web", "fireflyiii/core:version-6.2.16", 8080, "/var/www/html/storage", 4), pg()}),
	one("actual", "Actual", "Un budget personnel.", Accounts, "actualbudget/actual-server:25.10.0", 5006, "/data", 2, nil),
	one("wallos", "Wallos", "Le suivi des abonnements.", Accounts, "ghcr.io/ellite/wallos:2.45.0", 80, "/var/www/html/db", 1, nil),
	stack("ghostfolio", "Ghostfolio", "Un suivi de patrimoine.", Accounts, merge(pgEnv("ghostfolio", "ghostfolio"), map[string]string{
		"DATABASE_URL": "postgresql://ghostfolio:{{secret:DB}}@postgres:5432/ghostfolio?sslmode=disable", "REDIS_HOST": "redis", "REDIS_PORT": "6379",
		"ACCESS_TOKEN_SALT": "{{secret:SALT}}", "JWT_SECRET_KEY": "{{secret:JWT}}",
	}), []Service{pub("web", "ghostfolio/ghostfolio:2.168.0", 3333, "", 0), pg(), redis()}),
	stack("invoiceninja", "Invoice Ninja", "Des factures.", Accounts, map[string]string{
		"APP_URL": "{{url}}", "APP_KEY": "base64:{{encoded:KEY}}", "IN_USER_EMAIL": "{{input:email}}", "IN_PASSWORD": "{{secret:ADMIN}}",
		"DB_HOST": "mariadb", "DB_PORT": "3306", "DB_DATABASE": "ninja", "DB_USERNAME": "ninja", "DB_PASSWORD": "{{secret:DB}}", "MAIL_MAILER": "log",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "ninja", "MARIADB_USER": "ninja", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "invoiceninja/invoiceninja:5.11.78", 80, "/var/www/app/public/storage", 4), maria()}, contact),

	one("jellyfin", "Jellyfin", "Films, séries et musique.", Media, "jellyfin/jellyfin:10.10.7", 8096, "/config", 32, nil),
	one("navidrome", "Navidrome", "Une discothèque.", Media, "deluan/navidrome:0.55.2", 4533, "/data", 16, map[string]string{
		"ND_MUSICFOLDER": "/data/music", "ND_DATAFOLDER": "/data",
	}),
	stack("immich", "Immich", "Une photothèque.", Media, map[string]string{
		"DB_HOSTNAME": "postgres", "DB_PORT": "5432", "DB_USERNAME": "immich", "DB_PASSWORD": "{{secret:DB}}", "DB_DATABASE_NAME": "immich",
		"REDIS_HOSTNAME": "redis", "REDIS_PORT": "6379", "UPLOAD_LOCATION": "/data",
		"POSTGRES_USER": "immich", "POSTGRES_PASSWORD": "{{secret:DB}}", "POSTGRES_DB": "immich",
	}, []Service{
		pub("web", "ghcr.io/immich-app/immich-server:v1.135.3", 2283, "/data", 32),
		side("postgres", "ghcr.io/immich-app/postgres:16-vectorchord0.4.3", 5432, "/var/lib/postgresql/data", 8),
		redis(),
	}),
	stack("photoprism", "PhotoPrism", "Une photothèque.", Media, map[string]string{
		"PHOTOPRISM_ADMIN_USER": "admin", "PHOTOPRISM_ADMIN_PASSWORD": "{{secret:ADMIN}}", "PHOTOPRISM_DATABASE_DRIVER": "mysql",
		"PHOTOPRISM_DATABASE_SERVER": "mariadb:3306", "PHOTOPRISM_DATABASE_NAME": "photoprism", "PHOTOPRISM_DATABASE_USER": "photoprism",
		"PHOTOPRISM_DATABASE_PASSWORD": "{{secret:DB}}", "PHOTOPRISM_SITE_URL": "{{url}}", "PHOTOPRISM_ORIGINALS_LIMIT": "10000",
		"MARIADB_ROOT_PASSWORD": "{{secret:ROOT}}", "MARIADB_DATABASE": "photoprism", "MARIADB_USER": "photoprism", "MARIADB_PASSWORD": "{{secret:DB}}",
	}, []Service{pub("web", "photoprism/photoprism:240915-ce", 2342, "/photoprism/storage", 32), maria()}),
	one("audiobookshelf", "Audiobookshelf", "Des livres audio.", Media, "advplyr/audiobookshelf:2.26.0", 80, "/metadata", 16, nil),
	one("kavita", "Kavita", "Des bandes dessinées et des livres.", Media, "jvmilazz0/kavita:0.8.6", 5000, "/kavita/config", 16, nil),
	one("komga", "Komga", "Des bandes dessinées.", Media, "gotson/komga:1.21.2", 25600, "/config", 16, nil),
	one("calibre-web", "Calibre-Web", "Une bibliothèque de livres.", Media, "linuxserver/calibre-web:0.6.24", 8083, "/config", 8, nil),
	one("metube", "MeTube", "Le téléchargement de vidéos.", Media, "alexta69/metube:2025.03.12", 8081, "/downloads", 16, nil),
	one("pinchflat", "Pinchflat", "L'archivage de chaînes vidéo.", Media, "ghcr.io/kieraneglin/pinchflat:v2025.6.6", 8945, "/config", 16, nil),

	one("homepage", "Homepage", "Une page d'accueil de services.", Tools, "gethomepage/homepage:v1.2.0", 3000, "/app/config", 1, nil),
	one("dashy", "Dashy", "Un tableau de liens.", Tools, "lissy93/dashy:3.1.1", 8080, "/app/user-data", 1, nil),
	one("homer", "Homer", "Une page d'accueil simple.", Tools, "b4bz/homer:v25.5.2", 8080, "/www/assets", 1, nil),
	one("it-tools", "IT-Tools", "De petits outils pour le quotidien technique.", Tools, "corentinth/it-tools:2024.10.22-7ca5933", 80, "", 0, nil),
	one("stirling-pdf", "Stirling-PDF", "Des outils pour les PDF.", Tools, "stirlingtools/stirling-pdf:0.46.2", 8080, "/configs", 2, nil),
	one("cyberchef", "CyberChef", "Des transformations de texte et de données.", Tools, "ghcr.io/gchq/cyberchef:10.19.4", 80, "", 0, nil),
	one("drawio", "draw.io", "Des schémas.", Tools, "jgraph/drawio:27.0.9", 8080, "", 0, nil),
	one("searxng", "SearXNG", "Un métamoteur de recherche.", Tools, "searxng/searxng:2025.8.1", 8080, "/etc/searxng", 1, map[string]string{"SEARXNG_BASE_URL": "{{url}}/"}),
	one("whoogle", "Whoogle", "Une recherche sans compte.", Tools, "benbusby/whoogle-search:0.9.3", 5000, "", 0, nil),
	one("gatus", "Gatus", "Une page d'état de services.", Tools, "twinproduction/gatus:v5.17.0", 8080, "/data", 1, nil),
	one("gotify", "Gotify", "Des notifications vers le téléphone.", Tools, "gotify/server:2.6.3", 80, "/app/data", 1, nil),
	withArgs(one("ntfy", "ntfy", "Des notifications vers le téléphone.", Tools, "binwiederhier/ntfy:v2.14.0", 80, "/var/cache/ntfy", 1, map[string]string{"NTFY_BASE_URL": "{{url}}"}), "serve"),
	stack("paperless", "Paperless-ngx", "Une archive de documents.", Tools, merge(pgEnv("paperless", "paperless"), map[string]string{
		"PAPERLESS_REDIS": "redis://redis:6379", "PAPERLESS_DBHOST": "postgres", "PAPERLESS_DBPORT": "5432", "PAPERLESS_DBNAME": "paperless",
		"PAPERLESS_DBUSER": "paperless", "PAPERLESS_DBPASS": "{{secret:DB}}", "PAPERLESS_SECRET_KEY": "{{secret:SECRET}}", "PAPERLESS_URL": "{{url}}",
		"PAPERLESS_TIKA_ENABLED": "1", "PAPERLESS_TIKA_ENDPOINT": "http://tika:9998", "PAPERLESS_TIKA_GOTENBERG_ENDPOINT": "http://gotenberg:3000",
		"PAPERLESS_DATA_DIR": "/usr/src/paperless/data", "PAPERLESS_MEDIA_ROOT": "/usr/src/paperless/data/media", "PAPERLESS_CONSUMPTION_DIR": "/usr/src/paperless/data/consume",
	}), []Service{
		pub("web", "paperlessngx/paperless-ngx:2.15.3", 8000, "/usr/src/paperless/data", 16),
		pg(), redis(),
		side("gotenberg", "gotenberg/gotenberg:8", 3000, "", 0),
		side("tika", "apache/tika:2.9.2.1-full", 9998, "", 0),
	}),
}
