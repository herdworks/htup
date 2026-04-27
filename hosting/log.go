package hosting

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
)

type deployLogContextKey struct{}

type deployLogState struct {
	environment string
	active      bool
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

	ctx = context.WithValue(ctx, deployLogContextKey{}, &deployLogState{environment: environment, active: true})
	logDeploymentState(environment, "starting")

	return ctx, func(err error) {
		if err == nil {
			logDeploymentState(environment, "completed")
			return
		}
		logDeploymentState(environment, "failed")
	}
}

// DeploymentActive reports whether ctx is inside an active deployment log.
func DeploymentActive(ctx context.Context) bool {
	return deployLogActive(ctx)
}

// LogProvider writes one provider-scoped line.
func LogProvider(ctx context.Context, provider *Provider, resource, name, state string) {
	if !deployLogActive(ctx) {
		return
	}
	logDeployf("%s %s %q %s\n", providerLogPrefix(provider), resource, name, state)
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
	logDeployf("%s resource %q type %q %s\n", providerLogPrefix(provider), destination, mimetype, state)
}

func providerLogPrefix(provider *Provider) string {
	if provider == nil {
		return "[provider//]"
	}
	return fmt.Sprintf("[provider/%s/%s]", provider.Kind, provider.DisplayName())
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

func logDeployf(format string, args ...any) {
	deployLogOutput.Lock()
	defer deployLogOutput.Unlock()
	fmt.Fprintf(deployLogOutput.w, format, args...)
}

func logDeploymentState(environment, state string) {
	if environment == "" {
		logDeployf("[deployment] %s\n", state)
		return
	}
	logDeployf("[deployment] environment %q %s\n", environment, state)
}
