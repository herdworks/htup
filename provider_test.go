package hyperproviders

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

type fakeHostingProviderEngine struct {
	mu    sync.Mutex
	calls [][]*File
}

func (e *fakeHostingProviderEngine) Validate(provider *HostingProvider) error {
	return nil
}

func (e *fakeHostingProviderEngine) Ready(ctx context.Context, session *Session, provider *HostingProvider) error {
	return nil
}

func (e *fakeHostingProviderEngine) Deploy(ctx context.Context, session *Session, provider *HostingProvider, root string, files []*File) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, files)
	return nil
}

func TestEnvironment_Deploy_FansOutWithFilters(t *testing.T) {
	root := t.TempDir()
	html := filepath.Join(root, "page.html")
	png := filepath.Join(root, "image.png")
	for _, p := range []string{html, png} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	htmlEngine := &fakeHostingProviderEngine{}
	imgEngine := &fakeHostingProviderEngine{}
	RegisterHostingProviderEngine("html-only", htmlEngine)
	RegisterHostingProviderEngine("img-only", imgEngine)
	env := HostingEnvironment{"html", "img"}
	providers := HostingProviders{
		"html": {
			Kind:     "html-only",
			Endpoint: "https://example.com/html",
			Config:   map[string]any{},
			Include:  FilterRules{Types: []string{"^text/html"}},
		},
		"img": {
			Kind:     "img-only",
			Endpoint: "https://example.com/img",
			Config:   map[string]any{},
			Include:  FilterRules{Types: []string{"^image/"}},
		},
	}
	if err := env.Deploy(context.Background(), providers, root, nil); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(htmlEngine.calls) != 1 || len(htmlEngine.calls[0]) != 1 || htmlEngine.calls[0][0].RelPath() != "page.html" {
		t.Errorf("html provider got %+v, want one call with page.html", htmlEngine.calls)
	}
	if len(imgEngine.calls) != 1 || len(imgEngine.calls[0]) != 1 || imgEngine.calls[0][0].RelPath() != "image.png" {
		t.Errorf("img provider got %+v, want one call with image.png", imgEngine.calls)
	}
}

func TestEnvironment_PlanValidatesProviders(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := HostingEnvironment{"unknown"}
	providers := HostingProviders{
		"unknown": {
			Kind:     "unknown-plan",
			Endpoint: "https://example.com",
		},
	}
	_, err := env.Plan(providers, root)
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Plan error = %v, want ErrUnknownKind", err)
	}
}

func TestEnvironment_UnmarshalJSON_DecodesProviderNames(t *testing.T) {
	RegisterHostingProviderEngine("json-fake", &fakeHostingProviderEngine{})

	var env HostingEnvironment
	if err := json.Unmarshal([]byte(`["pages", "assets"]`), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(env) != 2 || env[0] != "pages" || env[1] != "assets" {
		t.Fatalf("environment = %#v, want pages/assets", env)
	}
}

func TestEnvironment_UnmarshalYAML_DecodesProviderNames(t *testing.T) {
	RegisterHostingProviderEngine("yaml-fake", &fakeHostingProviderEngine{})

	var env HostingEnvironment
	if err := yaml.Unmarshal([]byte(`
- pages
- assets
`), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(env) != 2 || env[0] != "pages" || env[1] != "assets" {
		t.Fatalf("environment = %#v, want pages/assets", env)
	}
}

func TestHostingProviders_PlanUsesSelectionNames(t *testing.T) {
	RegisterHostingProviderEngine("plan-name", &fakeHostingProviderEngine{})

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	providers := HostingProviders{
		"pages": {Kind: "plan-name", Endpoint: "https://example.com"},
	}
	selected, err := providers.Select([]string{"pages"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	plan, err := selected.Plan(root)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if _, ok := plan["pages"]; !ok {
		t.Fatalf("plan keys = %#v, want pages", plan)
	}
}

func TestHostingProviders_ResolveRejectsInvalidSelection(t *testing.T) {
	providers := HostingProviders{
		"nil": nil,
		"ok":  {Kind: "test", Endpoint: "https://example.com"},
	}
	for _, tc := range []struct {
		name  string
		names []string
	}{
		{name: "empty", names: nil},
		{name: "missing", names: []string{"missing"}},
		{name: "nil provider", names: []string{"nil"}},
		{name: "duplicate", names: []string{"ok", "ok"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := providers.Select(tc.names); err == nil {
				t.Fatal("Select error = nil")
			}
		})
	}
}

func TestHostingProvider_UnmarshalJSON_AllowsMissingKind(t *testing.T) {
	var provider HostingProvider
	if err := json.Unmarshal([]byte(`{"endpoint":"https://example.com","config":{}}`), &provider); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := provider.Validate(); !errors.Is(err, ErrMissingKind) {
		t.Fatalf("Validate error = %v, want ErrMissingKind", err)
	}
}

func TestHostingProvider_UnmarshalJSON_AllowsUnknownKind(t *testing.T) {
	var provider HostingProvider
	if err := json.Unmarshal([]byte(`{"kind":"unknown","endpoint":"https://example.com","config":{}}`), &provider); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := provider.Validate(); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Validate error = %v, want ErrUnknownKind", err)
	}
}

func TestHostingProvider_UnmarshalYAML_AllowsUnknownKind(t *testing.T) {
	var provider HostingProvider
	if err := yaml.Unmarshal([]byte(`
kind: unknown
endpoint: https://example.com
config: {}
`), &provider); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if err := provider.Validate(); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Validate error = %v, want ErrUnknownKind", err)
	}
}

func TestHostingProvider_UnmarshalJSON_ClearsStaleEngine(t *testing.T) {
	RegisterHostingProviderEngine("fresh", &fakeHostingProviderEngine{})

	provider := HostingProvider{Kind: "stale"}
	if err := json.Unmarshal([]byte(`{"kind":"unknown","endpoint":"https://example.com","config":{}}`), &provider); err != nil {
		t.Fatalf("Unmarshal unknown: %v", err)
	}
	if err := provider.Validate(); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Validate unknown error = %v, want ErrUnknownKind", err)
	}

	if err := json.Unmarshal([]byte(`{"kind":"fresh","endpoint":"https://example.com","config":{}}`), &provider); err != nil {
		t.Fatalf("Unmarshal fresh: %v", err)
	}
	if err := provider.Validate(); err != nil {
		t.Fatalf("Validate fresh: %v", err)
	}
}
