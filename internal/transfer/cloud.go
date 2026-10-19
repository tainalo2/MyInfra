package transfer

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

func CloudConfig(image, masterKey, databasePassword, bootstrapToken, registryHost, registryUser, registryPassword string) (string, error) {
	if !safeImage(image) || !safe(masterKey) || !safe(databasePassword) || !safe(bootstrapToken) {
		return "", fmt.Errorf("paramètre de démarrage refusé")
	}
	login := ""
	if registryHost != "" || registryUser != "" || registryPassword != "" {
		if !safe(registryHost) || !safe(registryUser) || !safe(registryPassword) {
			return "", fmt.Errorf("paramètre de démarrage refusé")
		}
		raw := registryAuth(registryHost, registryUser, registryPassword)
		login = "mkdir -p /root/.docker\n      printf '%s' " + strconv.Quote(base64.StdEncoding.EncodeToString(raw)) + " | base64 -d > /root/.docker/config.json\n"
	}
	return fmt.Sprintf(`#cloud-config
package_update: true
packages:
  - ca-certificates
  - curl
write_files:
  - path: /opt/myinfra/compose.yaml
    permissions: "0644"
    content: |
      services:
        postgres:
          image: postgres:16
          restart: unless-stopped
          environment:
            POSTGRES_USER: myinfra
            POSTGRES_PASSWORD: %q
            POSTGRES_DB: myinfra
          volumes:
            - pg:/var/lib/postgresql/data
        myinfra:
          image: %s
          restart: unless-stopped
          depends_on:
            - postgres
          environment:
            DATABASE_URL: postgres://myinfra:%s@postgres:5432/myinfra?sslmode=disable
            MYINFRA_MASTER_KEY: %q
            MYINFRA_PUBLIC_URL: http://auto
            MYINFRA_BOOTSTRAP_TOKEN: %q
            MYINFRA_ADDR: ":8080"
          ports:
            - "80:8080"
      volumes:
        pg:
  - path: /opt/myinfra/start.sh
    permissions: "0755"
    content: |
      #!/bin/sh
      set -eu
      %s
      ip=$(curl -4 -fsSL --max-time 8 https://api.ipify.org || true)
      if [ -n "$ip" ]; then
        sed -i "s#http://auto#http://$ip#" /opt/myinfra/compose.yaml
      fi
      docker compose -f /opt/myinfra/compose.yaml up -d
runcmd:
  - [sh, -c, "curl -fsSL https://get.docker.com | sh"]
  - [systemctl, enable, --now, docker]
  - [sh, /opt/myinfra/start.sh]
`, databasePassword, image, databasePassword, masterKey, bootstrapToken, login), nil
}

func registryAuth(host, user, password string) []byte {
	encoded := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	return []byte(fmt.Sprintf(`{"auths":{%s:{"username":%s,"password":%s,"auth":%s}}}`, strconv.Quote(host), strconv.Quote(user), strconv.Quote(password), strconv.Quote(encoded)))
}

func safe(value string) bool {
	return strings.TrimSpace(value) != "" && !strings.ContainsAny(value, "\r\n")
}

func safeImage(value string) bool {
	if !safe(value) {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(".-_/:@", r):
		default:
			return false
		}
	}
	return true
}
