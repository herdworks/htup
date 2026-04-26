package git

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/herdworks/hyperproviders-go/hosting"
)

// setupLocalRemote creates a bare git repo that serves as the remote for
// provider tests. Returns its file:// URL.
func setupLocalRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if _, err := git.PlainInit(remote, true); err != nil {
		t.Fatalf("PlainInit bare: %v", err)
	}
	return "file://" + remote
}

// seedSource writes files into dir.
func seedSource(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newProvider(t *testing.T, remote string) *hosting.Provider {
	t.Helper()
	return &hosting.Provider{
		Kind:     Kind,
		Name:     "test",
		Endpoint: remote,
		Config: map[string]any{
			"branch":       "main",
			"author_name":  "test",
			"author_email": "test@example.com",
			"cache_dir":    filepath.Join(t.TempDir(), "cache"),
		},
	}
}

func TestGitProvider_FirstDeploy(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{
		"index.html":   "<h1>home</h1>",
		"about/x.html": "<h1>about</h1>",
	})

	p := newProvider(t, remote)
	if err := p.Deploy(context.Background(), source, nil); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// Clone the remote into a fresh dir and confirm the files landed.
	verifyDir := t.TempDir()
	if _, err := git.PlainClone(verifyDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
	}); err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	for _, rel := range []string{"index.html", "about/x.html"} {
		if _, err := os.Stat(filepath.Join(verifyDir, rel)); err != nil {
			t.Errorf("expected %s in remote clone: %v", rel, err)
		}
	}
}

func TestGitProvider_SecondDeployIsNoOpWhenUnchanged(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{"index.html": "<h1>home</h1>"})

	p := newProvider(t, remote)
	if err := p.Deploy(context.Background(), source, nil); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}

	// Second deploy with identical content should be a no-op.
	if err := p.Deploy(context.Background(), source, nil); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
}

func TestGitProvider_TagEnabled(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{"index.html": "<h1>home</h1>"})

	p := newProvider(t, remote)
	p.Config["tag_enabled"] = true
	p.Config["tag_prefix"] = "build-"

	ctx := WithBuildID(context.Background(), "abc123")
	if err := p.Deploy(ctx, source, nil); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	// Clone with tags and verify our tag is present.
	verifyDir := t.TempDir()
	repo, err := git.PlainClone(verifyDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
		Tags:          git.AllTags,
	})
	if err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	tags, err := repo.Tags()
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	found := false
	if err := tags.ForEach(func(ref *plumbing.Reference) error {
		if ref.Name().Short() == "build-abc123" {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("tags iteration: %v", err)
	}
	if !found {
		t.Error("expected tag build-abc123 in remote, not found")
	}
}

func TestGitProvider_UnmarshalHostingProvider(t *testing.T) {
	raw := []byte(`{
		"kind": "git",
		"name": "p",
		"endpoint": "https://x.test/r",
		"config": {
			"branch": "main"
		}
	}`)
	var p hosting.Provider
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Kind != "git" || p.DisplayName() != "p" {
		t.Errorf("kind/name = %s/%s, want git/p", p.Kind, p.DisplayName())
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}
