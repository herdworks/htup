package s3

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/herdworks/hyperproviders-go/hosting"
	"github.com/herdworks/hyperproviders-go/secrets"
	"github.com/herdworks/hyperproviders-go/session"
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
	var provider hosting.Provider
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
	provider := &hosting.Provider{
		Kind:     Kind,
		Endpoint: "https://example.com",
		Config:   map[string]any{},
	}
	if err := (Engine{}).Validate(provider); err == nil {
		t.Error("expected error for missing bucket")
	}
}

func TestValidate_UsesBucketFromEndpointPath(t *testing.T) {
	provider := &hosting.Provider{
		Kind:     Kind,
		Endpoint: "https://example.com/path-bucket",
		Config:   map[string]any{},
		Secrets: secrets.Secrets{
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
	provider := &hosting.Provider{
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
	provider := &hosting.Provider{
		Config: map[string]any{
			"bucket": "configured-bucket",
			"region": "us-east-1",
		},
		Secrets: secrets.Secrets{
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
	provider := &hosting.Provider{
		Config: map[string]any{
			"addressing_style": "virtual",
			"region":           "us-east-1",
		},
		Secrets: secrets.Secrets{
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
	provider := &hosting.Provider{Config: map[string]any{"addressing_style": "sideways"}}

	if err := (Engine{}).Validate(provider); err == nil {
		t.Fatal("expected error for unknown addressing_style")
	}
}

func TestValidate_RejectsInvalidWorkers(t *testing.T) {
	provider := &hosting.Provider{Config: map[string]any{"workers": 0}}

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

	err := provider.Ready(context.Background(), &session.Session{})
	if err == nil {
		t.Fatal("Ready error = nil, want missing secret")
	}
	if !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("Ready error = %v, want access key", err)
	}
}

func TestReady_WithRuntimeSecrets(t *testing.T) {
	provider := readyProvider()
	runtime := &session.Session{
		Secrets: secrets.Values{
			"AWS_ACCESS_KEY_ID":     secrets.String("access"),
			"AWS_SECRET_ACCESS_KEY": secrets.String("secret"),
		},
	}

	if err := provider.Ready(context.Background(), runtime); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func readyProvider() *hosting.Provider {
	return &hosting.Provider{
		Kind:     Kind,
		Endpoint: "https://example.com",
		Config:   map[string]any{"bucket": "assets"},
		Secrets: secrets.Secrets{
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
