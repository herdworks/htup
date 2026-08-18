package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/herdworks/htup"
	"github.com/herdworks/htup/providers/runtime"
	"golang.org/x/sync/errgroup"
)

func TestUnmarshalHostingProvider(t *testing.T) {
	raw := []byte(`{
		"kind": "s3",
		"endpoint": "https://example.r2.cloudflarestorage.com",
		"config": {
			"bucket": "my-bucket",
			"region": "auto",
			"addressing_style": "path",
			"cache_control": "public, max-age=60",
			"conditional_mode": "etag",
			"workers": 4
		},
		"secrets": [
			{ "name": "access_key_id", "key": "AWS_ACCESS_KEY_ID" },
			{ "name": "secret_access_key", "key": "AWS_SECRET_ACCESS_KEY" }
		]
	}`)
	var provider htup.HostingProvider
	if err := json.Unmarshal(raw, &provider); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if provider.Kind != Kind {
		t.Errorf("Kind = %q, want %q", provider.Kind, Kind)
	}
	cfg, err := newSettings(&provider)
	if err != nil {
		t.Fatalf("newSettings: %v", err)
	}
	if cfg.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q, want my-bucket", cfg.Bucket)
	}
	if cfg.ConditionalMode != ConditionalModeETag {
		t.Errorf("ConditionalMode = %q, want %q", cfg.ConditionalMode, ConditionalModeETag)
	}
	if cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", cfg.Workers)
	}
	if err := provider.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestValidate_RequiresBucket(t *testing.T) {
	provider := &htup.HostingProvider{
		Kind:     Kind,
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if err := (Engine{}).Validate(provider); err == nil {
		t.Error("expected error for missing bucket")
	}
}

func TestValidate_UsesBucketFromEndpointPath(t *testing.T) {
	provider := &htup.HostingProvider{
		Kind:     Kind,
		Endpoint: "https://example.com/path-bucket",
		Config:   map[string]any{},
		Secrets: htup.Secrets{
			{Name: "access_key_id", Key: "AWS_ACCESS_KEY_ID"},
			{Name: "secret_access_key", Key: "AWS_SECRET_ACCESS_KEY"},
		},
	}

	if err := (Engine{}).Validate(provider); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	cfg, err := newSettings(provider)
	if err != nil {
		t.Fatalf("newSettings: %v", err)
	}
	if cfg.Bucket != "path-bucket" {
		t.Errorf("Bucket = %q, want path-bucket", cfg.Bucket)
	}
}

func TestValidate_RejectsUnknownConditionalMode(t *testing.T) {
	provider := &htup.HostingProvider{
		Kind:     Kind,
		Endpoint: "https://example.com",
		Config: map[string]any{
			"bucket":           "b",
			"conditional_mode": "bogus",
		},
	}
	if err := (Engine{}).Validate(provider); err == nil {
		t.Error("expected error for unknown conditional_mode")
	}
}

func TestValidate_UsesBucketFromConfig(t *testing.T) {
	provider := &htup.HostingProvider{
		Config: map[string]any{
			"bucket": "configured-bucket",
			"region": "us-east-1",
		},
		Secrets: htup.Secrets{
			{Name: "access_key_id", Key: "AWS_ACCESS_KEY_ID"},
			{Name: "secret_access_key", Key: "AWS_SECRET_ACCESS_KEY"},
		},
	}

	if err := (Engine{}).Validate(provider); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	cfg, err := newSettings(provider)
	if err != nil {
		t.Fatalf("newSettings: %v", err)
	}
	if cfg.Bucket != "configured-bucket" {
		t.Errorf("Bucket = %q, want configured-bucket", cfg.Bucket)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("Region = %q, want us-east-1", cfg.Region)
	}
}

func TestValidate_AllowsVirtualAddressingWithoutBucket(t *testing.T) {
	provider := &htup.HostingProvider{
		Config: map[string]any{
			"addressing_style": "virtual",
			"region":           "us-east-1",
		},
		Secrets: htup.Secrets{
			{Name: "access_key_id", Key: "AWS_ACCESS_KEY_ID"},
			{Name: "secret_access_key", Key: "AWS_SECRET_ACCESS_KEY"},
		},
	}

	if err := (Engine{}).Validate(provider); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	cfg, err := newSettings(provider)
	if err != nil {
		t.Fatalf("newSettings: %v", err)
	}
	if cfg.UsePathStyle {
		t.Error("UsePathStyle = true, want false")
	}
}

func TestValidate_RejectsUnknownAddressingStyle(t *testing.T) {
	provider := &htup.HostingProvider{Config: map[string]any{"addressing_style": "sideways"}}

	if err := (Engine{}).Validate(provider); err == nil {
		t.Fatal("expected error for unknown addressing_style")
	}
}

func TestValidate_RejectsInvalidWorkers(t *testing.T) {
	provider := &htup.HostingProvider{Config: map[string]any{"workers": 0}}

	err := (Engine{}).Validate(provider)
	if err == nil {
		t.Fatal("expected error for invalid workers")
	}
	if !strings.Contains(err.Error(), "workers must be at least 1") {
		t.Fatalf("error = %v, want workers minimum", err)
	}
}

func TestReady_RequiresRuntimeSecrets(t *testing.T) {
	provider := readyProvider()

	err := provider.Ready(context.Background(), nil)
	if err == nil {
		t.Fatal("Ready error = nil, want missing secret")
	}
	if !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("Ready error = %v, want access key", err)
	}
}

func TestReady_WithRuntimeSecrets(t *testing.T) {
	provider := readyProvider()
	secretProviders := runtimeSecretProviders(runtime.Secrets{
		"AWS_ACCESS_KEY_ID":     htup.StringSecret("access"),
		"AWS_SECRET_ACCESS_KEY": htup.StringSecret("secret"),
	})

	if err := provider.Ready(context.Background(), secretProviders); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func readyProvider() *htup.HostingProvider {
	return &htup.HostingProvider{
		Kind:     Kind,
		Endpoint: "https://example.com",
		Config:   map[string]any{"bucket": "assets"},
		Secrets: htup.Secrets{
			{Name: "access_key_id", Key: "AWS_ACCESS_KEY_ID"},
			{Name: "secret_access_key", Key: "AWS_SECRET_ACCESS_KEY"},
		},
	}
}

func TestWorkersOrDefault(t *testing.T) {
	if got := workersOrDefault(0); got != DefaultWorkers {
		t.Errorf("workersOrDefault() = %d, want %d", got, DefaultWorkers)
	}
	if got := workersOrDefault(3); got != 3 {
		t.Errorf("workersOrDefault() = %d, want 3", got)
	}
}

func TestApplyClientOptions_UsesProviderEndpoint(t *testing.T) {
	var options awss3.Options

	applyClientOptions(&options, "https://example.r2.cloudflarestorage.com", true)

	if options.BaseEndpoint == nil {
		t.Fatal("BaseEndpoint = nil, want provider endpoint")
	}
	if *options.BaseEndpoint != "https://example.r2.cloudflarestorage.com" {
		t.Errorf("BaseEndpoint = %q, want provider endpoint", *options.BaseEndpoint)
	}
	if !options.UsePathStyle {
		t.Error("UsePathStyle = false, want true when endpoint is configured")
	}
}

func TestApplyClientOptions_StripsBucketPathFromEndpoint(t *testing.T) {
	var options awss3.Options

	applyClientOptions(&options, "https://example.r2.cloudflarestorage.com/my-bucket", true)

	if options.BaseEndpoint == nil {
		t.Fatal("BaseEndpoint = nil, want provider endpoint")
	}
	if *options.BaseEndpoint != "https://example.r2.cloudflarestorage.com" {
		t.Errorf("BaseEndpoint = %q, want endpoint without bucket path", *options.BaseEndpoint)
	}
}

func TestNormalizeETag(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"abc"`, "abc"},
		{"abc", "abc"},
		{`""`, ""},
	} {
		if got := normalizeETag(tc.in); got != tc.want {
			t.Errorf("normalizeETag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCompareAndSwapFileReplacesExactExpectedBytes(t *testing.T) {
	root := t.TempDir()
	condition := preconditionForTest(t, root, true)
	var putBody []byte
	var ifMatch string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/assets/revision.json" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		switch request.Method {
		case http.MethodGet:
			writer.Header().Set("ETag", `"old-etag"`)
			_, _ = writer.Write([]byte("expected"))
		case http.MethodPut:
			ifMatch = request.Header.Get("If-Match")
			putBody, _ = io.ReadAll(request.Body)
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	provider := readyProvider()
	provider.Endpoint = server.URL
	if err := (Engine{}).compareAndSwap(context.Background(), readySecretProviders(), provider, condition, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ifMatch != `"old-etag"` {
		t.Fatalf("If-Match = %q", ifMatch)
	}
	if string(putBody) != "replacement" {
		t.Fatalf("PUT body = %q", putBody)
	}
}

func TestCompareAndSwapFileRejectsDifferentRemoteBytes(t *testing.T) {
	root := t.TempDir()
	condition := preconditionForTest(t, root, true)
	put := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			writer.Header().Set("ETag", `"old-etag"`)
			_, _ = writer.Write([]byte("different"))
		case http.MethodPut:
			put = true
			writer.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()
	provider := readyProvider()
	provider.Endpoint = server.URL
	err := (Engine{}).compareAndSwap(context.Background(), readySecretProviders(), provider, condition, io.Discard)
	if !errors.Is(err, htup.ErrDeploymentPreconditionFailed) {
		t.Fatalf("CompareAndSwapFile error = %v", err)
	}
	if put {
		t.Fatal("conditional PUT ran after an exact-byte mismatch")
	}
}

func TestCompareAndSwapFileCreatesOnlyWhenAbsent(t *testing.T) {
	root := t.TempDir()
	condition := preconditionForTest(t, root, false)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPut {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if got := request.Header.Get("If-None-Match"); got != "*" {
			t.Fatalf("If-None-Match = %q", got)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	provider := readyProvider()
	provider.Endpoint = server.URL
	if err := (Engine{}).compareAndSwap(context.Background(), readySecretProviders(), provider, condition, io.Discard); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one conditional PUT", requests)
	}
}

func TestDeployment_S3OperationOrdersPreconditionBulkAndEnqueue(t *testing.T) {
	root := t.TempDir()
	for rel, contents := range map[string]string{
		"bulk/first.txt":       "first",
		"bulk/second.txt":      "second",
		"queued/first.txt":     "queued first",
		"queued/second.txt":    "queued second",
		".deployment/expected": "old",
		".deployment/pending":  "new",
		".deployment.json":     `{"preconditions":[{"target":"revision.json","expected":".deployment/expected","contents":".deployment/pending"}],"enqueue":["queued/first.txt","queued/second.txt"]}`,
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	var writes []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			if request.URL.Path != "/assets/revision.json" {
				t.Fatalf("GET path = %q", request.URL.Path)
			}
			writer.Header().Set("ETag", `"old"`)
			_, _ = writer.Write([]byte("old"))
		case http.MethodPut:
			mu.Lock()
			writes = append(writes, strings.TrimPrefix(request.URL.Path, "/assets/"))
			mu.Unlock()
			writer.WriteHeader(http.StatusOK)
		default:
			writer.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	provider := readyProvider()
	provider.Endpoint = server.URL
	configPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(configPath, []byte("title: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deployment := &htup.Deployment{
		ConfigPath:  configPath,
		Providers:   htup.HostingProviders{"assets": provider},
		PayloadPath: root,
		Kind:        htup.CompleteDeployment,
	}
	if err := deployment.Execute(context.Background(), readySecretProviders(), io.Discard); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(writes) != 5 {
		t.Fatalf("writes = %#v", writes)
	}
	if writes[0] != "revision.json" {
		t.Fatalf("first write = %q, want precondition target", writes[0])
	}
	if got, want := writes[3:], []string{"queued/first.txt", "queued/second.txt"}; !slices.Equal(got, want) {
		t.Fatalf("enqueued writes = %#v, want %#v", got, want)
	}
	bulk := map[string]bool{writes[1]: true, writes[2]: true}
	if !bulk["bulk/first.txt"] || !bulk["bulk/second.txt"] {
		t.Fatalf("bulk writes = %#v", writes[1:3])
	}
}

func TestGroupErrorOrContextError(t *testing.T) {
	t.Run("worker failure", func(t *testing.T) {
		wantErr := errors.New("s3: put first.txt: request failed")
		g, ctx := errgroup.WithContext(context.Background())
		g.Go(func() error { return wantErr })
		<-ctx.Done()

		if err := groupErrorOrContextError(ctx, g); !errors.Is(err, wantErr) {
			t.Fatalf("groupErrorOrContextError() error = %v, want %v", err, wantErr)
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		g, ctx := errgroup.WithContext(parent)
		cancel()
		<-ctx.Done()

		if err := groupErrorOrContextError(ctx, g); !errors.Is(err, context.Canceled) {
			t.Fatalf("groupErrorOrContextError() error = %v, want context canceled", err)
		}
	})
}

func TestDeployment_S3UploadFailureLogsOriginalReason(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>test</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/assets/index.html" {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	provider := readyProvider()
	provider.Endpoint = server.URL
	configPath := filepath.Join(t.TempDir(), "site.yaml")
	if err := os.WriteFile(configPath, []byte("title: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deployment := &htup.Deployment{
		ConfigPath:  configPath,
		Providers:   htup.HostingProviders{"assets": provider},
		PayloadPath: root,
		Kind:        htup.CompleteDeployment,
	}
	var log bytes.Buffer
	err := deployment.Execute(context.Background(), readySecretProviders(), &log)
	if err == nil {
		t.Fatal("Execute() error = nil, want S3 upload error")
	}
	if !strings.Contains(err.Error(), "s3: put index.html:") {
		t.Fatalf("Execute() error = %v, want contextual S3 upload error", err)
	}
	for _, want := range []string{
		`[provider/s3/assets] hosting "assets" failed: s3: put index.html:`,
		`[deployment] failed: provider "assets": s3: put index.html:`,
	} {
		if !strings.Contains(log.String(), want) {
			t.Fatalf("deployment log missing %q in:\n%s", want, log.String())
		}
	}
}

func preconditionForTest(t *testing.T, root string, expected bool) htup.PreconditionFile {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".deployment"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".deployment", "pending.json"), []byte("replacement"), 0o644); err != nil {
		t.Fatal(err)
	}
	condition := `{"target":"revision.json","contents":".deployment/pending.json"}`
	if expected {
		if err := os.WriteFile(filepath.Join(root, ".deployment", "expected.json"), []byte("expected"), 0o644); err != nil {
			t.Fatal(err)
		}
		condition = `{"target":"revision.json","expected":".deployment/expected.json","contents":".deployment/pending.json"}`
	}
	if err := os.WriteFile(filepath.Join(root, htup.DeploymentInstructionsFile), []byte(`{"preconditions":[`+condition+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pending, err := os.ReadFile(filepath.Join(root, ".deployment", "pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	prepared := htup.PreconditionFile{Target: "revision.json", Content: pending}
	if expected {
		prepared.Expected, err = os.ReadFile(filepath.Join(root, ".deployment", "expected.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = pending
	return prepared
}

func readySecretProviders() htup.SecretsProviders {
	return runtimeSecretProviders(runtime.Secrets{
		"AWS_ACCESS_KEY_ID":     htup.StringSecret("access"),
		"AWS_SECRET_ACCESS_KEY": htup.StringSecret("secret"),
	})
}

func runtimeSecretProviders(secrets runtime.Secrets) htup.SecretsProviders {
	return htup.SecretsProviders{runtime.New(secrets)}
}
