package provision

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"myinfra/internal/catalog"
)

// ovhTarget finds the project, the region (GRA becomes GRA11), the flavor id
// and the Debian image that OVH expects for this offer.
func (c Client) ovhTarget(ctx context.Context, creds catalog.Creds, spec Spec) (string, string, string, string, error) {
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	projects, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project", nil)
	if err != nil {
		return "", "", "", "", err
	}
	var ids []string
	if json.Unmarshal(projects, &ids) != nil || len(ids) == 0 {
		return "", "", "", "", fmt.Errorf("Projet OVH introuvable.")
	}
	project := strings.TrimSpace(spec.ProjectID)
	if project == "" {
		project = strings.TrimSpace(creds.ProjectID)
	}
	if project == "" {
		project = ids[0]
	} else if !contains(ids, project) {
		return "", "", "", "", fmt.Errorf("Projet OVH introuvable.")
	}
	regionsBody, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/region", nil)
	if err != nil {
		return "", "", "", "", err
	}
	region := ovhRegion(regionsBody, spec.Location)
	if region == "" {
		return "", "", "", "", fmt.Errorf("Région OVH introuvable.")
	}
	flavorBody, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/flavor?region="+url.QueryEscape(region), nil)
	if err != nil {
		return "", "", "", "", err
	}
	flavor := matchFlavor(flavorBody, spec.OfferID, spec.OfferName)
	if flavor == "" {
		return "", "", "", "", fmt.Errorf("Ce type de machine n'est plus proposé à cet endroit.")
	}
	imageBody, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/image?region="+url.QueryEscape(region), nil)
	if err != nil {
		return "", "", "", "", err
	}
	image := debianImage(imageBody)
	if image == "" {
		return "", "", "", "", fmt.Errorf("Image Debian introuvable.")
	}
	return project, region, flavor, image, nil
}

// ovhKey returns the name of the project key holding this public key, adding
// it once: every machine of the same key shares it, nothing piles up.
func (c Client) ovhKey(ctx context.Context, creds catalog.Creds, project, publicKey string) (string, error) {
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	publicKey = strings.TrimSpace(publicKey)
	body, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/sshkey", nil)
	if err != nil {
		return "", err
	}
	var keys []struct {
		Name      string `json:"name"`
		PublicKey string `json:"publicKey"`
	}
	_ = json.Unmarshal(body, &keys)
	for _, key := range keys {
		if strings.TrimSpace(key.PublicKey) == publicKey {
			return key.Name, nil
		}
	}
	sum := sha256.Sum256([]byte(publicKey))
	name := fmt.Sprintf("myinfra-%x", sum[:6])
	if _, err := c.ovhJSON(ctx, creds, http.MethodPost, base+"/cloud/project/"+project+"/sshkey", map[string]string{
		"name": name, "publicKey": publicKey,
	}); err != nil {
		return "", err
	}
	return name, nil
}

func (c Client) createOVH(ctx context.Context, creds catalog.Creds, spec Spec) (Machine, error) {
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	project, region, flavor, image, err := c.ovhTarget(ctx, creds, spec)
	if err != nil {
		return Machine{}, err
	}
	created, err := c.ovhJSON(ctx, creds, http.MethodPost, base+"/cloud/project/"+project+"/instance", map[string]any{
		"flavorId": flavor,
		"imageId":  image,
		"name":     spec.Name,
		"region":   region,
		"userData": base64.StdEncoding.EncodeToString([]byte(spec.UserData)),
	})
	if err != nil {
		return Machine{}, err
	}
	var instance struct {
		ID          string `json:"id"`
		IPAddresses []struct {
			IP   string `json:"ip"`
			Type string `json:"type"`
		} `json:"ipAddresses"`
	}
	if json.Unmarshal(created, &instance) != nil || instance.ID == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	ip, err := waitIP(ctx, func() (string, error) {
		if found := ovhIP(instance.IPAddresses); found != "" {
			instance.IPAddresses = nil
			return found, nil
		}
		body, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/instance/"+instance.ID, nil)
		if err != nil {
			return "", err
		}
		var next struct {
			IPAddresses []struct {
				IP   string `json:"ip"`
				Type string `json:"type"`
			} `json:"ipAddresses"`
		}
		if json.Unmarshal(body, &next) != nil {
			return "", fmt.Errorf("Le provider ne répond pas.")
		}
		return ovhIP(next.IPAddresses), nil
	})
	if err != nil {
		return Machine{}, err
	}
	return Machine{ID: project + "/" + instance.ID, IPv4: ip}, nil
}

func (c Client) ovhProjects(ctx context.Context, creds catalog.Creds) ([]Project, error) {
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	projects, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project", nil)
	if err != nil {
		return nil, err
	}
	var ids []string
	if json.Unmarshal(projects, &ids) != nil {
		return nil, fmt.Errorf("Projet OVH introuvable.")
	}
	out := make([]Project, 0, len(ids))
	for _, id := range ids {
		name := id
		detail, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+id, nil)
		if err == nil {
			var info struct {
				Description string `json:"description"`
			}
			if json.Unmarshal(detail, &info) == nil && strings.TrimSpace(info.Description) != "" {
				name = strings.TrimSpace(info.Description)
			}
		}
		out = append(out, Project{ID: id, Name: name})
	}
	return out, nil
}

func (c Client) OVHFlavors(ctx context.Context, creds catalog.Creds, project, location string) ([]catalog.LiveFlavor, error) {
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	regionsBody, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/region", nil)
	if err != nil {
		return nil, err
	}
	region := ovhRegion(regionsBody, location)
	if region == "" {
		return nil, fmt.Errorf("Région OVH introuvable.")
	}
	body, err := c.ovhJSON(ctx, creds, http.MethodGet, base+"/cloud/project/"+project+"/flavor?region="+url.QueryEscape(region), nil)
	if err != nil {
		return nil, err
	}
	return parseLiveFlavors(body), nil
}

func (c Client) destroyOVH(ctx context.Context, creds catalog.Creds, id string) error {
	project, instance, ok := strings.Cut(id, "/")
	if !ok {
		return fmt.Errorf("Machine introuvable.")
	}
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	_, err := c.ovhJSON(ctx, creds, http.MethodDelete, base+"/cloud/project/"+project+"/instance/"+instance, nil)
	return err
}

func (c Client) ovhJSON(ctx context.Context, creds catalog.Creds, method, rawURL string, payload any) ([]byte, error) {
	base := c.base(c.OVHBase, "https://eu.api.ovh.com/1.0")
	status, raw, err := c.call(ctx, http.MethodGet, base+"/auth/time", nil, nil)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, refused(status, raw)
	}
	serverTime, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("OVH ne répond pas.")
	}
	var bodyText string
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyText = string(encoded)
	}
	stamp := strconv.FormatInt(serverTime, 10)
	sum := sha1.Sum([]byte(creds.ApplicationSecret + "+" + creds.ConsumerKey + "+" + method + "+" + rawURL + "+" + bodyText + "+" + stamp))
	header := map[string]string{
		"X-Ovh-Application": creds.ApplicationKey,
		"X-Ovh-Consumer":    creds.ConsumerKey,
		"X-Ovh-Timestamp":   stamp,
		"X-Ovh-Signature":   fmt.Sprintf("$1$%x", sum),
	}
	var send any
	if payload != nil {
		send = payload
	}
	status, raw, err = c.call(ctx, method, rawURL, header, send)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound && method == http.MethodDelete {
		return nil, nil
	}
	if status == http.StatusNotFound && method == http.MethodGet {
		return nil, errMachineGone
	}
	if status >= 300 {
		return nil, refused(status, raw)
	}
	return raw, nil
}

func ovhRegion(body []byte, want string) string {
	var names []string
	if json.Unmarshal(body, &names) != nil {
		var objects []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(body, &objects) != nil {
			return ""
		}
		for _, item := range objects {
			names = append(names, item.Name)
		}
	}
	want = strings.ToUpper(strings.TrimSpace(want))
	var sites []string
	var open []string
	exact := ""
	for _, name := range names {
		upper := strings.ToUpper(name)
		if upper == want {
			exact = name
			continue
		}
		if want == "" || !strings.HasPrefix(upper, want) {
			continue
		}
		if ovhSite(upper) {
			sites = append(sites, name)
			continue
		}
		if ovhOpen(upper, want) {
			open = append(open, name)
		}
	}
	if len(sites) > 0 {
		sort.Slice(sites, func(i, j int) bool {
			return ovhSiteNumber(sites[i]) > ovhSiteNumber(sites[j])
		})
		return sites[0]
	}
	for _, name := range open {
		if strings.HasSuffix(strings.ToUpper(name), "-A") {
			return name
		}
	}
	if len(open) > 0 {
		sort.Strings(open)
		return open[0]
	}
	return exact
}

func ovhOpen(name, want string) bool {
	rest, ok := strings.CutPrefix(name, want+"-")
	if !ok || rest == "" || strings.Contains(rest, "ARCHIVE") {
		return false
	}
	return true
}

func ovhSite(name string) bool {
	split := 0
	for split < len(name) && name[split] >= 'A' && name[split] <= 'Z' {
		split++
	}
	if split == 0 || split == len(name) {
		return false
	}
	for _, char := range name[split:] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func ovhSiteNumber(name string) int {
	split := 0
	for split < len(name) && (name[split] < '0' || name[split] > '9') {
		split++
	}
	number, _ := strconv.Atoi(name[split:])
	return number
}

func matchFlavor(body []byte, offerID, offerName string) string {
	var rows []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Available bool   `json:"available"`
		OSType    string `json:"osType"`
		Type      string `json:"type"`
		PlanCodes struct {
			Hourly  string `json:"hourly"`
			Monthly string `json:"monthly"`
		} `json:"planCodes"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return ""
	}
	var byName string
	for _, row := range rows {
		if row.ID == "" || !row.Available || baremetalFlavor(row.OSType, row.Type, row.Name) {
			continue
		}
		if offerID != "" && (strings.EqualFold(row.PlanCodes.Hourly, offerID) || strings.EqualFold(row.PlanCodes.Monthly, offerID)) {
			return row.ID
		}
		if byName == "" && offerName != "" && strings.EqualFold(row.Name, offerName) {
			byName = row.ID
		}
	}
	return byName
}

func baremetalFlavor(osType, kind, name string) bool {
	text := strings.ToLower(osType + " " + kind + " " + name)
	return strings.Contains(text, "baremetal") || strings.Contains(text, "windows") || strings.Contains(text, "metal")
}

func parseLiveFlavors(body []byte) []catalog.LiveFlavor {
	var rows []struct {
		Name      string `json:"name"`
		Type      string `json:"type"`
		OSType    string `json:"osType"`
		Available bool   `json:"available"`
		VCPUs     int    `json:"vcpus"`
		RAM       int    `json:"ram"`
		Disk      int    `json:"disk"`
		PlanCodes struct {
			Hourly  string `json:"hourly"`
			Monthly string `json:"monthly"`
		} `json:"planCodes"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return nil
	}
	out := make([]catalog.LiveFlavor, 0, len(rows))
	for _, row := range rows {
		out = append(out, catalog.LiveFlavor{
			Name: row.Name, Type: row.Type, OSType: row.OSType, Available: row.Available,
			VCPU: row.VCPUs, RAM: row.RAM, Disk: row.Disk,
			Hourly: row.PlanCodes.Hourly, Monthly: row.PlanCodes.Monthly,
		})
	}
	return out
}

func soldFlavors(body []byte) map[string]bool {
	var rows []struct {
		Name      string `json:"name"`
		Available bool   `json:"available"`
		OSType    string `json:"osType"`
		PlanCodes struct {
			Hourly  string `json:"hourly"`
			Monthly string `json:"monthly"`
		} `json:"planCodes"`
	}
	sold := map[string]bool{}
	if json.Unmarshal(body, &rows) != nil {
		return sold
	}
	for _, row := range rows {
		if !row.Available || strings.EqualFold(row.OSType, "windows") {
			continue
		}
		for _, value := range []string{row.PlanCodes.Hourly, row.PlanCodes.Monthly, row.Name} {
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "" {
				sold[value] = true
			}
		}
	}
	return sold
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func matchName(body []byte, want string) string {
	var rows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return ""
	}
	for _, row := range rows {
		if strings.EqualFold(row.Name, want) {
			return row.ID
		}
	}
	return ""
}

func debianImage(body []byte) string {
	var rows []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &rows) != nil {
		return ""
	}
	chosen := ""
	score := -1
	for _, row := range rows {
		name := strings.ToLower(row.Name)
		kind := strings.ToLower(row.Type)
		if row.ID == "" || !strings.Contains(name, "debian") {
			continue
		}
		if strings.Contains(kind, "baremetal") || strings.Contains(name, "baremetal") {
			continue
		}
		if kind != "" && kind != "linux" {
			continue
		}
		next := 1
		if strings.Contains(name, "13") || strings.Contains(name, "trixie") {
			next = 13
		} else if strings.Contains(name, "12") || strings.Contains(name, "bookworm") {
			next = 12
		}
		if next > score {
			score = next
			chosen = row.ID
		}
	}
	return chosen
}

func ovhIP(rows []struct {
	IP   string `json:"ip"`
	Type string `json:"type"`
}) string {
	var values []string
	for _, row := range rows {
		if row.Type == "" || row.Type == "public" {
			values = append(values, row.IP)
		}
	}
	return publicIP(values)
}
