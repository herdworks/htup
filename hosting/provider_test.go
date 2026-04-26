package hosting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/herdworks/hyperproviders-go/session"
)

type fakeEngine struct{}

func (e *fakeEngine) Validate(provider *Provider) error {
	return nil
}

func (e *fakeEngine) Deploy(ctx context.Context, session *session.Session, provider *Provider, files []*File) error {
	return nil
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

func TestProvider_NameFallbackAndValidate(t *testing.T) {
	RegisterEngine("test", &fakeEngine{})

	p := &Provider{
		Name:     "myprov",
		Kind:     "test",
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if p.DisplayName() != "myprov" {
		t.Errorf("DisplayName = %q, want myprov", p.DisplayName())
	}

	fallback := &Provider{Kind: "test"}
	if fallback.DisplayName() != "test" {
		t.Errorf("DisplayName fallback = %q, want test", fallback.DisplayName())
	}

	empty := &Provider{}
	if err := empty.ValidateRoot(); err == nil {
		t.Error("ValidateRoot should error when provider name is empty")
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
		Name:     "example",
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
		{name: "missing name", p: &Provider{Kind: "root-test", Endpoint: "https://example.com", Config: map[string]any{}}},
		{name: "missing kind", p: &Provider{Name: "example", Endpoint: "https://example.com", Config: map[string]any{}}},
		{name: "missing endpoint", p: &Provider{Name: "example", Kind: "root-test", Config: map[string]any{}}},
		{name: "unknown kind", p: &Provider{Name: "example", Kind: "unknown", Endpoint: "https://example.com", Config: map[string]any{}}},
	} {
		if err := tc.p.ValidateRoot(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	noConfig := &Provider{Name: "example", Kind: "root-test", Endpoint: "https://example.com"}
	if err := noConfig.ValidateRoot(); err != nil {
		t.Errorf("ValidateRoot without config: %v", err)
	}
}

func TestProvider_NameFallback(t *testing.T) {
	p := &Provider{Kind: "test"}
	if p.DisplayName() != "test" {
		t.Errorf("DisplayName fallback = %q, want test", p.DisplayName())
	}
	p.Name = "myprov"
	if p.DisplayName() != "myprov" {
		t.Errorf("DisplayName = %q, want myprov", p.DisplayName())
	}
}

func TestProvider_DecodeConfig(t *testing.T) {
	RegisterEngine("decode-test", &fakeEngine{})

	var p Provider
	raw := []byte(`{
		"kind": "decode-test",
		"name": "decode",
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
