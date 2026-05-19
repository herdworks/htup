package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/herdworks/hyperproviders-go/secrets"
	"github.com/herdworks/hyperproviders-go/session"
)

type fakeEngine struct{}

func (e *fakeEngine) Validate(provider *Provider) error {
	return nil
}

func (e *fakeEngine) Deploy(ctx context.Context, session *session.Session, provider *Provider, root string, files []*File) error {
	return nil
}

type failingEngine struct{}

func (e *failingEngine) Validate(provider *Provider) error {
	return nil
}

func (e *failingEngine) Deploy(ctx context.Context, session *session.Session, provider *Provider, root string, files []*File) error {
	return errors.New("deploy failed")
}

func mkFile(t *testing.T, root, rel string) *File {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := newFile(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestFilter_Empty(t *testing.T) {
	root := t.TempDir()
	f := mkFile(t, root, "a.html")
	filter := &Filter{}
	ok, err := filter.Match(f)
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if !ok {
		t.Error("empty filter should accept all files")
	}
}

func TestFilter_IncludePaths(t *testing.T) {
	root := t.TempDir()
	html := mkFile(t, root, "page.html")
	css := mkFile(t, root, "style.css")

	filter := &Filter{Include: FilterRules{Paths: []string{`\.html$`}}}
	htmlOK, _ := filter.Match(html)
	cssOK, _ := filter.Match(css)
	if !htmlOK {
		t.Error("html should pass include filter")
	}
	if cssOK {
		t.Error("css should not pass include filter")
	}
}

func TestFilter_ExcludeTypes(t *testing.T) {
	root := t.TempDir()
	html := mkFile(t, root, "page.html")
	// png file contents don't matter for extension-based mime detection
	png := mkFile(t, root, "img.png")

	filter := &Filter{Exclude: FilterRules{Types: []string{`^image/`}}}
	htmlOK, _ := filter.Match(html)
	pngOK, err := filter.Match(png)
	if err != nil {
		t.Fatalf("Match png: %v", err)
	}
	if !htmlOK {
		t.Error("html should not be excluded by image type filter")
	}
	if pngOK {
		t.Error("png should be excluded by image type filter")
	}
}

func TestFilter_IncludeAndExclude(t *testing.T) {
	root := t.TempDir()
	a := mkFile(t, root, "docs/a.html")
	b := mkFile(t, root, "docs/b.html")
	c := mkFile(t, root, "docs/draft/c.html")

	filter := &Filter{
		Include: FilterRules{Paths: []string{`\.html$`}},
		Exclude: FilterRules{Paths: []string{`^docs/draft/`}},
	}
	for _, tc := range []struct {
		file *File
		want bool
	}{
		{a, true},
		{b, true},
		{c, false},
	} {
		got, err := filter.Match(tc.file)
		if err != nil {
			t.Fatalf("Match: %v", err)
		}
		if got != tc.want {
			t.Errorf("file %s: got %v, want %v", tc.file.RelPath(), got, tc.want)
		}
	}
}

func TestFilter_InvalidRegex(t *testing.T) {
	root := t.TempDir()
	f := mkFile(t, root, "a.html")
	filter := &Filter{Include: FilterRules{Paths: []string{"(unclosed"}}}
	if _, err := filter.Match(f); err == nil {
		t.Error("expected error on invalid regex")
	}
}

func TestProvider_DisplayNameAndValidate(t *testing.T) {
	RegisterEngine("test", &fakeEngine{})

	p := &Provider{
		Kind:     "test",
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if p.DisplayName() != "test" {
		t.Errorf("DisplayName = %q, want test", p.DisplayName())
	}

	empty := &Provider{}
	if !errors.Is(empty.ValidateRoot(), ErrMissingKind) {
		t.Error("ValidateRoot should error when provider kind is empty")
	}
	if err := p.ValidateRoot(); err != nil {
		t.Errorf("ValidateRoot: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestProvider_ValidateRoot(t *testing.T) {
	RegisterEngine("root-test", &fakeEngine{})

	valid := &Provider{
		Kind:     "root-test",
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if err := valid.ValidateRoot(); err != nil {
		t.Fatalf("ValidateRoot: %v", err)
	}

	for _, tc := range []struct {
		name string
		p    *Provider
	}{
		{name: "missing kind", p: &Provider{Endpoint: "https://example.com", Config: map[string]any{}}},
		{name: "missing endpoint", p: &Provider{Kind: "root-test", Config: map[string]any{}}},
		{name: "unknown kind", p: &Provider{Kind: "unknown", Endpoint: "https://example.com", Config: map[string]any{}}},
	} {
		if err := tc.p.ValidateRoot(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	noConfig := &Provider{Kind: "root-test", Endpoint: "https://example.com"}
	if err := noConfig.ValidateRoot(); err != nil {
		t.Errorf("ValidateRoot without config: %v", err)
	}

	missingSecretKey := &Provider{
		Kind:     "root-test",
		Endpoint: "https://example.com",
		Secrets:  secrets.Secrets{{Name: "token"}},
	}
	if err := missingSecretKey.ValidateRoot(); !errors.Is(err, secrets.ErrMissingKey) {
		t.Fatalf("ValidateRoot missing secret key err = %v, want ErrMissingKey", err)
	}
}

func TestProvider_DeploymentFilesValidatesRoot(t *testing.T) {
	root := t.TempDir()
	mkFile(t, root, "index.html")

	provider := &Provider{
		Kind:     "unknown-deployment-files",
		Endpoint: "https://example.com",
	}
	err := func() error {
		_, err := provider.DeploymentFiles(root)
		return err
	}()
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("DeploymentFiles error = %v, want ErrUnknownKind", err)
	}
}

func TestProvider_DisplayName(t *testing.T) {
	p := &Provider{Kind: "test"}
	if p.DisplayName() != "test" {
		t.Errorf("DisplayName = %q, want test", p.DisplayName())
	}
}

func TestProvider_DecodeConfig(t *testing.T) {
	RegisterEngine("decode-test", &fakeEngine{})

	var p Provider
	raw := []byte(`{
		"kind": "decode-test",
		"endpoint": "https://example.com",
		"config": {
			"bucket": "my-bucket",
			"path_style": true
		}
	}`)
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	var cfg struct {
		Bucket       string `json:"bucket"`
		UsePathStyle bool   `json:"path_style"`
	}
	if err := p.DecodeConfig(&cfg); err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if cfg.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", cfg.Bucket)
	}
	if !cfg.UsePathStyle {
		t.Error("UsePathStyle = false, want true")
	}
}

func TestProvider_DestinationURL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		baseURL string
		rel     string
		want    string
	}{
		{
			name: "empty base url",
			rel:  "index.html",
			want: "/index.html",
		},
		{
			name: "empty base url with nested path",
			rel:  "blog/index.html",
			want: "/blog/index.html",
		},
		{
			name:    "base url",
			baseURL: "https://example.com",
			rel:     "blog/index.html",
			want:    "https://example.com/blog/index.html",
		},
		{
			name:    "base url with path",
			baseURL: "https://example.com/assets",
			rel:     "blog/index.html",
			want:    "https://example.com/assets/blog/index.html",
		},
		{
			name:    "path escaping",
			baseURL: "https://example.com",
			rel:     "blog/hello world.html",
			want:    "https://example.com/blog/hello%20world.html",
		},
		{
			name: "root relative escaping",
			rel:  "blog/hello world.html",
			want: "/blog/hello%20world.html",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &Provider{BaseURL: tc.baseURL}
			if got := provider.DestinationURL(tc.rel); got != tc.want {
				t.Fatalf("DestinationURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProvider_DeployLogsSecretsAndFileDecisions(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	RegisterEngine("log-test", &fakeEngine{})

	root := t.TempDir()
	mkFile(t, root, "index.html")
	mkFile(t, root, "style.css")

	provider := &Provider{
		Kind:     "log-test",
		Endpoint: "https://example.com",
		Include:  FilterRules{Paths: []string{`\.html$`}},
		Secrets: secrets.Secrets{
			{Name: "token", Key: "TOKEN"},
			{Name: "missing", Key: "MISSING"},
		},
	}
	runtime := &session.Session{
		Secrets: secrets.Values{
			"TOKEN": secrets.String("value"),
		},
	}

	if err := provider.DeployNamed(context.Background(), "assets", root, runtime); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		`[provider/log-test/assets] hosting "assets" init`,
		`[provider/log-test/assets] secret "token" found`,
		`[provider/log-test/assets] secret "missing" not_found`,
		`[provider/log-test/assets] file "index.html" included`,
		`[provider/log-test/assets] file "style.css" excluded`,
		`[provider/log-test/assets] hosting "assets" ready`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q in:\n%s", want, got)
		}
	}
}

func TestProvider_DeployLogsFailureFromEngine(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	RegisterEngine("fail-test", &failingEngine{})

	root := t.TempDir()
	mkFile(t, root, "index.html")

	provider := &Provider{
		Kind:     "fail-test",
		Endpoint: "https://example.com",
	}
	err := provider.DeployNamed(context.Background(), "assets", root, nil)
	if err == nil {
		t.Fatal("Deploy() error = nil, want error")
	}

	got := buf.String()
	for _, want := range []string{
		`[deployment] starting`,
		`[provider/fail-test/assets] hosting "assets" failed`,
		`[deployment] failed`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, `environment ""`) {
		t.Fatalf("log output contains empty environment name:\n%s", got)
	}
}

func TestRegisterEngine_PanicsOnInvalidRegistration(t *testing.T) {
	assertPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s: expected panic", name)
			}
		}()
		fn()
	}

	assertPanic("empty kind", func() {
		RegisterEngine("", &fakeEngine{})
	})
	assertPanic("nil engine", func() {
		RegisterEngine("nil-engine", nil)
	})
	RegisterEngine("duplicate-engine", &fakeEngine{})
	assertPanic("duplicate kind", func() {
		RegisterEngine("duplicate-engine", &fakeEngine{})
	})
}
