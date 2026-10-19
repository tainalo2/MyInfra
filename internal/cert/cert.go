package cert

import (
	"os"
	"strings"
)

func Zone(host, explicit string) string {
	explicit = strings.ToLower(strings.Trim(strings.TrimSpace(explicit), "."))
	if explicit != "" {
		return explicit
	}
	parts := strings.Split(strings.ToLower(strings.Trim(host, ".")), ".")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[1:], ".")
}

func Resolver(zone string) string {
	return strings.ReplaceAll(strings.ToLower(zone), ".", "-")
}

// Email is the Let's Encrypt account address. MYINFRA_ACME_EMAIL wins over
// the address made from the zone, which often has no mailbox behind it.
func Email(zone string) string {
	if email := strings.TrimSpace(os.Getenv("MYINFRA_ACME_EMAIL")); strings.Contains(email, "@") && !strings.ContainsAny(email, " \r\n\"") {
		return email
	}
	return "certif@" + zone
}
