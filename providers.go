package hyperproviders

import (
	"context"
	"fmt"

	// HyperTemplatesKit packages
	"github.com/herdworks/hyperproviders-go/s3"
	hypertemplates "github.com/herdworks/hypertemplates-go"
)

type HyperProvider struct {
	Config *HyperProviderConfig
	Engine HyperProviderEngine
}

func New(config *HyperProviderConfig) (provider *HyperProvider, err error) {
	if err = config.Validate(); err != nil {
		return nil, err
	}
	provider = &HyperProvider{Config: config}
	switch provider.Config.Kind {
	case "s3":
		engine := &s3.Provider{Endpoint: config.Endpoint, Bucket: config.Store}
		if err = config.Secrets.Validate("access_key_id", "secret_access_key"); err != nil {
			return nil, err
		}
		if engine.AccessKey, err = config.Secrets.Get("access_key_id").Value(); err != nil {
			return nil, err
		}
		if engine.SecretAccessKey, err = config.Secrets.Get("secret_access_key").Value(); err != nil {
			return nil, err
		}
		provider.Engine = engine
	default:
		err := fmt.Errorf("provider kind \"%s\" not supported", config.Kind)
		return nil, err
	}
	return provider, nil
}

// Publish a single directory (non-recursive)
func (provider *HyperProvider) Publish(context context.Context, buildPath string, domain string) error {
	hyperfiles := hypertemplates.GetFiles(buildPath, false)
	hyperfiles.SetRoot(buildPath)
	return provider.Engine.PutFiles(context, hyperfiles, buildPath, domain)
}

// Publish the entire website (recursive)
func (provider *HyperProvider) Deploy(context context.Context, buildPath string, domain string) error {
	hyperfiles := hypertemplates.GetFiles(buildPath, true)
	if len(hyperfiles) == 0 {
		return fmt.Errorf("no files found in build path: %s", buildPath)
	}
	hyperfiles.SetRoot(buildPath) // sets file.Base to the build path
	return provider.Engine.PutFiles(context, hyperfiles, buildPath, domain)
}

// =============================================================================
// Helper types, methods, and functions
// =============================================================================

type HyperProviderEngine interface {
	PutFile(context context.Context, hyperfile hypertemplates.HyperFile, buildPath string, domain string) error
	PutFiles(context context.Context, hyperfiles hypertemplates.HyperFiles, buildPath string, domain string) error
}

type HyperProviderConfig struct {
	Kind     string  `json:"kind" yaml:"kind"`
	Endpoint string  `json:"endpoint" yaml:"endpoint"`
	Store    string  `json:"store" yaml:"store"` // e.g. bucket name
	Secrets  Secrets `json:"secrets" yaml:"secrets"`
}

func (provider *HyperProviderConfig) Validate() (err error) {
	if len(provider.Kind) == 0 {
		return fmt.Errorf("missing required \"site.provider.kind\" setting")
	}
	if len(provider.Endpoint) == 0 {
		return fmt.Errorf("missing required \"site.provider.endpoint\" setting")
	}
	if len(provider.Store) == 0 {
		return fmt.Errorf("missing required \"site.provider.store\" setting")
	}
	return nil
}
