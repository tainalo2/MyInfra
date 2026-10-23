package market

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const (
	Sites         = "Sites"
	Collaboration = "Collaboration"
	Code          = "Code et données"
	Identity      = "Identité"
	Files         = "Fichiers et tableaux"
	Automation    = "Automatisation"
	Exchange      = "Échanges"
	Notes         = "Notes et liens"
	Accounts      = "Comptes personnels"
	Media         = "Médias"
	Tools         = "Outils"
)

type Input struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type Service struct {
	Name      string
	Image     string
	Published bool
	Listen    int
	Disk      bool
	DiskPath  string
	DiskGi    int
	Replicas  int
	Args      []string
}

type Article struct {
	ID       string
	Name     string
	Summary  string
	Category string
	Inputs   []Input
	Services []Service
	Env      map[string]string
}

type PublicService struct {
	Name      string `json:"name"`
	Image     string `json:"image"`
	Published bool   `json:"published"`
	Disk      bool   `json:"disk"`
}

type Card struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Site        string          `json:"site"`
	Logo        string          `json:"logo"`
	Category    string          `json:"category"`
	KeepsFiles  bool            `json:"keepsFiles"`
	Inputs      []Input         `json:"inputs"`
	Services    []PublicService `json:"services"`
}

type Group struct {
	Name     string `json:"name"`
	Articles []Card `json:"articles"`
}

var (
	idPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
	namePattern  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	imagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,200}$`)
	pathPattern  = regexp.MustCompile(`^/[A-Za-z0-9._/-]+$`)
	envPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	argPattern   = regexp.MustCompile(`^[A-Za-z0-9_.,:/=@+-]+$`)
	tokenPattern = regexp.MustCompile(`\{\{([a-z]+)(?::([A-Za-z0-9_-]+))?\}\}`)
	// addressPattern is what is left at installation for each delivery.
	addressPattern = regexp.MustCompile(`\{\{(url|host)\}\}`)
)

func init() {
	if err := Validate(articles); err != nil {
		panic(err)
	}
	if err := checkProfiles(articles); err != nil {
		panic(err)
	}
}

func Groups() []Group {
	var order []string
	byName := map[string][]Card{}
	for _, article := range articles {
		card := cardOf(article)
		if _, ok := byName[article.Category]; !ok {
			order = append(order, article.Category)
		}
		byName[article.Category] = append(byName[article.Category], card)
	}
	out := make([]Group, 0, len(order))
	for _, name := range order {
		out = append(out, Group{Name: name, Articles: byName[name]})
	}
	return out
}

func Logo(images ...string) string {
	want := map[string]bool{}
	for _, image := range images {
		base := imageBase(image)
		if base != "" {
			want[base] = true
		}
	}
	if len(want) == 0 {
		return ""
	}
	for _, article := range articles {
		for _, svc := range article.Services {
			if want[imageBase(svc.Image)] {
				return profiles[article.ID].Logo
			}
		}
	}
	return ""
}

func imageBase(image string) string {
	image = strings.TrimSpace(image)
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon > slash {
		image = image[:colon]
	}
	return image
}

func CardOf(id string) (Card, bool) {
	article, ok := find(id)
	if !ok {
		return Card{}, false
	}
	return cardOf(article), true
}

func find(id string) (Article, bool) {
	for _, article := range articles {
		if article.ID == id {
			return article, true
		}
	}
	return Article{}, false
}

func cardOf(article Article) Card {
	inputs := article.Inputs
	if inputs == nil {
		inputs = []Input{}
	}
	keeps := false
	services := make([]PublicService, 0, len(article.Services))
	for _, svc := range article.Services {
		if svc.Disk {
			keeps = true
		}
		services = append(services, PublicService{Name: svc.Name, Image: svc.Image, Published: svc.Published, Disk: svc.Disk})
	}
	info := profiles[article.ID]
	return Card{
		ID: article.ID, Name: article.Name, Summary: article.Summary, Description: info.Description,
		Site: info.Site, Logo: info.Logo, Category: article.Category,
		KeepsFiles: keeps, Inputs: inputs, Services: services,
	}
}

func checkProfiles(list []Article) error {
	seen := map[string]bool{}
	var missing []string
	for _, article := range list {
		info, ok := profiles[article.ID]
		if !ok || len(strings.TrimSpace(info.Description)) < 80 || !strings.HasPrefix(info.Site, "https://") {
			missing = append(missing, article.ID)
			continue
		}
		seen[article.ID] = true
	}
	if len(missing) > 0 {
		return fmt.Errorf("présentation incomplète: %s", strings.Join(missing, ", "))
	}
	if len(seen) != len(profiles) {
		return fmt.Errorf("présentation en trop")
	}
	return nil
}

// ServicesOf lists the services of an article as they are described, before
// any value is filled in.
func ServicesOf(id string) ([]Service, bool) {
	article, ok := find(id)
	if !ok {
		return nil, false
	}
	return append([]Service(nil), article.Services...), true
}

func Expand(id, host string, inputs map[string]string) ([]Service, map[string]string, error) {
	article, ok := find(id)
	if !ok {
		return nil, nil, fmt.Errorf("Cet article est introuvable.")
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " \n\r'\t") {
		return nil, nil, fmt.Errorf("Le nom public est illisible.")
	}
	if inputs == nil {
		inputs = map[string]string{}
	}
	for _, input := range article.Inputs {
		if strings.TrimSpace(inputs[input.Key]) == "" {
			return nil, nil, fmt.Errorf("Indiquez %s.", strings.ToLower(input.Label))
		}
	}
	secrets := map[string]string{}
	env := map[string]string{}
	for key, value := range article.Env {
		next, err := fill(value, host, inputs, secrets)
		if err != nil {
			return nil, nil, err
		}
		env[key] = next
	}
	return article.Services, env, nil
}

func fill(value, host string, inputs, secrets map[string]string) (string, error) {
	var failure error
	out := tokenPattern.ReplaceAllStringFunc(value, func(token string) string {
		if failure != nil {
			return token
		}
		parts := tokenPattern.FindStringSubmatch(token)
		switch parts[1] {
		case "url", "host":
			// The address stays a token: it is filled at each delivery.
			return token
		case "input":
			got := strings.TrimSpace(inputs[parts[2]])
			if got == "" {
				failure = fmt.Errorf("Indiquez les champs demandés.")
			}
			return got
		case "secret":
			return remember(secrets, parts[2], false)
		case "encoded":
			return remember(secrets, "encoded:"+parts[2], true)
		default:
			failure = fmt.Errorf("Une variable de l'article est illisible.")
			return token
		}
	})
	if failure != nil {
		return "", failure
	}
	if strings.Contains(addressPattern.ReplaceAllString(out, ""), "{{") {
		return "", fmt.Errorf("Une variable de l'article est illisible.")
	}
	return out, nil
}

// FillAddress puts the name an application answers on in place of {{url}}
// and {{host}}, at each delivery: a new name reaches every value.
func FillAddress(value, host string) string {
	if host == "" || !strings.Contains(value, "{{") {
		return value
	}
	value = strings.ReplaceAll(value, "{{url}}", "https://"+host)
	return strings.ReplaceAll(value, "{{host}}", host)
}

func remember(secrets map[string]string, name string, encoded bool) string {
	if value, ok := secrets[name]; ok {
		return value
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		buf = []byte("myinfra-catalogue-secret-fallback")
	}
	value := hex.EncodeToString(buf)
	if encoded {
		value = base64.StdEncoding.EncodeToString(buf)
	}
	secrets[name] = value
	return value
}

func Validate(list []Article) error {
	seen := map[string]bool{}
	for _, article := range list {
		if !idPattern.MatchString(article.ID) || seen[article.ID] {
			return fmt.Errorf("identifiant illisible: %s", article.ID)
		}
		seen[article.ID] = true
		if strings.TrimSpace(article.Name) == "" || strings.TrimSpace(article.Summary) == "" || strings.TrimSpace(article.Category) == "" {
			return fmt.Errorf("fiche incomplète: %s", article.ID)
		}
		published := 0
		names := map[string]bool{}
		for _, svc := range article.Services {
			if !namePattern.MatchString(svc.Name) || names[svc.Name] {
				return fmt.Errorf("service illisible: %s/%s", article.ID, svc.Name)
			}
			names[svc.Name] = true
			if !imagePattern.MatchString(svc.Image) || strings.Contains(svc.Image, "..") {
				return fmt.Errorf("image illisible: %s/%s", article.ID, svc.Name)
			}
			if svc.Listen < 1 || svc.Listen > 65535 {
				return fmt.Errorf("port illisible: %s/%s", article.ID, svc.Name)
			}
			if svc.Replicas < 1 || svc.Replicas > 10 {
				return fmt.Errorf("copies illisibles: %s/%s", article.ID, svc.Name)
			}
			if svc.Disk {
				if !pathPattern.MatchString(svc.DiskPath) || strings.Contains(svc.DiskPath, "..") || svc.DiskGi < 1 || svc.DiskGi > 1024 {
					return fmt.Errorf("disque illisible: %s/%s", article.ID, svc.Name)
				}
			}
			for _, arg := range svc.Args {
				if !argPattern.MatchString(arg) {
					return fmt.Errorf("argument illisible: %s/%s", article.ID, svc.Name)
				}
			}
			if svc.Published {
				published++
			}
		}
		if published != 1 {
			return fmt.Errorf("un seul service public: %s", article.ID)
		}
		inputs := map[string]bool{}
		for _, input := range article.Inputs {
			if !idPattern.MatchString(input.Key) || strings.TrimSpace(input.Label) == "" || inputs[input.Key] {
				return fmt.Errorf("question illisible: %s", article.ID)
			}
			inputs[input.Key] = true
			if !strings.Contains(joinedEnv(article.Env), "{{input:"+input.Key+"}}") {
				return fmt.Errorf("question inutilisée: %s/%s", article.ID, input.Key)
			}
		}
		for key, value := range article.Env {
			if !envPattern.MatchString(key) || strings.ContainsAny(value, "\n\r'") {
				return fmt.Errorf("variable illisible: %s/%s", article.ID, key)
			}
			if err := checkTokens(value, inputs); err != nil {
				return fmt.Errorf("%s: %w", article.ID, err)
			}
		}
	}
	return nil
}

func joinedEnv(env map[string]string) string {
	var b strings.Builder
	for _, value := range env {
		b.WriteString(value)
	}
	return b.String()
}

func checkTokens(value string, inputs map[string]bool) error {
	cleaned := tokenPattern.ReplaceAllString(value, "")
	if strings.Contains(cleaned, "{{") || strings.Contains(cleaned, "}}") {
		return fmt.Errorf("variable illisible")
	}
	for _, parts := range tokenPattern.FindAllStringSubmatch(value, -1) {
		switch parts[1] {
		case "url", "host":
		case "secret", "encoded":
			if parts[2] == "" {
				return fmt.Errorf("secret illisible")
			}
		case "input":
			if !inputs[parts[2]] {
				return fmt.Errorf("question inconnue %s", parts[2])
			}
		default:
			return fmt.Errorf("variable illisible")
		}
	}
	return nil
}

// CheckTokens checks the {{…}} of a value against the questions asked.
func CheckTokens(value string, inputs map[string]bool) error {
	return checkTokens(value, inputs)
}

// HasSecret tells whether a value asks for a secret made at installation.
func HasSecret(value string) bool {
	for _, parts := range tokenPattern.FindAllStringSubmatch(value, -1) {
		if parts[1] == "secret" || parts[1] == "encoded" {
			return true
		}
	}
	return false
}

// InputKey is how a question is named.
func InputKey(key string) bool {
	return idPattern.MatchString(key)
}

// ArticleOf finds an article as it is described, before any value is filled in.
func ArticleOf(id string) (Article, bool) {
	return find(id)
}

// IDs lists the articles of the catalogue.
func IDs() []string {
	out := make([]string, len(articles))
	for i, article := range articles {
		out[i] = article.ID
	}
	return out
}

// Filler fills the {{…}} of the values of one installation: a secret asked
// twice gets the same value. Every question must have its answer.
func Filler(host string, questions []Input, inputs map[string]string) (func(string) (string, error), error) {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " \n\r'\t") {
		return nil, fmt.Errorf("Le nom public est illisible.")
	}
	if inputs == nil {
		inputs = map[string]string{}
	}
	for _, input := range questions {
		if strings.TrimSpace(inputs[input.Key]) == "" {
			return nil, fmt.Errorf("Indiquez %s.", strings.ToLower(input.Label))
		}
	}
	secrets := map[string]string{}
	return func(value string) (string, error) {
		return fill(value, host, inputs, secrets)
	}, nil
}
