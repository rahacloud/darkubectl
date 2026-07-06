//go:build !unix

package cmd

// watchWindowResize is a no-op on platforms without SIGWINCH (e.g. Windows):
// the initial size is sent, but live resizing is not tracked.
func watchWindowResize(func()) func() {
	return func() {}
}
