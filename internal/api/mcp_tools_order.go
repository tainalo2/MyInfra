package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"myinfra/internal/blueprint"
	"myinfra/internal/catalog"
	"myinfra/internal/offerfind"
	"myinfra/internal/place"
	"myinfra/internal/provision"
	"myinfra/internal/store"
)

func offerWantSchema() map[string]any {
	return map[string]any{
		"providers":         list("Providers ou comptes acceptés (hetzner, scaleway, ovh… ou le nom d'un compte). Vide : tous.", text("")),
		"countries":         list("Pays acceptés : code ISO (FR, DE) ou nom (France).", text("")),
		"continents":        list("Continents acceptés.", choice("", place.Continents...)),
		"locations":         list("Lieux précis acceptés : identifiant du provider (fsn1, fr-par-1, GRA) ou ville (Paris, Helsinki).", text("")),
		"min_vcpu":          whole("Processeurs virtuels au moins.", 1, 512),
		"min_memory_gb":     number("Mémoire en Go au moins."),
		"min_disk_gb":       whole("Disque en Go au moins ; un volume est ajouté si l'offre le permet.", 1, 100000),
		"min_gpus":          whole("Cartes graphiques au moins. Absent : pas de contrainte.", 1, 64),
		"gpu":               text("Modèle de carte, par exemple L4 ou A40. Absent : n'importe quelle carte, si min_gpus est fixé."),
		"max_monthly_price": number("Prix mensuel maximum par machine."),
		"currency":          choice("Devise du prix maximum (EUR par défaut). Les prix ne sont jamais convertis.", "EUR", "USD"),
		"model":             text("Modèle exact, par son nom ou son identifiant (CX33, b3-8, s-2vcpu-4gb)."),
		"period":            choice("Facturation : au mois (par défaut quand l'offre le permet) ou à l'heure.", "month", "hour"),
		"quantity":          whole("Nombre de machines voulues.", 1, 10),
	}
}

type offerWantArgs struct {
	Providers       []string `json:"providers"`
	Countries       []string `json:"countries"`
	Continents      []string `json:"continents"`
	Locations       []string `json:"locations"`
	MinVCPU         int      `json:"min_vcpu"`
	MinMemoryGB     float64  `json:"min_memory_gb"`
	MinDiskGB       int      `json:"min_disk_gb"`
	MinGPUs         int      `json:"min_gpus"`
	GPU             string   `json:"gpu"`
	MaxMonthlyPrice float64  `json:"max_monthly_price"`
	Currency        string   `json:"currency"`
	Model           string   `json:"model"`
	Period          string   `json:"period"`
	Quantity        int      `json:"quantity"`
}

func (w offerWantArgs) want() offerfind.Want {
	return offerfind.Want{
		Providers: w.Providers, Countries: w.Countries, Continents: w.Continents, Locations: w.Locations,
		MinVCPU: w.MinVCPU, MinMemoryGB: w.MinMemoryGB, MinDiskGB: w.MinDiskGB, MinGPUs: w.MinGPUs, GPU: w.GPU, MaxMonthly: w.MaxMonthlyPrice,
		Currency: w.Currency, Model: w.Model, Period: w.Period, Quantity: w.Quantity,
	}
}

func matchBrief(m offerfind.Match) map[string]any {
	out := map[string]any{
		"account": m.Account, "provider": m.Provider, "model": m.Offer.Name, "location": m.Location.ID,
		"vcpu": m.Offer.VCPU, "memoryGb": float64(m.Offer.MemoryMB) / 1024, "diskGb": m.DiskGB,
		"period": m.Period, "monthly": moneyText(m.Monthly, m.Currency),
	}
	if m.Offer.GPUs > 0 {
		out["gpus"] = m.Offer.GPUs
		out["gpu"] = m.Offer.GPU
	}
	if m.Offer.ID != m.Offer.Name {
		out["modelId"] = m.Offer.ID
	}
	if m.Location.Label != "" {
		out["locationLabel"] = m.Location.Label
	}
	if m.Place != nil {
		out["city"] = m.Place.Label
		out["country"] = m.Place.Country
	}
	if m.Volume != nil {
		out["volume"] = fmt.Sprintf("%s de %d Go (%s/mois, compris)", m.Volume.Label, m.Volume.GB, moneyText(m.Volume.Monthly, m.Currency))
	}
	if len(m.Gaps) > 0 {
		out["gaps"] = m.Gaps
	}
	return out
}

func mcpOrderTools() []mcpTool {
	findSchema := offerWantSchema()
	orderSchema := map[string]any{
		"account":        text("Compte provider de l'offre choisie (champ account de find_server_offers)."),
		"location":       text("Lieu de l'offre (champ location de find_server_offers)."),
		"model":          text("Modèle de l'offre (champ model ou modelId de find_server_offers)."),
		"quantity":       whole("Nombre de machines (1 par défaut).", 1, 10),
		"names":          list("Noms des machines, un par machine. Par défaut : préfixe-1, préfixe-2…", text("")),
		"name_prefix":    text("Préfixe des noms quand names est vide (par défaut le nom du cluster, ou « serveur »)."),
		"period":         choice("Facturation, au mois par défaut quand l'offre le permet.", "month", "hour"),
		"min_disk_gb":    whole("Disque voulu en Go : un volume est ajouté si l'offre en a moins.", 1, 100000),
		"cluster":        text("Cluster existant où relier les machines."),
		"new_cluster":    text("Nom d'un nouveau cluster à créer avec ces machines."),
		"control_planes": whole("Plans de contrôle parmi les nouvelles machines : 1 ou 3 (nombre impair). Par défaut 1 pour un nouveau cluster, aucun pour un cluster qui en a déjà.", 0, 5),
		"ssh_keys":       list("Clés SSH à poser, par leur nom. Par défaut toutes.", text("")),
		"project":        text("Projet du compte (OVH, DigitalOcean), quand il en a plusieurs."),
		"confirmation":   confirmationArg,
	}
	return []mcpTool{
		{
			Name: "find_server_offers", Title: "Chercher des machines à acheter", Role: store.RoleRead,
			Description: "Cherche, chez les comptes providers enregistrés, les offres qui respectent les contraintes (provider, pays, continent, lieu, processeur, mémoire, disque, cartes graphiques, prix maximum, modèle exact), de la moins chère à la plus chère. Si aucune ne convient, rend les plus proches avec ce qui leur manque, et les raisons (compte absent, modèle inconnu…). Ne fait aucun achat.",
			Schema:      object(nil, findSchema),
			Routes:      []string{"POST /api/offers/search"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[offerWantArgs](raw)
				if err != nil {
					return "", err
				}
				var view offerSearchView
				if err := c.send(http.MethodPost, "/api/offers/search", args.want(), &view); err != nil {
					return "", err
				}
				out := map[string]any{"matches": []map[string]any{}, "alternatives": []map[string]any{}}
				for _, m := range view.Matches {
					out["matches"] = append(out["matches"].([]map[string]any), matchBrief(m))
				}
				for _, m := range view.Alternatives {
					out["alternatives"] = append(out["alternatives"].([]map[string]any), matchBrief(m))
				}
				if len(view.Matches) == 0 {
					out["message"] = "Aucune offre ne respecte toutes les contraintes. Les alternatives sont les plus proches ; chaque écart est dans gaps."
				} else {
					out["next"] = "Pour acheter, appelez order_servers avec account, location et model d'une offre."
				}
				for key, value := range map[string][]string{"impossible": view.Impossible, "notes": view.Notes, "warnings": view.Warnings} {
					if len(value) > 0 {
						out[key] = value
					}
				}
				out["searched"] = len(view.Searched)
				return asJSON(out), nil
			},
		},
		{
			Name: "order_servers", Title: "Acheter des machines", Role: store.RoleDeploy, Write: true, Destructive: true,
			Description: "Achète une ou plusieurs machines d'une offre précise (trouvée par find_server_offers), seules ou reliées à un cluster existant ou nouveau. Deux appels : le premier rend le récapitulatif de la commande (machines, provider, lieu, prix, coût théorique actuel et après achat, budget) et un code ; le second, avec le code, après l'accord explicite de l'utilisateur, passe la commande.",
			Schema:      object([]string{"account", "location", "model"}, orderSchema),
			Routes: []string{
				"GET /api/secrets", "GET /api/clusters", "GET /api/servers", "GET /api/costs",
				"GET /api/providers/{id}/offers", "GET /api/providers/{id}/projects",
				"POST /api/servers", "POST /api/templates", "POST /api/templates/{id}/runs",
			},
			Run: runOrder,
		},
		{
			Name: "get_order", Title: "Suivre une commande", Role: store.RoleRead,
			Description: "L'avancement d'une commande de machines : création chez le provider, puis installation, étape par étape.",
			Schema:      object([]string{"order"}, map[string]any{"order": text("Identifiant rendu par order_servers.")}),
			Routes:      []string{"GET /api/template-runs/{id}", "GET /api/servers/{id}"},
			Run: func(c *mcpCall, raw json.RawMessage) (string, error) {
				args, err := decodeArgs[struct{ Order string }](raw)
				if err != nil {
					return "", err
				}
				if id, ok := strings.CutPrefix(args.Order, "run:"); ok {
					var out any
					if err := c.get("/api/template-runs/"+url.PathEscape(id), &out); err != nil {
						return "", err
					}
					return asJSON(out), nil
				}
				ids, ok := strings.CutPrefix(args.Order, "servers:")
				if !ok {
					return "", failf("Identifiant de commande illisible : il commence par run: ou servers:.")
				}
				var out []map[string]any
				for _, id := range strings.Split(ids, ",") {
					var row serverView
					if err := c.get("/api/servers/"+url.PathEscape(id), &row); err != nil {
						return "", err
					}
					out = append(out, map[string]any{"name": row.Name, "status": row.Status, "address": row.Address, "steps": row.Deploy})
				}
				return asJSON(out), nil
			},
		},
	}
}

type orderArgs struct {
	Account       string   `json:"account"`
	Location      string   `json:"location"`
	Model         string   `json:"model"`
	Quantity      int      `json:"quantity"`
	Names         []string `json:"names"`
	NamePrefix    string   `json:"name_prefix"`
	Period        string   `json:"period"`
	MinDiskGB     int      `json:"min_disk_gb"`
	Cluster       string   `json:"cluster"`
	NewCluster    string   `json:"new_cluster"`
	ControlPlanes *int     `json:"control_planes"`
	SSHKeys       []string `json:"ssh_keys"`
	Project       string   `json:"project"`
	Confirmation  string   `json:"confirmation"`
}

// orderPlan is an order made ready: everything the recap shows and the
// purchase uses.
type orderPlan struct {
	account   secretView
	match     offerfind.Match
	names     []string
	planes    []bool
	keys      []secretView
	cluster   *named
	newName   string
	projectID string
	before    amounts
	budget    float64
}

func runOrder(c *mcpCall, raw json.RawMessage) (string, error) {
	args, err := decodeArgs[orderArgs](raw)
	if err != nil {
		return "", err
	}
	var plan orderPlan
	recap := func() (string, string, error) {
		var err error
		plan, err = c.planOrder(args)
		if err != nil {
			return "", "", err
		}
		return plan.recap(), fingerprintOf([]any{plan.match.Offer, plan.match.Volume, plan.match.Period, plan.names, plan.planes, plan.projectID}), nil
	}
	return c.confirmed(raw, args.Confirmation, recap, func() (string, error) { return c.placeOrder(plan) })
}

func (c *mcpCall) planOrder(args orderArgs) (orderPlan, error) {
	var plan orderPlan
	if args.Cluster != "" && args.NewCluster != "" {
		return plan, failf("Choisissez un cluster existant ou un nouveau cluster, pas les deux.")
	}
	quantity := max(args.Quantity, 1)
	accounts, err := c.secretsOf("provider_token")
	if err != nil {
		return plan, err
	}
	plan.account, err = pick("compte provider", args.Account, accounts, func(s secretView) named { return named{s.ID, s.Name} })
	if err != nil {
		return plan, err
	}
	keys, err := c.secretsOf("ssh_key")
	if err != nil {
		return plan, err
	}
	if len(keys) == 0 {
		return plan, failf("Aucune clé SSH n'est enregistrée : ajoutez-en une dans Réglages › Clés SSH avant d'acheter une machine.")
	}
	if len(args.SSHKeys) == 0 {
		plan.keys = keys
	} else {
		for _, name := range args.SSHKeys {
			key, err := pick("clé SSH", name, keys, func(s secretView) named { return named{s.ID, s.Name} })
			if err != nil {
				return plan, err
			}
			plan.keys = append(plan.keys, key)
		}
	}
	if needsProject(plan.account.Provider) {
		var projects []provision.Project
		if err := c.get("/api/providers/"+url.PathEscape(plan.account.ID)+"/projects", &projects); err != nil {
			return plan, err
		}
		project, err := pick("projet", firstNonEmpty(args.Project, onlyProject(projects)), projects, func(p provision.Project) named { return named{p.ID, p.Name} })
		if err != nil {
			return plan, err
		}
		plan.projectID = project.ID
	}
	query := url.Values{"location": {args.Location}}
	if plan.projectID != "" {
		query.Set("project", plan.projectID)
	}
	var cat catalog.Catalog
	if err := c.get("/api/providers/"+url.PathEscape(plan.account.ID)+"/offers?"+query.Encode(), &cat); err != nil {
		return plan, err
	}
	if !strings.EqualFold(cat.Location, args.Location) {
		return plan, failf("Le lieu « %s » n'existe pas chez %s. Lieux : %s.", args.Location, plan.account.Name, locationIDs(cat.Locations))
	}
	var candidates []offerfind.Candidate
	for _, o := range cat.Offers {
		candidates = append(candidates, offerfind.Candidate{AccountID: plan.account.ID, Account: plan.account.Name, Provider: plan.account.Provider, Currency: cat.Currency, Location: catalog.Location{ID: cat.Location, Label: labelOf(cat.Locations, cat.Location)}, Offer: o})
	}
	for i := range candidates {
		if p, ok := place.Of(candidates[i].Location.ID, candidates[i].Location.Label); ok {
			candidates[i].Place = &p
		}
	}
	found := offerfind.Find(candidates, offerfind.Want{Model: args.Model, Period: args.Period, MinDiskGB: args.MinDiskGB, Currency: cat.Currency})
	if len(found.Matches) == 0 {
		return plan, failf("Le modèle « %s » n'est pas proposé par %s à %s pour cette période, ou sans le disque demandé. Cherchez avec find_server_offers.", args.Model, plan.account.Name, args.Location)
	}
	plan.match = found.Matches[0]

	servers, err := c.serversList()
	if err != nil {
		return plan, err
	}
	existingPlane := false
	switch {
	case args.Cluster != "":
		cluster, err := c.cluster(args.Cluster)
		if err != nil {
			return plan, err
		}
		plan.cluster = &cluster
		for _, row := range servers {
			if row.ClusterID == cluster.ID && row.ControlPlane {
				existingPlane = true
			}
		}
	case args.NewCluster != "":
		name, ok := cleanName(args.NewCluster)
		if !ok {
			return plan, failf("Donnez un nom au nouveau cluster.")
		}
		var clusters []named
		if err := c.get("/api/clusters", &clusters); err != nil {
			return plan, err
		}
		for _, row := range clusters {
			if strings.EqualFold(row.Name, name) {
				return plan, failf("Un cluster s'appelle déjà « %s » : utilisez cluster=%s pour y ajouter des machines.", row.Name, row.Name)
			}
		}
		plan.newName = name
	}
	plan.names = args.Names
	if len(plan.names) == 0 {
		prefix := strings.TrimSpace(args.NamePrefix)
		if prefix == "" {
			prefix = "serveur"
			if plan.newName != "" {
				prefix = plan.newName
			} else if plan.cluster != nil {
				prefix = plan.cluster.Name
			}
		}
		prefix = strings.ToLower(strings.ReplaceAll(prefix, " ", "-"))
		taken := map[string]bool{}
		for _, row := range servers {
			taken[strings.ToLower(row.Name)] = true
		}
		for i := 1; len(plan.names) < quantity; i++ {
			if name := fmt.Sprintf("%s-%d", prefix, i); !taken[name] {
				plan.names = append(plan.names, name)
			}
		}
	}
	if len(plan.names) != quantity && args.Quantity > 0 {
		return plan, failf("%d noms pour %d machines.", len(plan.names), quantity)
	}
	seen := map[string]bool{}
	for _, name := range plan.names {
		cleaned, ok := cleanName(name)
		if !ok || seen[strings.ToLower(cleaned)] {
			return plan, failf("Nom de machine vide, trop long ou en double : %q.", name)
		}
		seen[strings.ToLower(cleaned)] = true
		for _, row := range servers {
			if strings.EqualFold(row.Name, cleaned) {
				return plan, failf("Une machine s'appelle déjà « %s ».", row.Name)
			}
		}
	}
	planes := 0
	if args.ControlPlanes != nil {
		planes = *args.ControlPlanes
	} else if plan.newName != "" || (plan.cluster != nil && !existingPlane) {
		planes = 1
	}
	if plan.cluster == nil && plan.newName == "" && planes > 0 {
		return plan, failf("Des plans de contrôle n'ont de sens que dans un cluster : précisez cluster ou new_cluster.")
	}
	if planes > len(plan.names) {
		return plan, failf("%d plans de contrôle pour %d machines.", planes, len(plan.names))
	}
	if planes > 0 && planes%2 == 0 {
		return plan, failf("Le nombre de plans de contrôle doit être impair (1, 3 ou 5) : avec un nombre pair, la perte d'une machine arrête le cluster.")
	}
	if plan.newName != "" && planes == 0 {
		return plan, failf("Un nouveau cluster a besoin d'au moins un plan de contrôle.")
	}
	plan.planes = make([]bool, len(plan.names))
	for i := 0; i < planes; i++ {
		plan.planes[i] = true
	}
	var costs costsView
	if err := c.get("/api/costs", &costs); err != nil {
		return plan, err
	}
	plan.before = amountsOf(costs.Monthly)
	plan.budget = costs.Budget
	return plan, nil
}

func (c *mcpCall) serversList() ([]serverView, error) {
	var rows []serverView
	err := c.get("/api/servers", &rows)
	return rows, err
}

func onlyProject(projects []provision.Project) string {
	if len(projects) == 1 {
		return projects[0].ID
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func locationIDs(locs []catalog.Location) string {
	var ids []string
	for _, l := range locs {
		ids = append(ids, l.ID)
	}
	return strings.Join(ids, ", ")
}

func labelOf(locs []catalog.Location, id string) string {
	for _, l := range locs {
		if l.ID == id {
			return l.Label
		}
	}
	return id
}

func (p orderPlan) added() amounts {
	out := amounts{}
	out.add(p.match.Currency, p.match.Theoretical*float64(len(p.names)))
	return out
}

func (p orderPlan) recap() string {
	m := p.match
	var b strings.Builder
	target := "machines seules (sans cluster)"
	if len(p.names) == 1 {
		target = "machine seule (sans cluster)"
	}
	switch {
	case p.newName != "":
		target = "nouveau cluster « " + p.newName + " »"
	case p.cluster != nil:
		target = "cluster « " + p.cluster.Name + " »"
	}
	fmt.Fprintf(&b, "Commande : %d machine(s) → %s\n\n", len(p.names), target)
	where := m.Location.ID
	if m.Location.Label != "" && m.Location.Label != m.Location.ID {
		where += " – " + m.Location.Label
	}
	if m.Place != nil {
		if country, ok := place.CountryOf(m.Place.Country); ok {
			where += " (" + country.Name + ")"
		}
	}
	for i, name := range p.names {
		role := ""
		if p.planes[i] {
			role = ", plan de contrôle"
		}
		fmt.Fprintf(&b, " %d. %s : %s / %s, %s, %d vCPU, %s Go RAM, %d Go disque, %s, %s/%s%s\n",
			i+1, name, p.account.Provider, p.account.Name, m.Offer.Name, m.Offer.VCPU, trimGB(m.Offer.MemoryMB), m.DiskGB, where,
			moneyText(m.Monthly, m.Currency), map[string]string{"month": "mois", "hour": "mois (facturé à l'heure)"}[m.Period], role)
	}
	if m.Volume != nil {
		fmt.Fprintf(&b, "Volume ajouté à chaque machine : %s de %d Go, %s/mois, compris ci-dessus.\n", m.Volume.Label, m.Volume.GB, moneyText(m.Volume.Monthly, m.Currency))
	}
	total := amounts{}
	total.add(m.Currency, m.Monthly*float64(len(p.names)))
	fmt.Fprintf(&b, "\nTotal de la commande : %s\n", total)
	after := p.before.plus(p.added())
	fmt.Fprintf(&b, "Coût théorique actuel : %s\n", p.before)
	fmt.Fprintf(&b, "Coût théorique après achat : %s (+%s)\n", after, p.added())
	b.WriteString("(Le coût théorique est celui de la page d'accueil : prix horaire × 730 h, machines seules")
	if m.Volume != nil {
		b.WriteString(", sans les volumes")
	}
	b.WriteString(". La facture réelle peut différer : plafond mensuel, taxes.)\n")
	if p.budget > 0 {
		state := "respecté"
		if after[currencyOf("EUR")] > p.budget {
			state = "DÉPASSÉ"
		}
		fmt.Fprintf(&b, "Budget mensuel (MYINFRA_BUDGET_MONTHLY) : %s — %s\n", moneyText(p.budget, "EUR"), state)
	}
	var keyNames []string
	for _, k := range p.keys {
		keyNames = append(keyNames, k.Name)
	}
	fmt.Fprintf(&b, "Clés SSH posées : %s\n", strings.Join(keyNames, ", "))
	if p.projectID != "" {
		fmt.Fprintf(&b, "Projet : %s\n", p.projectID)
	}
	b.WriteString("Les machines sont créées chez le provider et facturées dès leur création.\n")
	return b.String()
}

func trimGB(mb int) string {
	gb := float64(mb) / 1024
	if gb == float64(int(gb)) {
		return fmt.Sprintf("%d", int(gb))
	}
	return strings.Replace(fmt.Sprintf("%.1f", gb), ".", ",", 1)
}

func (c *mcpCall) placeOrder(p orderPlan) (string, error) {
	var keyIDs []string
	for _, k := range p.keys {
		keyIDs = append(keyIDs, k.ID)
	}
	storageID, storageGB := "", 0
	if p.match.Volume != nil {
		storageID, storageGB = p.match.Volume.StorageID, p.match.Volume.GB
	}
	if p.cluster == nil && p.newName == "" {
		var ids []string
		for _, name := range p.names {
			var row serverView
			err := c.send(http.MethodPost, "/api/servers", map[string]any{
				"name": name, "keyIds": keyIDs, "providerId": p.account.ID, "offerId": p.match.Offer.ID,
				"location": p.match.Location.ID, "period": p.match.Period, "storageId": storageID, "storageGb": storageGB,
				"projectId": p.projectID,
			}, &row)
			if err != nil {
				if len(ids) > 0 {
					return "", failf("Commande interrompue après %d machine(s) créée(s) (suivi : servers:%s) : %v", len(ids), strings.Join(ids, ","), err)
				}
				return "", err
			}
			ids = append(ids, row.ID)
		}
		return fmt.Sprintf("Commande passée : %d machine(s) en cours de création. Suivez-la avec get_order (order=\"servers:%s\").", len(ids), strings.Join(ids, ",")), nil
	}
	var servers []blueprint.Server
	for i, name := range p.names {
		servers = append(servers, blueprint.Server{
			Name: name, ProviderID: p.account.ID, Location: p.match.Location.ID, ProjectID: p.projectID,
			OfferID: p.match.Offer.ID, Period: p.match.Period, StorageID: storageID, StorageGB: storageGB,
			KeyIDs: keyIDs, ControlPlane: p.planes[i],
		})
	}
	clusterLabel := p.newName
	if p.cluster != nil {
		clusterLabel = p.cluster.Name
	}
	body := map[string]any{"servers": servers}
	if p.newName != "" {
		body["clusterName"] = p.newName
	}
	template := map[string]any{"name": clip("Commande MCP – "+clusterLabel+" – "+time.Now().Format("2006-01-02 15:04"), 80), "servers": servers}
	if p.newName != "" {
		template["clusterName"] = p.newName
	}
	var made templateView
	if err := c.send(http.MethodPost, "/api/templates", template, &made); err != nil {
		return "", err
	}
	if p.cluster != nil {
		body["targetClusterId"] = p.cluster.ID
	}
	var run runView
	if err := c.send(http.MethodPost, "/api/templates/"+url.PathEscape(made.ID)+"/runs", body, &run); err != nil {
		return "", err
	}
	return fmt.Sprintf("Commande passée : %d machine(s) pour le cluster « %s », créées puis installées l'une après l'autre. Le modèle « %s » en garde la trace. Suivez-la avec get_order (order=\"run:%s\").", len(p.names), clusterLabel, made.Name, run.ID), nil
}
