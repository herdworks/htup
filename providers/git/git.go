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
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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

// deployLocks serializes same-remote/same-branch deploys in this process so
// concurrent provider fan-out does not create non-fast-forward pushes.
var deployLocks sync.Map

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
	KnownHosts  string

	TagEnabled    bool
	TagPrefix     string
	TagRetainLast int

	PublishDir string
}

type providerConfig struct {
	Branch      string `json:"branch" yaml:"branch"`
	AuthorName  string `json:"author_name,omitempty" yaml:"author_name,omitempty"`
	AuthorEmail string `json:"author_email,omitempty" yaml:"author_email,omitempty"`
	KnownHosts  string `json:"known_hosts,omitempty" yaml:"known_hosts,omitempty"`

	TagEnabled    bool   `json:"tag_enabled,omitempty" yaml:"tag_enabled,omitempty"`
	TagPrefix     string `json:"tag_prefix,omitempty" yaml:"tag_prefix,omitempty"`
	TagRetainLast int    `json:"tag_retain_last,omitempty" yaml:"tag_retain_last,omitempty"`

	PublishDir string `json:"publish_dir,omitempty" yaml:"publish_dir,omitempty"`
}

func newSettings(provider *hosting.Provider) (*settings, error) {
	cfg := &settings{}
	if len(provider.Config) > 0 {
		if _, ok := provider.Config["cache_dir"]; ok {
			return nil, errors.New("git: cache_dir is not supported")
		}
		if _, ok := provider.Config["fresh"]; ok {
			return nil, errors.New("git: fresh is not supported")
		}

		var decoded providerConfig
		if err := provider.DecodeConfig(&decoded); err != nil {
			return nil, fmt.Errorf("git: %w", err)
		}
		publishDir, err := normalizePublishDir(decoded.PublishDir)
		if err != nil {
			return nil, err
		}
		cfg = &settings{
			Branch:        decoded.Branch,
			AuthorName:    decoded.AuthorName,
			AuthorEmail:   decoded.AuthorEmail,
			KnownHosts:    decoded.KnownHosts,
			TagEnabled:    decoded.TagEnabled,
			TagPrefix:     decoded.TagPrefix,
			TagRetainLast: decoded.TagRetainLast,
			PublishDir:    publishDir,
		}
	}
	if cfg.RemoteURL == "" {
		cfg.RemoteURL = provider.Endpoint
	}
	if cfg.TagPrefix == "" {
		cfg.TagPrefix = DefaultTagPrefix
	}
	if cfg.PublishDir == "" {
		cfg.PublishDir = "."
	}
	return cfg, nil
}

func normalizePublishDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ".", nil
	}
	slashDir := filepath.ToSlash(dir)
	if filepath.IsAbs(dir) || strings.HasPrefix(slashDir, "/") {
		return "", fmt.Errorf("git: publish_dir must be relative: %s", dir)
	}

	clean := path.Clean(slashDir)
	if clean == "." {
		return ".", nil
	}
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return "", fmt.Errorf("git: publish_dir cannot escape the repository root: %s", dir)
		}
		if part == ".git" {
			return "", fmt.Errorf("git: publish_dir cannot target .git: %s", dir)
		}
	}
	return clean, nil
}

// Validate validates Git-specific configuration.
func (Engine) Validate(provider *hosting.Provider) error {
	_, err := validateSettings(provider)
	return err
}

// Ready validates Git configuration and confirms local auth material can be
// resolved and parsed. It intentionally does not contact the remote.
func (Engine) Ready(ctx context.Context, runtime *session.Session, provider *hosting.Provider) error {
	if runtime == nil {
		runtime = &session.Session{}
	}
	cfg, err := validateSettings(provider)
	if err != nil {
		return err
	}
	_, err = resolveAuth(ctx, runtime, cfg, provider.Secrets)
	return err
}

func validateSettings(provider *hosting.Provider) (*settings, error) {
	cfg, err := newSettings(provider)
	if err != nil {
		return nil, err
	}
	if cfg.RemoteURL == "" {
		return nil, errors.New("git: endpoint is empty")
	}
	if cfg.Branch == "" {
		return nil, errors.New("git: branch is empty")
	}
	return cfg, nil
}

// Deploy executes the full flow: open-or-clone, fetch, reset, apply the
// deployment mode, stage, commit, optionally tag, push, and optionally prune tags.
// Replacement mode clears publish_dir first; overlay mode writes and stages only
// the provided files.
func (engine Engine) Deploy(ctx context.Context, runtime *session.Session, provider *hosting.Provider, root string, files []*hosting.File, mode hosting.DeploymentMode) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	cfg, err := newSettings(provider)
	if err != nil {
		return err
	}
	auth, err := resolveAuth(ctx, runtime, cfg, provider.Secrets)
	if err != nil {
		return fmt.Errorf("git: auth: %w", err)
	}

	lock := deployLock(cfg.RemoteURL, cfg.Branch)
	lock.Lock()
	defer lock.Unlock()

	workdir, err := cfg.workdir(root, hosting.ProviderName(ctx, provider))
	if err != nil {
		return fmt.Errorf("git: workdir: %w", err)
	}

	remoteBranch := "origin/" + cfg.Branch
	remoteResource := cfg.RemoteURL + "#" + cfg.Branch
	hosting.LogProvider(ctx, provider, "open", remoteResource, "starting")
	repo, wt, firstDeploy, err := openOrClone(ctx, workdir, cfg.RemoteURL, cfg.Branch, auth)
	if err != nil {
		hosting.LogProvider(ctx, provider, "open", remoteResource, "failed")
		return fmt.Errorf("git: open repo: %w", err)
	}
	hosting.LogProvider(ctx, provider, "open", remoteResource, "completed")
	if firstDeploy && mode == hosting.DeploymentModeOverlay {
		_ = os.RemoveAll(workdir)
		return overlayMissingBranchError(cfg.Branch)
	}

	if !firstDeploy {
		hosting.LogProvider(ctx, provider, "fetch", remoteBranch, "starting")
		if err := fetchOrigin(ctx, repo, auth); err != nil {
			hosting.LogProvider(ctx, provider, "fetch", remoteBranch, "failed")
			if mode == hosting.DeploymentModeOverlay && isBranchMissing(err) {
				return overlayMissingBranchError(cfg.Branch)
			}
			return fmt.Errorf("git: fetch origin: %w", err)
		}
		hosting.LogProvider(ctx, provider, "fetch", remoteBranch, "completed")

		hosting.LogProvider(ctx, provider, "reset", remoteBranch, "starting")
		if err := resetToOrigin(repo, wt, cfg.Branch); err != nil {
			hosting.LogProvider(ctx, provider, "reset", remoteBranch, "failed")
			if mode == hosting.DeploymentModeOverlay && isBranchMissing(err) {
				return overlayMissingBranchError(cfg.Branch)
			}
			return fmt.Errorf("git: reset to origin: %w", err)
		}
		hosting.LogProvider(ctx, provider, "reset", remoteBranch, "completed")
	} else {
		hosting.LogProvider(ctx, provider, "fetch", remoteBranch, "skipped")
		hosting.LogProvider(ctx, provider, "reset", remoteBranch, "skipped")
	}

	switch mode {
	case hosting.DeploymentModeReplacement:
		if err := clearPublishDir(wt.Filesystem, cfg.PublishDir); err != nil {
			return fmt.Errorf("git: clear publish dir: %w", err)
		}
		if err := copyFiles(wt.Filesystem, cfg.PublishDir, files); err != nil {
			return fmt.Errorf("git: write files: %w", err)
		}
		if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			return fmt.Errorf("git: stage: %w", err)
		}
	case hosting.DeploymentModeOverlay:
		if err := copyFiles(wt.Filesystem, cfg.PublishDir, files); err != nil {
			return fmt.Errorf("git: write files: %w", err)
		}
		if err := stageFiles(wt, cfg.PublishDir, files); err != nil {
			return fmt.Errorf("git: stage: %w", err)
		}
	}
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("git: status: %w", err)
	}
	if err := logResourceChanges(ctx, provider, cfg.PublishDir, files, status); err != nil {
		return err
	}
	if status.IsClean() {
		hosting.LogProvider(ctx, provider, "commit", cfg.Branch, "skipped")
		hosting.LogProvider(ctx, provider, "push", remoteBranch, "skipped")
		return nil
	}

	buildID := buildIDFrom(ctx)
	sig := cfg.signature()
	hosting.LogProvider(ctx, provider, "commit", cfg.Branch, "starting")
	commitHash, err := wt.Commit(cfg.commitMessage(buildID), &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		hosting.LogProvider(ctx, provider, "commit", cfg.Branch, "failed")
		return fmt.Errorf("git: commit: %w", err)
	}
	hosting.LogProvider(ctx, provider, "commit", shortHash(commitHash.String()), "completed")

	var tagRef string
	if cfg.TagEnabled {
		tagRef = cfg.TagPrefix + buildID
		hosting.LogProvider(ctx, provider, "tag", tagRef, "starting")
		if _, err := repo.CreateTag(tagRef, commitHash, &git.CreateTagOptions{
			Tagger:  sig,
			Message: "hyperproviders deploy " + buildID,
		}); err != nil {
			hosting.LogProvider(ctx, provider, "tag", tagRef, "failed")
			return fmt.Errorf("git: tag %s: %w", tagRef, err)
		}
		hosting.LogProvider(ctx, provider, "tag", tagRef, "completed")
	}

	pushOpts := &git.PushOptions{
		RemoteName: "origin",
		Auth:       auth,
		RefSpecs:   pushRefSpecs(cfg.Branch),
	}
	if tagRef != "" {
		pushOpts.RefSpecs = append(pushOpts.RefSpecs, gogitconfig.RefSpec(fmt.Sprintf("refs/tags/%s:refs/tags/%s", tagRef, tagRef)))
	}
	hosting.LogProvider(ctx, provider, "push", remoteBranch, "starting")
	if err := repo.PushContext(ctx, pushOpts); err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			hosting.LogProvider(ctx, provider, "push", remoteBranch, "skipped")
		} else {
			hosting.LogProvider(ctx, provider, "push", remoteBranch, "failed")
			return fmt.Errorf("git: push: %w", err)
		}
	} else {
		hosting.LogProvider(ctx, provider, "push", remoteBranch, "completed")
	}

	if cfg.TagEnabled && cfg.TagRetainLast > 0 {
		hosting.LogProvider(ctx, provider, "prune", cfg.TagPrefix, "starting")
		if err := cfg.pruneTags(ctx, repo, auth); err != nil {
			hosting.LogProvider(ctx, provider, "prune", cfg.TagPrefix, "failed")
			return fmt.Errorf("git: prune tags: %w", err)
		}
		hosting.LogProvider(ctx, provider, "prune", cfg.TagPrefix, "completed")
	}

	return nil
}

// workdir returns the provider workspace path for the local clone. The
// workspace lives under the deployment root's reserved ProviderWorkspaceDir so
// callers don't need to configure platform-specific cache paths.
func (cfg *settings) workdir(root, providerName string) (string, error) {
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	key := sha256.Sum256([]byte(strings.Join([]string{
		cfg.RemoteURL,
		cfg.Branch,
		cfg.PublishDir,
		providerName,
	}, "\x00")))
	return filepath.Join(absRoot, hosting.ProviderWorkspaceDir, Kind, hex.EncodeToString(key[:16])), nil
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

func logResourceChanges(ctx context.Context, provider *hosting.Provider, publishDir string, files []*hosting.File, status git.Status) error {
	included := make(map[string]*hosting.File, len(files))
	for _, file := range files {
		included[repoPath(publishDir, file.RelPath())] = file
	}

	for _, file := range files {
		mimetype, err := file.Mimetype()
		if err != nil {
			return fmt.Errorf("git: mimetype %s: %w", file.RelPath(), err)
		}
		repoRel := repoPath(publishDir, file.RelPath())
		hosting.LogResource(ctx, provider, provider.DestinationURL(file.RelPath()), mimetype, gitResourceAction(status[repoRel]))
	}

	var deleted []string
	for repoRel, fileStatus := range status {
		if _, ok := included[repoRel]; ok {
			continue
		}
		if gitResourceAction(fileStatus) != "delete" {
			continue
		}
		publicRel, ok := publicRelFromRepoPath(publishDir, repoRel)
		if ok {
			deleted = append(deleted, publicRel)
		}
	}
	sort.Strings(deleted)
	for _, rel := range deleted {
		hosting.LogResource(ctx, provider, provider.DestinationURL(rel), hosting.MimetypeForPath(rel), "delete")
	}
	return nil
}

func gitResourceAction(status *git.FileStatus) string {
	if status == nil {
		return "skip"
	}
	code := status.Staging
	if code == git.Unmodified {
		code = status.Worktree
	}
	switch code {
	case git.Added, git.Untracked:
		return "create"
	case git.Modified, git.Renamed, git.Copied, git.UpdatedButUnmerged:
		return "update"
	case git.Deleted:
		return "delete"
	default:
		return "skip"
	}
}

func publicRelFromRepoPath(publishDir, repoRel string) (string, bool) {
	if publishDir == "." {
		return repoRel, true
	}
	prefix := publishDir + "/"
	if !strings.HasPrefix(repoRel, prefix) {
		return "", false
	}
	return strings.TrimPrefix(repoRel, prefix), true
}

func shortHash(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:12]
}

func deployLock(remoteURL, branch string) *sync.Mutex {
	key := remoteURL + "\x00" + branch
	lock, _ := deployLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func overlayMissingBranchError(branch string) error {
	return fmt.Errorf("git: overlay deployment requires existing remote branch %q; run a replacement deployment first", branch)
}

// pushRefSpecs returns the RefSpecs for pushing the current HEAD of the local
// branch to origin. The same RefSpec works for first-deploy and subsequent
// deploys; go-git creates the remote ref when it does not exist.
func pushRefSpecs(branch string) []gogitconfig.RefSpec {
	local := plumbing.NewBranchReferenceName(branch)
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
