package htup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func TestBeginDeployment_UsesEnvironmentName(t *testing.T) {
	var buf bytes.Buffer

	_, _, finish := BeginDeployment(context.Background(), "production", &buf)
	finish(nil)

	want := "[deployment] environment \"production\" starting\n[deployment] environment \"production\" completed\n"
	if got := buf.String(); got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

func TestBeginDeployment_WithoutEnvironmentName(t *testing.T) {
	var buf bytes.Buffer

	_, _, finish := BeginDeployment(context.Background(), "", &buf)
	finish(nil)

	want := "[deployment] starting\n[deployment] completed\n"
	if got := buf.String(); got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
}

func TestBeginDeployment_NestedDeployDoesNotDuplicateStartOrComplete(t *testing.T) {
	var buf bytes.Buffer

	ctx, w, finish := BeginDeployment(context.Background(), "production", &buf)
	_, _, nestedFinish := BeginDeployment(ctx, "production", w)
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

	_, _, finish := BeginDeployment(context.Background(), "production", &buf)
	finish(context.Canceled)

	got := buf.String()
	if !strings.Contains(got, "failed") {
		t.Fatalf("log output = %q, want failed", got)
	}
}

func TestBeginDeployment_WritesGeneratedLogFile(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer

	ctx := WithDeploymentRoot(context.Background(), root)
	ctx, w, finish := BeginDeployment(ctx, "production", &buf)
	LogProvider(w, ctx, &HostingProvider{Kind: "s3"}, "hosting", "assets", "ready")
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

func TestBeginDeployment_GeneratedLogSurvivesMirrorFailure(t *testing.T) {
	root := t.TempDir()
	ctx := WithDeploymentRoot(context.Background(), root)
	ctx, w, finish := BeginDeployment(ctx, "production", failingWriter{err: errors.New("stdout closed")})
	LogProvider(w, ctx, &HostingProvider{Kind: "s3"}, "hosting", "assets", "ready")
	finish(errors.New("upload failed\nwith details"))

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
	for _, want := range []string{
		`[deployment] environment "production" starting`,
		`[provider/s3/s3] hosting "assets" ready`,
		`[deployment] environment "production" failed: upload failed with details`,
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("log file missing %q in:\n%s", want, data)
		}
	}
}

func TestBestEffortMirrorWriter_PropagatesPrimaryFailure(t *testing.T) {
	wantErr := errors.New("disk full")
	w := &bestEffortMirrorWriter{
		primary: failingWriter{err: wantErr},
		mirror:  &bytes.Buffer{},
	}

	if _, err := w.Write([]byte("log line\n")); !errors.Is(err, wantErr) {
		t.Fatalf("Write() error = %v, want %v", err, wantErr)
	}
}

func TestProviderLogsQuoteValues(t *testing.T) {
	var buf bytes.Buffer

	file := mkFile(t, t.TempDir(), "blog/index.html")
	provider := &HostingProvider{Kind: "s3"}
	ctx, w, finish := BeginDeployment(context.Background(), "production", &buf)
	ctx = withProviderName(ctx, "assets")
	LogProvider(w, ctx, provider, "hosting", "assets", "ready")
	LogSecret(w, ctx, provider, "access_key_id", "found")
	LogFile(w, ctx, provider, file, "included")
	LogResource(w, ctx, provider, "https://example.com/blog/index.html", "text/html", "completed")
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
