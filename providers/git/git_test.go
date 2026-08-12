package git

import (
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/herdworks/htup"
	"github.com/herdworks/htup/providers/files"
	"github.com/herdworks/htup/providers/runtime"
	stdssh "golang.org/x/crypto/ssh"
)

func runtimeSecretProviders(secrets runtime.Secrets) htup.SecretsProviders {
	return htup.SecretsProviders{runtime.New(secrets)}
}

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

func newProvider(t *testing.T, remote string) *htup.HostingProvider {
	t.Helper()

	return &htup.HostingProvider{
		Kind:     Kind,
		Endpoint: remote,
		Config: map[string]any{
			"branch":       "main",
			"author_name":  "test",
			"author_email": "test@example.com",
		},
	}
}

func deploy(ctx context.Context, provider *htup.HostingProvider, root string, secretProviders htup.SecretsProviders, kind htup.DeploymentKind, w io.Writer) error {
	configPath := root + ".yaml"
	defer os.Remove(configPath)
	if err := os.WriteFile(configPath, []byte("title: test\n"), 0o644); err != nil {
		return err
	}
	return (&htup.Deployment{ConfigPath: configPath, Providers: htup.HostingProviders{"test": provider}, PayloadPath: root, Kind: kind}).Execute(ctx, secretProviders, w)
}

func TestGitProvider_FirstDeploy(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{
		"index.html":   "<h1>home</h1>",
		"about/x.html": "<h1>about</h1>",
	})

	p := newProvider(t, remote)
	if err := deploy(context.Background(), p, source, nil, htup.CompleteDeployment, nil); err != nil {
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

func TestGitProvider_FirstDeployToMissingBranchDoesNotTouchExistingBranch(t *testing.T) {
	remote := setupLocalRemote(t)
	mainSource := t.TempDir()
	seedSource(t, mainSource, map[string]string{"README.md": "main branch"})

	mainProvider := newProvider(t, remote)
	if err := deploy(context.Background(), mainProvider, mainSource, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("seed main Deploy: %v", err)
	}

	publicSource := t.TempDir()
	seedSource(t, publicSource, map[string]string{"index.html": "<h1>public</h1>"})

	publicProvider := newProvider(t, remote)
	publicProvider.Config["branch"] = "public"
	if err := deploy(context.Background(), publicProvider, publicSource, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("public Deploy: %v", err)
	}

	verifyMainDir := t.TempDir()
	if _, err := git.PlainClone(verifyMainDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
	}); err != nil {
		t.Fatalf("PlainClone main: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(verifyMainDir, "README.md")); err != nil || string(got) != "main branch" {
		t.Fatalf("main README.md = %q, %v; want preserved content", got, err)
	}
	if _, err := os.Stat(filepath.Join(verifyMainDir, "index.html")); !os.IsNotExist(err) {
		t.Fatalf("main index.html should be absent, got err %v", err)
	}

	verifyPublicDir := t.TempDir()
	if _, err := git.PlainClone(verifyPublicDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("public"),
	}); err != nil {
		t.Fatalf("PlainClone public: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(verifyPublicDir, "index.html")); err != nil || string(got) != "<h1>public</h1>" {
		t.Fatalf("public index.html = %q, %v; want deployed content", got, err)
	}
	if _, err := os.Stat(filepath.Join(verifyPublicDir, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("public README.md should be absent, got err %v", err)
	}
}

func TestGitProvider_SecondDeployIsNoOpWhenUnchanged(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{"index.html": "<h1>home</h1>"})

	p := newProvider(t, remote)
	if err := deploy(context.Background(), p, source, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("first Deploy: %v", err)
	}

	// Second deploy with identical content should be a no-op.
	if err := deploy(context.Background(), p, source, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
}

func TestGitProvider_PreconditionReplacesFinalTreeInOneCommit(t *testing.T) {
	remote := setupLocalRemote(t)
	provider := newProvider(t, remote)
	seed := t.TempDir()
	seedSource(t, seed, map[string]string{"index.html": "old page", "revision.json": "old revision"})
	if err := deploy(context.Background(), provider, seed, nil, htup.CompleteDeployment, io.Discard); err != nil {
		t.Fatalf("seed Deploy: %v", err)
	}

	next := t.TempDir()
	seedSource(t, next, map[string]string{
		"index.html":           "new page",
		".deployment/expected": "old revision",
		".deployment/pending":  "new revision",
		".deployment.json":     `{"preconditions":[{"target":"revision.json","expected":".deployment/expected","contents":".deployment/pending"}]}`,
	})
	if err := deploy(context.Background(), provider, next, nil, htup.CompleteDeployment, io.Discard); err != nil {
		t.Fatalf("conditional Deploy: %v", err)
	}

	verify := cloneBranch(t, remote)
	for rel, want := range map[string]string{"index.html": "new page", "revision.json": "new revision"} {
		got, err := os.ReadFile(filepath.Join(verify, rel))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
	if got := branchCommitCount(t, remote); got != 2 {
		t.Fatalf("commit count = %d, want 2", got)
	}
}

func TestGitProvider_PreconditionCreateOnlyAndMismatch(t *testing.T) {
	remote := setupLocalRemote(t)
	provider := newProvider(t, remote)
	seed := t.TempDir()
	seedSource(t, seed, map[string]string{"index.html": "old page"})
	if err := deploy(context.Background(), provider, seed, nil, htup.CompleteDeployment, io.Discard); err != nil {
		t.Fatalf("seed Deploy: %v", err)
	}

	create := t.TempDir()
	seedSource(t, create, map[string]string{
		"index.html":          "created page",
		".deployment/pending": "created revision",
		".deployment.json":    `{"preconditions":[{"target":"revision.json","contents":".deployment/pending"}]}`,
	})
	if err := deploy(context.Background(), provider, create, nil, htup.CompleteDeployment, io.Discard); err != nil {
		t.Fatalf("create-only Deploy: %v", err)
	}

	mismatch := t.TempDir()
	seedSource(t, mismatch, map[string]string{
		"index.html":           "should not publish",
		".deployment/expected": "wrong revision",
		".deployment/pending":  "replacement revision",
		".deployment.json":     `{"preconditions":[{"target":"revision.json","expected":".deployment/expected","contents":".deployment/pending"}]}`,
	})
	err := deploy(context.Background(), provider, mismatch, nil, htup.CompleteDeployment, io.Discard)
	if !errors.Is(err, htup.ErrDeploymentPreconditionFailed) {
		t.Fatalf("mismatched Deploy error = %v, want precondition failure", err)
	}

	verify := cloneBranch(t, remote)
	for rel, want := range map[string]string{"index.html": "created page", "revision.json": "created revision"} {
		got, err := os.ReadFile(filepath.Join(verify, rel))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
	if got := branchCommitCount(t, remote); got != 2 {
		t.Fatalf("commit count after mismatch = %d, want 2", got)
	}
}

func cloneBranch(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainClone(dir, false, &git.CloneOptions{URL: remote, ReferenceName: plumbing.NewBranchReferenceName("main")}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func branchCommitCount(t *testing.T, remote string) int {
	t.Helper()
	repo, err := git.PlainOpen(strings.TrimPrefix(remote, "file://"))
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatal(err)
	}
	iter, err := repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		t.Fatal(err)
	}
	defer iter.Close()
	count := 0
	if err := iter.ForEach(func(*object.Commit) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestGitProvider_PublishDirReplacesSubtree(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{
		"README.md":       "keep me",
		"public/old.html": "remove me",
	})

	p := newProvider(t, remote)
	if err := deploy(context.Background(), p, source, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("seed Deploy: %v", err)
	}

	nextSource := t.TempDir()
	seedSource(t, nextSource, map[string]string{
		"index.html":   "<h1>home</h1>",
		"about/x.html": "<h1>about</h1>",
	})
	p.Config["publish_dir"] = "public"
	if err := deploy(context.Background(), p, nextSource, nil, htup.CompleteDeployment, nil); err != nil {
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

func TestGitProvider_OverlayDeployPreservesExistingBranchFiles(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{
		"index.html":              "<h1>home</h1>",
		"posts/old/index.html":    "<h1>old</h1>",
		"assets/site.css":         "body { color: black; }",
		"tags/example/index.html": "<h1>tag</h1>",
	})

	p := newProvider(t, remote)
	if err := deploy(context.Background(), p, source, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("seed Deploy: %v", err)
	}

	partialSource := t.TempDir()
	seedSource(t, partialSource, map[string]string{
		"index.html":           "<h1>updated home</h1>",
		"posts/new/index.html": "<h1>new</h1>",
	})
	if err := deploy(context.Background(), p, partialSource, nil, htup.IncrementalDeployment, nil); err != nil {
		t.Fatalf("overlay Deploy: %v", err)
	}

	verifyDir := t.TempDir()
	if _, err := git.PlainClone(verifyDir, false, &git.CloneOptions{
		URL:           remote,
		ReferenceName: plumbing.NewBranchReferenceName("main"),
	}); err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	for rel, want := range map[string]string{
		"index.html":              "<h1>updated home</h1>",
		"posts/new/index.html":    "<h1>new</h1>",
		"posts/old/index.html":    "<h1>old</h1>",
		"assets/site.css":         "body { color: black; }",
		"tags/example/index.html": "<h1>tag</h1>",
	} {
		got, err := os.ReadFile(filepath.Join(verifyDir, rel))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
}

func TestGitProvider_OverlayDeployRequiresExistingBranch(t *testing.T) {
	remote := setupLocalRemote(t)
	source := t.TempDir()
	seedSource(t, source, map[string]string{"index.html": "<h1>home</h1>"})

	p := newProvider(t, remote)
	err := deploy(context.Background(), p, source, nil, htup.IncrementalDeployment, nil)
	if err == nil {
		t.Fatal("Deploy() error = nil, want overlay missing branch error")
	}
	if !strings.Contains(err.Error(), "overlay deployment requires existing remote branch") {
		t.Fatalf("Deploy() error = %v, want overlay missing branch error", err)
	}
	if err := deploy(context.Background(), p, source, nil, htup.CompleteDeployment, nil); err != nil {
		t.Fatalf("replacement Deploy after failed overlay: %v", err)
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
	pages.Config["publish_dir"] = "pages"
	assets := newProvider(t, remote)
	assets.Config["publish_dir"] = "assets"

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, deploy := range []func() error{
		func() error {
			return deploy(context.Background(), pages, pagesSource, nil, htup.CompleteDeployment, nil)
		},
		func() error {
			return deploy(context.Background(), assets, assetsSource, nil, htup.CompleteDeployment, nil)
		},
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

func TestGitProvider_ReadyFileRemoteNeedsNoAuth(t *testing.T) {
	p := newProvider(t, "file:///tmp/remote.git")

	if err := p.Ready(context.Background(), nil); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestGitProvider_ReadyHTTPRequiresRuntimeToken(t *testing.T) {
	p := newProvider(t, "https://example.com/repo.git")
	p.Secrets = htup.Secrets{{Name: "token", Key: "GIT_TOKEN"}}

	err := p.Ready(context.Background(), nil)
	if err == nil {
		t.Fatal("Ready error = nil, want missing token")
	}
	if !strings.Contains(err.Error(), "GIT_TOKEN") {
		t.Fatalf("Ready error = %v, want GIT_TOKEN", err)
	}
}

func TestGitProvider_ReadyHTTPWithRuntimeToken(t *testing.T) {
	p := newProvider(t, "https://example.com/repo.git")
	p.Secrets = htup.Secrets{{Name: "token", Key: "GIT_TOKEN"}}
	secretProviders := runtimeSecretProviders(runtime.Secrets{"GIT_TOKEN": htup.StringSecret("secret")})

	if err := p.Ready(context.Background(), secretProviders); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestGitProvider_ReadySSHRequiresPrivateKey(t *testing.T) {
	p := newProvider(t, "git@example.com:repo.git")
	p.Secrets = htup.Secrets{{Name: "ssh_key_path", Key: "DEPLOY_SSH_KEY"}}
	secretProviders := runtimeSecretProviders(runtime.Secrets{"DEPLOY_SSH_KEY": htup.StringSecret("/tmp/deploy_key")})

	err := p.Ready(context.Background(), secretProviders)
	if err == nil {
		t.Fatal("Ready error = nil, want missing ssh_private_key")
	}
	if !strings.Contains(err.Error(), secretSSHPrivateKey) {
		t.Fatalf("Ready error = %v, want %s", err, secretSSHPrivateKey)
	}
}

func TestGitProvider_ReadySSHWithRuntimePrivateKey(t *testing.T) {
	p := newProvider(t, "git@example.com:repo.git")
	p.Secrets = htup.Secrets{{Name: secretSSHPrivateKey, Key: "DEPLOY_SSH_KEY"}}
	secretProviders := runtimeSecretProviders(runtime.Secrets{"DEPLOY_SSH_KEY": htup.StringSecret(testSSHPrivateKeyPEM(t))})

	if err := p.Ready(context.Background(), secretProviders); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	cfg, err := newSettings(p)
	if err != nil {
		t.Fatalf("newSettings: %v", err)
	}
	auth, err := resolveAuth(context.Background(), secretProviders, cfg, p.Secrets)
	if err != nil {
		t.Fatalf("resolveAuth: %v", err)
	}
	publicKeys, ok := auth.(*gitssh.PublicKeys)
	if !ok {
		t.Fatalf("auth = %T, want *gitssh.PublicKeys", auth)
	}
	if publicKeys.HostKeyCallback == nil {
		t.Fatal("HostKeyCallback = nil, want explicit callback")
	}
}

func TestGitProvider_ReadySSHWithFilesPrivateKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "deploy_key.pem")
	if err := os.WriteFile(keyPath, []byte(testSSHPrivateKeyPEM(t)), 0o600); err != nil {
		t.Fatal(err)
	}

	p := newProvider(t, "git@example.com:repo.git")
	p.Secrets = htup.Secrets{{Name: secretSSHPrivateKey, Key: keyPath}}
	secretProviders := htup.SecretsProviders{files.New()}

	if err := p.Ready(context.Background(), secretProviders); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestGitProvider_ReadySSHWithKnownHosts(t *testing.T) {
	p := newProvider(t, "git@example.com:repo.git")
	p.Config["known_hosts"] = testKnownHostsLine(t)
	p.Secrets = htup.Secrets{{Name: secretSSHPrivateKey, Key: "DEPLOY_SSH_KEY"}}
	secretProviders := runtimeSecretProviders(runtime.Secrets{"DEPLOY_SSH_KEY": htup.StringSecret(testSSHPrivateKeyPEM(t))})

	if err := p.Ready(context.Background(), secretProviders); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestGitProvider_ReadySSHRejectsInvalidKnownHosts(t *testing.T) {
	p := newProvider(t, "git@example.com:repo.git")
	p.Config["known_hosts"] = "not known_hosts"
	p.Secrets = htup.Secrets{{Name: secretSSHPrivateKey, Key: "DEPLOY_SSH_KEY"}}
	secretProviders := runtimeSecretProviders(runtime.Secrets{"DEPLOY_SSH_KEY": htup.StringSecret(testSSHPrivateKeyPEM(t))})

	err := p.Ready(context.Background(), secretProviders)
	if err == nil {
		t.Fatal("Ready error = nil, want known_hosts error")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("Ready error = %v, want known_hosts", err)
	}
}

func TestGitProvider_ReadySSHReportsMissingPassphrase(t *testing.T) {
	p := newProvider(t, "git@example.com:repo.git")
	p.Secrets = htup.Secrets{
		{Name: secretSSHPrivateKey, Key: "DEPLOY_SSH_KEY"},
		{Name: secretSSHPassphrase, Key: "DEPLOY_SSH_PASSPHRASE"},
	}
	secretProviders := runtimeSecretProviders(runtime.Secrets{"DEPLOY_SSH_KEY": htup.StringSecret(testSSHPrivateKeyPEM(t))})

	err := p.Ready(context.Background(), secretProviders)
	if err == nil {
		t.Fatal("Ready error = nil, want missing passphrase")
	}
	if !strings.Contains(err.Error(), secretSSHPassphrase) {
		t.Fatalf("Ready error = %v, want %s", err, secretSSHPassphrase)
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

	prefix := filepath.Join(root, htup.ProviderWorkspaceDir, Kind) + string(filepath.Separator)
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
	if err := deploy(ctx, p, source, nil, htup.CompleteDeployment, nil); err != nil {
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
		"endpoint": "https://x.test/r",
		"config": {
			"branch": "main"
		}
	}`)
	var p htup.HostingProvider
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if p.Kind != "git" || p.DisplayName() != "git" {
		t.Errorf("kind/name = %s/%s, want git/git", p.Kind, p.DisplayName())
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func testSSHPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := stdssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func testKnownHostsLine(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := stdssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return "example.com " + strings.TrimSpace(string(stdssh.MarshalAuthorizedKey(signer.PublicKey())))
}
