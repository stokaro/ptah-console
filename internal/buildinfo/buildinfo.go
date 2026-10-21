// Package buildinfo says which build of the console is running.
//
// A release stamps the version and commit with -ldflags. A build from a
// checkout falls back to what the Go toolchain recorded, and says so rather
// than inventing a version.
package buildinfo

import (
	"runtime"
	"runtime/debug"

	"github.com/stokaro/ptah-console/web"
)

// Set at link time by a release:
//
//	-X github.com/stokaro/ptah-console/internal/buildinfo.version=v0.1.0
//	-X github.com/stokaro/ptah-console/internal/buildinfo.commit=<sha>
var (
	version string
	commit  string
)

// Info is one build's identity.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	// Modified is true for a build from a checkout with uncommitted changes.
	Modified bool `json:"modified"`
	// WebApp is true when the binary carries the web app, which a build with
	// the with_frontend tag does.
	WebApp bool `json:"webApp"`
	// Operator is the Ptah Operator module version the console was built
	// against, as Go recorded it, and empty when the binary links no part of
	// the operator.
	Operator string `json:"operator,omitempty"`
}

// operatorModule is the module whose API types and plan reader the console
// links.
const operatorModule = "github.com/stokaro/ptah-operator"

// Read returns the running build's identity.
func Read() Info {
	info := Info{Version: version, Commit: commit, GoVersion: runtime.Version(), WebApp: web.Built}
	build, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
		for _, dep := range build.Deps {
			if dep.Path == operatorModule {
				info.Operator = dep.Version
				if dep.Replace != nil {
					info.Operator = dep.Replace.Version
				}
			}
		}
		if info.Version == "" && build.Main.Version != "" && build.Main.Version != "(devel)" {
			info.Version = build.Main.Version
		}
	}
	if info.Version == "" {
		info.Version = "devel"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	return info
}
