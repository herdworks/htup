package hosting

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type deployLogContextKey struct{}
type deploymentRootContextKey struct{}
type providerNameContextKey struct{}

type deployLogState struct {
	environment string
	active      bool
	w           io.Writer
	file        *os.File
	path        string
}

var deployLogOutput = struct {
	sync.Mutex
	w io.Writer
}{
	w: os.Stdout,
}

// BeginDeployment writes the deployment start line and returns a completion
// callback. Nested deploy calls share the active environment without writing
// duplicate start or end lines.
func BeginDeployment(ctx context.Context, environment string) (context.Context, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	state, _ := deployLogStateFrom(ctx)
	if state != nil && state.active {
		return ctx, func(error) {}
	}

	w, file, path := newDeploymentLogWriter(ctx)
	ctx = context.WithValue(ctx, deployLogContextKey{}, &deployLogState{
		environment: environment,
		active:      true,
		w:           w,
		file:        file,
		path:        path,
	})
	logDeploymentState(ctx, environment, "starting")

	return ctx, func(err error) {
		if err == nil {
			logDeploymentState(ctx, environment, "completed")
			closeDeploymentLogFile(file)
			return
		}
		logDeploymentState(ctx, environment, "failed")
		closeDeploymentLogFile(file)
	}
}

// DeploymentActive reports whether ctx is inside an active deployment log.
func DeploymentActive(ctx context.Context) bool {
	return deployLogActive(ctx)
}

// WithDeploymentRoot attaches the deployment root used for generated provider
// workspaces and deployment logs.
func WithDeploymentRoot(ctx context.Context, root string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if root == "" {
		return ctx
	}
	return context.WithValue(ctx, deploymentRootContextKey{}, root)
}

// LogProvider writes one provider-scoped line.
func LogProvider(ctx context.Context, provider *Provider, resource, name, state string) {
	if !deployLogActive(ctx) {
		return
	}
	logDeployf(ctx, "%s %s %q %s\n", providerLogPrefix(ctx, provider), resource, name, state)
}

// LogSecret writes one provider-scoped secret resolution line. It logs only the
// configured secret name, never the secret value.
func LogSecret(ctx context.Context, provider *Provider, name, state string) {
	LogProvider(ctx, provider, "secret", name, state)
}

// LogFile writes one provider-scoped source file decision line.
func LogFile(ctx context.Context, provider *Provider, file *File, state string) {
	if file == nil {
		return
	}
	LogProvider(ctx, provider, "file", file.RelPath(), state)
}

// LogResource writes one provider-scoped destination resource line.
func LogResource(ctx context.Context, provider *Provider, destination, mimetype, state string) {
	if !deployLogActive(ctx) {
		return
	}
	logDeployf(ctx, "%s resource %q type %q %s\n", providerLogPrefix(ctx, provider), destination, mimetype, state)
}

// WithProviderName attaches the selected provider name to ctx for provider logs.
func WithProviderName(ctx context.Context, name string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, providerNameContextKey{}, name)
}

// ProviderName returns the selected provider name from ctx, falling back to the
// provider's direct display name.
func ProviderName(ctx context.Context, provider *Provider) string {
	if ctx != nil {
		if name, ok := ctx.Value(providerNameContextKey{}).(string); ok && name != "" {
			return name
		}
	}
	if provider == nil {
		return ""
	}
	return provider.DisplayName()
}

func providerLogPrefix(ctx context.Context, provider *Provider) string {
	if provider == nil {
		return "[provider//]"
	}
	return fmt.Sprintf("[provider/%s/%s]", provider.Kind, ProviderName(ctx, provider))
}

func deployLogStateFrom(ctx context.Context) (*deployLogState, bool) {
	if ctx == nil {
		return nil, false
	}
	state, ok := ctx.Value(deployLogContextKey{}).(*deployLogState)
	return state, ok
}

func deployLogActive(ctx context.Context) bool {
	state, ok := deployLogStateFrom(ctx)
	return ok && state.active
}

func logDeployf(ctx context.Context, format string, args ...any) {
	deployLogOutput.Lock()
	defer deployLogOutput.Unlock()
	fmt.Fprintf(deployLogWriter(ctx), format, args...)
}

func logDeploymentState(ctx context.Context, environment, state string) {
	if environment == "" {
		logDeployf(ctx, "[deployment] %s\n", state)
		return
	}
	logDeployf(ctx, "[deployment] environment %q %s\n", environment, state)
}

func deployLogWriter(ctx context.Context) io.Writer {
	state, ok := deployLogStateFrom(ctx)
	if ok && state.w != nil {
		return state.w
	}
	return deployLogOutput.w
}

func newDeploymentLogWriter(ctx context.Context) (io.Writer, *os.File, string) {
	base := deployLogOutput.w
	root, _ := ctx.Value(deploymentRootContextKey{}).(string)
	if root == "" {
		return base, nil, ""
	}

	file, path, err := createDeploymentLogFile(root)
	if err != nil {
		fmt.Fprintf(base, "[deployment] log file failed: %v\n", err)
		return base, nil, ""
	}
	return io.MultiWriter(base, file), file, path
}

func createDeploymentLogFile(root string) (*os.File, string, error) {
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, "", err
	}
	logDir := filepath.Join(absRoot, ProviderWorkspaceDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(logDir, time.Now().UTC().Format("20060102T150405.000000000Z")+".log")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, "", err
	}
	return file, path, nil
}

func closeDeploymentLogFile(file *os.File) {
	if file != nil {
		_ = file.Close()
	}
}
