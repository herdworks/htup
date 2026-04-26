// Package git implements a hyperproviders.HostingProviderEngine that publishes
// files to a git remote (HTTPS or SSH). It uses go-git/v5 for pure-Go git
// operations: fetch, hard-reset to origin/<branch>, overwrite working tree,
// stage, commit, optionally tag, push.
//
// The provider is designed for "git as sync mechanism, not collaboration":
// every deploy is a linear commit on top of origin/<branch>. No merges.
package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/herdworks/hyperproviders-go/hosting"
	"github.com/herdworks/hyperproviders-go/session"
)

const (
	Kind              = "git"
	DefaultTagPrefix  = "build-"
	defaultAuthorName = "hyperproviders"
	defaultAuthorMail = "hyperproviders@localhost"
)

func init() {
	hosting.RegisterEngine(Kind, Engine{})
}

// contextKey is the unexported key type for build-id propagation.
type contextKey struct{ name string }

// BuildIDKey is the context key GitProvider reads for the build ID used in
// commit messages and tag names. When absent, a UTC timestamp is substituted.
var BuildIDKey = contextKey{"hyperproviders.build_id"}

// WithBuildID returns a copy of ctx carrying buildID.
func WithBuildID(ctx context.Context, buildID string) context.Context {
	return context.WithValue(ctx, BuildIDKey, buildID)
}

func buildIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(BuildIDKey).(string); ok && v != "" {
		return v
	}
	return time.Now().UTC().Format("20060102T150405Z")
}

// Engine deploys files to a git remote.
type Engine struct{}

type settings struct {
	RemoteURL   string
	Branch      string
	AuthorName  string
	AuthorEmail string

	TagEnabled    bool
	TagPrefix     string
	TagRetainLast int

	CacheDir string
	Fresh    bool
}

type providerConfig struct {
	Branch      string `json:"branch" yaml:"branch"`
	AuthorName  string `json:"author_name,omitempty" yaml:"author_name,omitempty"`
	AuthorEmail string `json:"author_email,omitempty" yaml:"author_email,omitempty"`

	TagEnabled    bool   `json:"tag_enabled,omitempty" yaml:"tag_enabled,omitempty"`
	TagPrefix     string `json:"tag_prefix,omitempty" yaml:"tag_prefix,omitempty"`
	TagRetainLast int    `json:"tag_retain_last,omitempty" yaml:"tag_retain_last,omitempty"`

	CacheDir string `json:"cache_dir,omitempty" yaml:"cache_dir,omitempty"`
	Fresh    bool   `json:"fresh,omitempty" yaml:"fresh,omitempty"`
}

func newSettings(provider *hosting.Provider) (*settings, error) {
	cfg := &settings{}
	if len(provider.Config) > 0 {
		var decoded providerConfig
		if err := provider.DecodeConfig(&decoded); err != nil {
			return nil, fmt.Errorf("git: %w", err)
		}
		cfg = &settings{
			Branch:        decoded.Branch,
			AuthorName:    decoded.AuthorName,
			AuthorEmail:   decoded.AuthorEmail,
			TagEnabled:    decoded.TagEnabled,
			TagPrefix:     decoded.TagPrefix,
			TagRetainLast: decoded.TagRetainLast,
			CacheDir:      decoded.CacheDir,
			Fresh:         decoded.Fresh,
		}
	}
	if cfg.RemoteURL == "" {
		cfg.RemoteURL = provider.Endpoint
	}
	if cfg.TagPrefix == "" {
		cfg.TagPrefix = DefaultTagPrefix
	}
	return cfg, nil
}

// Validate validates Git-specific configuration.
func (Engine) Validate(provider *hosting.Provider) error {
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	if cfg.RemoteURL == "" {
		return errors.New("git: endpoint is empty")
	}
	if cfg.Branch == "" {
		return errors.New("git: branch is empty")
	}
	return nil
}

// Deploy executes the full flow: open-or-clone, fetch, reset, overwrite working
// tree, stage, commit, optionally tag, push, and optionally prune tags.
func (engine Engine) Deploy(ctx context.Context, runtime *session.Session, provider *hosting.Provider, files []*hosting.File) error {
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	auth, err := resolveAuth(ctx, runtime, cfg.RemoteURL, provider.Secrets)
	if err != nil {
		return fmt.Errorf("git: auth: %w", err)
	}

	workdir, err := cfg.workdir()
	if err != nil {
		return fmt.Errorf("git: workdir: %w", err)
	}
	if cfg.Fresh {
		if err := os.RemoveAll(workdir); err != nil {
			return fmt.Errorf("git: clear workdir: %w", err)
		}
	}

	repo, wt, firstDeploy, err := openOrClone(ctx, workdir, cfg.RemoteURL, cfg.Branch, auth)
	if err != nil {
		return fmt.Errorf("git: open repo: %w", err)
	}

	if !firstDeploy {
		if err := resetToOrigin(ctx, repo, wt, cfg.Branch, auth); err != nil {
			return fmt.Errorf("git: reset to origin: %w", err)
		}
	}

	if err := clearWorktree(wt.Filesystem); err != nil {
		return fmt.Errorf("git: clear worktree: %w", err)
	}
	if err := copyFiles(wt.Filesystem, files); err != nil {
		return fmt.Errorf("git: write files: %w", err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("git: stage: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("git: status: %w", err)
	}
	if status.IsClean() {
		return nil
	}

	buildID := buildIDFrom(ctx)
	sig := cfg.signature()
	commitHash, err := wt.Commit(cfg.commitMessage(buildID), &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return fmt.Errorf("git: commit: %w", err)
	}

	var tagRef string
	if cfg.TagEnabled {
		tagRef = cfg.TagPrefix + buildID
		if _, err := repo.CreateTag(tagRef, commitHash, &git.CreateTagOptions{
			Tagger:  sig,
			Message: "hyperproviders deploy " + buildID,
		}); err != nil {
			return fmt.Errorf("git: tag %s: %w", tagRef, err)
		}
	}

	pushOpts := &git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   pushRefSpecs(cfg.Branch, firstDeploy),
	}
	if tagRef != "" {
		pushOpts.RefSpecs = append(pushOpts.RefSpecs, gogitconfig.RefSpec(fmt.Sprintf("refs/tags/%s:refs/tags/%s", tagRef, tagRef)))
	}
	if err := repo.PushContext(ctx, pushOpts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("git: push: %w", err)
	}

	if cfg.TagEnabled && cfg.TagRetainLast > 0 {
		if err := cfg.pruneTags(ctx, repo, auth); err != nil {
			return fmt.Errorf("git: prune tags: %w", err)
		}
	}

	return nil
}

// workdir returns the absolute cache path for this remote+branch. When
// CacheDir is unset we hash the remote+branch under the OS user cache dir so
// deploys to different targets don't collide.
func (cfg *settings) workdir() (string, error) {
	if cfg.CacheDir != "" {
		return filepath.Abs(cfg.CacheDir)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(cfg.RemoteURL + "\x00" + cfg.Branch))
	return filepath.Join(base, "hyperproviders", "git", hex.EncodeToString(key[:16])), nil
}

func (cfg *settings) signature() *object.Signature {
	name := cfg.AuthorName
	if name == "" {
		name = defaultAuthorName
	}
	email := cfg.AuthorEmail
	if email == "" {
		email = defaultAuthorMail
	}
	return &object.Signature{Name: name, Email: email, When: time.Now()}
}

func (cfg *settings) commitMessage(buildID string) string {
	return fmt.Sprintf("deploy %s", buildID)
}

// pushRefSpecs returns the RefSpecs for pushing the current HEAD of the local
// branch to origin. The same RefSpec works for first-deploy and subsequent
// deploys; go-git creates the remote ref when it does not exist.
func pushRefSpecs(branch string, firstDeploy bool) []gogitconfig.RefSpec {
	local := plumbing.NewBranchReferenceName(branch)
	_ = firstDeploy
	return []gogitconfig.RefSpec{gogitconfig.RefSpec(fmt.Sprintf("%s:%s", local, local))}
}

// pruneTags deletes local and remote tags so only TagRetainLast tags remain
// for the configured TagPrefix. Tag ordering is lexicographic, which is fine
// for the default timestamp-like build IDs.
func (cfg *settings) pruneTags(ctx context.Context, repo *git.Repository, auth transport.AuthMethod) error {
	iter, err := repo.Tags()
	if err != nil {
		return err
	}
	var matching []string
	if err := iter.ForEach(func(ref *plumbing.Reference) error {
		short := ref.Name().Short()
		if len(cfg.TagPrefix) == 0 || (len(short) >= len(cfg.TagPrefix) && short[:len(cfg.TagPrefix)] == cfg.TagPrefix) {
			matching = append(matching, short)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(matching) <= cfg.TagRetainLast {
		return nil
	}
	sort.Strings(matching)
	drop := matching[:len(matching)-cfg.TagRetainLast]
	var refSpecs []gogitconfig.RefSpec
	for _, name := range drop {
		if err := repo.DeleteTag(name); err != nil && !errors.Is(err, git.ErrTagNotFound) {
			return fmt.Errorf("delete local tag %s: %w", name, err)
		}
		refSpecs = append(refSpecs, gogitconfig.RefSpec(fmt.Sprintf(":refs/tags/%s", name)))
	}
	if len(refSpecs) == 0 {
		return nil
	}
	return repo.PushContext(ctx, &git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   refSpecs,
	})
}
