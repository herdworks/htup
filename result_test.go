package htup

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDeploymentResultRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	startedAt := time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Second)
	want := DeploymentResult{
		DeploymentInstructions: DeploymentInstructions{Ignore: []string{"build.log"}, Enqueue: []string{"feed.json"}},
		ID:                     "deployment-1",
		Status:                 DeploymentSucceeded,
		StartedAt:              &startedAt,
		CompletedAt:            &completedAt,
		Environment:            "production",
		Providers:              []string{"pages"},
		Kind:                   IncrementalDeployment,
	}
	if err := WriteDeploymentResult(root, want); err != nil {
		t.Fatalf("WriteDeploymentResult() error = %v", err)
	}
	got, err := LoadDeploymentResult(root)
	if err != nil {
		t.Fatalf("LoadDeploymentResult() error = %v", err)
	}
	if got.ID != want.ID || got.Status != want.Status || got.Environment != want.Environment || got.Kind != want.Kind {
		t.Fatalf("result identity = %#v, want %#v", got, want)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(startedAt) || got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("result lifecycle = %#v, want %s through %s", got, startedAt, completedAt)
	}
	if !slices.Equal(got.Providers, want.Providers) || !slices.Equal(got.Ignore, want.Ignore) || !slices.Equal(got.Enqueue, want.Enqueue) {
		t.Fatalf("result fields = %#v, want %#v", got, want)
	}
}

func TestLoadDeploymentResultSupportsLegacyInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), []byte(`{"ignore":["build.log"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := LoadDeploymentResult(root)
	if err != nil {
		t.Fatalf("LoadDeploymentResult() error = %v", err)
	}
	if result.Status != "" || !slices.Equal(result.Ignore, []string{"build.log"}) {
		t.Fatalf("legacy result = %#v", result)
	}
	instructions, err := LoadDeploymentInstructions(root)
	if err != nil || !slices.Equal(instructions.Ignore, []string{"build.log"}) {
		t.Fatalf("LoadDeploymentInstructions() = %#v, %v", instructions, err)
	}
}

func TestDeploymentResultRejectsTransientStatuses(t *testing.T) {
	t.Parallel()
	for _, status := range []DeploymentStatus{DeploymentPending, DeploymentPlanning, DeploymentReady} {
		t.Run(string(status), func(t *testing.T) {
			if err := (DeploymentResult{Status: status}).Validate(); err == nil {
				t.Fatalf("Validate() error = nil for %q", status)
			}
		})
	}
}

func TestLoadDeploymentResultRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()
	for name, raw := range map[string]string{
		"unknown field":  `{"unknown":true}`,
		"trailing value": `{} {}`,
		"malformed":      `{`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), []byte(raw), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadDeploymentResult(root); err == nil {
				t.Fatal("LoadDeploymentResult() error = nil")
			}
		})
	}
}

func TestWriteDeploymentResultRequiresLifecycleIdentity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	startedAt := time.Now().UTC()
	for name, result := range map[string]DeploymentResult{
		"id":         {Status: DeploymentRunning, StartedAt: &startedAt},
		"status":     {ID: "deployment-1", StartedAt: &startedAt},
		"started at": {ID: "deployment-1", Status: DeploymentRunning},
	} {
		t.Run(name, func(t *testing.T) {
			if err := WriteDeploymentResult(root, result); err == nil {
				t.Fatal("WriteDeploymentResult() error = nil")
			}
		})
	}
}

func TestDeploymentIgnoreReservesResultFiles(t *testing.T) {
	t.Parallel()
	ignore := DeploymentIgnore(nil)
	for _, path := range []string{DeploymentInstructionsFile, BuildResultFile} {
		if !ignore.Match(path) {
			t.Fatalf("Match(%q) = false, want true", path)
		}
	}
}

func TestWriteDeploymentResultAtomicallyReplacesRecord(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	startedAt := time.Now().UTC()
	for _, status := range []DeploymentStatus{DeploymentRunning, DeploymentSucceeded} {
		if err := WriteDeploymentResult(root, DeploymentResult{ID: "deployment-1", Status: status, StartedAt: &startedAt}); err != nil {
			t.Fatalf("WriteDeploymentResult(%q) error = %v", status, err)
		}
	}
	result, err := LoadDeploymentResult(root)
	if err != nil || result.Status != DeploymentSucceeded {
		t.Fatalf("LoadDeploymentResult() = %#v, %v", result, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+DeploymentInstructionsFile+".") {
			t.Fatalf("temporary result file remains: %s", entry.Name())
		}
	}
}
