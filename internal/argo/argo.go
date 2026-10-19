package argo

import "encoding/json"

type Application struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Sync      string `json:"sync"`
	Health    string `json:"health"`
}

func Parse(raw []byte) ([]Application, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Sync struct {
					Status string `json:"status"`
				} `json:"sync"`
				Health struct {
					Status string `json:"status"`
				} `json:"health"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Application, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, Application{
			Name: item.Metadata.Name, Namespace: item.Metadata.Namespace,
			Sync: item.Status.Sync.Status, Health: item.Status.Health.Status,
		})
	}
	return out, nil
}
