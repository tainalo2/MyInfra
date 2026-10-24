package pack

import (
	"reflect"
	"strings"
	"testing"
)

const sample = `type: myinfra-package
version: 1
name: Wiki
summary: Un wiki et sa base.
inputs:
  - key: admin-email
    label: E-mail de l'administrateur
env:
  ADMIN_EMAIL: "{{input:admin-email}}"
  DB_PASSWORD: "{{secret:db}}"
  PORT: "80"
  ENABLED: "true"
services:
  - name: web
    image: ghcr.io/acme/wiki:2
    public: true
    port: 3000
    volumes:
      - path: /data
        sizeGi: 8
      - path: /cache
    files:
      - path: /etc/wiki/config.ini
        content: |
          [server]
          url = {{url}}
    env:
      LOG: debug
    resources:
      cpu: 500m
      memory: 512Mi
    health:
      http: /health
    dependsOn: [db]
  - name: db
    image: postgres:16
    port: 5432
    volumes:
      - path: /var/lib/postgresql/data
        sizeGi: 16
`

func TestParseReadsAPackageAndWritesItBack(t *testing.T) {
	p, problems := Parse([]byte(sample))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if p.Services[0].Files[0].Content != "[server]\nurl = {{url}}\n" || p.Services[0].Volumes[1].Path != "/cache" || p.Env["PORT"] != "80" {
		t.Fatalf("%+v", p)
	}
	disk, rest := p.Services[0].Disk()
	if disk.Path != "/data" || len(rest.Volumes) != 1 || rest.Volumes[0].Path != "/cache" || len(p.Services[0].Volumes) != 2 {
		t.Fatalf("%+v %+v", disk, rest)
	}
	text, err := YAML(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"type: myinfra-package\n", "        content: |\n          [server]\n", "  PORT: \"80\"\n", "  ENABLED: \"true\"\n"} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}
	again, problems := Parse(text)
	if len(problems) > 0 || !reflect.DeepEqual(again, p) {
		t.Fatalf("%v\n%s", problems, text)
	}
	raw, err := JSON(p)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, problems := Parse(raw)
	if len(problems) > 0 || !reflect.DeepEqual(fromJSON, p) {
		t.Fatalf("%v\n%s", problems, raw)
	}
}

func TestParsePointsAtTheLine(t *testing.T) {
	// at is the text on the line the problem must point at; empty for any.
	cases := []struct {
		name, from, to, path, at string
	}{
		{"image", "image: postgres:16", "image: 'postgres 16'", "services[1].image", "image: 'postgres 16'"},
		{"loop", "    port: 5432\n", "    port: 5432\n    dependsOn: [web]\n", "services", "services:"},
		{"address", "      LOG: debug", "      LOG: \"{{host:db}}\"", "services[0].env.LOG", "LOG:"},
		{"secret", "      LOG: debug", "      LOG: \"{{secret:x}}\"", "services[0].env.LOG", "LOG:"},
		{"input", "{{input:admin-email}}", "{{input:other}}", "env.ADMIN_EMAIL", "ADMIN_EMAIL:"},
		{"unknown", "    port: 5432\n", "    port: 5432\n    replica: 2\n", "replica", "replica: 2"},
		{"type", "    port: 5432\n", "    port: \"x\"\n", "services[1].port", "port: \"x\""},
		{"size", "  - path: /cache", "  - path: /cache\n        sizeGi: 2000", "services[0].volumes[1].sizeGi", "sizeGi: 2000"},
		{"syntax", "    port: 3000\n", "    port: 3000\n   bad\n", "", ""},
	}
	for _, c := range cases {
		if !strings.Contains(sample, c.from) {
			t.Fatalf("%s: %q is not in the sample", c.name, c.from)
		}
		text := strings.Replace(sample, c.from, c.to, 1)
		_, problems := Parse([]byte(text))
		if len(problems) == 0 {
			t.Fatalf("%s: no problem", c.name)
		}
		found := false
		for _, problem := range problems {
			if problem.Path == c.path && problem.Line > 0 && (c.at == "" || problem.Line == lineWith(text, c.at)) && problem.Message != "" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: %+v", c.name, problems)
		}
	}
}

func lineWith(text, needle string) int {
	for i, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return -1
}

func TestExpandFillsEveryValueOnce(t *testing.T) {
	p, problems := Parse([]byte(sample))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.Env["DB_URL"] = "postgres://wiki:{{secret:db}}@db/wiki"
	if _, _, err := Expand(p, "wiki.exemple.fr", nil); err == nil {
		t.Fatal("a question without answer is accepted")
	}
	services, env, err := Expand(p, "wiki.exemple.fr", map[string]string{"admin-email": "moi@exemple.fr"})
	if err != nil {
		t.Fatal(err)
	}
	if env["ADMIN_EMAIL"] != "moi@exemple.fr" || len(env["DB_PASSWORD"]) != 64 || env["DB_URL"] != "postgres://wiki:"+env["DB_PASSWORD"]+"@db/wiki" {
		t.Fatal(env)
	}
	if services[0].Files[0].Content != "[server]\nurl = {{url}}\n" || p.Services[0].Files[0].Content != "[server]\nurl = {{url}}\n" {
		t.Fatalf("%+v", services[0].Files)
	}
}

func TestNothingWaitsForAServiceWithoutPort(t *testing.T) {
	p, _ := Parse([]byte(sample))
	p.Services[1].NoPort = true
	p.Services[1].Port = 0
	problems := Check(p)
	if len(problems) != 1 || problems[0].Path != "services[0].dependsOn[0]" {
		t.Fatal(problems)
	}
	p.Services[0].NoPort = true
	if len(Check(p)) < 2 {
		t.Fatal("a public service without port is accepted")
	}
}
