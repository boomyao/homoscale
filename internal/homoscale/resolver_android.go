//go:build android

package homoscale

import "io"

func installSystemResolver(_ *Config, _ io.Writer) io.Closer {
	return nopCloser{}
}
