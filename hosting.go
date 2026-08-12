package htup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/herdworks/hyperfiles-go/mimetypes"
)

// HostingEngine owns provider-specific validation and execution of a complete operation.
type HostingEngine interface {
	Validate(*HostingProvider) error
	Ready(context.Context, SecretsProviders, *HostingProvider) error
	ValidateOperation(*HostingProvider, DeploymentOperation) error
	PerformOperation(context.Context, SecretsProviders, DeploymentOperation, io.Writer) error
}

const (
	// HostingProviderKindGit publishes a deployment to a Git remote over HTTPS or SSH.
	HostingProviderKindGit = "git"
	// HostingProviderKindS3 publishes a deployment to an S3-compatible object store.
	HostingProviderKindS3 = "s3"
)

type HostingProvider struct {
	// Kind selects a HostingEngine. The built-in kinds are "git" and "s3";
	// other registered kinds may be experimental and therefore undocumented.
	Kind string `json:"kind" yaml:"kind" jsonschema:"required,description=Built-in kinds are 'git' and 's3'. Other registered kinds may be experimental and undocumented."`
	// Endpoint is provider-specific: Git uses its remote URL, while S3 uses an
	// S3-compatible endpoint and may derive a path-style bucket from its path.
	Endpoint string         `json:"endpoint" yaml:"endpoint" jsonschema:"required,description=Provider-specific connection target. Git uses a remote URL. S3 uses an S3-compatible endpoint and may derive a path-style bucket from its path."`
	BaseURL  string         `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	Secrets  Secrets        `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Include  FilterRules    `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude  FilterRules    `json:"exclude,omitempty" yaml:"exclude,omitempty"`
	engine   HostingEngine
}

func (HostingProvider) JSONSchemaTitle() string {
	return "Hosting provider"
}
func (HostingProvider) JSONSchemaName() string { return "provider" }
func (HostingProvider) JSONSchemaSummary() string {
	return "Configuration for one website hosting provider."
}

func (HostingProvider) JSONSchemaDescription() string {
	return "Configuration for one website hosting provider."
}

type HostingProviders map[string]*HostingProvider

type Environment []string

// Filter narrows provider file ownership using path and MIME regex rules.
type Filter struct {
	Include FilterRules `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude FilterRules `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

type FilterRules struct {
	Paths []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	Types []string `json:"types,omitempty" yaml:"types,omitempty"`
}

func (FilterRules) JSONSchemaTitle() string {
	return "Hosting provider filter rules"
}
func (FilterRules) JSONSchemaName() string    { return "provider-filter-rules" }
func (FilterRules) JSONSchemaSummary() string { return "Optional provider path and MIME type rules." }

func (FilterRules) JSONSchemaDescription() string {
	return "Optional path and MIME type rules that select provider-owned files."
}

type compiledFilter struct{ includePath, includeType, excludePath, excludeType []*regexp.Regexp }

const ProviderWorkspaceDir = ".deployment"

var hostingProviderEngines = struct {
	sync.RWMutex
	known map[string]HostingEngine
}{known: map[string]HostingEngine{}}

func (providers HostingProviders) Select(names []string) (HostingProviders, error) {
	if len(names) == 0 {
		return nil, errors.New("no providers selected")
	}
	selected := make(HostingProviders, len(names))
	for _, name := range names {
		if name == "" {
			return nil, errors.New("provider name is empty")
		}
		if _, ok := selected[name]; ok {
			return nil, fmt.Errorf("provider %q selected more than once", name)
		}
		provider, ok := providers[name]
		if !ok {
			return nil, fmt.Errorf("provider %q not found", name)
		}
		if provider == nil {
			return nil, fmt.Errorf("provider %q is empty", name)
		}
		selected[name] = provider
	}
	return selected, nil
}

func (environment Environment) Select(providers HostingProviders) (HostingProviders, error) {
	return providers.Select([]string(environment))
}

func registerEngine(kind string, engine HostingEngine) {
	if kind == "" {
		panic("hyperproviders: RegisterEngine with empty kind")
	}
	if engine == nil {
		panic("hyperproviders: RegisterEngine with nil engine")
	}
	hostingProviderEngines.Lock()
	defer hostingProviderEngines.Unlock()
	if _, ok := hostingProviderEngines.known[kind]; ok {
		panic("hyperproviders: RegisterEngine called twice for kind " + kind)
	}
	hostingProviderEngines.known[kind] = engine
}
func hostingProviderEngine(kind string) (HostingEngine, bool) {
	hostingProviderEngines.RLock()
	defer hostingProviderEngines.RUnlock()
	engine, ok := hostingProviderEngines.known[kind]
	return engine, ok
}
func (p *HostingProvider) DisplayName() string {
	if p == nil {
		return ""
	}
	return p.Kind
}
func (p *HostingProvider) DecodeConfig(target any) error {
	if len(p.Config) == 0 {
		return nil
	}
	data, err := json.Marshal(p.Config)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	return nil
}
func (p *HostingProvider) DestinationURL(rel string) string {
	if p == nil || p.BaseURL == "" {
		return rootRelativeURL(rel)
	}
	joined, err := url.JoinPath(p.BaseURL, rel)
	if err != nil {
		return strings.TrimRight(p.BaseURL, "/") + "/" + strings.TrimLeft(rel, "/")
	}
	return joined
}
func rootRelativeURL(rel string) string {
	return (&url.URL{Path: "/" + strings.TrimLeft(rel, "/")}).String()
}
func (p *HostingProvider) Validate() error {
	if err := p.ValidateRoot(); err != nil {
		return err
	}
	return p.engine.Validate(p)
}
func (p *HostingProvider) Ready(ctx context.Context, secretProviders SecretsProviders) error {
	if err := p.ValidateRoot(); err != nil {
		return err
	}
	return p.engine.Ready(ctx, secretProviders, p)
}
func (p *HostingProvider) ValidateOperation(operation DeploymentOperation) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return p.engine.ValidateOperation(p, operation)
}
func (p *HostingProvider) PerformOperation(ctx context.Context, secretProviders SecretsProviders, operation DeploymentOperation, w io.Writer) error {
	return p.engine.PerformOperation(ctx, secretProviders, operation, w)
}

func (p *HostingProvider) UnmarshalJSON(data []byte) error {
	type alias HostingProvider
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = HostingProvider(decoded)
	p.engine = nil
	return nil
}

func (p *HostingProvider) UnmarshalYAML(value *yaml.Node) error {
	type alias HostingProvider
	var decoded alias
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*p = HostingProvider(decoded)
	p.engine = nil
	return nil
}

func (p *HostingProvider) ValidateRoot() error {
	if p.Kind == "" {
		return ErrHostingProviderMissingKind
	}
	if p.Endpoint == "" {
		return ErrHostingProviderMissingEndpoint
	}
	if err := p.attachEngine(); err != nil {
		return err
	}
	if err := p.Secrets.Validate(); err != nil {
		return fmt.Errorf("provider %q secrets: %w", p.DisplayName(), err)
	}
	if _, err := p.filter().compile(); err != nil {
		return fmt.Errorf("provider %q filter: %w", p.DisplayName(), err)
	}
	return nil
}
func (p *HostingProvider) filter() Filter {
	if p == nil {
		return Filter{}
	}
	return Filter{Include: p.Include, Exclude: p.Exclude}
}
func (p *HostingProvider) deploymentFiles(ctx context.Context, w io.Writer, files []*File) ([]*File, error) {
	filter, err := p.filter().compile()
	if err != nil {
		return nil, err
	}
	kept := make([]*File, 0, len(files))
	for _, file := range files {
		ok, err := filter.Match(file)
		if err != nil {
			return nil, err
		}
		if ok {
			LogFile(w, ctx, p, file, "included")
			kept = append(kept, file)
		} else {
			LogFile(w, ctx, p, file, "excluded")
		}
	}
	return kept, nil
}
func (p *HostingProvider) logSecrets(ctx context.Context, secretProviders SecretsProviders, w io.Writer) {
	for _, secret := range p.Secrets {
		state := "found"
		_, err := secretProviders.Resolve(ctx, secret.LookupKey())
		switch {
		case err == nil:
		case errors.Is(err, ErrSecretNotFound):
			state = "not_found"
		case errors.Is(err, ErrSecretEmpty):
			state = "empty"
		default:
			state = "error"
		}
		LogSecret(w, ctx, p, secret.Name, state)
	}
}
func (p *HostingProvider) attachEngine() error {
	p.engine = nil
	if p.Kind == "" {
		return ErrHostingProviderMissingKind
	}
	engine, ok := hostingProviderEngine(p.Kind)
	if !ok {
		return fmt.Errorf("%w: %s", ErrHostingProviderUnknownKind, p.Kind)
	}
	p.engine = engine
	return nil
}

func (f Filter) Match(file *File) (bool, error) {
	c, err := f.compile()
	if err != nil {
		return false, err
	}
	return c.Match(file)
}
func (f *compiledFilter) Match(file *File) (bool, error) {
	mime := ""
	if len(f.includeType) > 0 || len(f.excludeType) > 0 {
		var err error
		mime, err = file.Mimetype()
		if err != nil {
			return false, err
		}
	}
	return f.MatchResource(file.RelPath(), mime)
}
func (f *compiledFilter) MatchResource(resourcePath, mime string) (bool, error) {
	if len(f.includePath) > 0 && !matchAny(f.includePath, resourcePath, false) {
		return false, nil
	}
	base := ""
	if len(f.includeType) > 0 || len(f.excludeType) > 0 {
		var err error
		base, err = mimetypes.Base(mime)
		if err != nil {
			return false, err
		}
	}
	if len(f.includeType) > 0 && !matchAny(f.includeType, base, false) {
		return false, nil
	}
	if matchAny(f.excludePath, resourcePath, false) || matchAny(f.excludeType, base, false) {
		return false, nil
	}
	return true, nil
}
func (f Filter) compile() (*compiledFilter, error) {
	a, err := compileAll(f.Include.Paths)
	if err != nil {
		return nil, err
	}
	b, err := compileAll(f.Include.Types)
	if err != nil {
		return nil, err
	}
	c, err := compileAll(f.Exclude.Paths)
	if err != nil {
		return nil, err
	}
	d, err := compileAll(f.Exclude.Types)
	if err != nil {
		return nil, err
	}
	return &compiledFilter{a, b, c, d}, nil
}
func compileAll(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", pattern, err)
		}
		out = append(out, re)
	}
	return out, nil
}
func matchAny(list []*regexp.Regexp, value string, empty bool) bool {
	if len(list) == 0 {
		return empty
	}
	for _, re := range list {
		if re.MatchString(value) {
			return true
		}
	}
	return false
}
