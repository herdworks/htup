package htup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
)

// Deployment is the sole high-level entry point for a selected provider set.
type Deployment struct {
	ConfigPath   string
	ConfigBytes  []byte
	PayloadPath  string
	Providers    HostingProviders
	Environment  string
	Kind         DeploymentKind
	instructions DeploymentInstructions
	plan         DeploymentPlan
	status       DeploymentStatus
	mu           sync.Mutex
}

// DeploymentPlan separates operations that must observe conditional ordering
// from ordinary independent provider operations.
type DeploymentPlan struct {
	Sequential []DeploymentOperation
	Concurrent []DeploymentOperation
}

// DeploymentOperation is the complete provider-native work for one provider.
type DeploymentOperation struct {
	HostingProvider *HostingProvider
	Root            string
	Kind            DeploymentKind
	Preconditions   []PreconditionFile
	Bulk            []*File
	Enqueued        []*File
}

type DeploymentKind string

const (
	CompleteDeployment    DeploymentKind = "complete"
	IncrementalDeployment DeploymentKind = "incremental"
)

type DeploymentStatus string

const (
	DeploymentPending   DeploymentStatus = "pending"
	DeploymentPlanning  DeploymentStatus = "planning"
	DeploymentReady     DeploymentStatus = "ready"
	DeploymentExecuting DeploymentStatus = "executing"
	DeploymentSucceeded DeploymentStatus = "succeeded"
	DeploymentFailed    DeploymentStatus = "failed"
)

func (kind DeploymentKind) Validate() error {
	switch kind {
	case CompleteDeployment, IncrementalDeployment:
		return nil
	case "":
		return errors.New("deployment kind is required")
	default:
		return fmt.Errorf("unknown deployment kind %q", kind)
	}
}

// Prepare validates and materializes an immutable deployment plan without
// provider readiness checks or remote writes.
func (d *Deployment) Prepare(ctx context.Context, w io.Writer) error {
	d.mu.Lock()
	if d.status == "" {
		d.status = DeploymentPending
	}
	if d.status != DeploymentPending {
		status := d.status
		d.mu.Unlock()
		return fmt.Errorf("deployment cannot be prepared from status %q", status)
	}
	d.status = DeploymentPlanning
	d.mu.Unlock()
	instructions, plan, err := d.generatePlan(ctx, w)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.status = DeploymentFailed
		return err
	}
	d.instructions = copyInstructions(instructions)
	d.plan = copyPlan(plan)
	d.status = DeploymentReady
	return nil
}

// Execute performs one prepared deployment. It checks every provider's local
// readiness before the first provider operation can write remotely.
func (d *Deployment) Execute(ctx context.Context, secrets SecretsProviders, w io.Writer) (err error) {
	ctx = WithDeploymentRoot(ctx, d.PayloadPath)
	if !DeploymentActive(ctx) {
		var finish func(error)
		ctx, w, finish = BeginDeployment(ctx, "", w)
		defer func() { finish(err) }()
	}
	if d.Status() == DeploymentPending {
		if err := d.Prepare(ctx, w); err != nil {
			return err
		}
	}
	d.mu.Lock()
	if d.status != DeploymentReady {
		status := d.status
		d.mu.Unlock()
		return fmt.Errorf("deployment cannot be executed from status %q", status)
	}
	plan := copyPlan(d.plan)
	d.mu.Unlock()
	if w == nil {
		w = os.Stdout
	}
	for _, op := range append(append([]DeploymentOperation{}, plan.Sequential...), plan.Concurrent...) {
		opCtx := withProviderName(ctx, providerName(d.Providers, op.HostingProvider))
		op.HostingProvider.logSecrets(opCtx, secrets, w)
		if err := op.HostingProvider.Ready(opCtx, secrets); err != nil {
			d.fail()
			return fmt.Errorf("provider %q readiness: %w", providerName(d.Providers, op.HostingProvider), err)
		}
		LogProvider(w, opCtx, op.HostingProvider, "hosting", providerName(d.Providers, op.HostingProvider), "ready")
	}
	d.mu.Lock()
	d.status = DeploymentExecuting
	d.mu.Unlock()
	for _, op := range plan.Sequential {
		opCtx := withProviderName(ctx, providerName(d.Providers, op.HostingProvider))
		if err := op.HostingProvider.PerformOperation(opCtx, secrets, op, w); err != nil {
			d.fail()
			return fmt.Errorf("provider %q: %w", providerName(d.Providers, op.HostingProvider), err)
		}
	}
	var wg sync.WaitGroup
	var lock sync.Mutex
	var errs []error
	for _, op := range plan.Concurrent {
		op := op
		wg.Add(1)
		go func() {
			defer wg.Done()
			opCtx := withProviderName(ctx, providerName(d.Providers, op.HostingProvider))
			if err := op.HostingProvider.PerformOperation(opCtx, secrets, op, w); err != nil {
				lock.Lock()
				errs = append(errs, fmt.Errorf("provider %q: %w", providerName(d.Providers, op.HostingProvider), err))
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		d.fail()
		return err
	}
	d.mu.Lock()
	d.status = DeploymentSucceeded
	d.mu.Unlock()
	return nil
}

func (d *Deployment) Status() DeploymentStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.status == "" {
		return DeploymentPending
	}
	return d.status
}

// Instructions returns a copy of the declarations loaded during the successful
// preparation. It is empty until the deployment is ready or has executed.
func (d *Deployment) Instructions() DeploymentInstructions {
	d.mu.Lock()
	defer d.mu.Unlock()
	return copyInstructions(d.instructions)
}

// Plan returns a copy of the operation plan materialized during the successful
// preparation. It is empty until the deployment is ready or has executed.
func (d *Deployment) Plan() DeploymentPlan {
	d.mu.Lock()
	defer d.mu.Unlock()
	return copyPlan(d.plan)
}

// generatePlan loads instructions once, walks once, materializes source
// bytes once, and validates every provider operation before returning a plan.
func (d *Deployment) generatePlan(ctx context.Context, w io.Writer) (DeploymentInstructions, DeploymentPlan, error) {
	d.mu.Lock()
	configPath, configBytes, payloadPath, providers, environment, kind := d.ConfigPath, append([]byte(nil), d.ConfigBytes...), d.PayloadPath, d.Providers, d.Environment, d.Kind
	d.mu.Unlock()
	var err error
	if err := kind.Validate(); err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	if err := ValidateDeploymentConfigPath(configPath); err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	if err := ValidateDeploymentPayloadPath(configPath, payloadPath); err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	if configBytes == nil {
		configBytes, err = ReadDeploymentConfig(configPath)
		if err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, err
		}
	}
	config, err := ParseDeploymentConfig(configPath, configBytes)
	if err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	if len(providers) != 0 && environment != "" {
		return DeploymentInstructions{}, DeploymentPlan{}, errors.New("deployment providers and environment are mutually exclusive")
	}
	if len(providers) == 0 {
		providers, err = config.ProvidersForEnvironment(environment)
		if err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, err
		}
	}
	instructions, err := LoadDeploymentInstructions(payloadPath)
	if err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	ignore, err := deploymentIgnore(payloadPath, instructions)
	if err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	files, err := walk(payloadPath, ignore)
	if err != nil {
		return DeploymentInstructions{}, DeploymentPlan{}, err
	}
	type materialized struct {
		index int
		file  PreconditionFile
		mime  string
	}
	conditions := make([]materialized, 0, len(instructions.Preconditions))
	for i, condition := range instructions.Preconditions {
		content, err := instructionBytes(payloadPath, condition.Contents)
		if err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("precondition contents %q: %w", condition.Contents, err)
		}
		mime := MimetypeForPath(condition.Target)
		var expected []byte
		if condition.Expected != "" {
			expected, err = instructionBytes(payloadPath, condition.Expected)
			if err != nil {
				return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("precondition expected %q: %w", condition.Expected, err)
			}
		}
		conditions = append(conditions, materialized{i, PreconditionFile{Target: condition.Target, Expected: expected, Content: content}, mime})
	}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	owners := make([]int, len(conditions))
	type planned struct {
		name  string
		op    DeploymentOperation
		first int
	}
	ordered := make([]planned, 0, len(names))
	for _, name := range names {
		provider := providers[name]
		if provider == nil {
			return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("provider %q is empty", name)
		}
		if err := provider.Validate(); err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("provider %q: %w", name, err)
		}
		filter, err := provider.filter().compile()
		if err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, err
		}
		op := DeploymentOperation{HostingProvider: provider, Root: payloadPath, Kind: kind}
		first := -1
		for _, condition := range conditions {
			accepted, err := filter.MatchResource(condition.file.Target, condition.mime)
			if err != nil {
				return DeploymentInstructions{}, DeploymentPlan{}, err
			}
			if !accepted {
				LogProvider(w, ctx, provider, "precondition", condition.file.Target, "skipped-filtered")
				continue
			}
			owners[condition.index]++
			if first < 0 {
				first = condition.index
			}
			op.Preconditions = append(op.Preconditions, copyPrecondition(condition.file))
		}
		selected, err := provider.deploymentFiles(ctx, w, files)
		if err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, err
		}
		claimed := make(map[*File]bool, len(selected))
		for _, pattern := range instructions.Enqueue {
			for _, file := range selected {
				if claimed[file] {
					continue
				}
				matched, err := path.Match(pattern, file.RelPath())
				if err != nil {
					return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("enqueue %q: %w", pattern, err)
				}
				if matched {
					claimed[file] = true
					op.Enqueued = append(op.Enqueued, file)
				}
			}
		}
		for _, file := range selected {
			if !claimed[file] {
				op.Bulk = append(op.Bulk, file)
			}
		}
		if err := provider.ValidateOperation(op); err != nil {
			return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("provider %q operation: %w", name, err)
		}
		ordered = append(ordered, planned{name, op, first})
	}
	for i, count := range owners {
		if count == 0 {
			return DeploymentInstructions{}, DeploymentPlan{}, fmt.Errorf("precondition %q has no selected provider owner", conditions[i].file.Target)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.first < 0 && b.first < 0 {
			return a.name < b.name
		}
		if a.first < 0 {
			return false
		}
		if b.first < 0 {
			return true
		}
		if a.first == b.first {
			return a.name < b.name
		}
		return a.first < b.first
	})
	var plan DeploymentPlan
	for _, item := range ordered {
		if item.first >= 0 {
			plan.Sequential = append(plan.Sequential, item.op)
		} else {
			plan.Concurrent = append(plan.Concurrent, item.op)
		}
	}
	return instructions, plan, nil
}

func (d *Deployment) fail() { d.mu.Lock(); d.status = DeploymentFailed; d.mu.Unlock() }

func instructionBytes(root, rel string) ([]byte, error) {
	if err := deploymentRelative(rel); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	if _, err := newFile(root, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

func copyPrecondition(in PreconditionFile) PreconditionFile {
	return PreconditionFile{Target: in.Target, Expected: append([]byte(nil), in.Expected...), Content: append([]byte(nil), in.Content...)}
}

func copyInstructions(in DeploymentInstructions) DeploymentInstructions {
	out := DeploymentInstructions{
		Preconditions: make([]Precondition, len(in.Preconditions)),
		Ignore:        append([]string(nil), in.Ignore...),
		Enqueue:       append([]string(nil), in.Enqueue...),
	}
	copy(out.Preconditions, in.Preconditions)
	return out
}

func copyPlan(in DeploymentPlan) DeploymentPlan {
	out := DeploymentPlan{Sequential: make([]DeploymentOperation, len(in.Sequential)), Concurrent: make([]DeploymentOperation, len(in.Concurrent))}
	for i, op := range in.Sequential {
		out.Sequential[i] = copyOperation(op)
	}
	for i, op := range in.Concurrent {
		out.Concurrent[i] = copyOperation(op)
	}
	return out
}

func copyOperation(in DeploymentOperation) DeploymentOperation {
	out := in
	out.Preconditions = make([]PreconditionFile, len(in.Preconditions))
	for i, p := range in.Preconditions {
		out.Preconditions[i] = PreconditionFile{Target: p.Target, Expected: append([]byte(nil), p.Expected...), Content: append([]byte(nil), p.Content...)}
	}
	out.Bulk = append([]*File(nil), in.Bulk...)
	out.Enqueued = append([]*File(nil), in.Enqueued...)
	return out
}

func providerName(providers HostingProviders, wanted *HostingProvider) string {
	for name, provider := range providers {
		if provider == wanted {
			return name
		}
	}
	return wanted.DisplayName()
}
func RegisterEngine(kind string, engine HostingEngine) { registerEngine(kind, engine) }
