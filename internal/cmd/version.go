package cmd

import "runtime/debug"

// version is the release version. The release build sets it with -ldflags.
var version = ""

// versionText returns the version of this binary. A release build has the
// tag. A "go install" build has the module version. All other builds give "dev".
func versionText() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
