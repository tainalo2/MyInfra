// Package exo signs Exoscale API v2 requests (EXO2-HMAC-SHA256).
package exo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const DefaultZone = "ch-gva-2"

// Host returns the API root of a zone, without the trailing slash.
func Host(override, zone string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimRight(override, "/")
	}
	if strings.TrimSpace(zone) == "" {
		zone = DefaultZone
	}
	return "https://api-" + zone + ".exoscale.com"
}

// Authorization builds the header value for one request. rawURL is the full
// request URL, body the exact bytes sent (nil when there is none).
func Authorization(method, rawURL string, body []byte, key, secret string, now time.Time) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	expires := now.Add(5 * time.Minute).Unix()
	query := parsed.Query()
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, name)
	}
	sort.Strings(names)
	var values strings.Builder
	for _, name := range names {
		for _, value := range query[name] {
			values.WriteString(value)
		}
	}
	message := strings.Join([]string{
		method + " " + parsed.Path,
		string(body),
		values.String(),
		"",
		fmt.Sprint(expires),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	parts := []string{"EXO2-HMAC-SHA256 credential=" + strings.TrimSpace(key)}
	if len(names) > 0 {
		parts = append(parts, "signed-query-args="+strings.Join(names, ";"))
	}
	parts = append(parts, "expires="+fmt.Sprint(expires), "signature="+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return strings.Join(parts, ","), nil
}

// Header is Authorization wrapped for the shared HTTP helpers.
func Header(method, rawURL string, body []byte, key, secret string) (map[string]string, error) {
	value, err := Authorization(method, rawURL, body, key, secret, time.Now())
	if err != nil {
		return nil, err
	}
	return map[string]string{"Authorization": value}, nil
}
