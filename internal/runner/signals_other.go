//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package runner

import "os"

func forwardedSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
