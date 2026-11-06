// Package gittest isolates tests that run git from the developer's git
// configuration.
package gittest

import "os"

// Isolate makes git ignore the global and system configuration, whose hooks,
// filters, or trace2 targets can change test repositories, sometimes while
// the test cleans them up. Call it from TestMain before m.Run.
func Isolate() {
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}
