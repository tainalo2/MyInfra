package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"myinfra/internal/dns"
	"myinfra/internal/dnsplan"
	"myinfra/internal/store"
)

type dnsArgs struct {
	Zone         string   `json:"zone"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Values       []string `json:"values"`
	TTL          int      `json:"ttl"`
	Proxied      *bool    `json:"proxied"`
	Confirmation string   `json:"confirmation"`
}

// change reads the values as people write them: an MX as "10 mail.host".
func (d dnsArgs) change(remove bool) (dnsplan.Change, error) {
	c := dnsplan.Change{Zone: d.Zone, Name: d.Name, Type: strings.ToUpper(strings.TrimSpace(d.Type)), TTL: d.TTL, Proxied: d.Proxied, Delete: remove}
	for _, raw := range d.Values {
		v := dns.Value{Content: strings.TrimSpace(raw)}
		if c.Type == "MX" {
			prio, host, ok := strings.Cut(v.Content, " ")
			n, err := strconv.Atoi(prio)
			if !ok || err != nil {
				return c, failf("Un MX s'écrit « priorité nom », par exemple « 10 mail.protonmail.ch ».")
			}
			v = dns.Value{Content: strings.TrimSpace(host), Priority: n}
		}
		c.Values = append(c.Values, v)
	}
	return c, nil
}

func (c *mcpCall) planDNS(change dnsplan.Change) (dnsplan.Plan, error) {
	var plan dnsplan.Plan
	err := c.send(http.MethodPost, "/api/dns/records/plan", change, &plan)
	return plan, err
}

var dnsTypes = []string{"A", "AAAA", "CNAME", "TXT", "MX"}

func dnsChangeSchema(withValues bool) map[string]any {
	props := map[string]any{
		"name": text("Nom complet, par exemple docs.moydomain.name, ou moydomain.name pour la racine."),
		"type": choice("Type d'enregistrement.", dnsTypes...),
		"zone": text("Zone, si le nom pouvait appartenir à plusieurs ; trouvée seule sinon."),
	}
	required := []string{"name", "type"}
	if withValues {
		props["values"] = list("Valeurs voulues pour ce nom et ce type, qui remplacent les actuelles : adresses pour A/AAAA, une cible pour CNAME, textes pour TXT, « 10 mail.hôte » pour MX.", text(""))
		props["ttl"] = whole("TTL en secondes (60 à 86400). Par défaut celui des enregistrements remplacés, ou celui du provider.", 60, 86400)
		props["proxied"] = flag("Cloudflare seulement : passer par le proxy de Cloudflare (A, AAAA, CNAME). Par défaut, inchangé.")
		required = append(required, "values")
	}
	return object(required, props)
}

func mcpDNSTools() []mcpTool {
	setSchema := dnsChangeSchema(true)
	setSchema["properties"].(map[string]any)["confirmation"] = confirmationArg
	deleteSchema := dnsChangeSchema(false)
	deleteSchema["properties"].(map[string]any)["confirmation"] = confirmationArg
	return []mcpTool{
		{
			Name: "list_dns_records", Title: "Enregistrements DNS", Role: store.RoleRead,
			Description: "Les enregistrements d'une zone (tous types, 500 au plus), ou ceux d'un nom. usedBy dit quelle application MyInfra se sert d'un nom.",
			Schema: object([]string{"zone"}, map[string]any{
				"zone": text("Zone, par exemple moydomain.name (list_dns_zones les donne)."),
				"name": text("Un nom seul de la zone."),
				"type": text("Un type seul (A, CNAME, MX, TXT…)."),
			}),
			Routes: []string{"GET /api/dns/zones/{zone}/records"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[dnsArgs](raw)
				if err != nil {
					return "", err
				}
				query := url.Values{}
				if args.Name != "" {
					query.Set("name", args.Name)
				}
				if args.Type != "" {
					query.Set("type", args.Type)
				}
				var out any
				if err := c.get("/api/dns/zones/"+url.PathEscape(strings.Trim(args.Zone, "."))+"/records?"+query.Encode(), &out); err != nil {
					return "", err
				}
				return asJSON(out), nil
			},
		},
		{
			Name: "plan_dns_change", Title: "Prévoir un changement DNS", Role: store.RoleRead,
			Description: "Dit, sans rien changer, ce qu'écrire ces valeurs ferait : les enregistrements avant et après, ceux qui seraient supprimés, ce que cela touche d'autre (e-mail, certificats, joker, CNAME), ou pourquoi c'est refusé.",
			Schema:      dnsChangeSchema(true),
			Routes:      []string{"POST /api/dns/records/plan"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[dnsArgs](raw)
				if err != nil {
					return "", err
				}
				change, err := args.change(false)
				if err != nil {
					return "", err
				}
				plan, err := c.planDNS(change)
				if err != nil {
					return "", err
				}
				if plan.Refused != "" {
					return "Refusé : " + plan.Refused, nil
				}
				note := "Écrire ces valeurs se fait sans confirmation : aucun enregistrement existant ni nom sensible n'est touché."
				if plan.Confirm {
					note = "Écrire ces valeurs demandera l'accord de l'utilisateur."
				}
				return plan.Recap() + "\n" + note, nil
			},
		},
		{
			Name: "set_dns_record", Title: "Écrire un enregistrement DNS", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Donne à un nom et un type exactement ces valeurs, chez le provider DNS. Un nom libre se crée tout de suite. Si des enregistrements existants sont remplacés ou supprimés, ou si le nom est sensible (racine, MX, SPF/DKIM/DMARC, certificats, joker masqué), le premier appel rend le récapitulatif avant/après et un code ; le second, avec le code, après l'accord de l'utilisateur, écrit. Les noms des applications MyInfra se changent avec set_domains.",
			Schema:      setSchema,
			Routes:      []string{"POST /api/dns/records/plan", "PUT /api/dns/records"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[dnsArgs](raw)
				if err != nil {
					return "", err
				}
				change, err := args.change(false)
				if err != nil {
					return "", err
				}
				return c.dnsWrite(raw, args.Confirmation, change, http.MethodPut)
			},
		},
		{
			Name: "delete_dns_record", Title: "Supprimer un enregistrement DNS", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Supprime tous les enregistrements d'un nom et d'un type. Deux appels : le récapitulatif de ce qui part et un code, puis la suppression avec le code après l'accord de l'utilisateur.",
			Schema:      deleteSchema,
			Routes:      []string{"POST /api/dns/records/plan", "DELETE /api/dns/records"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[dnsArgs](raw)
				if err != nil {
					return "", err
				}
				change, err := args.change(true)
				if err != nil {
					return "", err
				}
				return c.dnsWrite(raw, args.Confirmation, change, http.MethodDelete)
			},
		},
		{
			Name: "republish_dns_name", Title: "Republier le nom d'une application", Role: store.RoleAdmin, Write: true,
			Description: "Réécrit les adresses d'un nom que MyInfra gère (celui d'une application), quand elles ont été changées à la main chez le provider.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom complet.")}),
			Routes:      []string{"GET /api/dns/zones", "POST /api/dns/records/publish"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[dnsArgs](raw)
				if err != nil {
					return "", err
				}
				zone, err := c.zoneOf(args.Name)
				if err != nil {
					return "", err
				}
				if err := c.send(http.MethodPost, "/api/dns/records/publish", map[string]string{"zone": zone, "name": args.Name}, nil); err != nil {
					return "", err
				}
				return args.Name + " republié avec les adresses de son application.", nil
			},
		},
		{
			Name: "drop_stale_dns_name", Title: "Retirer un nom orphelin", Role: store.RoleAdmin, Write: true, Destructive: true,
			Description: "Retire les adresses (A, AAAA) d'un nom qu'aucune application n'utilise plus, laissé par une machine ou une copie supprimée. Deux appels : ce qui part et un code, puis le retrait avec le code après l'accord de l'utilisateur.",
			Schema:      object([]string{"name"}, map[string]any{"name": text("Nom complet."), "confirmation": confirmationArg}),
			Routes:      []string{"GET /api/dns/zones", "GET /api/dns/zones/{zone}/records", "POST /api/dns/records/drop"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[dnsArgs](raw)
				if err != nil {
					return "", err
				}
				zone, err := c.zoneOf(args.Name)
				if err != nil {
					return "", err
				}
				name := strings.ToLower(strings.Trim(args.Name, "."))
				recap := func() (string, string, error) {
					var rows []map[string]any
					if err := c.get("/api/dns/zones/"+url.PathEscape(zone)+"/records?name="+url.QueryEscape(name), &rows); err != nil {
						return "", "", err
					}
					var lines []string
					for _, row := range rows {
						if row["type"] == "A" || row["type"] == "AAAA" {
							if used, ok := row["usedBy"]; ok {
								return "", "", failf("%s sert à %v : il ne se retire pas comme un nom orphelin.", name, used)
							}
							lines = append(lines, "  "+row["type"].(string)+" "+row["content"].(string))
						}
					}
					if len(lines) == 0 {
						return "", "", failf("%s n'a aucune adresse à retirer.", name)
					}
					return "Retrait du nom orphelin " + name + " (zone " + zone + ") :\n" + strings.Join(lines, "\n") + "\n", fingerprintOf(lines), nil
				}
				return c.confirmed(raw, args.Confirmation, recap, func() (string, error) {
					if err := c.send(http.MethodPost, "/api/dns/records/drop", map[string]string{"zone": zone, "name": name}, nil); err != nil {
						return "", err
					}
					return "Adresses de " + name + " retirées.", nil
				})
			},
		},
	}
}

// dnsWrite plans the change, asks first when the plan says so, and writes.
func (c *mcpCall) dnsWrite(raw json.RawMessage, code string, change dnsplan.Change, method string) (string, error) {
	plan, err := c.planDNS(change)
	if err != nil {
		return "", err
	}
	if plan.Refused != "" {
		return "", failf("%s", plan.Refused)
	}
	if plan.Noop {
		return "Rien à changer : " + plan.Change.Name + " a déjà ces enregistrements.", nil
	}
	apply := func() (string, error) {
		var done dnsplan.Plan
		if err := c.send(method, "/api/dns/records", change, &done); err != nil {
			return "", err
		}
		return "Fait.\n" + done.Recap(), nil
	}
	if !plan.Confirm {
		return apply()
	}
	recap := func() (string, string, error) {
		fresh, err := c.planDNS(change)
		if err != nil {
			return "", "", err
		}
		if fresh.Refused != "" {
			return "", "", failf("%s", fresh.Refused)
		}
		return fresh.Recap(), fingerprintOf([]any{fresh.Before, fresh.Removes, fresh.Impacts}), nil
	}
	return c.confirmed(raw, code, recap, apply)
}

// zoneOf finds the zone of a name among those of the DNS accounts.
func (c *mcpCall) zoneOf(name string) (string, error) {
	var zones []zoneView
	if err := c.get("/api/dns/zones", &zones); err != nil {
		return "", err
	}
	name = strings.ToLower(strings.Trim(strings.TrimSpace(name), "."))
	best := ""
	var names []string
	for _, z := range zones {
		names = append(names, z.Name)
		if (name == z.Name || strings.HasSuffix(name, "."+z.Name)) && len(z.Name) > len(best) {
			best = z.Name
		}
	}
	if best == "" {
		return "", failf("%s n'est dans aucune zone des comptes DNS. Zones : %s.", name, strings.Join(names, ", "))
	}
	return best, nil
}
