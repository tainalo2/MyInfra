package pack

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var (
	// tokenPattern is a value MyInfra fills in: {{url}}, {{host}},
	// {{input:key}}, {{secret:name}} or {{encoded:name}}.
	tokenPattern = regexp.MustCompile(`\{\{([a-z]+)(?::([A-Za-z0-9_-]+))?\}\}`)
	// addressPattern is what is left at installation for each delivery.
	addressPattern = regexp.MustCompile(`\{\{(url|host)\}\}`)
	inputKey       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)
)

// fill fills the {{…}} of one value at installation. The address stays a
// token, filled at each delivery.
func fill(value string, inputs, secrets map[string]string) (string, error) {
	var failure error
	out := tokenPattern.ReplaceAllStringFunc(value, func(token string) string {
		if failure != nil {
			return token
		}
		parts := tokenPattern.FindStringSubmatch(token)
		switch parts[1] {
		case "url", "host":
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
			failure = fmt.Errorf("Une variable du paquet est illisible.")
			return token
		}
	})
	if failure != nil {
		return "", failure
	}
	if strings.Contains(addressPattern.ReplaceAllString(out, ""), "{{") {
		return "", fmt.Errorf("Une variable du paquet est illisible.")
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

// checkTokens checks the {{…}} of a value against the questions asked.
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

// hasSecret tells whether a value asks for a secret made at installation.
func hasSecret(value string) bool {
	for _, parts := range tokenPattern.FindAllStringSubmatch(value, -1) {
		if parts[1] == "secret" || parts[1] == "encoded" {
			return true
		}
	}
	return false
}

// filler fills the {{…}} of the values of one installation: a secret asked
// twice gets the same value. Every question must have its answer.
func filler(host string, questions []Input, inputs map[string]string) (func(string) (string, error), error) {
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
		return fill(value, inputs, secrets)
	}, nil
}
