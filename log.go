package htup

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type deployLogState struct {
	environment string
	active      bool
	w           io.Writer
	file        *os.File
	path        string
}

type synchronizedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

type bestEffortMirrorWriter struct {
	primary io.Writer
	mirror  io.Writer
}

type deployLogContextKey struct{}
type deploymentRootContextKey struct{}
type providerNameContextKey struct{}

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (w *bestEffortMirrorWriter) Write(p []byte) (int, error) {
	n, err := w.primary.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	if w.mirror != nil {
		_, _ = w.mirror.Write(p)
	}
	return n, nil
}

// BeginDeployment writes the deployment start line and returns the final log
// writer plus a completion callback. Nested deploy calls share the active
// environment without writing duplicate start or end lines.
func BeginDeployment(ctx context.Context, environment string, w io.Writer) (context.Context, io.Writer, func(error)) {
	if ctx == nil {
		ctx = context.Background()
	}
	if w == nil {
		w = os.Stdout
	}
	state, _ := deployLogStateFrom(ctx)
	if state != nil && state.active {
		return ctx, deploymentWriter(ctx, w), func(error) {}
	}

	w, file, path := newDeploymentLogWriter(ctx, w)
	w = &synchronizedWriter{w: w}
	ctx = context.WithValue(ctx, deployLogContextKey{}, &deployLogState{
		environment: environment,
		active:      true,
		w:           w,
		file:        file,
		path:        path,
	})
	logDeploymentState(w, environment, "starting")

	return ctx, w, func(err error) {
		if err == nil {
			logDeploymentState(w, environment, "completed")
			closeDeploymentLogFile(file)
			return
		}
		logDeploymentFailure(w, environment, err)
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
func LogProvider(w io.Writer, ctx context.Context, provider *HostingProvider, resource, name, state string) {
	if !deployLogActive(ctx) {
		return
	}
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(deploymentWriter(ctx, w), "%s %s %q %s\n", providerLogPrefix(ctx, provider), resource, name, state)
}

// LogSecret writes one provider-scoped secret resolution line. It logs only the
// configured secret name, never the secret value.
func LogSecret(w io.Writer, ctx context.Context, provider *HostingProvider, name, state string) {
	LogProvider(w, ctx, provider, "secret", name, state)
}

// LogFile writes one provider-scoped source file decision line.
func LogFile(w io.Writer, ctx context.Context, provider *HostingProvider, file *File, state string) {
	if file == nil {
		return
	}
	LogProvider(w, ctx, provider, "file", file.RelPath(), state)
}

// LogResource writes one provider-scoped destination resource line.
func LogResource(w io.Writer, ctx context.Context, provider *HostingProvider, destination, mimetype, state string) {
	if !deployLogActive(ctx) {
		return
	}
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprintf(deploymentWriter(ctx, w), "%s resource %q type %q %s\n", providerLogPrefix(ctx, provider), destination, mimetype, state)
}

// withProviderName attaches the selected provider name to ctx for provider logs.
func withProviderName(ctx context.Context, name string) context.Context {
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
func ProviderName(ctx context.Context, provider *HostingProvider) string {
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

func providerLogPrefix(ctx context.Context, provider *HostingProvider) string {
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

func logDeploymentState(w io.Writer, environment, state string) {
	if environment == "" {
		fmt.Fprintf(w, "[deployment] %s\n", state)
		return
	}
	fmt.Fprintf(w, "[deployment] environment %q %s\n", environment, state)
}

func logDeploymentFailure(w io.Writer, environment string, err error) {
	state := "failed"
	if reason := errorReason(err); reason != "" {
		state += ": " + reason
	}
	logDeploymentState(w, environment, state)
}

func errorReason(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(err.Error()))
}

func deploymentWriter(ctx context.Context, fallback io.Writer) io.Writer {
	state, ok := deployLogStateFrom(ctx)
	if ok && state.w != nil {
		return state.w
	}
	return fallback
}

func newDeploymentLogWriter(ctx context.Context, base io.Writer) (io.Writer, *os.File, string) {
	root, _ := ctx.Value(deploymentRootContextKey{}).(string)
	if root == "" {
		return base, nil, ""
	}

	file, path, err := createDeploymentLogFile(root)
	if err != nil {
		fmt.Fprintf(base, "[deployment] log file failed: %v\n", err)
		return base, nil, ""
	}
	return &bestEffortMirrorWriter{primary: file, mirror: base}, file, path
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
