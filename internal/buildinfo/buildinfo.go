// Package buildinfo carries build-time identity for the binary.
package buildinfo

// Version is the current version. It is overridden at release time via
// -ldflags "-X github.com/simplycubed/code/internal/buildinfo.Version=...".
var Version = "0.0.0-dev"
