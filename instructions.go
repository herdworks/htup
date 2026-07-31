package hyperproviders

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type DeploymentInstructions struct {
	Preconditions []Precondition `json:"preconditions,omitempty"`
	Ignore        []string       `json:"ignore,omitempty"`
	Enqueue       []string       `json:"enqueue,omitempty"`
}

type Precondition struct {
	Target   string `json:"target"`
	Expected string `json:"expected,omitempty"`
	Contents string `json:"contents"`
}

type DeploymentIgnore []string

const (
	DeploymentInstructionsFile = ".deployment.json"
	DeploymentIgnoreFile       = ".deploymentignore"
)

func (ignore DeploymentIgnore) Match(rel string) bool {
	rel = strings.TrimLeft(filepath.ToSlash(rel), "/")
	if rel == DeploymentIgnoreFile || rel == DeploymentInstructionsFile {
		return true
	}
	for _, entry := range ignore {
		if matched, err := path.Match(entry, rel); err == nil && matched {
			return true
		}
		if strings.HasPrefix(rel, entry) {
			return true
		}
	}
	return false
}
func (ignore DeploymentIgnore) MatchDir(rel string) bool {
	rel = strings.Trim(strings.TrimLeft(filepath.ToSlash(rel), "/"), "/")
	return rel != "" && ignore.Match(rel+"/")
}
func (instructions DeploymentInstructions) Validate() error {
	for _, condition := range instructions.Preconditions {
		if err := deploymentRelative(condition.Target); err != nil {
			return fmt.Errorf("precondition target: %w", err)
		}
		if condition.Contents == "" {
			return errors.New("precondition contents is required")
		}
		if err := deploymentRelative(condition.Contents); err != nil {
			return fmt.Errorf("precondition contents: %w", err)
		}
		if condition.Expected != "" {
			if err := deploymentRelative(condition.Expected); err != nil {
				return fmt.Errorf("precondition expected: %w", err)
			}
		}
	}
	for _, pattern := range append(append([]string{}, instructions.Ignore...), instructions.Enqueue...) {
		if pattern == "" || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\\") {
			return fmt.Errorf("invalid deployment glob %q", pattern)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("invalid deployment glob %q: %w", pattern, err)
		}
	}
	return nil
}
func LoadDeploymentInstructions(root string) (DeploymentInstructions, error) {
	raw, err := os.ReadFile(filepath.Join(root, DeploymentInstructionsFile))
	if errors.Is(err, os.ErrNotExist) {
		return DeploymentInstructions{}, nil
	}
	if err != nil {
		return DeploymentInstructions{}, err
	}
	var instructions DeploymentInstructions
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&instructions); err != nil {
		return DeploymentInstructions{}, fmt.Errorf("decode %s: %w", DeploymentInstructionsFile, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return DeploymentInstructions{}, fmt.Errorf("decode %s: trailing JSON value", DeploymentInstructionsFile)
	}
	if err := instructions.Validate(); err != nil {
		return DeploymentInstructions{}, err
	}
	if instructions.Ignore != nil {
		if _, err := os.Stat(filepath.Join(root, DeploymentIgnoreFile)); err == nil {
			return DeploymentInstructions{}, fmt.Errorf("%s ignore cannot be combined with %s", DeploymentInstructionsFile, DeploymentIgnoreFile)
		} else if !errors.Is(err, os.ErrNotExist) {
			return DeploymentInstructions{}, err
		}
	}
	return instructions, nil
}
func deploymentIgnore(root string, instructions DeploymentInstructions) (DeploymentIgnore, error) {
	if instructions.Ignore != nil {
		return DeploymentIgnore(instructions.Ignore), nil
	}
	data, err := os.ReadFile(filepath.Join(root, DeploymentIgnoreFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries DeploymentIgnore
	for _, line := range strings.Split(string(data), "\n") {
		entry := filepath.ToSlash(strings.TrimLeft(strings.TrimSpace(line), "/"))
		if entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
func deploymentRelative(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") || path.Clean(value) != value || value == "." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("must be a normalized deployment-relative path: %q", value)
	}
	return nil
}
