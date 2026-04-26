package hyperproviders

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Environment is a named deployment target with one or more providers.
// A typical setup has Environments named "development", "staging", and
// "production", each configuring the appropriate providers.
type Environment struct {
	Name      string             `json:"name"      yaml:"name"`
	Providers []*HostingProvider `json:"providers" yaml:"providers"`
}

// Deploy fans out to every provider concurrently. Each provider receives the
// deployment root and owns its file discovery/filtering. Errors from individual
// providers are aggregated with errors.Join — one provider failing does not
// prevent the others from running. Respects ctx cancellation.
func (e *Environment) Deploy(ctx context.Context, root string, session *Session) error {
	if len(e.Providers) == 0 {
		return fmt.Errorf("environment %q has no providers", e.Name)
	}

	errs := make([]error, len(e.Providers))
	var wg sync.WaitGroup
	for i, provider := range e.Providers {
		wg.Add(1)
		go func(i int, provider *HostingProvider) {
			defer wg.Done()
			if err := provider.Deploy(ctx, root, session); err != nil {
				errs[i] = fmt.Errorf("provider %q: %w", provider.DisplayName(), err)
			}
		}(i, provider)
	}
	wg.Wait()

	return errors.Join(errs...)
}

// Plan returns each provider's deployment-relative paths after discovery and
// provider filtering. It is intended for dry-run and diagnostics.
func (e *Environment) Plan(root string) (map[string][]string, error) {
	plan := map[string][]string{}
	for _, provider := range e.Providers {
		files, err := provider.DeploymentFiles(root)
		if err != nil {
			return nil, fmt.Errorf("provider %q files: %w", provider.DisplayName(), err)
		}
		paths := make([]string, 0, len(files))
		for _, file := range files {
			paths = append(paths, file.RelPath())
		}
		plan[provider.DisplayName()] = paths
	}
	return plan, nil
}
