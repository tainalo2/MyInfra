package assistant

import (
	"context"
	"fmt"
	"strings"
)

// langNames names the interface languages for the model.
var langNames = map[string]string{"fr": "French", "en": "English", "de": "German", "it": "Italian", "pt": "Portuguese"}

var roleNotes = map[string]string{
	"read":   "read only: may look, not change anything",
	"deploy": "may deploy and change applications and servers",
	"admin":  "administrator: everything, users and keys included",
}

// systemPrompt is the profile's prompt followed by where the turn happens:
// the page open, the account's role, the language, and the infrastructure
// as it is now. Without it the model knows neither MyInfra nor the user's
// servers, and answers like a stranger.
func (s *Service) systemPrompt(ctx context.Context, base string, q chatReq, role string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(base))
	b.WriteString("\n\n## Where you are\n")
	b.WriteString("You run inside the user's own MyInfra instance, beside the interface they are using.\n")
	if q.Path != "" {
		fmt.Fprintf(&b, "- Page open in the interface: %s\n", q.Path)
	}
	if note, ok := roleNotes[role]; ok {
		fmt.Fprintf(&b, "- Role of the signed-in account: %s (%s)\n", role, note)
	}
	if name, ok := langNames[q.Lang]; ok {
		fmt.Fprintf(&b, "- Interface language: %s. Answer in %s unless the user writes in another language.\n", name, name)
	}
	b.WriteString("\n## The user's infrastructure, now\n")
	snapshot := ""
	if s.Snapshot != nil {
		snapshot = strings.TrimSpace(s.Snapshot(ctx))
	}
	if snapshot == "" {
		b.WriteString("Not available.\n")
	} else {
		b.WriteString(snapshot)
		b.WriteString("\n")
	}
	return b.String()
}
