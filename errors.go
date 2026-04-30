package hyperproviders

import (
	"github.com/herdworks/hyperproviders-go/hosting"
	"github.com/herdworks/hyperproviders-go/secrets"
)

var (
	ErrMissingKind     = hosting.ErrMissingKind
	ErrMissingEndpoint = hosting.ErrMissingEndpoint
	ErrUnknownKind     = hosting.ErrUnknownKind

	ErrSecretNotFound        = secrets.ErrNotFound
	ErrSecretEmpty           = secrets.ErrEmpty
	ErrResolveSecretRequired = secrets.ErrResolveRequired

	ErrInvalidRoot    = hosting.ErrInvalidRoot
	ErrOutsideRoot    = hosting.ErrOutsideRoot
	ErrNotRegularFile = hosting.ErrNotRegularFile
	ErrSymlink        = hosting.ErrSymlink
)
