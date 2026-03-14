package homoscale

import (
	"fmt"
	"runtime"
)

var runtimeGOOS = runtime.GOOS

func runningOnAndroid() bool {
	return runtimeGOOS == "android"
}

func ensureDaemonSupported() error {
	if !runningOnAndroid() {
		return nil
	}
	return fmt.Errorf("daemon mode is not supported on Android; run homoscale in the foreground or let the host app supervise it")
}
