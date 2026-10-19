package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"myinfra/internal/catalog"
)

func kamateraHeader(creds catalog.Creds) map[string]string {
	return map[string]string{
		"AuthClientId": strings.TrimSpace(creds.ApplicationKey),
		"AuthSecret":   strings.TrimSpace(creds.ApplicationSecret),
		"Accept":       "application/json",
	}
}

// kamateraOffer reads "2A-4096-40": cores and type, RAM in MB, disk in GB.
func kamateraOffer(id string) (cpu string, ram, disk int, err error) {
	parts := strings.Split(strings.TrimSpace(id), "-")
	if len(parts) != 3 || len(parts[0]) < 2 {
		return "", 0, 0, fmt.Errorf("Type de machine inconnu.")
	}
	ram, errRAM := strconv.Atoi(parts[1])
	disk, errDisk := strconv.Atoi(parts[2])
	if errRAM != nil || errDisk != nil || ram <= 0 || disk <= 0 {
		return "", 0, 0, fmt.Errorf("Type de machine inconnu.")
	}
	return parts[0], ram, disk, nil
}

func kamateraPassword() string {
	return strings.NewReplacer("-", "x", "_", "y").Replace(randomPass())
}

func (c Client) createKamatera(ctx context.Context, creds catalog.Creds, spec Spec) (Machine, error) {
	base := c.base(c.KamateraBase, "https://cloudcli.cloudwm.com")
	header := kamateraHeader(creds)
	cpu, ram, disk, err := kamateraOffer(spec.OfferID)
	if err != nil {
		return Machine{}, err
	}
	if spec.RootGB > disk {
		disk = spec.RootGB
	}
	status, body, err := c.call(ctx, http.MethodGet, base+"/service/server?images=1&datacenter="+spec.Location, header, nil)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	image, err := kamateraDebian(body)
	if err != nil {
		return Machine{}, err
	}
	password := kamateraPassword()
	request := map[string]any{
		"name":               machineLabel(spec.Name),
		"password":           password,
		"passwordValidate":   password,
		"datacenter":         spec.Location,
		"image":              image,
		"cpu":                cpu,
		"ram":                ram,
		"disk":               "size=" + strconv.Itoa(disk),
		"dailybackup":        "no",
		"managed":            "no",
		"network":            "name=wan,ip=auto",
		"quantity":           "1",
		"billingcycle":       "monthly",
		"monthlypackage":     kamateraTraffic(spec.Location),
		"poweronaftercreate": "yes",
		"script-file":        spec.UserData,
	}
	if len(spec.SSHKeys) > 0 {
		request["ssh-key"] = spec.SSHKeys[0]
	}
	status, body, err = c.call(ctx, http.MethodPost, base+"/service/server", header, request)
	if err != nil {
		return Machine{}, err
	}
	if status >= 300 {
		return Machine{}, refused(status, body)
	}
	command, err := kamateraCommand(body)
	if err != nil {
		return Machine{}, err
	}
	log, err := c.kamateraWait(ctx, base, header, command)
	if err != nil {
		return Machine{}, err
	}
	name := ""
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "Name: ") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "Name: "))
		}
	}
	if name == "" {
		return Machine{}, fmt.Errorf("Le provider a refusé la machine.")
	}
	var machine Machine
	machine.IPv4, err = waitIP(ctx, func() (string, error) {
		status, body, err := c.call(ctx, http.MethodPost, base+"/service/server/info", header, map[string]string{"name": name})
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		id, ip := kamateraServer(body)
		if id != "" {
			machine.ID = id
		}
		return ip, nil
	})
	if err != nil {
		return machine, err
	}
	if machine.ID == "" {
		return machine, fmt.Errorf("Le provider a refusé la machine.")
	}
	return machine, nil
}

// kamateraTraffic picks the included traffic package; every datacenter offers t1000 or t5000.
func kamateraTraffic(datacenter string) string {
	switch strings.ToUpper(datacenter) {
	case "AS", "AS-SG", "AS-TY", "AU-SY":
		return "t1000"
	default:
		return "t5000"
	}
}

var kamateraVersion = regexp.MustCompile(`^\s*(\d+)`)

// kamateraDebian picks the newest Debian server image: os is "Debian", code starts with the version ("13 64bit").
func kamateraDebian(body []byte) (string, error) {
	var items []struct {
		ID   string `json:"id"`
		OS   string `json:"os"`
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &items) != nil {
		return "", fmt.Errorf("Debian est introuvable.")
	}
	best, score := "", 0
	for _, item := range items {
		if item.ID == "" || !strings.EqualFold(strings.TrimSpace(item.OS), "debian") || !strings.Contains(item.Code, "64") {
			continue
		}
		match := kamateraVersion.FindStringSubmatch(item.Code)
		if len(match) < 2 {
			continue
		}
		version, _ := strconv.Atoi(match[1])
		if version > score {
			best, score = item.ID, version
		}
	}
	if best == "" {
		return "", fmt.Errorf("Debian est introuvable.")
	}
	return best, nil
}

func kamateraCommand(body []byte) (string, error) {
	var ids []json.RawMessage
	if json.Unmarshal(body, &ids) != nil || len(ids) == 0 {
		var wrapped struct {
			CommandIDs []json.RawMessage `json:"commandIds"`
		}
		if json.Unmarshal(body, &wrapped) != nil || len(wrapped.CommandIDs) == 0 {
			return "", fmt.Errorf("Le provider a refusé la machine.")
		}
		ids = wrapped.CommandIDs
	}
	return strings.Trim(string(ids[0]), `"`), nil
}

func (c Client) kamateraWait(ctx context.Context, base string, header map[string]string, command string) (string, error) {
	deadline := time.Now().Add(10 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("Le provider ne répond pas.")
		case <-time.After(pollEvery):
		}
		status, body, err := c.call(ctx, http.MethodGet, base+"/service/queue?id="+command, header, nil)
		if err != nil {
			return "", err
		}
		if status >= 300 {
			return "", refused(status, body)
		}
		var commands []struct {
			Status string `json:"status"`
			Log    string `json:"log"`
		}
		if json.Unmarshal(body, &commands) != nil || len(commands) != 1 {
			return "", fmt.Errorf("Le provider ne répond pas.")
		}
		switch commands[0].Status {
		case "complete":
			return commands[0].Log, nil
		case "error":
			return "", refused(http.StatusOK, []byte(commands[0].Log))
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("Le provider ne répond pas.")
		}
	}
}

func kamateraServer(body []byte) (string, string) {
	var servers []struct {
		ID       string `json:"id"`
		Networks []struct {
			Network string   `json:"network"`
			IPs     []string `json:"ips"`
		} `json:"networks"`
	}
	if json.Unmarshal(body, &servers) != nil || len(servers) == 0 {
		return "", ""
	}
	var wan, all []string
	for _, network := range servers[0].Networks {
		all = append(all, network.IPs...)
		if strings.HasPrefix(network.Network, "wan-") {
			wan = append(wan, network.IPs...)
		}
	}
	ip := publicIP(wan)
	if ip == "" {
		ip = publicIP(all)
	}
	return servers[0].ID, ip
}

func (c Client) destroyKamatera(ctx context.Context, creds catalog.Creds, id string) error {
	base := c.base(c.KamateraBase, "https://cloudcli.cloudwm.com")
	header := kamateraHeader(creds)
	status, body, err := c.call(ctx, http.MethodPost, base+"/service/server/terminate", header, map[string]any{"id": id, "force": true})
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status >= 300 {
		return refused(status, body)
	}
	command, err := kamateraCommand(body)
	if err != nil {
		return nil
	}
	_, err = c.kamateraWait(ctx, base, header, command)
	return err
}
