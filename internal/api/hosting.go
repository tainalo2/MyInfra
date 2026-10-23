package api

import (
	"encoding/json"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// hostingView is what the owner says of a machine added by hand: who rents it
// and for how much. A bought machine takes both from its offer.
type hostingView struct {
	// Provider is a known provider ("hetzner", "ovh"...) or "other", then named by Name.
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
	Price    string `json:"price,omitempty"`
	Period   string `json:"period,omitempty"`
	Currency string `json:"currency,omitempty"`
}

var hostingProvider = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

func manualHosting(raw []byte) *hostingView {
	if planOf(raw) != nil || len(raw) == 0 {
		return nil
	}
	var body struct {
		Hosting *hostingView `json:"hosting"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Hosting == nil {
		return nil
	}
	if body.Hosting.Provider == "" && body.Hosting.Price == "" {
		return nil
	}
	return body.Hosting
}

// rates gives the hourly or monthly price of a machine added by hand, as an offer would.
func (h *hostingView) rates() (hourly, monthly string) {
	if h == nil || h.Price == "" {
		return "", ""
	}
	if h.Period == "hour" {
		return h.Price, ""
	}
	return "", h.Price
}

func cleanHosting(in hostingView) (hostingView, string) {
	out := hostingView{
		Provider: strings.ToLower(strings.TrimSpace(in.Provider)),
		Name:     strings.Join(strings.Fields(in.Name), " "),
		Price:    strings.ReplaceAll(strings.TrimSpace(in.Price), ",", "."),
		Period:   strings.TrimSpace(in.Period),
		Currency: strings.ToUpper(strings.TrimSpace(in.Currency)),
	}
	if out.Provider != "" && !hostingProvider.MatchString(out.Provider) {
		return hostingView{}, "Choisissez un provider."
	}
	if out.Provider == "other" {
		if out.Name == "" {
			return hostingView{}, "Donnez le nom du provider."
		}
		if utf8.RuneCountInString(out.Name) > 60 || strings.IndexFunc(out.Name, unicode.IsControl) >= 0 || strings.ContainsAny(out.Name, "<>") {
			return hostingView{}, "Ce nom de provider n'est pas valide."
		}
	} else {
		out.Name = ""
	}
	if out.Price == "" {
		out.Period, out.Currency = "", ""
		return out, ""
	}
	price, ok := new(big.Rat).SetString(out.Price)
	if !ok || price.Sign() < 0 || len(out.Price) > 16 || strings.ContainsAny(out.Price, "eE/") {
		return hostingView{}, "Indiquez un prix valide."
	}
	if out.Period != "hour" {
		out.Period = "month"
	}
	if out.Currency != "USD" {
		out.Currency = "EUR"
	}
	return out, ""
}

func (a *API) setServerHosting(w http.ResponseWriter, r *http.Request) {
	var body hostingView
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	hosting, problem := cleanHosting(body)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	owner := ownerID(r.Context())
	row, err := a.Store.Server(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if row.ProviderID != "" {
		writeError(w, http.StatusConflict, "Le provider et le prix d'un serveur acheté viennent de son offre.")
		return
	}
	raw, err := json.Marshal(hosting)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	if err := a.Store.SetServerHosting(r.Context(), owner, row.ID, raw); err != nil {
		a.mapErr(w, err)
		return
	}
	row, err = a.Store.Server(r.Context(), owner, row.ID)
	if err != nil {
		a.mapErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.toServer(row))
}
