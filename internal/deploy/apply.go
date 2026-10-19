package deploy

import (
	"encoding/base64"
	"strconv"
)

func KubectlApply(manifest string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(manifest))
	return "printf %s " + strconv.Quote(encoded) + " | base64 -d | k3s kubectl apply -f -"
}

// RunScript runs a shell script on the machine without exposing it to the
// quoting of the SSH command line.
func RunScript(script string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	return "printf %s " + strconv.Quote(encoded) + " | base64 -d | sh"
}
