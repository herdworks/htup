package htup

import "errors"

var (
	// ErrDeploymentPreconditionFailed reports that a provider rejected an atomic conditional
	// write because the authoritative remote resource did not match its expected
	// state.
	ErrDeploymentPreconditionFailed = errors.New("deployment precondition failed")

	// ErrDeploymentUnsupportedPrecondition reports that an engine cannot provide an atomic
	// conditional write. Callers must not fall back to a read then blind write.
	ErrDeploymentUnsupportedPrecondition = errors.New("deployment preconditions are not supported by this provider")

	// ErrDeploymentUnsupportedEnqueue reports that an engine cannot preserve the requested
	// post-bulk upload ordering.
	ErrDeploymentUnsupportedEnqueue = errors.New("deployment enqueue ordering is not supported by this provider")

	ErrHostingProviderMissingKind     = errors.New("provider kind is empty")
	ErrHostingProviderMissingEndpoint = errors.New("provider endpoint is empty")
	ErrHostingProviderUnknownKind     = errors.New("unknown provider kind")

	ErrFileInvalidRoot = errors.New("file root is invalid")
	ErrFileOutsideRoot = errors.New("file path is outside root")
	ErrFileNotRegular  = errors.New("path is not a regular file")
	ErrFileSymlink     = errors.New("symlinks are not supported")

	ErrSecretNotFound    = errors.New("secret not found")
	ErrSecretEmpty       = errors.New("secret resolved to empty value")
	ErrSecretMissingName = errors.New("secret name is required")
	ErrSecretMissingKey  = errors.New("secret key is required")
)
