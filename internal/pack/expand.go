package pack

import (
	"fmt"

	"myinfra/internal/spec"
)

// Expand fills in the {{…}} of a package for one installation: the common
// variables, and each service's own variables and files. A secret named
// twice gets the same value.
func Expand(p Package, host string, inputs map[string]string) ([]Service, map[string]string, error) {
	if problems := Check(p); len(problems) > 0 {
		return nil, nil, fmt.Errorf("Le paquet n'est pas valide : %s", problems[0].Error())
	}
	fill, err := filler(host, p.Inputs, inputs)
	if err != nil {
		return nil, nil, err
	}
	env, err := fillMap(p.Env, fill)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Service, len(p.Services))
	for i, svc := range p.Services {
		next := svc
		next.Args = append([]string(nil), svc.Args...)
		next.Command = append([]string(nil), svc.Command...)
		next.Ports = append([]spec.Port(nil), svc.Ports...)
		next.Volumes = append([]spec.Volume(nil), svc.Volumes...)
		next.DependsOn = append([]string(nil), svc.DependsOn...)
		if svc.Resources != nil {
			copied := *svc.Resources
			next.Resources = &copied
		}
		if svc.Health != nil {
			copied := *svc.Health
			next.Health = &copied
		}
		if next.Env, err = fillMap(svc.Env, fill); err != nil {
			return nil, nil, err
		}
		next.Files = make([]spec.File, len(svc.Files))
		for j, file := range svc.Files {
			content, err := fill(file.Content)
			if err != nil {
				return nil, nil, err
			}
			next.Files[j] = spec.File{Path: file.Path, Content: content}
		}
		if len(next.Files) == 0 {
			next.Files = nil
		}
		out[i] = next
	}
	return out, env, nil
}

func fillMap(in map[string]string, fill func(string) (string, error)) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		next, err := fill(value)
		if err != nil {
			return nil, err
		}
		out[key] = next
	}
	return out, nil
}
