package catalog

import "strings"

// gpuModels are matched longest first, so L40S is not read as L4.
var gpuModels = []string{"H200", "H100", "L40S", "A100", "A40", "A30", "A16", "A10", "V100", "RTX", "L4", "T4"}

// gpuModel reads a graphics card model out of a provider's name or identifier.
// Count 0 means there is no card. An unknown card is just "GPU".
func gpuModel(count int, parts ...string) string {
	if count <= 0 {
		return ""
	}
	text := strings.ToUpper(strings.Join(parts, " "))
	for _, model := range gpuModels {
		if strings.Contains(text, model) {
			return model
		}
	}
	return "GPU"
}
