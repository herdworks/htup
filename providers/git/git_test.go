package git

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestGitProvider_PublishDirReplacesSubtree(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{
		"README.md":       "keep me",
		"public/old.html": "remove me",
	})

	p := newProvider(t, remote)
	if err := p.Deploy(context.Background(), source, nil); err != nil {
		t.Fatalf("seed Deploy: %v", err)
	}

	nextSource := t.TempDir()
	seedSource(t, nextSource, map[string]string{
		"index.html":   "<h1>home</h1>",
		"about/x.html": "<h1>about</h1>",
	})
	p.Config["publish_dir"] = "public"
	if err := p.Deploy(context.Background(), nextSource, nil); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	verifyDir := t.TempDir()
	if _, err := git.PlainClone(verifyDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
	}); err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(verifyDir, "README.md")); err != nil || string(got) != "keep me" {
		t.Fatalf("README.md = %q, %v; want preserved content", got, err)
	}
	for _, rel := range []string{"public/index.html", "public/about/x.html"} {
		if _, err := os.Stat(filepath.Join(verifyDir, rel)); err != nil {
			t.Errorf("expected %s in remote clone: %v", rel, err)
		}
	}
	for _, rel := range []string{"index.html", "public/old.html"} {
		if _, err := os.Stat(filepath.Join(verifyDir, rel)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be absent, got err %v", rel, err)
		}
	}
}

func TestGitProvider_SerializesSameRemoteBranchDeploys(t *testing.T) {
	remote := setupLocalRemote(t)
	pagesSource := t.TempDir()
	assetsSource := t.TempDir()
	seedSource(t, pagesSource, map[string]string{
		"index.html": "<h1>home</h1>",
	})
	seedSource(t, assetsSource, map[string]string{
		"app.css": "body { color: black; }",
	})

	pages := newProvider(t, remote)
	pages.Name = "pages"
	pages.Config["publish_dir"] = "pages"
	assets := newProvider(t, remote)
	assets.Name = "assets"
	assets.Config["publish_dir"] = "assets"

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, deploy := range []func() error{
		func() error { return pages.Deploy(context.Background(), pagesSource, nil) },
		func() error { return assets.Deploy(context.Background(), assetsSource, nil) },
	} {
		wg.Add(1)
		go func(deploy func() error) {
			defer wg.Done()
			errs <- deploy()
		}(deploy)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Deploy: %v", err)
		}
	}

	verifyDir := t.TempDir()
	if _, err := git.PlainClone(verifyDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
	}); err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	for _, rel := range []string{"pages/index.html", "assets/app.css"} {
		if _, err := os.Stat(filepath.Join(verifyDir, rel)); err != nil {
			t.Errorf("expected %s in remote clone: %v", rel, err)
		}
	}
}

func TestGitProvider_ValidatePublishDir(t *testing.T) {
	for _, tc := range []struct {
		name       string
		publishDir string
		wantErr    bool
	}{
		{name: "default root"},
		{name: "dot root", publishDir: "./"},
		{name: "nested", publishDir: "./public"},
		{name: "escapes root", publishDir: "../public", wantErr: true},
		{name: "absolute", publishDir: "/public", wantErr: true},
		{name: "git dir", publishDir: ".git", wantErr: true},
		{name: "nested git dir", publishDir: "site/.git/assets", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, "file:///tmp/remote.git")
			if tc.publishDir != "" {
				p.Config["publish_dir"] = tc.publishDir
			}
			err := p.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestGitProvider_ValidateRejectsCacheDir(t *testing.T) {
	p := newProvider(t, "file:///tmp/remote.git")
	p.Config["cache_dir"] = filepath.Join(t.TempDir(), "cache")

	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if got, want := err.Error(), "git: cache_dir is not supported"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestGitProvider_ValidateRejectsFresh(t *testing.T) {
	p := newProvider(t, "file:///tmp/remote.git")
	p.Config["fresh"] = true

	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if got, want := err.Error(), "git: fresh is not supported"; got != want {
		t.Fatalf("Validate() error = %q, want %q", got, want)
	}
}

func TestSettings_WorkdirSeparatesProviderWorkspaces(t *testing.T) {
	root := t.TempDir()
	cfg := &settings{
		RemoteURL:  "file:///tmp/remote.git",
		Branch:     "main",
		PublishDir: ".",
	}

	first, err := cfg.workdir(root, "pages")
	if err != nil {
		t.Fatalf("workdir first: %v", err)
	}
	second, err := cfg.workdir(root, "assets")
	if err != nil {
		t.Fatalf("workdir second: %v", err)
	}
	cfg.PublishDir = "public"
	third, err := cfg.workdir(root, "pages")
	if err != nil {
		t.Fatalf("workdir third: %v", err)
	}

	prefix := filepath.Join(root, hosting.ProviderWorkspaceDir, Kind) + string(filepath.Separator)
	for _, got := range []string{first, second, third} {
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("workdir %q does not live under %q", got, prefix)
		}
	}
	if first == second {
		t.Fatal("workdir should differ by provider name")
	}
	if first == third {
		t.Fatal("workdir should differ by publish dir")
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
