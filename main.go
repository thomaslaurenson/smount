// Command smount mounts remote directories over SSH using sshfs.
//
// It is an entry point only. The command line lives in cmd, and everything
// below it in internal.
package main

import (
	"fmt"
	"os"

	"github.com/thomaslaurenson/smount/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "smount: %v\n", err)
		os.Exit(1)
	}
}
