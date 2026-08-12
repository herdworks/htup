package htup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fakeEngine struct{}

func deployProvider(ctx context.Context, provider *HostingProvider, root string, secretProviders SecretsProviders, kind DeploymentKind, w io.Writer) error {
	configPath := root + ".yaml"
	defer os.Remove(configPath)
	if err := os.WriteFile(configPath, []byte("title: test\n"), 0o644); err != nil {
		return err
	}
	return (&Deployment{ConfigPath: configPath, Providers: HostingProviders{provider.DisplayName(): provider}, PayloadPath: root, Kind: kind}).Execute(ctx, secretProviders, w)
}

func (e *fakeEngine) Validate(provider *HostingProvider) error {
	return nil
}

func (e *fakeEngine) Ready(ctx context.Context, secretProviders SecretsProviders, provider *HostingProvider) error {
	return nil
}

func (e *fakeEngine) ValidateOperation(provider *HostingProvider, operation DeploymentOperation) error {
	if len(operation.Preconditions) > 0 {
		return ErrDeploymentUnsupportedPrecondition
	}
	return nil
}
func (e *fakeEngine) PerformOperation(ctx context.Context, secretProviders SecretsProviders, operation DeploymentOperation, w io.Writer) error {
	return nil
}

type failingEngine struct{}

func (e *failingEngine) Validate(provider *HostingProvider) error {
	return nil
}

func (e *failingEngine) Ready(ctx context.Context, secretProviders SecretsProviders, provider *HostingProvider) error {
	return nil
}

func (e *failingEngine) ValidateOperation(provider *HostingProvider, operation DeploymentOperation) error {
	return nil
}
func (e *failingEngine) PerformOperation(ctx context.Context, secretProviders SecretsProviders, operation DeploymentOperation, w io.Writer) error {
	return errors.New("deploy failed")
}

type failingReadyEngine struct{}

func (e *failingReadyEngine) Validate(provider *HostingProvider) error {
	return nil
}

func (e *failingReadyEngine) Ready(ctx context.Context, secretProviders SecretsProviders, provider *HostingProvider) error {
	return errors.New("ready failed")
}

func (e *failingReadyEngine) ValidateOperation(provider *HostingProvider, operation DeploymentOperation) error {
	return nil
}
func (e *failingReadyEngine) PerformOperation(ctx context.Context, secretProviders SecretsProviders, operation DeploymentOperation, w io.Writer) error {
	return nil
}

type orderedEngine struct {
	preconditions []PreconditionFile
	phases        [][]string
}

func (e *orderedEngine) Validate(provider *HostingProvider) error { return nil }

func (e *orderedEngine) Ready(ctx context.Context, secretProviders SecretsProviders, provider *HostingProvider) error {
	return nil
}

func (e *orderedEngine) ValidateOperation(provider *HostingProvider, operation DeploymentOperation) error {
	return nil
}
func (e *orderedEngine) PerformOperation(ctx context.Context, secretProviders SecretsProviders, operation DeploymentOperation, w io.Writer) error {
	e.preconditions = nil
	for _, condition := range operation.Preconditions {
		e.preconditions = append(e.preconditions, condition)
	}
	e.phases = append(e.phases, paths(operation.Bulk), paths(operation.Enqueued))
	return nil
}
func paths(files []*File) []string {
	out := make([]string, len(files))
	for i, file := range files {
		out[i] = file.RelPath()
	}
	return out
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

func TestFilter_TypeRulesUseBaseMIMEType(t *testing.T) {
	root := t.TempDir()
	html := mkFile(t, root, "page.html")
	filter := &Filter{Include: FilterRules{Types: []string{`^text/html$`}}}
	ok, err := filter.Match(html)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("base MIME type rule should match text/html with charset parameter")
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

	p := &HostingProvider{
		Kind:     "test",
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if p.DisplayName() != "test" {
		t.Errorf("DisplayName = %q, want test", p.DisplayName())
	}

	empty := &HostingProvider{}
	if !errors.Is(empty.ValidateRoot(), ErrHostingProviderMissingKind) {
		t.Error("ValidateRoot should error when provider kind is empty")
	}
	if err := p.ValidateRoot(); err != nil {
		t.Errorf("ValidateRoot: %v", err)
	}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestProvider_DeploymentInstructionsPreconditionAndEnqueue(t *testing.T) {
	engine := &orderedEngine{}
	RegisterEngine("ordered-test", engine)
	root := t.TempDir()
	for _, rel := range []string{"ordinary.txt", "queued/first.json", "queued/final.json", ".deployment/pending.json"} {
		mkFile(t, root, rel)
	}
	instructions := []byte(`{
  "preconditions":[{"target":".hsync/revision.json","contents":".deployment/pending.json"}],
  "enqueue":["queued/*.json","queued/final.json"]
}`)
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), instructions, 0o644); err != nil {
		t.Fatal(err)
	}
	p := &HostingProvider{Kind: "ordered-test", Endpoint: "https://example.test"}
	if err := deployProvider(context.Background(), p, root, nil, IncrementalDeployment, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(engine.preconditions) != 1 || engine.preconditions[0].Target != ".hsync/revision.json" {
		t.Fatalf("preconditions = %#v", engine.preconditions)
	}
	want := [][]string{{"ordinary.txt"}, {"queued/final.json", "queued/first.json"}}
	if !reflect.DeepEqual(engine.phases, want) {
		t.Fatalf("phases = %#v, want %#v", engine.phases, want)
	}
}

func TestProvider_UnsupportedPreconditionStopsUploads(t *testing.T) {
	RegisterEngine("plain-test", &fakeEngine{})
	root := t.TempDir()
	mkFile(t, root, "ordinary.txt")
	mkFile(t, root, ".deployment/pending.json")
	if err := os.WriteFile(filepath.Join(root, DeploymentInstructionsFile), []byte(`{"preconditions":[{"target":"revision.json","contents":".deployment/pending.json"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &HostingProvider{Kind: "plain-test", Endpoint: "https://example.test"}
	err := deployProvider(context.Background(), p, root, nil, IncrementalDeployment, io.Discard)
	if !errors.Is(err, ErrDeploymentUnsupportedPrecondition) {
		t.Fatalf("Deploy error = %v, want unsupported precondition", err)
	}
}

func TestProvider_ValidateRoot(t *testing.T) {
	RegisterEngine("root-test", &fakeEngine{})

	valid := &HostingProvider{
		Kind:     "root-test",
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if err := valid.ValidateRoot(); err != nil {
		t.Fatalf("ValidateRoot: %v", err)
	}

	for _, tc := range []struct {
		name string
		p    *HostingProvider
	}{
		{name: "missing kind", p: &HostingProvider{Endpoint: "https://example.com", Config: map[string]any{}}},
		{name: "missing endpoint", p: &HostingProvider{Kind: "root-test", Config: map[string]any{}}},
		{name: "unknown kind", p: &HostingProvider{Kind: "unknown", Endpoint: "https://example.com", Config: map[string]any{}}},
	} {
		if err := tc.p.ValidateRoot(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	noConfig := &HostingProvider{Kind: "root-test", Endpoint: "https://example.com"}
	if err := noConfig.ValidateRoot(); err != nil {
		t.Errorf("ValidateRoot without config: %v", err)
	}

	missingSecretKey := &HostingProvider{
		Kind:     "root-test",
		Endpoint: "https://example.com",
		Secrets:  Secrets{{Name: "token"}},
	}
	if err := missingSecretKey.ValidateRoot(); !errors.Is(err, ErrSecretMissingKey) {
		t.Fatalf("ValidateRoot missing secret key err = %v, want ErrSecretMissingKey", err)
	}
}

func TestProvider_ReadyDelegatesEngine(t *testing.T) {
	RegisterEngine("ready-fails", &failingReadyEngine{})

	provider := &HostingProvider{
		Kind:     "ready-fails",
		Endpoint: "https://example.com",
	}

	if err := provider.Ready(context.Background(), nil); err == nil || err.Error() != "ready failed" {
		t.Fatalf("Ready error = %v, want ready failed", err)
	}
}

func TestProvider_DisplayName(t *testing.T) {
	p := &HostingProvider{Kind: "test"}
	if p.DisplayName() != "test" {
		t.Errorf("DisplayName = %q, want test", p.DisplayName())
	}
}

func TestProvider_DecodeConfig(t *testing.T) {
	RegisterEngine("decode-test", &fakeEngine{})

	var p HostingProvider
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
			provider := &HostingProvider{BaseURL: tc.baseURL}
			if got := provider.DestinationURL(tc.rel); got != tc.want {
				t.Fatalf("DestinationURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProvider_DeployLogsSecretsAndFileDecisions(t *testing.T) {
	var buf bytes.Buffer

	RegisterEngine("log-test", &fakeEngine{})

	root := t.TempDir()
	mkFile(t, root, "index.html")
	mkFile(t, root, "style.css")

	provider := &HostingProvider{
		Kind:     "log-test",
		Endpoint: "https://example.com",
		Include:  FilterRules{Paths: []string{`\.html$`}},
		Secrets: Secrets{
			{Name: "token", Key: "TOKEN"},
			{Name: "missing", Key: "MISSING"},
		},
	}
	secretProviders := SecretsProviders{&fakeSecretProvider{
		name:   "runtime",
		values: map[string]string{"TOKEN": "value"},
	}}

	if err := deployProvider(context.Background(), provider, root, secretProviders, CompleteDeployment, &buf); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		`[provider/log-test/log-test] secret "token" found`,
		`[provider/log-test/log-test] secret "missing" not_found`,
		`[provider/log-test/log-test] file "index.html" included`,
		`[provider/log-test/log-test] file "style.css" excluded`,
		`[provider/log-test/log-test] hosting "log-test" ready`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q in:\n%s", want, got)
		}
	}
}

func TestProvider_DeployLogsFailureFromEngine(t *testing.T) {
	var buf bytes.Buffer

	RegisterEngine("fail-test", &failingEngine{})

	root := t.TempDir()
	mkFile(t, root, "index.html")

	provider := &HostingProvider{
		Kind:     "fail-test",
		Endpoint: "https://example.com",
	}
	err := deployProvider(context.Background(), provider, root, nil, CompleteDeployment, &buf)
	if err == nil {
		t.Fatal("Deploy() error = nil, want error")
	}

	got := buf.String()
	for _, want := range []string{
		`[deployment] starting`,
		`[provider/fail-test/fail-test] hosting "fail-test" ready`,
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
