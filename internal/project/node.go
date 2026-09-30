package project

import "encoding/json"

// PackageJSON is what a package.json declares: its dependencies, and those
// for developing it; its license, as an SPDX expression; and the
// directories below it that are its workspaces, as glob patterns, which
// yarn and npm install together.
type PackageJSON struct {
	Dependencies    map[string]string
	DevDependencies map[string]string
	License         string
	Workspaces      []string
}

// ReadPackageJSON reads a package.json. A license that isn't a string, as
// the old {"type": …} object isn't, is left out; workspaces are an array
// of patterns, or yarn's object holding them as its packages, and any
// other shape names none.
func ReadPackageJSON(data []byte) (PackageJSON, error) {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		License         any               `json:"license"`
		Workspaces      json.RawMessage   `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageJSON{}, err
	}
	found := PackageJSON{Dependencies: manifest.Dependencies, DevDependencies: manifest.DevDependencies}
	found.License, _ = manifest.License.(string)
	if len(manifest.Workspaces) > 0 && json.Unmarshal(manifest.Workspaces, &found.Workspaces) != nil {
		var yarn struct {
			Packages []string `json:"packages"`
		}
		// Workspaces of another shape name none, and leave the rest of
		// the manifest as it reads.
		_ = json.Unmarshal(manifest.Workspaces, &yarn)
		found.Workspaces = yarn.Packages
	}
	return found, nil
}
