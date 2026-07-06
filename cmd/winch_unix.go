//go:build unix

package cmd

import (
	"os"
	"os/signal"
	"syscall"
)

// watchWindowResize calls onResize whenever the terminal window is resized
// (SIGWINCH). The returned function stops watching.
func watchWindowResize(onResize func()) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			onResize()
		}
	}()
	return func() { signal.Stop(ch) }
}
