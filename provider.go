package hyperproviders

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/herdworks/hyperproviders-go/hosting"
)

type HostingProvider = hosting.Provider
type HostingProviders map[string]*HostingProvider
type HostingProviderEngine = hosting.Engine
type HostingEnvironment []string

type File = hosting.File
type FilterRules = hosting.FilterRules

// Deploy deploys every provider in the map concurrently. Each provider
// receives the deployment root and owns its file discovery/filtering. Errors
// from individual providers are aggregated with errors.Join.
func (providers HostingProviders) Deploy(ctx context.Context, root string, session *Session) (err error) {
	if err := providers.Validate(); err != nil {
		return err
	}
	if !hosting.DeploymentActive(ctx) {
		var finish func(error)
		ctx, finish = hosting.BeginDeployment(ctx, "")
		defer func() { finish(err) }()
	}

	errs := make([]error, 0, len(providers))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for name, provider := range providers {
		wg.Add(1)
		go func(name string, provider *HostingProvider) {
			defer wg.Done()
			if err := provider.DeployNamed(ctx, name, root, session); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("provider %q: %w", name, err))
				mu.Unlock()
			}
		}(name, provider)
	}
	wg.Wait()

	err = errors.Join(errs...)
	return err
}

// Ready runs local runtime-aware readiness checks for every selected provider.
// It validates provider configuration and confirms runtime-resolved local
// credentials are usable, but does not perform network probes.
func (providers HostingProviders) Ready(ctx context.Context, session *Session) error {
	if err := providers.Validate(); err != nil {
		return err
	}

	errs := make([]error, 0, len(providers))
	for name, provider := range providers {
		if err := provider.Ready(ctx, session); err != nil {
			errs = append(errs, fmt.Errorf("provider %q: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// Plan returns each selected provider's deployment-relative paths after
// discovery and provider filtering.
func (providers HostingProviders) Plan(root string) (map[string][]string, error) {
	if err := providers.Validate(); err != nil {
		return nil, err
	}

	plan := map[string][]string{}
	for name, provider := range providers {
		if err := provider.Validate(); err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		files, err := provider.DeploymentFiles(root)
		if err != nil {
			return nil, fmt.Errorf("provider %q files: %w", name, err)
		}
		paths := make([]string, 0, len(files))
		for _, file := range files {
			paths = append(paths, file.RelPath())
		}
		plan[name] = paths
	}
	return plan, nil
}

// Select returns a provider map containing only names, preserving the original
// provider pointers.
func (providers HostingProviders) Select(names []string) (HostingProviders, error) {
	if len(names) == 0 {
		return nil, errors.New("no providers selected")
	}
	seen := map[string]struct{}{}
	selected := make(HostingProviders, len(names))
	for _, name := range names {
		if name == "" {
			return nil, errors.New("provider name is empty")
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("provider %q selected more than once", name)
		}
		seen[name] = struct{}{}
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

// Validate checks that providers is non-empty and contains only named,
// non-empty provider values.
func (providers HostingProviders) Validate() error {
	if len(providers) == 0 {
		return errors.New("no providers selected")
	}
	for name, provider := range providers {
		if name == "" {
			return errors.New("provider name is empty")
		}
		if provider == nil {
			return fmt.Errorf("provider %q is empty", name)
		}
	}
	return nil
}

// Select returns the environment's selected providers from providers.
func (environment HostingEnvironment) Select(providers HostingProviders) (HostingProviders, error) {
	return providers.Select([]string(environment))
}

// Deploy deploys the environment's provider names against providers.
func (environment HostingEnvironment) Deploy(ctx context.Context, providers HostingProviders, root string, session *Session) error {
	selected, err := environment.Select(providers)
	if err != nil {
		return err
	}
	return selected.Deploy(ctx, root, session)
}

// Ready runs local runtime-aware readiness checks for the environment's
// selected provider names.
func (environment HostingEnvironment) Ready(ctx context.Context, providers HostingProviders, session *Session) error {
	selected, err := environment.Select(providers)
	if err != nil {
		return err
	}
	return selected.Ready(ctx, session)
}

// Plan returns the environment's selected provider plan against providers.
func (environment HostingEnvironment) Plan(providers HostingProviders, root string) (map[string][]string, error) {
	selected, err := environment.Select(providers)
	if err != nil {
		return nil, err
	}
	return selected.Plan(root)
}

const ProviderWorkspaceDir = hosting.ProviderWorkspaceDir

func RegisterHostingProviderEngine(kind string, engine HostingProviderEngine) {
	hosting.RegisterEngine(kind, engine)
}

func IsProviderKind(kind string) bool {
	return hosting.IsProviderKind(kind)
}
