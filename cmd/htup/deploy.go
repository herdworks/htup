package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/herdworks/htup"
)

// runDeploy implements `htup deploy`.
func runDeploy(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	var (
		configPath = fs.String("config", "", "path to JSON or YAML config file")
		envName    = fs.String("environment", "", "environment name from config")
		source     = fs.String("source", "", "source directory to deploy")
		modeName   = fs.String("mode", "", "deployment kind: complete or incremental")
		dryRun     = fs.Bool("dry-run", false, "list files that would be deployed without uploading")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *envName == "" || *source == "" || *modeName == "" {
		fs.Usage()
		return fmt.Errorf("deploy: --config, --environment, --source, and --mode are required")
	}
	kind := htup.DeploymentKind(*modeName)
	if err := kind.Validate(); err != nil {
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
	selected, err := env.Select(cfg.Providers)
	if err != nil {
		return err
	}
	deployment := &htup.Deployment{ConfigPath: *configPath, PayloadPath: *source, Environment: *envName, Kind: kind}

	if *dryRun {
		prepareCtx, w, finish := htup.BeginDeployment(htup.WithDeploymentRoot(ctx, *source), *envName, os.Stdout)
		err := deployment.Prepare(prepareCtx, w)
		finish(err)
		if err == nil {
			renderPlan(os.Stdout, selected, deployment.Plan())
		}
		return err
	}
	return deployment.Execute(ctx, SecretProviders(), nil)
}

func renderPlan(w io.Writer, providers htup.HostingProviders, plan htup.DeploymentPlan) {
	for _, group := range []struct {
		name       string
		operations []htup.DeploymentOperation
	}{
		{name: "sequential", operations: plan.Sequential},
		{name: "concurrent", operations: plan.Concurrent},
	} {
		for _, operation := range group.operations {
			name := operation.HostingProvider.DisplayName()
			for candidate, provider := range providers {
				if provider == operation.HostingProvider {
					name = candidate
					break
				}
			}
			fmt.Fprintf(w, "plan %s provider %q: %d preconditions, %d bulk, %d enqueued\n", group.name, name, len(operation.Preconditions), len(operation.Bulk), len(operation.Enqueued))
			for _, precondition := range operation.Preconditions {
				fmt.Fprintf(w, "  precondition %s\n", precondition.Target)
			}
			for _, file := range operation.Bulk {
				fmt.Fprintf(w, "  bulk %s\n", file.RelPath())
			}
			for _, file := range operation.Enqueued {
				fmt.Fprintf(w, "  enqueue %s\n", file.RelPath())
			}
		}
	}
}

// runValidate implements `htup validate`. Parses the config and runs
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

// runReady implements `htup ready`. It parses config and runs runtime-aware
// local readiness checks for each selected provider. Secrets are resolved from
// OS environment variables via SecretProviders().
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

	secretProviders := SecretProviders()
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
			if err := p.Ready(ctx, secretProviders); err != nil {
				return fmt.Errorf("environment %q provider %q: %w", envName, providerName, err)
			}
			fmt.Printf("ready  %s / %s (%s)\n", envName, providerName, p.Kind)
		}
	}
	return nil
}

// runList implements `htup list`. Prints environments + provider kinds.
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
