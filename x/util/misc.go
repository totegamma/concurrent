package util

import (
	"runtime/debug"
)

// GetGitHash returns the git hash of the current build.
func GetGitHash() string {
	hash := "unknown"
	if info, available := debug.ReadBuildInfo(); available {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				hash = setting.Value
				break
			}
		}
	}
	return hash
}

// GetGitShortHash returns the short git hash of the current build.
func GetGitShortHash() string {
	return GetGitHash()[:7]
}

// GetVersion returns the version of the current build.
func GetVersion() string {
	version := "unknown"
	if info, available := debug.ReadBuildInfo(); available {
		version = info.Main.Version
	}
	return version
}

// GetFullVersion returns the full version of the current build.
func GetFullVersion() string {
	return GetVersion() + "-" + GetGitShortHash()
}

