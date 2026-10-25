package library

import (
	"fmt"
	"strconv"
	"strings"

	"myinfra/internal/selfupdate"
)

// Bumps lists the items of next that changed since previous without a newer
// version, or whose version went back: a copy could not tell them apart.
func Bumps(previous, next *Library) []string {
	var out []string
	compare := func(kind, id string, before, after Item, sumBefore, sumAfter string) {
		switch {
		case selfupdate.Newer(before.Version, after.Version):
			out = append(out, fmt.Sprintf("%s %s : version %s plus ancienne que %s", kind, id, after.Version, before.Version))
		case sumBefore != sumAfter && !selfupdate.Newer(after.Version, before.Version):
			out = append(out, fmt.Sprintf("%s %s : changé, sa version %s doit monter", kind, id, after.Version))
		}
	}
	for _, p := range next.Packages {
		if old, ok := previous.Package(p.ID); ok {
			compare("paquet", p.ID, old.Item, p.Item, old.Sum, p.Sum)
		}
	}
	for _, t := range next.Templates {
		if old, ok := previous.Template(t.ID); ok {
			compare("template", t.ID, old.Item, t.Item, old.Sum, t.Sum)
		}
	}
	// An id stays: templates and copies refer to it.
	for _, p := range previous.Packages {
		if _, ok := next.Package(p.ID); !ok {
			out = append(out, "paquet "+p.ID+" : retiré, un identifiant publié reste")
		}
	}
	return out
}

// Level is how far version next goes from current: patch, minor or major.
func Level(current, next string) string {
	a, b := numbers(current), numbers(next)
	switch {
	case a[0] != b[0]:
		return "major"
	case a[1] != b[1]:
		return "minor"
	default:
		return "patch"
	}
}

func numbers(version string) [3]int {
	var out [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), "-")
	for i, part := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out
}

// ReadArchive reads the library of an archive without its signature, to
// compare a tree with a previous release.
func ReadArchive(archive []byte) (*Library, error) {
	fsys, index, err := unpack(archive)
	if err != nil {
		return nil, err
	}
	return Read(fsys, index.Release, "")
}
