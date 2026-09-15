package macos

import "fmt"

type Release struct {
	Darwin  int
	Product string
	Name    string
	Slug    string
}

func ReleaseForDarwin(darwin int) (Release, error) {
	releases := map[int]Release{
		21: {Darwin: 21, Product: "12", Name: "Monterey", Slug: "monterey"},
		22: {Darwin: 22, Product: "13", Name: "Ventura", Slug: "ventura"},
		23: {Darwin: 23, Product: "14", Name: "Sonoma", Slug: "sonoma"},
		24: {Darwin: 24, Product: "15", Name: "Sequoia", Slug: "sequoia"},
		25: {Darwin: 25, Product: "26", Name: "Tahoe", Slug: "tahoe"},
	}
	release, ok := releases[darwin]
	if !ok {
		return Release{}, fmt.Errorf("macos: unknown release for Darwin %d", darwin)
	}
	return release, nil
}
