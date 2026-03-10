package homoscale

import (
	"fmt"
	"runtime"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func Version() string {
	return version
}

func VersionSummary() string {
	return fmt.Sprintf("homoscale %s (%s, %s) %s/%s", version, commit, date, runtime.GOOS, runtime.GOARCH)
}

func VersionDetails() map[string]string {
	return map[string]string{
		"version": version,
		"commit":  commit,
		"date":    date,
		"goos":    runtime.GOOS,
		"goarch":  runtime.GOARCH,
	}
}
