package hosting

import "errors"

var (
	ErrMissingKind     = errors.New("provider kind is empty")
	ErrMissingEndpoint = errors.New("provider endpoint is empty")
	ErrUnknownKind     = errors.New("unknown provider kind")

	ErrInvalidRoot    = errors.New("file root is invalid")
	ErrOutsideRoot    = errors.New("file path is outside root")
	ErrNotRegularFile = errors.New("path is not a regular file")
	ErrSymlink        = errors.New("symlinks are not supported")
)
