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
		dryRun     = fs.Bool("dry-run", false, "list files that would be deployed without uploading")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *envName == "" || *source == "" {
		fs.Usage()
		return fmt.Errorf("deploy: --config, --environment, and --source are required")
	}

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	env, err := cfg.Environment(*envName)
	if err != nil {
		return err
	}
	for _, p := range env.Providers {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("validate provider %q: %w", p.DisplayName(), err)
		}
	}

	if *dryRun {
		return printDryRun(os.Stdout, env, *source)
	}

	return env.Deploy(ctx, *source, Session())
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
	for _, env := range sortedEnvs(cfg) {
		for _, p := range env.Providers {
			if err := p.Validate(); err != nil {
				return fmt.Errorf("environment %q provider %q: %w", env.Name, p.DisplayName(), err)
			}
			fmt.Printf("ok  %s / %s (%s)\n", env.Name, p.DisplayName(), p.Kind)
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
	for _, env := range sortedEnvs(cfg) {
		fmt.Println(env.Name)
		for _, p := range env.Providers {
			fmt.Printf("  - %s (%s)\n", p.DisplayName(), p.Kind)
		}
	}
	return nil
}

func sortedEnvs(cfg *Config) []*hp.Environment {
	names := make([]string, 0, len(cfg.Environments))
	for name := range cfg.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	envs := make([]*hp.Environment, 0, len(names))
	for _, name := range names {
		envs = append(envs, cfg.Environments[name])
	}
	return envs
}

func printDryRun(w *os.File, env *hp.Environment, root string) error {
	plan, err := env.Plan(root)
	if err != nil {
		return err
	}
	for _, p := range env.Providers {
		fmt.Fprintf(w, "# provider %s (%s)\n", p.DisplayName(), p.Kind)
		for _, path := range plan[p.DisplayName()] {
			fmt.Fprintln(w, path)
		}
	}
	return nil
}
