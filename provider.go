package hyperproviders

import "github.com/herdworks/hyperproviders-go/hosting"

type HostingProvider = hosting.Provider
type HostingProviderEngine = hosting.Engine
type ProviderFilter = hosting.Filter
type FilterRules = hosting.FilterRules
type File = hosting.File

const ProviderWorkspaceDir = hosting.ProviderWorkspaceDir

func RegisterHostingProviderEngine(kind string, engine HostingProviderEngine) {
	hosting.RegisterEngine(kind, engine)
}

func IsProviderKind(kind string) bool {
	return hosting.IsProviderKind(kind)
}
