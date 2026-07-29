// Command simplycubed is the CLI for SimplyCubed Code, a GitHub-native
// autonomous coding agent. The bulk of the system runs as a GitHub Action; this
// binary is the same engine, runnable locally for development and debugging.
//
// This is an early scaffold. See STATUS.md for what exists and what does not.
package main

import (
	"fmt"
	"os"

	"github.com/simplycubed/code/internal/buildinfo"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version" || args[0] == "-v") {
		fmt.Println(buildinfo.Version)
		return
	}
	fmt.Fprintf(os.Stderr, "simplycubed %s: early scaffold, not yet usable. See STATUS.md\n", buildinfo.Version)
}
