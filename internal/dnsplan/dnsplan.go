// Package dnsplan says what a change to the records of a name would do
// before it is made: the records before and after, what else it touches,
// whether it must be agreed first, or why it is refused. It reads no network.
package dnsplan

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"myinfra/internal/dns"
)

// Change is the record set asked for one name and type; Delete removes it.
type Change struct {
	Zone    string      `json:"zone"`
	Name    string      `json:"name"`
	Type    string      `json:"type"`
	Values  []dns.Value `json:"values,omitempty"`
	TTL     int         `json:"ttl,omitempty"`
	Proxied *bool       `json:"proxied,omitempty"`
	Delete  bool        `json:"delete,omitempty"`
}

// Plan is what a change would do.
type Plan struct {
	Change  Change      `json:"change"`
	Before  []dns.Entry `json:"before"`
	After   []dns.Entry `json:"after"`
	Removes []dns.Entry `json:"removes"`
	// Impacts explain what is touched besides the records asked.
	Impacts []string `json:"impacts"`
	// Confirm is true when existing records or sensitive names are touched.
	Confirm bool `json:"confirm"`
	// Refused, when set, is why the change cannot be made at all.
	Refused string `json:"refused,omitempty"`
	// Noop is true when the provider holds the records asked already.
	Noop bool `json:"noop,omitempty"`
}

var hostname = regexp.MustCompile(`^(\*\.)?([a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9])?\.)*[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9])?$`)

func clean(name string) string { return strings.ToLower(strings.Trim(strings.TrimSpace(name), ".")) }

// Make plans change against the records of the zone (those of the name and
// the wildcards above it at least) and the names MyInfra writes itself,
// each with what uses it.
func Make(change Change, current []dns.Entry, managed map[string]string) Plan {
	change.Zone, change.Name = clean(change.Zone), clean(change.Name)
	change.Type = strings.ToUpper(strings.TrimSpace(change.Type))
	p := Plan{Change: change, Before: []dns.Entry{}, After: []dns.Entry{}, Removes: []dns.Entry{}, Impacts: []string{}}
	for _, e := range current {
		if e.Name == change.Name {
			p.Before = append(p.Before, e)
		}
	}
	if reason := refusal(&p.Change, managed); reason != "" {
		p.Refused = reason
		return p
	}
	change = p.Change
	same := []dns.Entry{}
	others := []dns.Entry{}
	for _, e := range p.Before {
		if e.Type == change.Type {
			same = append(same, e)
		} else {
			others = append(others, e)
		}
	}
	if change.Delete {
		if len(same) == 0 {
			p.Refused = fmt.Sprintf("%s n'a aucun enregistrement %s : rien à supprimer.", change.Name, change.Type)
			return p
		}
		p.Removes = same
		p.After = others
		p.Confirm = true
		p.Impacts = append(p.Impacts, fmt.Sprintf("Supprime %d enregistrement(s) %s de %s.", len(same), change.Type, change.Name))
		sensitive(&p)
		return p
	}
	wanted := map[string]bool{}
	for _, v := range change.Values {
		wanted[valueKey(v)] = true
	}
	held := map[string]bool{}
	for _, e := range same {
		held[valueKey(dns.Value{Content: e.Content, Priority: e.Priority})] = true
	}
	for _, e := range same {
		if !wanted[valueKey(dns.Value{Content: e.Content, Priority: e.Priority})] {
			p.Removes = append(p.Removes, e)
		}
	}
	// A CNAME stands alone on its name: one side or the other goes.
	if change.Type == "CNAME" && len(others) > 0 {
		for _, e := range others {
			p.Removes = append(p.Removes, e)
		}
		p.Impacts = append(p.Impacts, fmt.Sprintf("Un CNAME ne cohabite avec aucun autre enregistrement : les %d autre(s) enregistrement(s) de %s sont supprimés.", len(others), change.Name))
		others = nil
	}
	for _, e := range others {
		if e.Type == "CNAME" {
			p.Removes = append(p.Removes, e)
			p.Impacts = append(p.Impacts, fmt.Sprintf("%s est aujourd'hui un CNAME vers %s : il est supprimé pour faire place au %s.", change.Name, e.Content, change.Type))
		}
	}
	keptOthers := others[:0]
	for _, e := range others {
		if e.Type != "CNAME" {
			keptOthers = append(keptOthers, e)
		}
	}
	p.After = append(p.After, keptOthers...)
	// A replaced set keeps the TTL and the proxy it had, unless asked.
	inheritTTL, inheritProxy := 0, (*bool)(nil)
	if len(same) > 0 {
		inheritTTL, inheritProxy = same[0].TTL, same[0].Proxied
	}
	for _, v := range change.Values {
		ttl := change.TTL
		proxied := inheritProxy
		if ttl == 0 {
			ttl = inheritTTL
		}
		for _, e := range same {
			if valueKey(dns.Value{Content: e.Content, Priority: e.Priority}) == valueKey(v) {
				if ttl == 0 {
					ttl = e.TTL
				}
				proxied = e.Proxied
			}
		}
		if change.Proxied != nil {
			proxied = change.Proxied
		}
		p.After = append(p.After, dns.Entry{Name: change.Name, Type: change.Type, Content: v.Content, Priority: v.Priority, TTL: ttl, Proxied: proxied})
	}
	sortEntries(p.After)
	allHeld := len(held) == len(wanted)
	for key := range wanted {
		if !held[key] {
			allHeld = false
		}
	}
	if allHeld && len(p.Removes) == 0 && change.TTL == 0 && change.Proxied == nil {
		p.Noop = true
		p.Impacts = append(p.Impacts, "Le provider a déjà exactement ces enregistrements : rien à changer.")
		return p
	}
	if len(same) > 0 {
		p.Confirm = true
		p.Impacts = append(p.Impacts, fmt.Sprintf("Remplace les enregistrements %s existants de %s.", change.Type, change.Name))
	}
	if len(p.Removes) > len(same) {
		p.Confirm = true
	}
	if len(p.Before) == 0 {
		if wild := wildcardOver(change.Name, current); wild != "" {
			p.Confirm = true
			p.Impacts = append(p.Impacts, fmt.Sprintf("%s répond aujourd'hui par le joker %s : ce nom ne le suivra plus.", change.Name, wild))
		}
	}
	if use, ok := managed[change.Name]; ok {
		p.Impacts = append(p.Impacts, fmt.Sprintf("%s sert à %s : ses adresses ne changent pas.", change.Name, use))
	}
	sensitive(&p)
	return p
}

// refusal checks the change on its own and puts its values in order.
func refusal(c *Change, managed map[string]string) string {
	if c.Zone == "" || (c.Name != c.Zone && !strings.HasSuffix(c.Name, "."+c.Zone)) {
		return "Ce nom n'est pas dans ce domaine."
	}
	if !hostname.MatchString(c.Name) {
		return "Ce nom est illisible."
	}
	switch c.Type {
	case "NS", "SOA":
		return "Les enregistrements " + c.Type + " pilotent la zone elle-même : ils se changent chez le provider, pas par MyInfra."
	case "A", "AAAA", "CNAME", "TXT", "MX":
	default:
		return "MyInfra n'écrit pas encore les enregistrements " + c.Type + " : faites-le chez le provider."
	}
	if use, ok := managed[c.Name]; ok && (c.Type == "A" || c.Type == "AAAA" || c.Type == "CNAME") {
		return fmt.Sprintf("%s sert à %s : MyInfra écrit ses adresses lui-même. Changez ses domaines depuis l'application (set_domains) plutôt qu'ici.", c.Name, use)
	}
	if c.Delete {
		return ""
	}
	if len(c.Values) == 0 {
		return "Donnez au moins une valeur, ou demandez la suppression."
	}
	if len(c.Values) > 20 {
		return "Vingt valeurs au plus par nom et par type."
	}
	if c.Type == "CNAME" {
		if c.Name == c.Zone {
			return "Un CNAME ne se pose pas à la racine du domaine : utilisez un A ou un AAAA."
		}
		if len(c.Values) != 1 {
			return "Un CNAME n'a qu'une seule cible."
		}
	}
	if c.TTL != 0 && (c.TTL < 60 || c.TTL > 86400) {
		return "Le TTL va de 60 à 86400 secondes."
	}
	seen := map[string]bool{}
	for i, v := range c.Values {
		v.Content = strings.TrimSpace(v.Content)
		switch c.Type {
		case "A":
			ip := net.ParseIP(v.Content)
			if ip == nil || ip.To4() == nil {
				return v.Content + " n'est pas une adresse IPv4."
			}
			v.Content = ip.String()
		case "AAAA":
			ip := net.ParseIP(v.Content)
			if ip == nil || ip.To4() != nil {
				return v.Content + " n'est pas une adresse IPv6."
			}
			v.Content = ip.String()
		case "CNAME", "MX":
			v.Content = clean(v.Content)
			if !hostname.MatchString(v.Content) || strings.HasPrefix(v.Content, "*.") {
				return v.Content + " n'est pas un nom de machine."
			}
			if c.Type == "MX" && (v.Priority < 0 || v.Priority > 65535) {
				return "La priorité d'un MX va de 0 à 65535."
			}
		case "TXT":
			if v.Content == "" || len(v.Content) > 2048 {
				return "Un TXT a de 1 à 2048 caractères."
			}
		}
		if c.Type != "MX" {
			v.Priority = 0
		}
		if seen[valueKey(v)] {
			return "Une valeur est en double."
		}
		seen[valueKey(v)] = true
		c.Values[i] = v
	}
	if c.Proxied != nil && c.Type != "A" && c.Type != "AAAA" && c.Type != "CNAME" {
		c.Proxied = nil
	}
	return ""
}

// sensitive marks the names and types whose change cuts mail or
// certificates when wrong.
func sensitive(p *Plan) {
	c := p.Change
	first := strings.SplitN(c.Name, ".", 2)[0]
	var warn string
	switch {
	case c.Type == "MX":
		warn = "Les MX disent où arrive le courrier du domaine : une erreur coupe la réception des e-mails."
	case first == "_dmarc" || strings.Contains(c.Name, "._domainkey.") || (c.Type == "TXT" && hasSPF(p)):
		warn = "Ces TXT (SPF, DKIM, DMARC) disent qui peut envoyer pour le domaine : une erreur envoie les e-mails en indésirables."
	case first == "_acme-challenge":
		warn = "Ce nom sert à prouver le domaine pour les certificats HTTPS : une erreur bloque leur renouvellement."
	case c.Name == c.Zone:
		warn = "C'est la racine du domaine : le site principal ou la vérification du domaine en dépendent souvent."
	}
	if warn != "" {
		p.Confirm = true
		p.Impacts = append(p.Impacts, warn)
	}
}

func hasSPF(p *Plan) bool {
	for _, v := range p.Change.Values {
		if strings.HasPrefix(strings.ToLower(v.Content), "v=spf1") {
			return true
		}
	}
	for _, e := range p.Removes {
		if strings.HasPrefix(strings.ToLower(e.Content), "v=spf1") {
			return true
		}
	}
	return false
}

// wildcardOver is the wildcard that answers for name now, if any.
func wildcardOver(name string, current []dns.Entry) string {
	parts := strings.Split(name, ".")
	for i := 1; i < len(parts); i++ {
		wild := "*." + strings.Join(parts[i:], ".")
		for _, e := range current {
			if e.Name == wild {
				return wild
			}
		}
	}
	return ""
}

func valueKey(v dns.Value) string { return fmt.Sprintf("%d %s", v.Priority, v.Content) }

func sortEntries(list []dns.Entry) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Type != list[j].Type {
			return list[i].Type < list[j].Type
		}
		if list[i].Priority != list[j].Priority {
			return list[i].Priority < list[j].Priority
		}
		return list[i].Content < list[j].Content
	})
}

// Recap writes a plan for a person to read before agreeing.
func (p Plan) Recap() string {
	var b strings.Builder
	what := "Modification"
	if p.Change.Delete {
		what = "Suppression"
	} else if len(p.Before) == 0 {
		what = "Création"
	}
	fmt.Fprintf(&b, "%s DNS : %s %s (zone %s)\n", what, p.Change.Name, p.Change.Type, p.Change.Zone)
	b.WriteString("\nAvant :\n")
	writeEntries(&b, p.Before)
	b.WriteString("Après :\n")
	writeEntries(&b, p.After)
	if len(p.Removes) > 0 {
		b.WriteString("Supprimés :\n")
		writeEntries(&b, p.Removes)
	}
	for _, impact := range p.Impacts {
		b.WriteString("Impact : " + impact + "\n")
	}
	return b.String()
}

func writeEntries(b *strings.Builder, list []dns.Entry) {
	if len(list) == 0 {
		b.WriteString("  (rien)\n")
		return
	}
	for _, e := range list {
		line := "  " + e.Type + " "
		if e.Type == "MX" {
			line += fmt.Sprintf("%d ", e.Priority)
		}
		line += e.Content
		if e.TTL > 1 {
			line += fmt.Sprintf(" (TTL %d s)", e.TTL)
		}
		if e.Proxied != nil && *e.Proxied {
			line += " [proxy Cloudflare]"
		}
		b.WriteString(line + "\n")
	}
}
