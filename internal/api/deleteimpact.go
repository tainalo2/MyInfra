package api

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"myinfra/internal/store"
)

// A deletion impact says, before anything starts, what a deletion would
// take away: the steps are the very ones the deletion would run.

type impactMachine struct {
	Name     string `json:"name"`
	Address  string `json:"address,omitempty"`
	Provider string `json:"provider,omitempty"`
	Account  string `json:"account,omitempty"`
	Offer    string `json:"offer,omitempty"`
	Location string `json:"location,omitempty"`
	// Destroyed is true for a machine bought through MyInfra: it is
	// destroyed at its provider. A machine added by hand is only forgotten.
	Destroyed bool    `json:"destroyed"`
	Monthly   float64 `json:"monthly,omitempty"`
	Currency  string  `json:"currency,omitempty"`
}

type deleteImpactView struct {
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	HostsMyInfra bool            `json:"hostsMyInfra,omitempty"`
	Machines     []impactMachine `json:"machines"`
	Applications []string        `json:"applications"`
	Domains      []string        `json:"domains"`
	Steps        []string        `json:"steps"`
	Notes        []string        `json:"notes"`
	// Saved is what the machines destroyed cost each month.
	Saved  []amountView `json:"saved"`
	Before []amountView `json:"before"`
	After  []amountView `json:"after"`
}

func (a *API) machineOf(row store.Server) impactMachine {
	view := a.toServer(row)
	out := impactMachine{Name: row.Name, Address: view.Address, Provider: view.Provider, Account: view.ProviderName, Destroyed: row.ProviderServerID != ""}
	if view.Plan != nil {
		out.Offer, out.Location = view.Plan.Name, view.Plan.Location
	}
	if hourly, currency, ok := serverHourly(view); ok {
		out.Monthly, out.Currency = round2(hourly*hoursPerMonth), currency
	}
	return out
}

func (a *API) deletionImpact(ctx context.Context, owner, kind, id string) (deleteImpactView, error) {
	out := deleteImpactView{Kind: kind, Machines: []impactMachine{}, Applications: []string{}, Domains: []string{}, Notes: []string{}}
	saved := amounts{}
	apps, err := a.Store.ListApplications(ctx, owner)
	if err != nil {
		return out, err
	}
	self, selfOK := a.selfRecord(ctx)
	var steps []destructionStep
	var names []store.Name
	switch kind {
	case "server":
		row, err := a.Store.Server(ctx, owner, id)
		if err != nil {
			return out, err
		}
		out.Name = row.Name
		if names, err = a.Store.ServerNames(ctx, owner, row.ID); err != nil {
			return out, err
		}
		steps = serverSteps(row, names)
		out.Machines = append(out.Machines, a.machineOf(row))
		for _, app := range apps {
			if app.ServerID == row.ID {
				out.Applications = append(out.Applications, app.Name)
			}
		}
		if row.ClusterID != "" {
			out.Notes = append(out.Notes, "La machine est d'abord vidée de ses pods (drain) : les applications du cluster continuent sur les autres nœuds s'il en reste.")
			if row.ControlPlane {
				out.Notes = append(out.Notes, "C'est un plan de contrôle du cluster : sans autre plan de contrôle, le cluster ne se pilote plus.")
			}
		}
	case "cluster":
		cluster, err := a.Store.Cluster(ctx, owner, id)
		if err != nil {
			return out, err
		}
		out.Name = cluster.Name
		out.HostsMyInfra = selfOK && self.ClusterID == cluster.ID
		if names, err = a.Store.ClusterNames(ctx, owner, cluster.ID); err != nil {
			return out, err
		}
		rows, err := a.Store.ListServers(ctx, owner)
		if err != nil {
			return out, err
		}
		var members []store.Server
		for _, row := range rows {
			if row.ClusterID == cluster.ID {
				members = append(members, row)
				out.Machines = append(out.Machines, a.machineOf(row))
			}
		}
		steps = clusterSteps(members, names, cluster.ID)
		for _, app := range apps {
			if app.ClusterID == cluster.ID {
				out.Applications = append(out.Applications, app.Name)
			}
		}
		if cluster.Origin == "imported" {
			out.Notes = append(out.Notes, "Le cluster importé reste en place chez lui : MyInfra l'oublie seulement.")
		}
	case "application":
		if id == selfAppID {
			out.Name, out.HostsMyInfra = "MyInfra", true
			break
		}
		app, err := a.Store.Application(ctx, owner, id)
		if err != nil {
			return out, err
		}
		out.Name = app.Name
		out.Applications = append(out.Applications, app.Name)
		if names, err = a.Store.ApplicationNames(ctx, owner, app.ID); err != nil {
			return out, err
		}
		steps = appSteps(app, names)
		for _, disk := range sortedSet(sharedNames(app.Services)) {
			size := 0
			for _, svc := range app.Services {
				for _, volume := range svc.Spec.Volumes {
					if volume.Disk == disk {
						size = max(size, sizeGi(volume.SizeGi))
					}
				}
			}
			out.Notes = append(out.Notes, "Le disque partagé "+disk+" ("+strconv.Itoa(size)+" Gio), avec tous ses fichiers, part avec l'application.")
		}
		if app.DestinationKind == "cluster" {
			steps = append(steps, destructionStep{Label: "Retirer les charges du cluster"})
		}
	}
	for _, name := range names {
		out.Domains = append(out.Domains, name.Hostname)
	}
	sort.Strings(out.Domains)
	for _, step := range steps {
		out.Steps = append(out.Steps, step.Label)
	}
	for _, machine := range out.Machines {
		if machine.Destroyed && machine.Monthly > 0 {
			saved.add(machine.Currency, machine.Monthly)
		}
	}
	if len(out.Applications) > 0 {
		out.Notes = append(out.Notes, "Les disques des applications partent avec elles. Les sauvegardes hors site (S3) sont gardées.")
	}
	if len(out.Domains) > 0 {
		out.Notes = append(out.Notes, "Les noms de domaine listés sont retirés du DNS.")
	}
	before, _, err := a.infraCosts(ctx, owner)
	if err != nil {
		return out, err
	}
	out.Saved, out.Before, out.After = saved.views(), before.views(), before.minus(saved).views()
	return out, nil
}

// recap writes the impact for a person to read before agreeing.
func (v deleteImpactView) recap() string {
	var b strings.Builder
	what := map[string]string{"server": "la machine", "cluster": "le cluster", "application": "l'application"}[v.Kind]
	b.WriteString("Suppression de " + what + " « " + v.Name + " »\n")
	if len(v.Machines) > 0 {
		b.WriteString("\nMachines :\n")
		for _, m := range v.Machines {
			fate := "oubliée par MyInfra (ajoutée à la main, elle reste chez son hébergeur)"
			if m.Destroyed {
				fate = "DÉTRUITE chez le provider"
			}
			line := " - " + m.Name
			if m.Address != "" {
				line += " (" + m.Address + ")"
			}
			if m.Provider != "" {
				line += ", " + m.Provider
				if m.Account != "" {
					line += " / " + m.Account
				}
			}
			if m.Offer != "" {
				line += ", " + m.Offer
			}
			if m.Location != "" {
				line += ", " + m.Location
			}
			if m.Monthly > 0 {
				line += ", " + moneyText(m.Monthly, m.Currency) + "/mois"
			}
			b.WriteString(line + " : " + fate + "\n")
		}
	}
	if len(v.Applications) > 0 {
		b.WriteString("\nApplications supprimées : " + strings.Join(v.Applications, ", ") + "\n")
	}
	if len(v.Domains) > 0 {
		b.WriteString("Noms DNS retirés : " + strings.Join(v.Domains, ", ") + "\n")
	}
	if len(v.Steps) > 0 {
		b.WriteString("\nÉtapes, dans l'ordre :\n")
		for i, step := range v.Steps {
			b.WriteString(" " + strconv.Itoa(i+1) + ". " + step + "\n")
		}
	}
	for _, note := range v.Notes {
		b.WriteString("Note : " + note + "\n")
	}
	if saved := amountsOf(v.Saved); len(saved) > 0 {
		b.WriteString("\nCoût théorique actuel : " + amountsOf(v.Before).String() + "\n")
		b.WriteString("Coût théorique après : " + amountsOf(v.After).String() + " (économie " + saved.String() + ")\n")
	}
	return b.String()
}

func (a *API) deleteImpact(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view, err := a.deletionImpact(r.Context(), ownerID(r.Context()), kind, r.PathValue("id"))
		if err != nil {
			a.mapErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}
