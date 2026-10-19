package catalog

func sampleHetzner(location string) Catalog {
	locations := []Location{
		{ID: "fsn1", Label: "Falkenstein"},
		{ID: "nbg1", Label: "Nuremberg"},
		{ID: "hel1", Label: "Helsinki"},
	}
	chosen := pickLocation(locations, location, "fsn1")
	volume := Storage{ID: "volume", Label: "Volume", MinGB: 10, MaxGB: 10240, HourlyPerGB: "0.00006", MonthlyPerGB: "0.044"}
	offers := []Offer{
		{ID: "cx23", Name: "CX23", Location: chosen, VCPU: 2, MemoryMB: 4096, DiskGB: 40, Hourly: "0.0088", Monthly: "5.49", Storage: []Storage{volume}},
		{ID: "cx33", Name: "CX33", Location: chosen, VCPU: 4, MemoryMB: 8192, DiskGB: 80, Hourly: "0.0136", Monthly: "8.49", Storage: []Storage{volume}},
		{ID: "cax21", Name: "CAX21", Location: chosen, VCPU: 4, MemoryMB: 8192, DiskGB: 80, Hourly: "0.0168", Monthly: "10.49", Storage: []Storage{volume}},
	}
	return Catalog{
		Provider: "hetzner", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Hetzner facture à l'heure, avec un plafond au mois. Le volume est un disque en plus, au gigaoctet.",
		Offers:  offers,
	}
}

func sampleScaleway(location string) Catalog {
	chosen := pickLocation(scalewayZones, location, "fr-par-1")
	return Catalog{
		Provider: "scaleway", Currency: "EUR", Location: chosen, Locations: scalewayZones,
		Periods: []string{"hour", "month"},
		Note:    "Scaleway facture à l'heure. Le mois affiché correspond à 730 heures.",
		Offers: []Offer{
			{
				ID: "PLAY2-PICO", Name: "PLAY2-PICO", Location: chosen, VCPU: 1, MemoryMB: 2048, DiskGB: 20,
				Hourly: "0.0146", Monthly: "10.66",
			},
			{
				ID: "DEV1-S", Name: "DEV1-S", Location: chosen, VCPU: 2, MemoryMB: 2048, DiskGB: 20,
				Hourly: "0.01", Monthly: "7.3",
				Storage: []Storage{{ID: "l_ssd", Label: "SSD local", MinGB: 1, MaxGB: 1000}},
			},
		},
	}
}

func sampleOVH(location string) Catalog {
	chosen := pickLocation(ovhRegions, location, "GRA")
	storage := []Storage{{
		ID: "volume.classic.consumption", Label: "Volume Classic", MinGB: 1, MaxGB: 4000,
		HourlyPerGB: "0.000059", MonthlyPerGB: "0.042",
	}}
	return Catalog{
		Provider: "ovh", Currency: "EUR", Location: chosen, Locations: ovhRegions,
		Periods: []string{"hour", "month"},
		Note:    "OVH Public Cloud se facture à l'heure ou au mois. Les volumes sont en plus, au gigaoctet. Prix France, hors taxe.",
		Offers: []Offer{
			{ID: "b3-8.consumption", Name: "b3-8", Location: chosen, VCPU: 2, MemoryMB: 8192, DiskGB: 50, Hourly: "0.0512", Monthly: "37.376", Storage: storage},
			{ID: "d2-4.consumption", MonthlyID: "d2-4.monthly.postpaid", Name: "d2-4", Location: chosen, VCPU: 2, MemoryMB: 4096, DiskGB: 50, Hourly: "0.028", Monthly: "12.4", Storage: storage},
		},
	}
}

func sampleDigitalOcean(location string) Catalog {
	locations := []Location{
		{ID: "ams3", Label: "Amsterdam 3"},
		{ID: "fra1", Label: "Frankfurt 1"},
		{ID: "lon1", Label: "London 1"},
	}
	chosen := pickLocation(locations, location, "ams3")
	volume := digitalOceanVolume()
	return Catalog{
		Provider: "digitalocean", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "DigitalOcean facture à la seconde, avec un plafond au mois pour les offres groupées. Le volume coûte 0,10 $ par Gio et par mois. Prix en dollars.",
		Offers: []Offer{
			{ID: "s-1vcpu-1gb", Name: "s-1vcpu-1gb", Location: chosen, VCPU: 1, MemoryMB: 1024, DiskGB: 25, Hourly: "0.00744", Monthly: "5", Storage: []Storage{volume}},
			{ID: "s-2vcpu-2gb", Name: "s-2vcpu-2gb", Location: chosen, VCPU: 2, MemoryMB: 2048, DiskGB: 60, Hourly: "0.017857", Monthly: "12", Storage: []Storage{volume}},
		},
	}
}

func sampleInfomaniak(location string) Catalog {
	locations := []Location{
		{ID: "dc3-a", Label: "Genève"},
		{ID: "dc4-a", Label: "Zurich"},
	}
	chosen := pickLocation(locations, location, "dc3-a")
	return Catalog{
		Provider: "infomaniak", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Infomaniak facture chaque heure entamée. Le mois affiché correspond à 730 heures.",
		Offers: []Offer{
			{ID: "a1", Name: "a1-ram2-disk20-perf1", Location: chosen, VCPU: 1, MemoryMB: 2048, DiskGB: 20, Hourly: "0.02", Monthly: monthlyFromHourly("0.02")},
			{ID: "a2", Name: "a2-ram4-disk50-perf1", Location: chosen, VCPU: 2, MemoryMB: 4096, DiskGB: 50, Hourly: "0.04", Monthly: monthlyFromHourly("0.04")},
		},
	}
}

func sampleUpCloud(location string) Catalog {
	locations := []Location{
		{ID: "de-fra1", Label: "Francfort"},
		{ID: "fi-hel1", Label: "Helsinki"},
		{ID: "nl-ams1", Label: "Amsterdam"},
	}
	chosen := pickLocation(locations, location, "de-fra1")
	return Catalog{
		Provider: "upcloud", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "UpCloud facture à l'heure. Le mois affiché correspond à 730 heures.",
		Offers: []Offer{
			{ID: "1xCPU-2GB", Name: "1xCPU-2GB", Location: chosen, VCPU: 1, MemoryMB: 2048, DiskGB: 50, Hourly: "0.0104", Monthly: monthlyFromHourly("0.0104")},
			{ID: "2xCPU-4GB", Name: "2xCPU-4GB", Location: chosen, VCPU: 2, MemoryMB: 4096, DiskGB: 80, Hourly: "0.0208", Monthly: monthlyFromHourly("0.0208")},
		},
	}
}

func sampleVultr(location string) Catalog {
	locations := []Location{
		{ID: "ams", Label: "Amsterdam"},
		{ID: "cdg", Label: "Paris"},
		{ID: "fra", Label: "Francfort"},
	}
	chosen := pickLocation(locations, location, "ams")
	return Catalog{
		Provider: "vultr", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Vultr facture à l'heure, avec un plafond au mois. Prix en dollars.",
		Offers: []Offer{
			{ID: "vc2-1c-1gb", Name: "vc2-1c-1gb", Location: chosen, VCPU: 1, MemoryMB: 1024, DiskGB: 25, Hourly: "0.007", Monthly: "5"},
			{ID: "vc2-2c-2gb", Name: "vc2-2c-2gb", Location: chosen, VCPU: 2, MemoryMB: 2048, DiskGB: 55, Hourly: "0.015", Monthly: "10"},
		},
	}
}

func sampleAkamai(location string) Catalog {
	locations := []Location{
		{ID: "fr-par", Label: "Paris, FR"},
		{ID: "nl-ams", Label: "Amsterdam, NL"},
		{ID: "eu-central", Label: "Francfort, DE"},
	}
	chosen := pickLocation(locations, location, "fr-par")
	return Catalog{
		Provider: "akamai", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Akamai Connected Cloud facture à l'heure, avec un plafond au mois. Prix en dollars.",
		Offers: []Offer{
			{ID: "g6-nanode-1", Name: "Nanode 1GB", Location: chosen, VCPU: 1, MemoryMB: 1024, DiskGB: 25, Hourly: "0.0075", Monthly: "5"},
			{ID: "g6-standard-1", Name: "Linode 2GB", Location: chosen, VCPU: 1, MemoryMB: 2048, DiskGB: 50, Hourly: "0.015", Monthly: "10"},
		},
	}
}

func sampleExoscale(location string) Catalog {
	locations := []Location{
		{ID: "ch-gva-2", Label: "Genève, CH"},
		{ID: "de-fra-1", Label: "Francfort, DE"},
		{ID: "at-vie-1", Label: "Vienne, AT"},
	}
	chosen := pickLocation(locations, location, "ch-gva-2")
	disk := []Storage{{ID: "boot", Label: "Disque", MinGB: 10, MaxGB: 800, Required: true, HourlyPerGB: "0.00014", MonthlyPerGB: "0.1022"}}
	return Catalog{
		Provider: "exoscale", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Exoscale facture à l'heure. Le mois affiché correspond à 730 heures. Le disque est facturé en plus.",
		Offers: []Offer{
			{ID: "tiny-id", Name: "Standard Tiny", Location: chosen, VCPU: 1, MemoryMB: 1024, Hourly: "0.01458", Monthly: "10.6434", Storage: disk},
			{ID: "small-id", Name: "Standard Small", Location: chosen, VCPU: 2, MemoryMB: 2048, Hourly: "0.02333", Monthly: "17.0309", Storage: disk},
		},
	}
}

func sampleGcore(location string) Catalog {
	locations := []Location{
		{ID: "luxembourg-2-76", Label: "Luxembourg-2, LU"},
		{ID: "amsterdam-3-85", Label: "Amsterdam, NL"},
	}
	chosen := pickGcore(locations, location)
	disk := []Storage{{ID: "boot", Label: "Disque", MinGB: 10, MaxGB: 1000, Required: true}}
	return Catalog{
		Provider: "gcore", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "Gcore facture à l'heure. Le disque est facturé en plus. Les machines partagées et bare metal sont écartées.",
		Offers: []Offer{
			{ID: "g1-standard-1-2", Name: "g1-standard-1-2", Location: chosen, VCPU: 1, MemoryMB: 2048, Hourly: "0.0137", Monthly: "10", Storage: disk},
			{ID: "g1-standard-2-4", Name: "g1-standard-2-4", Location: chosen, VCPU: 2, MemoryMB: 4096, Hourly: "0.0274", Monthly: "20", Storage: disk},
		},
	}
}

func sampleKamatera(location string) Catalog {
	locations := []Location{
		{ID: "EU-FR", Label: "Francfort, Allemagne"},
		{ID: "EU", Label: "Amsterdam, Pays-Bas"},
		{ID: "EU-LO", Label: "Londres, Royaume-Uni"},
	}
	chosen := pickLocation(locations, location, "EU-FR")
	return Catalog{
		Provider: "kamatera", Currency: "USD", Location: chosen, Locations: locations,
		Periods: []string{"month"},
		Note:    "Kamatera facture ici au mois, trafic inclus. Prix en dollars.",
		Offers: []Offer{
			{ID: "1A-1024-20", Name: "Disponibilité 1 vCPU 1 Go", Location: chosen, VCPU: 1, MemoryMB: 1024, DiskGB: 20, Monthly: "4"},
			{ID: "2A-4096-40", Name: "Disponibilité 2 vCPU 4 Go", Location: chosen, VCPU: 2, MemoryMB: 4096, DiskGB: 40, Monthly: "19"},
		},
	}
}

func sampleOpenStack(location string) Catalog {
	locations := []Location{{ID: "region", Label: "Région"}}
	chosen := pickLocation(locations, location, "region")
	return Catalog{
		Provider: "openstack", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour"},
		Offers:  []Offer{{ID: "m1.small", Name: "m1.small", Location: chosen, VCPU: 1, MemoryMB: 2048, DiskGB: 20}},
	}
}

func sampleProxmox(location string) Catalog {
	locations := []Location{{ID: "pve", Label: "pve"}}
	chosen := pickLocation(locations, location, "pve")
	return Catalog{
		Provider: "proxmox", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"month"},
		Offers:  []Offer{{ID: "100", Name: "debian", Location: chosen, VCPU: 1, MemoryMB: 1024, DiskGB: 8}},
	}
}

func sampleIonos(location string) Catalog {
	locations := []Location{
		{ID: "de-fra", Label: "Francfort"},
		{ID: "fr-par", Label: "Paris"},
		{ID: "gb-lhr", Label: "Londres"},
	}
	chosen := pickLocation(locations, location, "de-fra")
	disk := []Storage{{ID: "boot", Label: "Disque", MinGB: 10, MaxGB: 4000, Required: true, HourlyPerGB: "0.000056", MonthlyPerGB: "0.04"}}
	return Catalog{
		Provider: "ionos", Currency: "EUR", Location: chosen, Locations: locations,
		Periods: []string{"hour", "month"},
		Note:    "IONOS facture à la minute. Le mois affiché correspond à 730 heures. Le disque est facturé en plus.",
		Offers: []Offer{
			{ID: "vcpu-1-2", Name: "vCPU 1 x 2 Go", Location: chosen, VCPU: 1, MemoryMB: 2048, Hourly: "0.016", Monthly: "11.68", Storage: disk},
			{ID: "vcpu-2-4", Name: "vCPU 2 x 4 Go", Location: chosen, VCPU: 2, MemoryMB: 4096, Hourly: "0.032", Monthly: "23.36", Storage: disk},
		},
	}
}
