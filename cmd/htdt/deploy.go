package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	hp "github.com/herdworks/hyperproviders-go"
)

// runDeploy implements `htdt deploy`.
func runDeploy(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	var (
		configPath = fs.String("config", "", "path to JSON or YAML config file")
		envName    = fs.String("environment", "", "environment name from config")
		source     = fs.String("source", "", "source directory to deploy")
		modeName   = fs.String("mode", "", "deployment mode: replacement or overlay")
		dryRun     = fs.Bool("dry-run", false, "list files that would be deployed without uploading")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *envName == "" || *source == "" || *modeName == "" {
		fs.Usage()
		return fmt.Errorf("deploy: --config, --environment, --source, and --mode are required")
	}
	mode := hp.DeploymentMode(*modeName)
	if err := mode.Validate(); err != nil {
		return fmt.Errorf("deploy: %w", err)
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	env, err := cfg.Environment(*envName)
	if err != nil {
		return err
	}
	for _, name := range *env {
		p := cfg.Providers[name]
		if p == nil {
			return fmt.Errorf("validate provider %q: provider not found", name)
		}
		if err := p.Validate(); err != nil {
			return fmt.Errorf("validate provider %q: %w", name, err)
		}
	}

	if *dryRun {
		return printDryRun(os.Stdout, cfg.Providers, env, *source)
	}

	return env.Deploy(ctx, cfg.Providers, *source, Session(), mode, nil)
}

// runValidate implements `htdt validate`. Parses the config and runs
// Validate() on every provider of every environment.
func runValidate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to JSON or YAML config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		fs.Usage()
		return fmt.Errorf("validate: --config is required")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	for _, envName := range sortedEnvNames(cfg) {
		env := cfg.Environments[envName]
		if env == nil {
			return fmt.Errorf("environment %q is empty", envName)
		}
		for _, providerName := range *env {
			p := cfg.Providers[providerName]
			if p == nil {
				return fmt.Errorf("environment %q provider %q: provider not found", envName, providerName)
			}
			if err := p.Validate(); err != nil {
				return fmt.Errorf("environment %q provider %q: %w", envName, providerName, err)
			}
			fmt.Printf("ok  %s / %s (%s)\n", envName, providerName, p.Kind)
		}
	}
	return nil
}

// runReady implements `htdt ready`. It parses config and runs runtime-aware
// local readiness checks for each selected provider. Secrets are resolved from
// OS environment variables via Session().
func runReady(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ready", flag.ContinueOnError)
	var (
		configPath = fs.String("config", "", "path to JSON or YAML config file")
		envName    = fs.String("environment", "", "optional environment name from config")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		fs.Usage()
		return fmt.Errorf("ready: --config is required")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}

	envNames := sortedEnvNames(cfg)
	if *envName != "" {
		if _, err := cfg.Environment(*envName); err != nil {
			return err
		}
		envNames = []string{*envName}
	}

	session := Session()
	for _, envName := range envNames {
		env := cfg.Environments[envName]
		if env == nil {
			return fmt.Errorf("environment %q is empty", envName)
		}
		for _, providerName := range *env {
			p := cfg.Providers[providerName]
			if p == nil {
				return fmt.Errorf("environment %q provider %q: provider not found", envName, providerName)
			}
			if err := p.Ready(ctx, session); err != nil {
				return fmt.Errorf("environment %q provider %q: %w", envName, providerName, err)
			}
			fmt.Printf("ready  %s / %s (%s)\n", envName, providerName, p.Kind)
		}
	}
	return nil
}

// runList implements `htdt list`. Prints environments + provider kinds.
func runList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to JSON or YAML config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		fs.Usage()
		return fmt.Errorf("list: --config is required")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	for _, envName := range sortedEnvNames(cfg) {
		env := cfg.Environments[envName]
		fmt.Println(envName)
		if env == nil {
			fmt.Println("  - (empty)")
			continue
		}
		for _, providerName := range *env {
			p := cfg.Providers[providerName]
			if p == nil {
				fmt.Printf("  - %s (missing)\n", providerName)
				continue
			}
			fmt.Printf("  - %s (%s)\n", providerName, p.Kind)
		}
	}
	return nil
}

func sortedEnvNames(cfg *Config) []string {
	names := make([]string, 0, len(cfg.Environments))
	for name := range cfg.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func printDryRun(w *os.File, providers hp.HostingProviders, env *hp.HostingEnvironment, root string) error {
	plan, err := env.Plan(providers, root)
	if err != nil {
		return err
	}
	for _, providerName := range *env {
		p := providers[providerName]
		fmt.Fprintf(w, "# provider %s (%s)\n", providerName, p.Kind)
		for _, path := range plan[providerName] {
			fmt.Fprintln(w, path)
		}
	}
	return nil
}
