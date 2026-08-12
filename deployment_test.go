package htup

import (
	"context"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeProviderEngine struct {
	mu    sync.Mutex
	calls [][]*File
	names []string
}

func deploymentTestConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(path, []byte("title: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (*fakeProviderEngine) Validate(*HostingProvider) error { return nil }
func (*fakeProviderEngine) Ready(context.Context, SecretsProviders, *HostingProvider) error {
	return nil
}
func (*fakeProviderEngine) ValidateOperation(*HostingProvider, DeploymentOperation) error {
	return nil
}
func (e *fakeProviderEngine) PerformOperation(ctx context.Context, _ SecretsProviders, operation DeploymentOperation, _ io.Writer) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, append(append([]*File{}, operation.Bulk...), operation.Enqueued...))
	e.names = append(e.names, ProviderName(ctx, operation.HostingProvider))
	return nil
}

func TestDeploymentPrepareMaterializesOneOperationPerProvider(t *testing.T) {
	root := t.TempDir()
	for rel, contents := range map[string]string{
		"index.html":           "page",
		"queued.json":          "queued",
		".deployment/expected": "old",
		".deployment/pending":  "new",
		".deployment.json":     `{"preconditions":[{"target":"state.json","expected":".deployment/expected","contents":".deployment/pending"}],"enqueue":["queued.json"]}`,
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, b := &fakeProviderEngine{}, &fakeProviderEngine{}
	RegisterEngine("operation-a", a)
	RegisterEngine("operation-b", b)
	d := &Deployment{ConfigPath: deploymentTestConfig(t, root), Providers: HostingProviders{"z": {Kind: "operation-b", Endpoint: "https://z", Include: FilterRules{Types: []string{"^application/json$"}}}, "a": {Kind: "operation-a", Endpoint: "https://a", Include: FilterRules{Types: []string{"^application/json$"}}}}, PayloadPath: root, Kind: CompleteDeployment}
	if err := d.Prepare(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if instructions := d.Instructions(); len(instructions.Preconditions) != 1 || instructions.Preconditions[0].Contents != ".deployment/pending" {
		t.Fatalf("Instructions() = %#v", instructions)
	}
	plan := d.Plan()
	if len(plan.Sequential) != 2 || len(plan.Concurrent) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Sequential[0].HostingProvider.Kind != "operation-a" || plan.Sequential[1].HostingProvider.Kind != "operation-b" {
		t.Fatalf("sequential provider order = %q, %q", plan.Sequential[0].HostingProvider.Kind, plan.Sequential[1].HostingProvider.Kind)
	}
	for _, op := range plan.Sequential {
		if len(op.Preconditions) != 1 || len(op.Bulk) != 0 || len(op.Enqueued) != 1 {
			t.Fatalf("operation = %#v", op)
		}
		op.Preconditions[0].Content[0] = 'x'
	}
	if got := d.Plan().Sequential[0].Preconditions[0].Content[0]; got != 'n' {
		t.Fatalf("plan snapshot mutated: %q", got)
	}
	instructions := d.Instructions()
	instructions.Preconditions[0].Contents = "changed"
	if got := d.Instructions().Preconditions[0].Contents; got != ".deployment/pending" {
		t.Fatalf("instructions snapshot mutated: %q", got)
	}
}

func TestDeploymentKind_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    DeploymentKind
		wantErr bool
	}{
		{name: "complete", kind: CompleteDeployment},
		{name: "incremental", kind: IncrementalDeployment},
		{name: "empty", wantErr: true},
		{name: "unknown", kind: DeploymentKind("unknown"), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.kind.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

func TestDeploymentPrepareSelectsEnvironmentFromConfigSnapshot(t *testing.T) {
	engine := &fakeProviderEngine{}
	RegisterEngine("config-snapshot", engine)
	root := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(configPath, []byte("providers:\n  selected:\n    kind: config-snapshot\n    endpoint: https://example.com\nenvironments:\n  production:\n    - selected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("page"), 0o644); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("providers: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deployment := &Deployment{ConfigPath: configPath, ConfigBytes: bytes, PayloadPath: root, Environment: "production", Kind: CompleteDeployment}
	if err := deployment.Prepare(context.Background(), io.Discard); err != nil {
		t.Fatal(err)
	}
	if plan := deployment.Plan(); len(plan.Concurrent) != 1 || plan.Concurrent[0].HostingProvider.Kind != "config-snapshot" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestDeploymentPrepareRejectsConfigDirectoryPayload(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "site.yaml")
	if err := os.WriteFile(configPath, []byte("title: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deployment := &Deployment{
		ConfigPath:  configPath,
		PayloadPath: filepath.Join(configDir, "payload", ".."),
		Providers:   HostingProviders{"explicit": {Kind: "operation-a", Endpoint: "https://example.com"}},
		Kind:        CompleteDeployment,
	}
	if err := deployment.Prepare(context.Background(), io.Discard); err == nil {
		t.Fatal("Prepare() error = nil")
	}
	if deployment.Status() != DeploymentFailed {
		t.Fatalf("status = %q, want %q", deployment.Status(), DeploymentFailed)
	}
}

func TestValidateDeploymentPayloadPath(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "site.yaml")
	if err := ValidateDeploymentPayloadPath(configPath, filepath.Join(configDir, "payload", "..")); err == nil {
		t.Fatal("ValidateDeploymentPayloadPath() error = nil")
	}
	if err := ValidateDeploymentPayloadPath(configPath, filepath.Join(configDir, "build")); err != nil {
		t.Fatalf("ValidateDeploymentPayloadPath() error = %v", err)
	}
}

func TestDeploymentPrepareRejectsAmbiguousProviderSelection(t *testing.T) {
	root := t.TempDir()
	configPath := deploymentTestConfig(t, root)
	deployment := &Deployment{ConfigPath: configPath, PayloadPath: root, Providers: HostingProviders{"explicit": {Kind: "operation-a", Endpoint: "https://example.com"}}, Environment: "production", Kind: CompleteDeployment}
	if err := deployment.Prepare(context.Background(), io.Discard); err == nil {
		t.Fatal("Prepare() error = nil")
	}
}

func TestDeploymentPrepareRejectsUnownedPrecondition(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pending"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".deployment.json"), []byte(`{"preconditions":[{"target":"state.json","contents":"pending"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &fakeProviderEngine{}
	RegisterEngine("unowned", engine)
	d := &Deployment{ConfigPath: deploymentTestConfig(t, root), Providers: HostingProviders{"only": {Kind: "unowned", Endpoint: "https://only", Include: FilterRules{Paths: []string{"^other/"}}}}, PayloadPath: root, Kind: IncrementalDeployment}
	if err := d.Prepare(context.Background(), nil); err == nil {
		t.Fatal("Prepare error = nil")
	}
	if d.Status() != DeploymentFailed {
		t.Fatalf("status = %q", d.Status())
	}
}

func TestDeploymentExecuteReadinessFailureDoesNotPerform(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("page"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine := &notReadyOperationEngine{}
	RegisterEngine("not-ready-operation", engine)
	d := &Deployment{ConfigPath: deploymentTestConfig(t, root), Providers: HostingProviders{"only": {Kind: "not-ready-operation", Endpoint: "https://only"}}, PayloadPath: root, Kind: CompleteDeployment}
	if err := d.Execute(context.Background(), nil, nil); !errors.Is(err, errNotReadyOperation) {
		t.Fatalf("Execute error = %v", err)
	}
	if engine.performed {
		t.Fatal("operation performed after readiness failure")
	}
}

var errNotReadyOperation = errors.New("not ready")

type notReadyOperationEngine struct{ performed bool }

func (*notReadyOperationEngine) Validate(*HostingProvider) error { return nil }
func (*notReadyOperationEngine) Ready(context.Context, SecretsProviders, *HostingProvider) error {
	return errNotReadyOperation
}
func (*notReadyOperationEngine) ValidateOperation(*HostingProvider, DeploymentOperation) error {
	return nil
}
func (e *notReadyOperationEngine) PerformOperation(context.Context, SecretsProviders, DeploymentOperation, io.Writer) error {
	e.performed = true
	return nil
}

type operationOrderEngine struct {
	mu      sync.Mutex
	calls   []string
	started chan string
	release <-chan struct{}
	fail    string
}

func (*operationOrderEngine) Validate(*HostingProvider) error { return nil }
func (*operationOrderEngine) Ready(context.Context, SecretsProviders, *HostingProvider) error {
	return nil
}
func (*operationOrderEngine) ValidateOperation(*HostingProvider, DeploymentOperation) error {
	return nil
}
func (e *operationOrderEngine) PerformOperation(ctx context.Context, _ SecretsProviders, operation DeploymentOperation, _ io.Writer) error {
	name := ProviderName(ctx, operation.HostingProvider)
	e.mu.Lock()
	e.calls = append(e.calls, name)
	e.mu.Unlock()
	if name == e.fail {
		return errors.New("operation failed")
	}
	if e.started != nil && (name == "third" || name == "fourth") {
		e.started <- name
		<-e.release
	}
	return nil
}

func (e *operationOrderEngine) Calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

func TestDeploymentExecuteRunsConditionalOperationsBeforeConcurrentOperations(t *testing.T) {
	root := deploymentOrderRoot(t)
	release := make(chan struct{})
	engine := &operationOrderEngine{started: make(chan string, 2), release: release}
	RegisterEngine("operation-order", engine)
	deployment := deploymentOrderDeployment(t, root, "operation-order")
	errs := make(chan error, 1)
	go func() { errs <- deployment.Execute(context.Background(), nil, io.Discard) }()
	for i := 0; i < 2; i++ {
		select {
		case <-engine.started:
		case <-time.After(time.Second):
			t.Fatal("concurrent operations did not both start")
		}
	}
	close(release)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	calls := engine.Calls()
	if len(calls) != 4 || calls[0] != "first" || calls[1] != "second" {
		t.Fatalf("operation calls = %#v, want first then second before concurrent operations", calls)
	}
}

func TestDeploymentExecuteStopsAfterSequentialOperationFailure(t *testing.T) {
	root := deploymentOrderRoot(t)
	engine := &operationOrderEngine{fail: "first"}
	RegisterEngine("operation-order-fail", engine)
	err := deploymentOrderDeployment(t, root, "operation-order-fail").Execute(context.Background(), nil, io.Discard)
	if err == nil {
		t.Fatal("Execute error = nil, want sequential failure")
	}
	if got, want := engine.Calls(), []string{"first"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("operation calls = %#v, want %#v", got, want)
	}
}

func deploymentOrderRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, contents := range map[string]string{
		".deployment/first":  "first",
		".deployment/second": "second",
		".deployment.json":   `{"preconditions":[{"target":"first.json","contents":".deployment/first"},{"target":"second.json","contents":".deployment/second"}]}`,
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func deploymentOrderDeployment(t *testing.T, root, kind string) *Deployment {
	return &Deployment{Providers: HostingProviders{
		"first":  {Kind: kind, Endpoint: "https://first", Include: FilterRules{Paths: []string{"^first\\.json$"}}},
		"second": {Kind: kind, Endpoint: "https://second", Include: FilterRules{Paths: []string{"^second\\.json$"}}},
		"third":  {Kind: kind, Endpoint: "https://third", Include: FilterRules{Paths: []string{"^none/"}}},
		"fourth": {Kind: kind, Endpoint: "https://fourth", Include: FilterRules{Paths: []string{"^none/"}}},
	}, ConfigPath: deploymentTestConfig(t, root), PayloadPath: root, Kind: CompleteDeployment}
}

func TestDeploymentExecuteFansOutWithFilters(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"page.html", "image.png"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	html, image := &fakeProviderEngine{}, &fakeProviderEngine{}
	RegisterEngine("deployment-html", html)
	RegisterEngine("deployment-image", image)
	d := &Deployment{ConfigPath: deploymentTestConfig(t, root), Providers: HostingProviders{"html": {Kind: "deployment-html", Endpoint: "https://html", Include: FilterRules{Types: []string{"^text/html"}}}, "image": {Kind: "deployment-image", Endpoint: "https://image", Include: FilterRules{Types: []string{"^image/"}}}}, PayloadPath: root, Kind: CompleteDeployment}
	if err := d.Execute(context.Background(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(html.calls) != 1 || len(html.calls[0]) != 1 || html.calls[0][0].RelPath() != "page.html" {
		t.Fatalf("HTML calls = %#v", html.calls)
	}
	if len(image.calls) != 1 || len(image.calls[0]) != 1 || image.calls[0][0].RelPath() != "image.png" {
		t.Fatalf("image calls = %#v", image.calls)
	}
	if len(html.names) != 1 || html.names[0] != "html" || len(image.names) != 1 || image.names[0] != "image" {
		t.Fatalf("provider names = html:%v image:%v", html.names, image.names)
	}
}

func TestProvidersSelectRejectsInvalidSelection(t *testing.T) {
	providers := HostingProviders{"nil": nil, "ok": {Kind: "test", Endpoint: "https://example.com"}}
	for _, names := range [][]string{nil, {"missing"}, {"nil"}, {"ok", "ok"}} {
		if _, err := providers.Select(names); err == nil {
			t.Fatalf("Select(%#v) error = nil", names)
		}
	}
}

func TestEnvironmentDecodes(t *testing.T) {
	for _, decode := range []func(any) error{func(value any) error { return json.Unmarshal([]byte(`["pages","assets"]`), value) }, func(value any) error { return yaml.Unmarshal([]byte("- pages\n- assets\n"), value) }} {
		var environment Environment
		if err := decode(&environment); err != nil {
			t.Fatal(err)
		}
		if len(environment) != 2 || environment[0] != "pages" || environment[1] != "assets" {
			t.Fatalf("environment = %#v", environment)
		}
	}
}

func TestProviderDecodingValidation(t *testing.T) {
	for _, source := range [][]byte{[]byte(`{"endpoint":"https://example.com"}`), []byte(`{"kind":"unknown","endpoint":"https://example.com"}`)} {
		var provider HostingProvider
		if err := json.Unmarshal(source, &provider); err != nil {
			t.Fatal(err)
		}
		if err := provider.Validate(); err == nil {
			t.Fatal("Validate error = nil")
		}
	}
	var provider HostingProvider
	if err := yaml.Unmarshal([]byte("kind: unknown\nendpoint: https://example.com\n"), &provider); err != nil {
		t.Fatal(err)
	}
	if err := provider.Validate(); err == nil {
		t.Fatal("Validate error = nil")
	}
}
