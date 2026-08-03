package hyperproviders

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFile_RelPath(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "blog", "post")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(nested, "index.html")
	if err := os.WriteFile(abs, []byte("<html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := newFile(root, abs)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}

	got := f.RelPath()
	want := "blog/post/index.html"
	if got != want {
		t.Errorf("RelPath = %q, want %q", got, want)
	}
	if strings.HasPrefix(got, "/") {
		t.Errorf("RelPath should not have leading slash: %q", got)
	}
}

func TestFile_Metadata(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "page.html")
	content := []byte("<!doctype html><title>hi</title>")
	if err := os.WriteFile(abs, content, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := newFile(root, abs)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}

	mime, err := f.Mimetype()
	if err != nil {
		t.Fatalf("Mimetype: %v", err)
	}
	if !strings.HasPrefix(mime, "text/html") {
		t.Errorf("Mimetype = %q, want text/html prefix", mime)
	}

	etag, err := f.ETag()
	if err != nil {
		t.Fatalf("ETag: %v", err)
	}
	if len(etag) != 32 {
		t.Errorf("ETag length = %d, want 32 (md5 hex)", len(etag))
	}
}

func TestFile_RejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "other.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newFile(root, outside); err == nil {
		t.Fatal("expected error for path outside root, got nil")
	}
}

func TestFile_RejectsNonRegular(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := newFile(root, dir); err == nil {
		t.Fatal("expected error for non-regular file, got nil")
	}
}

func TestFile_RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	link := filepath.Join(root, "link.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := newFile(root, link); !errors.Is(err, ErrFileSymlink) {
		t.Fatalf("newFile symlink error = %v, want ErrFileSymlink", err)
	}
}

func TestWalk_DeterministicAndRelative(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		"zeta/last.txt",
		"alpha/first.txt",
		"beta/nested/deep.txt",
		"README.md",
	}
	for _, p := range paths {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := walk(root, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != len(paths) {
		t.Fatalf("Walk returned %d files, want %d", len(files), len(paths))
	}

	got := make([]string, len(files))
	for i, f := range files {
		got[i] = f.RelPath()
		if strings.HasPrefix(got[i], "/") {
			t.Errorf("RelPath should not have leading slash: %q", got[i])
		}
	}

	want := []string{
		"README.md",
		"alpha/first.txt",
		"beta/nested/deep.txt",
		"zeta/last.txt",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Walk[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWalk_IgnoresProviderWorkspaceDir(t *testing.T) {
	root := t.TempDir()
	paths := map[string]string{
		"index.html":  "deploy me",
		".git/config": "ignore me",
		"nested/" + ProviderWorkspaceDir + "/file.txt": "deploy nested workspace name",
		ProviderWorkspaceDir + "/git/cache/file":       "ignore me",
	}
	for rel, content := range paths {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := walk(root, nil)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("Walk returned %d files, want 2", len(files))
	}
	got := []string{files[0].RelPath(), files[1].RelPath()}
	want := []string{"index.html", "nested/" + ProviderWorkspaceDir + "/file.txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RelPath[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestWalk_AppliesDeploymentIgnore(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		"index.html",
		"assets/app.css",
		"build.log",
		"logs/current.log",
		DeploymentIgnoreFile,
	}
	for _, rel := range paths {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, DeploymentIgnoreFile), []byte("\n/build.log\n logs/ \n"), 0o644); err != nil {
		t.Fatal(err)
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
		t.Fatalf("Walk: %v", err)
	}
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.RelPath())
	}
	want := []string{"assets/app.css", "index.html"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("walk paths = %v, want %v", got, want)
	}
}

func TestProvider_DeploymentFilesAppliesDeploymentIgnoreBeforeProviderFilter(t *testing.T) {
	RegisterEngine("deploymentignore-test", &fakeEngine{})

	root := t.TempDir()
	for _, rel := range []string{"keep.log", "ignored.log"} {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, DeploymentIgnoreFile), []byte("ignored.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	provider := &HostingProvider{
		Kind:     "deploymentignore-test",
		Endpoint: "https://example.com",
		Include:  FilterRules{Paths: []string{`\.log$`}},
	}
	files, err := providerFiles(root, provider)
	if err != nil {
		t.Fatalf("DeploymentFiles: %v", err)
	}
	if len(files) != 1 || files[0].RelPath() != "keep.log" {
		t.Fatalf("DeploymentFiles paths = %v, want [keep.log]", files)
	}
}

func TestWalk_RejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	link := filepath.Join(root, "link.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := walk(root, nil); !errors.Is(err, ErrFileSymlink) {
		t.Fatalf("walk symlink error = %v, want ErrFileSymlink", err)
	}
}

func TestWalk_RejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := walk(root, nil); !errors.Is(err, ErrFileSymlink) {
		t.Fatalf("walk symlink dir error = %v, want ErrFileSymlink", err)
	}
}

func TestProvider_DeploymentFilesFilters(t *testing.T) {
	RegisterEngine("file-test", &fakeEngine{})

	root := t.TempDir()
	for _, p := range []string{"site/a.html", "site/b.css", "site/image.png", "site/.git/config"} {
		abs := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	provider := &HostingProvider{
		Kind:     "file-test",
		Endpoint: "https://example.com",
		Exclude:  FilterRules{Paths: []string{`\.git/`}},
	}
	files, err := providerFiles(mustAbs(t, root), provider)
	if err != nil {
		t.Fatalf("DeploymentFiles: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.RelPath(), ".git/") {
			t.Errorf("excluded path still in results: %s", f.RelPath())
		}
	}

	provider = &HostingProvider{
		Kind:     "file-test",
		Endpoint: "https://example.com",
		Include:  FilterRules{Paths: []string{`\.html$`}},
	}
	only, err := providerFiles(root, provider)
	if err != nil {
		t.Fatalf("DeploymentFiles: %v", err)
	}
	if len(only) != 1 {
		t.Fatalf("include filter returned %d files, want 1", len(only))
	}
	if only[0].RelPath() != "site/a.html" {
		t.Errorf("include filter returned %s, want site/a.html", only[0].RelPath())
	}

	provider = &HostingProvider{
		Kind:     "file-test",
		Endpoint: "https://example.com",
		Include:  FilterRules{Paths: []string{`\.html$`}},
		Exclude:  FilterRules{Paths: []string{`a\.html$`}},
	}
	only, err = providerFiles(root, provider)
	if err != nil {
		t.Fatalf("DeploymentFiles: %v", err)
	}
	if len(only) != 0 {
		t.Fatalf("exclude should win over include; got %d files", len(only))
	}
}

func providerFiles(root string, provider *HostingProvider) ([]*File, error) {
	instructions, err := LoadDeploymentInstructions(root)
	if err != nil {
		return nil, err
	}
	ignore, err := deploymentIgnore(root, instructions)
	if err != nil {
		return nil, err
	}
	files, err := walk(root, ignore)
	if err != nil {
		return nil, err
	}
	return provider.deploymentFiles(nil, nil, files)
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
