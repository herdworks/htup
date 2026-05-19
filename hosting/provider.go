package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/herdworks/hyperproviders-go/secrets"
	"github.com/herdworks/hyperproviders-go/session"
	"gopkg.in/yaml.v3"
)

// ProviderWorkspaceDir is reserved for provider-managed deployment workspace
// state under the deployment root. Discovery never deploys files from this
// directory.
const ProviderWorkspaceDir = ".provider"

// Provider is a concrete provider configured for an environment. It owns
// common provider fields and delegates provider-specific behavior to engine.
type Provider struct {
	Kind     string          `json:"kind"              yaml:"kind"`
	Endpoint string          `json:"endpoint"          yaml:"endpoint"`
	BaseURL  string          `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	Config   map[string]any  `json:"config,omitempty"  yaml:"config,omitempty"`
	Secrets  secrets.Secrets `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Include  FilterRules     `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude  FilterRules     `json:"exclude,omitempty" yaml:"exclude,omitempty"`

	engine Engine
}

// Engine defines provider-specific behavior behind a concrete
// Provider. Engines do not own common provider fields, file discovery,
// filtering, or environment fan-out.
type Engine interface {
	Validate(provider *Provider) error
	Deploy(ctx context.Context, runtime *session.Session, provider *Provider, root string, files []*File) error
}

var hostingProviderEngines = struct {
	sync.RWMutex
	known map[string]Engine
}{
	known: map[string]Engine{},
}

// RegisterEngine registers engine for kind. Concrete provider packages call
// this from init so validation, planning, and deployment can bind provider
// behavior.
func RegisterEngine(kind string, engine Engine) {
	if kind == "" {
		panic("hyperproviders: RegisterEngine with empty kind")
	}
	if engine == nil {
		panic("hyperproviders: RegisterEngine with nil engine")
	}
	hostingProviderEngines.Lock()
	defer hostingProviderEngines.Unlock()
	if _, exists := hostingProviderEngines.known[kind]; exists {
		panic("hyperproviders: RegisterEngine called twice for kind " + kind)
	}
	hostingProviderEngines.known[kind] = engine
}

// IsProviderKind reports whether kind has a registered provider engine.
func IsProviderKind(kind string) bool {
	_, ok := hostingProviderEngine(kind)
	return ok
}

func hostingProviderEngine(kind string) (Engine, bool) {
	hostingProviderEngines.RLock()
	defer hostingProviderEngines.RUnlock()
	engine, ok := hostingProviderEngines.known[kind]
	return engine, ok
}

// DisplayName returns the fallback provider display name for direct provider
// calls. Deployments selected from a provider map should pass the map key to
// DeployNamed instead.
func (p *Provider) DisplayName() string {
	if p == nil {
		return ""
	}
	return p.Kind
}

// DecodeConfig decodes the provider-specific Config object into target.
func (p *Provider) DecodeConfig(target any) error {
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

// DestinationURL returns the human-readable deployment destination for rel.
// BaseURL is a public URL prefix for deployed files. When BaseURL is unset, the
// destination falls back to a root-relative path.
func (p *Provider) DestinationURL(rel string) string {
	if p == nil || p.BaseURL == "" {
		return rootRelativeURL(rel)
	}
	joined, err := url.JoinPath(p.BaseURL, rel)
	if err != nil {
		return strings.TrimRight(p.BaseURL, "/") + "/" + strings.TrimLeft(rel, "/")
	}
	return joined
}

// Validate validates root-level fields, then delegates provider-specific
// configuration checks to the provider engine.
func (p *Provider) Validate() error {
	if err := p.ValidateRoot(); err != nil {
		return err
	}
	return p.engine.Validate(p)
}

// ValidateRoot runs baseline checks shared by every Provider.
func (p *Provider) ValidateRoot() error {
	if p.Kind == "" {
		return ErrMissingKind
	}
	if p.Endpoint == "" {
		return ErrMissingEndpoint
	}
	if err := p.attachEngine(); err != nil {
		return err
	}
	if err := p.Secrets.Validate(); err != nil {
		return fmt.Errorf("provider %q secrets: %w", p.DisplayName(), err)
	}
	filter := p.filter()
	if _, err := filter.compile(); err != nil {
		return fmt.Errorf("provider %q filter: %w", p.DisplayName(), err)
	}
	return nil
}

func rootRelativeURL(rel string) string {
	return (&url.URL{Path: "/" + strings.TrimLeft(rel, "/")}).String()
}

// DeploymentFiles returns the validated files this provider would deploy from
// root after applying the provider filter.
func (p *Provider) DeploymentFiles(root string) ([]*File, error) {
	if err := p.ValidateRoot(); err != nil {
		return nil, err
	}
	files, err := walk(root)
	if err != nil {
		return nil, err
	}
	return p.deploymentFiles(nil, files)
}

func (p *Provider) deploymentFiles(ctx context.Context, files []*File) ([]*File, error) {
	providerFilter := p.filter()
	filter, err := providerFilter.compile()
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
			LogFile(ctx, p, file, "included")
			kept = append(kept, file)
			continue
		}
		LogFile(ctx, p, file, "excluded")
	}
	return kept, nil
}

// Deploy discovers files, applies the provider filter, and delegates the
// selected files to the provider engine.
func (p *Provider) Deploy(ctx context.Context, root string, runtime *session.Session) (err error) {
	return p.DeployNamed(ctx, "", root, runtime)
}

// DeployNamed discovers files, applies the provider filter, and delegates the
// selected files to the provider engine using name as the provider display name.
func (p *Provider) DeployNamed(ctx context.Context, name string, root string, runtime *session.Session) (err error) {
	ctx = WithProviderName(ctx, p.providerName(name))
	if !DeploymentActive(ctx) {
		var finish func(error)
		ctx, finish = BeginDeployment(ctx, "")
		defer func() { finish(err) }()
	}

	providerName := ProviderName(ctx, p)
	LogProvider(ctx, p, "hosting", providerName, "init")

	if err := p.Validate(); err != nil {
		LogProvider(ctx, p, "hosting", providerName, "failed")
		return err
	}
	p.logSecrets(ctx, runtime)

	files, err := walk(root)
	if err != nil {
		LogProvider(ctx, p, "hosting", providerName, "failed")
		return err
	}
	files, err = p.deploymentFiles(ctx, files)
	if err != nil {
		LogProvider(ctx, p, "hosting", providerName, "failed")
		return err
	}
	LogProvider(ctx, p, "hosting", providerName, "ready")

	if err := p.engine.Deploy(ctx, runtime, p, root, files); err != nil {
		LogProvider(ctx, p, "hosting", providerName, "failed")
		return err
	}
	return nil
}

func (p *Provider) filter() Filter {
	if p == nil {
		return Filter{}
	}
	return Filter{Include: p.Include, Exclude: p.Exclude}
}

func (p *Provider) providerName(name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return p.DisplayName()
}

func (p *Provider) logSecrets(ctx context.Context, runtime *session.Session) {
	for _, secret := range p.Secrets {
		state := "found"
		_, err := runtime.ResolveSecret(ctx, secret.LookupKey())
		switch {
		case err == nil:
		case errors.Is(err, secrets.ErrNotFound):
			state = "not_found"
		case errors.Is(err, secrets.ErrEmpty):
			state = "empty"
		default:
			state = "error"
		}
		LogSecret(ctx, p, secret.Name, state)
	}
}

// UnmarshalJSON decodes the common provider envelope. Provider engines are
// resolved during validation, planning, and deployment, not while parsing
// config.
func (p *Provider) UnmarshalJSON(data []byte) error {
	type hostingProvider Provider
	var decoded hostingProvider
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = Provider(decoded)
	p.engine = nil
	return nil
}

// UnmarshalYAML decodes the common provider envelope. Provider engines are
// resolved during validation, planning, and deployment, not while parsing
// config.
func (p *Provider) UnmarshalYAML(value *yaml.Node) error {
	type hostingProvider Provider
	var decoded hostingProvider
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*p = Provider(decoded)
	p.engine = nil
	return nil
}

func (p *Provider) attachEngine() error {
	p.engine = nil
	if p.Kind == "" {
		return ErrMissingKind
	}
	engine, ok := hostingProviderEngine(p.Kind)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownKind, p.Kind)
	}
	p.engine = engine
	return nil
}

// Filter narrows the set of files a provider accepts. Include rules
// define an allow-list; Exclude rules subtract from it. An empty filter
// accepts everything.
//
// Within an Include list, any match admits the file (OR semantics). Across
// Include.Paths and Include.Types, both lists must be consulted — if both are
// non-empty, the file must match at least one pattern in EACH list.
//
// hyperproviders does not dedupe across providers: the same file may
// legitimately be deployed by multiple providers (e.g. CDN + backup store).
type Filter struct {
	Include FilterRules `json:"include,omitempty" yaml:"include,omitempty"`
	Exclude FilterRules `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}

type compiledFilter struct {
	includePath []*regexp.Regexp
	includeType []*regexp.Regexp
	excludePath []*regexp.Regexp
	excludeType []*regexp.Regexp
}

// FilterRules is a pair of regex lists: one applied to File.RelPath() and one
// applied to File.Mimetype().
type FilterRules struct {
	Paths []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	Types []string `json:"types,omitempty" yaml:"types,omitempty"`
}

// Match reports whether file passes the filter. Returns an error if any
// pattern fails to compile.
func (f *Filter) Match(file *File) (bool, error) {
	filter, err := f.compile()
	if err != nil {
		return false, err
	}
	return filter.Match(file)
}

func (f *compiledFilter) Match(file *File) (bool, error) {
	path := file.RelPath()

	var mimetype string
	needMime := len(f.includeType) > 0 || len(f.excludeType) > 0
	if needMime {
		m, err := file.Mimetype()
		if err != nil {
			return false, fmt.Errorf("mimetype %q: %w", path, err)
		}
		mimetype = m
	}

	if len(f.includePath) > 0 && !matchAny(f.includePath, path, false) {
		return false, nil
	}
	if len(f.includeType) > 0 && !matchAny(f.includeType, mimetype, false) {
		return false, nil
	}
	if matchAny(f.excludePath, path, false) {
		return false, nil
	}
	if matchAny(f.excludeType, mimetype, false) {
		return false, nil
	}
	return true, nil
}

func (f *Filter) compile() (*compiledFilter, error) {
	includePath, err := compileAll(f.Include.Paths)
	if err != nil {
		return nil, err
	}
	includeType, err := compileAll(f.Include.Types)
	if err != nil {
		return nil, err
	}
	excludePath, err := compileAll(f.Exclude.Paths)
	if err != nil {
		return nil, err
	}
	excludeType, err := compileAll(f.Exclude.Types)
	if err != nil {
		return nil, err
	}
	return &compiledFilter{
		includePath: includePath,
		includeType: includeType,
		excludePath: excludePath,
		excludeType: excludeType,
	}, nil
}

// compileAll compiles a list of regex patterns, returning the first error.
func compileAll(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// matchAny reports whether any compiled regex matches s. When list is empty,
// returns emptyResult — lets callers distinguish "no rules = keep" from
// "no rules = drop".
func matchAny(list []*regexp.Regexp, s string, emptyResult bool) bool {
	if len(list) == 0 {
		return emptyResult
	}
	for _, re := range list {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
