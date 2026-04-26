package hosting

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	size     int64
	mimetype string
	sha256   string

	etagOnce sync.Once
	etagErr  error
	etag     string
}

// newFile constructs a File for a regular file on disk.
// Root is the source tree root; absPath must live under Root.
// Both paths are cleaned (via filepath.Clean) and resolved to absolute form.
func newFile(root, absPath string) (*File, error) {
	cleanRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidRoot, root)
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
		return nil, fmt.Errorf("%w: %s", ErrSymlink, cleanPath)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNotRegularFile, cleanPath)
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is outside %s", ErrOutsideRoot, cleanPath, cleanRoot)
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, fmt.Errorf("%w: %s is outside %s", ErrOutsideRoot, cleanPath, cleanRoot)
	}
	return &File{absPath: cleanPath, root: cleanRoot}, nil
}

// Root returns the source root that contains the file.
func (f *File) Root() string {
	return f.root
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

// Size returns the file size in bytes. Cached after first call.
func (f *File) Size() (int64, error) {
	f.loadMeta()
	return f.size, f.metaErr
}

// Mimetype returns the file's MIME type. Cached after first call.
// Fallback chain: extension lookup → content sniff → "application/octet-stream".
func (f *File) Mimetype() (string, error) {
	f.loadMeta()
	return f.mimetype, f.metaErr
}

// Hash returns the hex-encoded SHA-256 of the file contents. Cached.
func (f *File) Hash() (string, error) {
	f.loadMeta()
	return f.sha256, f.metaErr
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
		info, err := os.Stat(f.absPath)
		if err != nil {
			f.metaErr = err
			return
		}
		f.size = info.Size()

		r, err := os.Open(f.absPath)
		if err != nil {
			f.metaErr = err
			return
		}
		defer r.Close()

		h := sha256.New()
		head := make([]byte, 512)
		n, err := io.ReadFull(r, head)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			f.metaErr = err
			return
		}
		head = head[:n]
		h.Write(head)

		if _, err := io.Copy(h, r); err != nil {
			f.metaErr = err
			return
		}
		f.sha256 = hex.EncodeToString(h.Sum(nil))

		f.mimetype = detectMimetype(f.absPath, head)
	})
}

// detectMimetype chooses a MIME type for path using:
//  1. extension lookup (covers text/html, text/css, image/*, etc. reliably)
//  2. content sniff via net/http.DetectContentType on the first 512 bytes
//  3. fallback to application/octet-stream
func detectMimetype(path string, head []byte) string {
	if m := mimetypeByExtension(strings.ToLower(filepath.Ext(path))); m != "" {
		return m
	}
	if len(head) > 0 {
		if m := http.DetectContentType(head); m != "" && m != "application/octet-stream" {
			return m
		}
	}
	return "application/octet-stream"
}

// mimetypeByExtension returns an explicit MIME type for common web/static-site
// file extensions. Returns empty string for unknown extensions so the caller
// can fall through to content-sniffing.
func mimetypeByExtension(ext string) string {
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".txt", ".md":
		return "text/plain; charset=utf-8"
	case ".ico":
		return "image/x-icon"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg":
		return "audio/ogg"
	case ".pdf":
		return "application/pdf"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".otf":
		return "font/otf"
	}
	return ""
}
