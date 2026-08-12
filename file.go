package htup

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/herdworks/hypermedia/mimetypes"
)

// File is an immutable handle to a deployable artifact.
// Metadata (size, mimetype, hash) is computed lazily and cached.
//
// Paths are normalized internally. RelPath always returns forward-slash
// separated output with no leading slash, matching what hosting providers
// expect for keys/paths.
type File struct {
	absPath string
	root    string

	metaOnce sync.Once
	metaErr  error
	mimetype string

	etagOnce sync.Once
	etagErr  error
	etag     string
}

// PreconditionFile is the immutable, local materialization of one declared
// conditional write. A nil Expected is create-only; a non-nil empty Expected
// requires an existing empty target.
type PreconditionFile struct {
	Target   string
	Expected []byte
	Content  []byte
}

// LocalPath returns the absolute local filesystem path for the file.
func (f *File) LocalPath() string {
	return f.absPath
}

// RelPath returns the deployment path: LocalPath relative to Root, in forward-slash
// form with no leading slash. This is the object key / repo path providers use.
func (f *File) RelPath() string {
	rel, err := filepath.Rel(f.root, f.absPath)
	if err != nil {
		return filepath.ToSlash(f.absPath)
	}
	return filepath.ToSlash(rel)
}

// Open returns a fresh io.ReadCloser for the file; caller must Close.
func (f *File) Open() (io.ReadCloser, error) {
	return os.Open(f.absPath)
}

// Mimetype returns the file's MIME type. Cached after first call.
// Fallback chain: extension lookup → content sniff → "application/octet-stream".
func (f *File) Mimetype() (string, error) {
	f.loadMeta()
	return f.mimetype, f.metaErr
}

// ETag returns the hex-encoded MD5 of the file contents, matching the S3 ETag
// format for non-multipart uploads. Provided for S3 conditional-PUT use only.
// Not cached in the main metadata path because most callers won't need it.
func (f *File) ETag() (string, error) {
	f.etagOnce.Do(func() {
		r, err := f.Open()
		if err != nil {
			f.etagErr = err
			return
		}
		defer r.Close()
		h := md5.New()
		if _, err := io.Copy(h, r); err != nil {
			f.etagErr = err
			return
		}
		f.etag = hex.EncodeToString(h.Sum(nil))
	})
	return f.etag, f.etagErr
}

func (f *File) loadMeta() {
	f.metaOnce.Do(func() {
		r, err := os.Open(f.absPath)
		if err != nil {
			f.metaErr = err
			return
		}
		defer r.Close()

		head := make([]byte, 512)
		n, err := io.ReadFull(r, head)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			f.metaErr = err
			return
		}
		head = head[:n]
		f.mimetype, f.metaErr = mimetypes.Detect(f.absPath, bytes.NewReader(head))
	})
}

// MimetypeForPath returns the MIME type implied by path's extension, falling
// back to application/octet-stream.
func MimetypeForPath(path string) string {
	if m := mimetypes.FromPath(path); m != "" {
		return m
	}
	return "application/octet-stream"
}

// newFile constructs a File for a regular file on disk.
// Root is the source tree root; absPath must live under Root.
// Both paths are cleaned (via filepath.Clean) and resolved to absolute form.
func newFile(root, absPath string) (*File, error) {
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFileInvalidRoot, root)
	}
	cleanPath, err := filepath.Abs(filepath.Clean(absPath))
	if err != nil {
		return nil, fmt.Errorf("invalid file path %q: %w", absPath, err)
	}
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", cleanPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s", ErrFileSymlink, cleanPath)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrFileNotRegular, cleanPath)
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is outside %s", ErrFileOutsideRoot, cleanPath, cleanRoot)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, fmt.Errorf("%w: %s is outside %s", ErrFileOutsideRoot, cleanPath, cleanRoot)
	}
	return &File{absPath: cleanPath, root: cleanRoot}, nil
}

// walk returns every regular file under root, recursively, sorted by path.
// The returned Files have Root set to the absolute form of the argument, so
// each File's RelPath is source-relative (see File.RelPath).
//
// walk reserves ProviderWorkspaceDir for provider-managed deployment state.
// HostingProvider filters are applied after discovery.
func walk(root string, ignore DeploymentIgnore) ([]*File, error) {
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFileInvalidRoot, root)
	}
	var files []*File
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrFileSymlink, path)
		}
		if path != absRoot {
			rel, err := filepath.Rel(absRoot, path)
			if err != nil {
				return fmt.Errorf("%w: %s is outside %s", ErrFileOutsideRoot, path, absRoot)
			}
			rel = filepath.ToSlash(rel)
			first, _, _ := strings.Cut(rel, "/")
			if first == ProviderWorkspaceDir || first == ".git" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if ignore.Match(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() && ignore.MatchDir(rel) {
				return filepath.SkipDir
			}
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, err := newFile(absRoot, path)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath() < files[j].RelPath() })
	return files, nil
}
