package htup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DeploymentResult is the durable lifecycle record for a deployment output.
type DeploymentResult struct {
	DeploymentInstructions
	ID          string           `json:"id,omitempty"`
	Status      DeploymentStatus `json:"status,omitempty"`
	StartedAt   *time.Time       `json:"started_at,omitempty"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	Error       string           `json:"error,omitempty"`
	Environment string           `json:"environment,omitempty"`
	Providers   []string         `json:"providers,omitempty"`
	Kind        DeploymentKind   `json:"kind,omitempty"`
}

func (result DeploymentResult) Validate() error {
	if err := result.DeploymentInstructions.Validate(); err != nil {
		return err
	}
	if result.Status != "" && result.Status != DeploymentRunning && result.Status != DeploymentSucceeded && result.Status != DeploymentFailed {
		return fmt.Errorf("unknown deployment result status %q", result.Status)
	}
	if result.Kind != "" {
		return result.Kind.Validate()
	}
	return nil
}

func LoadDeploymentResult(root string) (DeploymentResult, error) {
	raw, err := os.ReadFile(filepath.Join(root, DeploymentInstructionsFile))
	if errors.Is(err, os.ErrNotExist) {
		return DeploymentResult{}, nil
	}
	if err != nil {
		return DeploymentResult{}, err
	}
	var result DeploymentResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return DeploymentResult{}, fmt.Errorf("decode %s: %w", DeploymentInstructionsFile, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return DeploymentResult{}, fmt.Errorf("decode %s: trailing JSON value", DeploymentInstructionsFile)
	}
	return result, result.Validate()
}

func WriteDeploymentResult(root string, result DeploymentResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	if result.ID == "" {
		return errors.New("deployment result id is required")
	}
	if result.Status == "" {
		return errors.New("deployment result status is required")
	}
	if result.StartedAt == nil || result.StartedAt.IsZero() {
		return errors.New("deployment result started_at is required")
	}
	data, err := json.MarshalIndent(result, "", "\t")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(root, "."+DeploymentInstructionsFile+".*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0644); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(root, DeploymentInstructionsFile))
}
