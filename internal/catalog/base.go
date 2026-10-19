package catalog

import "fmt"

func (cat Catalog) Base() (Offer, error) {
	priced := make([]Offer, 0, len(cat.Offers))
	for _, offer := range cat.Offers {
		if offer.ID == "" || (offer.Hourly == "" && offer.Monthly == "") {
			continue
		}
		priced = append(priced, offer)
	}
	if len(priced) == 0 {
		return Offer{}, fmt.Errorf("Aucune offre.")
	}
	roomy := make([]Offer, 0, len(priced))
	for _, offer := range priced {
		if offer.MemoryMB >= 2048 {
			roomy = append(roomy, offer)
		}
	}
	if len(roomy) > 0 {
		priced = roomy
	}
	best := priced[0]
	for _, offer := range priced[1:] {
		if offerCost(offer) < offerCost(best) {
			best = offer
		}
	}
	return best, nil
}

func offerCost(offer Offer) float64 {
	if offer.Monthly != "" {
		if value, err := parseFloat(offer.Monthly); err == nil && value > 0 {
			return value
		}
	}
	if offer.Hourly != "" {
		if value, err := parseFloat(offer.Hourly); err == nil && value > 0 {
			return value * 730
		}
	}
	return 1e18
}
