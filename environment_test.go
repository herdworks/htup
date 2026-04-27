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
	env := &Environment{
		Name: "test",
		Providers: []*HostingProvider{
			{
				Name:     "html",
				Kind:     "html-only",
				Endpoint: "https://example.com/html",
				Config:   map[string]any{},
				Filter:   ProviderFilter{Include: FilterRules{Types: []string{"^text/html"}}},
			},
			{
				Name:     "img",
				Kind:     "img-only",
				Endpoint: "https://example.com/img",
				Config:   map[string]any{},
				Filter:   ProviderFilter{Include: FilterRules{Types: []string{"^image/"}}},
			},
		},
	}
	if err := env.Deploy(context.Background(), root, nil); err != nil {
		t.Fatalf("Deploy: %v", err)
	}

	if len(htmlEngine.calls) != 1 || len(htmlEngine.calls[0]) != 1 || htmlEngine.calls[0][0].RelPath() != "page.html" {
		t.Errorf("html provider got %+v, want one call with page.html", htmlEngine.calls)
	}
	if len(imgEngine.calls) != 1 || len(imgEngine.calls[0]) != 1 || imgEngine.calls[0][0].RelPath() != "image.png" {
		t.Errorf("img provider got %+v, want one call with image.png", imgEngine.calls)
	}
}

func TestEnvironment_UnmarshalJSON_AttachesProviderEngine(t *testing.T) {
	RegisterHostingProviderEngine("json-fake", &fakeHostingProviderEngine{})

	var env Environment
	if err := json.Unmarshal([]byte(`{
		"name": "e",
		"providers": [
			{
				"kind": "json-fake",
				"name": "myprov",
				"endpoint": "https://example.com",
				"config": {}
			}
		]
	}`), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(env.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(env.Providers))
	}
	if env.Providers[0].DisplayName() != "myprov" {
		t.Errorf("name = %q, want myprov", env.Providers[0].DisplayName())
	}
	if err := env.Providers[0].Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestEnvironment_UnmarshalYAML_AttachesProviderEngine(t *testing.T) {
	RegisterHostingProviderEngine("yaml-fake", &fakeHostingProviderEngine{})

	var env Environment
	if err := yaml.Unmarshal([]byte(`
name: e
providers:
  - kind: yaml-fake
    name: myprov
    endpoint: https://example.com
    config: {}
`), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(env.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(env.Providers))
	}
	if env.Providers[0].Kind != "yaml-fake" {
		t.Errorf("kind = %q, want yaml-fake", env.Providers[0].Kind)
	}
	if err := env.Providers[0].Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestHostingProvider_UnmarshalJSON_ErrorsForMissingKind(t *testing.T) {
	var provider HostingProvider
	err := json.Unmarshal([]byte(`{"name":"missing","endpoint":"https://example.com","config":{}}`), &provider)
	if !errors.Is(err, ErrMissingKind) {
		t.Fatalf("error = %v, want ErrMissingKind", err)
	}
}

func TestHostingProvider_UnmarshalJSON_ErrorsForUnknownKind(t *testing.T) {
	var provider HostingProvider
	err := json.Unmarshal([]byte(`{"kind":"unknown","name":"p","endpoint":"https://example.com","config":{}}`), &provider)
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("error = %v, want ErrUnknownKind", err)
	}
}

func TestHostingProvider_UnmarshalJSON_ClearsStaleEngine(t *testing.T) {
	RegisterHostingProviderEngine("fresh", &fakeHostingProviderEngine{})

	provider := HostingProvider{Kind: "stale"}
	err := json.Unmarshal([]byte(`{"kind":"unknown","name":"p","endpoint":"https://example.com","config":{}}`), &provider)
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("error = %v, want ErrUnknownKind", err)
	}

	if err := json.Unmarshal([]byte(`{"kind":"fresh","name":"p","endpoint":"https://example.com","config":{}}`), &provider); err != nil {
		t.Fatalf("Unmarshal fresh: %v", err)
	}
	if err := provider.Validate(); err != nil {
		t.Fatalf("Validate fresh: %v", err)
	}
}
