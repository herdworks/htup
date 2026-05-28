package hosting

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeginDeployment_UsesEnvironmentName(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	_, finish := BeginDeployment(context.Background(), "production")
	finish(nil)

	want := "[deployment] environment \"production\" starting\n[deployment] environment \"production\" completed\n"
	if got := buf.String(); got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

func TestBeginDeployment_WithoutEnvironmentName(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	_, finish := BeginDeployment(context.Background(), "")
	finish(nil)

	want := "[deployment] starting\n[deployment] completed\n"
	if got := buf.String(); got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

func TestBeginDeployment_NestedDeployDoesNotDuplicateStartOrComplete(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	ctx, finish := BeginDeployment(context.Background(), "production")
	_, nestedFinish := BeginDeployment(ctx, "production")
	nestedFinish(nil)
	finish(nil)

	got := buf.String()
	if count := strings.Count(got, "starting"); count != 1 {
		t.Fatalf("starting line count = %d, want 1 in %q", count, got)
	}
	if count := strings.Count(got, "completed"); count != 1 {
		t.Fatalf("completed line count = %d, want 1 in %q", count, got)
	}
}

func TestBeginDeployment_LogsFailedAfterError(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	_, finish := BeginDeployment(context.Background(), "production")
	finish(context.Canceled)

	got := buf.String()
	if !strings.Contains(got, "failed") {
		t.Fatalf("log output = %q, want failed", got)
	}
}

func TestBeginDeployment_WritesGeneratedLogFile(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	ctx := WithDeploymentRoot(context.Background(), root)
	ctx, finish := BeginDeployment(ctx, "production")
	LogProvider(ctx, &Provider{Kind: "s3"}, "hosting", "assets", "ready")
	finish(nil)

	entries, err := os.ReadDir(filepath.Join(root, ProviderWorkspaceDir, "logs"))
	if err != nil {
		t.Fatalf("ReadDir(logs) error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("log file count = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(root, ProviderWorkspaceDir, "logs", entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	if got := string(data); got != buf.String() {
		t.Fatalf("log file = %q, want stdout copy %q", got, buf.String())
	}
}

func TestProviderLogsQuoteValues(t *testing.T) {
	var buf bytes.Buffer
	restore := setDeployLogOutput(&buf)
	defer restore()

	file := mkFile(t, t.TempDir(), "blog/index.html")
	provider := &Provider{Kind: "s3"}
	ctx, finish := BeginDeployment(context.Background(), "production")
	ctx = WithProviderName(ctx, "assets")
	LogProvider(ctx, provider, "hosting", "assets", "ready")
	LogSecret(ctx, provider, "access_key_id", "found")
	LogFile(ctx, provider, file, "included")
	LogResource(ctx, provider, "https://example.com/blog/index.html", "text/html", "completed")
	finish(nil)

	want := "[deployment] environment \"production\" starting\n" +
		"[provider/s3/assets] hosting \"assets\" ready\n" +
		"[provider/s3/assets] secret \"access_key_id\" found\n" +
		"[provider/s3/assets] file \"blog/index.html\" included\n" +
		"[provider/s3/assets] resource \"https://example.com/blog/index.html\" type \"text/html\" completed\n" +
		"[deployment] environment \"production\" completed\n"
	if got := buf.String(); got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

func setDeployLogOutput(w io.Writer) func() {
	deployLogOutput.Lock()
	previous := deployLogOutput.w
	deployLogOutput.w = w
	deployLogOutput.Unlock()

	return func() {
		deployLogOutput.Lock()
		deployLogOutput.w = previous
		deployLogOutput.Unlock()
	}
}
