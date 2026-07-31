package hyperproviders

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDeploymentInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	raw := []byte(`{"preconditions":[{"target":".hypersync/revision.json","contents":".deployment/pending.json"}],"enqueue":[".hypersync/manifests/*.json",".hypersync/revision.json"]}`)
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	instructions, err := LoadDeploymentInstructions(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := instructions.Preconditions[0].Target; got != ".hypersync/revision.json" {
		t.Fatalf("target = %q", got)
	}
}

func TestLoadDeploymentInstructionsRejectsIgnoreConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), []byte(`{"ignore":["*.tmp"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DeploymentIgnoreFile), []byte("*.bak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeploymentInstructions(root); err == nil {
		t.Fatal("expected ignore conflict")
	}
}

func TestLoadDeploymentInstructionsRejectsEmptyJSONIgnoreWithLegacyIgnore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), []byte(`{"ignore":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, DeploymentIgnoreFile), []byte("*.bak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDeploymentInstructions(root); err == nil {
		t.Fatal("expected ignore conflict")
	}
}

func TestWalkAppliesJSONDeploymentIgnore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), []byte(`{"ignore":["ignored.txt"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ignored.txt", "kept.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	instructions, err := LoadDeploymentInstructions(root)
	if err != nil {
		t.Fatal(err)
	}
	ignore, err := deploymentIgnore(root, instructions)
	if err != nil {
		t.Fatal(err)
	}
	files, err := walk(root, ignore)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].RelPath() != "kept.txt" {
		t.Fatalf("files = %#v", files)
	}
}
