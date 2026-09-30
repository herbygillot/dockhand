package project

import "encoding/json"

// PackageJSON is what a package.json declares: its dependencies, and those
// for developing it.
type PackageJSON struct {
	Dependencies    map[string]string
	DevDependencies map[string]string
}

// ReadPackageJSON reads a package.json.
func ReadPackageJSON(data []byte) (PackageJSON, error) {
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return PackageJSON{}, err
	}
	return PackageJSON{Dependencies: manifest.Dependencies, DevDependencies: manifest.DevDependencies}, nil
}
