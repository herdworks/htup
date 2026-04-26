package hosting

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"

	"github.com/herdworks/hyperproviders-go/secrets"
	"github.com/herdworks/hyperproviders-go/session"
	"gopkg.in/yaml.v3"
)

// Provider is a concrete provider configured for an environment. It owns
// common provider fields and delegates provider-specific behavior to engine.
type Provider struct {
	Name     string          `json:"name"              yaml:"name"`
	Kind     string          `json:"kind"              yaml:"kind"`
	Endpoint string          `json:"endpoint"          yaml:"endpoint"`
	Config   map[string]any  `json:"config,omitempty"  yaml:"config,omitempty"`
	Secrets  secrets.Secrets `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Filter   Filter          `json:"filter,omitempty"  yaml:"filter,omitempty"`

	engine Engine
}

// Engine defines provider-specific behavior behind a concrete
// Provider. Engines do not own common provider fields, file discovery,
// filtering, or environment fan-out.
type Engine interface {
	Validate(provider *Provider) error
	Deploy(ctx context.Context, runtime *session.Session, provider *Provider, files []*File) error
}

var hostingProviderEngines = struct {
	sync.RWMutex
	known map[string]Engine
}{
	known: map[string]Engine{},
}

// RegisterEngine registers engine for kind. Concrete provider
// packages call this from init so config unmarshalling can bind providers.
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

// DisplayName returns the display name. It falls back to Kind when Name is unset.
func (p *Provider) DisplayName() string {
	if p == nil {
		return ""
	}
	if p.Name != "" {
		return p.Name
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
	if p.Name == "" {
		return ErrMissingName
	}
	if p.Kind == "" {
		return ErrMissingKind
	}
	if p.Endpoint == "" {
		return ErrMissingEndpoint
	}
	if err := p.attachEngine(); err != nil {
		return err
	}
	if err := p.Filter.compile(); err != nil {
		return fmt.Errorf("provider %q filter: %w", p.DisplayName(), err)
	}
	return nil
}

// DeploymentFiles returns the validated files this provider would deploy from
// root after applying the provider filter.
func (p *Provider) DeploymentFiles(root string) ([]*File, error) {
	files, err := walk(root)
	if err != nil {
		return nil, err
	}
	return p.deploymentFiles(files)
}

func (p *Provider) deploymentFiles(files []*File) ([]*File, error) {
	kept := make([]*File, 0, len(files))
	for _, file := range files {
		ok, err := p.Filter.Match(file)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, file)
		}
	}
	return kept, nil
}

// Deploy discovers files, applies the provider filter, and delegates the
// selected files to the provider engine.
func (p *Provider) Deploy(ctx context.Context, root string, runtime *session.Session) error {
	if err := p.Validate(); err != nil {
		return err
	}
	files, err := p.DeploymentFiles(root)
	if err != nil {
		return err
	}
	return p.engine.Deploy(ctx, runtime, p, files)
}

// UnmarshalJSON decodes the common provider envelope and attaches the
// registered engine for its kind.
func (p *Provider) UnmarshalJSON(data []byte) error {
	type hostingProvider Provider
	var decoded hostingProvider
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = Provider(decoded)
	return p.attachEngine()
}

// UnmarshalYAML decodes the common provider envelope and attaches the
// registered engine for its kind.
func (p *Provider) UnmarshalYAML(value *yaml.Node) error {
	type hostingProvider Provider
	var decoded hostingProvider
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*p = Provider(decoded)
	return p.attachEngine()
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

	once        sync.Once
	compileErr  error
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

// Match reports whether file passes the filter. Regex compilation happens
// exactly once per Filter and is cached. Returns an error if any
// pattern fails to compile.
func (f *Filter) Match(file *File) (bool, error) {
	if err := f.compile(); err != nil {
		return false, err
	}

	path := file.RelPath()

	var mime string
	needMime := len(f.includeType) > 0 || len(f.excludeType) > 0
	if needMime {
		m, err := file.Mimetype()
		if err != nil {
			return false, fmt.Errorf("mimetype %q: %w", path, err)
		}
		mime = m
	}

	if len(f.includePath) > 0 && !matchAny(f.includePath, path, false) {
		return false, nil
	}
	if len(f.includeType) > 0 && !matchAny(f.includeType, mime, false) {
		return false, nil
	}
	if matchAny(f.excludePath, path, false) {
		return false, nil
	}
	if matchAny(f.excludeType, mime, false) {
		return false, nil
	}
	return true, nil
}

func (f *Filter) compile() error {
	f.once.Do(func() {
		if f.includePath, f.compileErr = compileAll(f.Include.Paths); f.compileErr != nil {
			return
		}
		if f.includeType, f.compileErr = compileAll(f.Include.Types); f.compileErr != nil {
			return
		}
		if f.excludePath, f.compileErr = compileAll(f.Exclude.Paths); f.compileErr != nil {
			return
		}
		f.excludeType, f.compileErr = compileAll(f.Exclude.Types)
	})
	return f.compileErr
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
