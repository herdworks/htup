package hosting

import (
	"os"
	"path/filepath"
	"strings"
)

const DeploymentIgnoreFile = ".deploymentignore"

type deploymentIgnore []string

func loadDeploymentIgnore(root string) (deploymentIgnore, error) {
	data, err := os.ReadFile(filepath.Join(root, DeploymentIgnoreFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	entries := deploymentIgnore{}
	for _, line := range strings.Split(string(data), "\n") {
		entry := strings.TrimSpace(line)
		entry = strings.TrimLeft(entry, "/")
		entry = filepath.ToSlash(entry)
		if entry == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (ignore deploymentIgnore) Match(rel string) bool {
	rel = strings.TrimLeft(filepath.ToSlash(rel), "/")
	if rel == DeploymentIgnoreFile {
		return true
	}
	for _, entry := range ignore {
		if strings.HasPrefix(rel, entry) {
			return true
		}
	}
	return false
}

func (ignore deploymentIgnore) MatchDir(rel string) bool {
	rel = strings.Trim(strings.TrimLeft(filepath.ToSlash(rel), "/"), "/")
	if rel == "" {
		return false
	}
	return ignore.Match(rel + "/")
}
