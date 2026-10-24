package pack

import (
	"strings"
	"testing"
)

// The example of the guide must stay a valid package.
func TestGuideExampleIsValid(t *testing.T) {
	start := strings.Index(Guide, "## Un paquet complet")
	if start < 0 {
		t.Fatal("exemple absent")
	}
	body := Guide[start:]
	body = body[strings.Index(body, "```yaml\n")+len("```yaml\n"):]
	body = body[:strings.Index(body, "```")]
	if _, problems := Parse([]byte(body)); len(problems) > 0 {
		t.Fatal(problems)
	}
}
