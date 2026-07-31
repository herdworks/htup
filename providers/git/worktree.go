package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	hosting "github.com/herdworks/hyperproviders-go"
)

func validatePrecondition(wt *git.Worktree, publishDir string, condition hosting.PreconditionFile) error {
	rel := repoPath(publishDir, condition.Target)
	current, err := wt.Filesystem.Open(rel)
	if condition.Expected == nil {
		if err == nil {
			_ = current.Close()
			return fmt.Errorf("%w: target %q already exists", hosting.ErrDeploymentPreconditionFailed, condition.Target)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("git: read precondition target %s: %w", condition.Target, err)
		}
	} else {
		if err != nil {
			return fmt.Errorf("%w: target %q does not exist", hosting.ErrDeploymentPreconditionFailed, condition.Target)
		}
		actual, readErr := io.ReadAll(current)
		closeErr := current.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if !bytes.Equal(actual, condition.Expected) {
			return fmt.Errorf("%w: target %q differs from expected contents", hosting.ErrDeploymentPreconditionFailed, condition.Target)
		}
	}
	return nil
}

func materializePrecondition(wt *git.Worktree, publishDir string, condition hosting.PreconditionFile) error {
	rel := repoPath(publishDir, condition.Target)
	if err := wt.Filesystem.MkdirAll(path.Dir(rel), 0o755); err != nil {
		return err
	}
	dst, err := wt.Filesystem.Create(rel)
	if err != nil {
		return err
	}
	if _, err := dst.Write(condition.Content); err != nil {
		_ = dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return nil
}

// openOrClone opens the repository at workdir if it exists, or clones it from
// remote otherwise. Returns (repo, worktree, firstDeploy, err). firstDeploy
// is true when the remote branch does not yet exist — the caller should skip
// the reset-to-origin step and push with --set-upstream.
func openOrClone(ctx context.Context, workdir, remote, branch string, auth transport.AuthMethod) (*git.Repository, *git.Worktree, bool, error) {
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return nil, nil, false, err
	}

	repo, err := git.PlainOpen(workdir)
	if err != nil && !errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, nil, false, err
	}

	if errors.Is(err, git.ErrRepositoryNotExists) {
		cloneOpts := &git.CloneOptions{
			URL:           remote,
			Auth:          auth,
			ReferenceName: plumbing.NewBranchReferenceName(branch),
			SingleBranch:  true,
		}
		repo, err = git.PlainCloneContext(ctx, workdir, false, cloneOpts)
		if err != nil {
			if isBranchMissing(err) {
				return initRepoForFirstDeploy(workdir, remote, branch)
			}
			return nil, nil, false, err
		}
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, false, err
	}
	return repo, wt, false, nil
}

// initRepoForFirstDeploy creates an empty repository with the specified
// branch set as HEAD so the first deploy can commit and push to a remote
// branch that does not exist yet.
func initRepoForFirstDeploy(workdir, remote, branch string) (*git.Repository, *git.Worktree, bool, error) {
	if err := os.RemoveAll(workdir); err != nil {
		return nil, nil, false, err
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		return nil, nil, false, err
	}
	repo, err := git.PlainInit(workdir, false)
	if err != nil {
		return nil, nil, false, err
	}

	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))
	if err := repo.Storer.SetReference(head); err != nil {
		return nil, nil, false, err
	}

	if _, err := repo.CreateRemote(&gogitconfig.RemoteConfig{Name: "origin", URLs: []string{remote}}); err != nil {
		return nil, nil, false, err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, false, err
	}
	return repo, wt, true, nil
}

// isBranchMissing reports whether err indicates the remote branch does not
// exist — the signal to switch into first-deploy mode.
func isBranchMissing(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return true
	}
	if errors.Is(err, git.NoMatchingRefSpecError{}) {
		return true
	}
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return true
	}
	return false
}

// fetchOrigin fetches refs from origin. A no-op fetch is considered success.
func fetchOrigin(ctx context.Context, repo *git.Repository, auth transport.AuthMethod) error {
	err := repo.FetchContext(ctx, &git.FetchOptions{RemoteName: "origin", Auth: auth})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}

// resetToOrigin hard-resets the working tree to origin/<branch>. Used on every
// non-first deploy.
func resetToOrigin(repo *git.Repository, wt *git.Worktree, branch string) error {
	ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return err
	}
	return wt.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: ref.Hash()})
}

// clearWorktree removes everything at the root of fs except the .git directory.
// Used before copying a new set of files so removed files stop existing in
// the working tree (and are picked up as deletes by AddWithOptions).
func clearWorktree(fs billy.Filesystem) error {
	entries, err := fs.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := removeAll(fs, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func clearPublishDir(fs billy.Filesystem, publishDir string) error {
	if publishDir == "." {
		return clearWorktree(fs)
	}

	if _, err := fs.Stat(publishDir); err != nil {
		if os.IsNotExist(err) {
			return fs.MkdirAll(publishDir, 0o755)
		}
		return err
	}
	if err := removeAll(fs, publishDir); err != nil {
		return err
	}
	return fs.MkdirAll(publishDir, 0o755)
}

func removeAll(fs billy.Filesystem, rel string) error {
	info, err := fs.Stat(rel)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fs.Remove(rel)
	}
	entries, err := fs.ReadDir(rel)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := removeAll(fs, path.Join(rel, e.Name())); err != nil {
			return err
		}
	}
	return fs.Remove(rel)
}

// copyFiles writes each hosting.File into fs under publishDir. Intermediate
// directories are created as needed.
func copyFiles(fs billy.Filesystem, publishDir string, files []*hosting.File) error {
	for _, file := range files {
		rel := repoPath(publishDir, file.RelPath())
		if err := fs.MkdirAll(path.Dir(rel), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", rel, err)
		}
		if err := copyFile(fs, file, rel); err != nil {
			return err
		}
	}
	return nil
}

func stageFiles(wt *git.Worktree, publishDir string, files []*hosting.File) error {
	for _, file := range files {
		rel := repoPath(publishDir, file.RelPath())
		if _, err := wt.Add(rel); err != nil {
			return fmt.Errorf("add %s: %w", rel, err)
		}
	}
	return nil
}

func repoPath(publishDir, rel string) string {
	if publishDir == "." {
		return rel
	}
	return path.Join(publishDir, rel)
}

func copyFile(fs billy.Filesystem, file *hosting.File, rel string) error {
	dst, err := fs.Create(rel)
	if err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	defer dst.Close()

	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", file.LocalPath(), err)
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy %s: %w", rel, err)
	}
	return nil
}
