// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 evroc

package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the semantic version (set by ldflags at build time).
	Version = "dev"

	// GitCommit is the git commit hash (set by ldflags at build time).
	GitCommit = "unknown"

	// BuildDate is the build date in RFC3339 format (set by ldflags at build time).
	BuildDate = "unknown"
)

// Info contains version information.
type Info struct {
	Version   string `json:"version"`
	GitCommit string `json:"gitCommit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

// Get returns the version information.
func Get() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

// String returns a human-readable version string.
func (i Info) String() string {
	return fmt.Sprintf("Version: %s, GitCommit: %s, BuildDate: %s, GoVersion: %s, Platform: %s",
		i.Version, i.GitCommit, i.BuildDate, i.GoVersion, i.Platform)
}
