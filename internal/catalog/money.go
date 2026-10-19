package catalog

import "strconv"

func parseFloat(raw string) (float64, error) {
	return strconv.ParseFloat(raw, 64)
}
