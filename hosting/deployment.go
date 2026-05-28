package hosting

import "fmt"

// DeploymentMode describes how authoritative a deployment artifact set is for
// the provider target.
type DeploymentMode string

const (
	// DeploymentModeReplacement treats the artifact set as the desired provider
	// target contents. Providers may delete remote resources absent from the
	// artifact set when they can support that behavior.
	DeploymentModeReplacement DeploymentMode = "replacement"

	// DeploymentModeOverlay treats the artifact set as a non-authoritative set of
	// writes. Providers must preserve remote resources absent from the artifact set.
	DeploymentModeOverlay DeploymentMode = "overlay"
)

// Validate reports whether mode is one of the supported deployment modes.
func (mode DeploymentMode) Validate() error {
	switch mode {
	case DeploymentModeReplacement, DeploymentModeOverlay:
		return nil
	case "":
		return fmt.Errorf("deployment mode is required")
	default:
		return fmt.Errorf("unknown deployment mode %q", mode)
	}
}
