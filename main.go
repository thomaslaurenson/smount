// Command smount mounts remote directories over SSH using sshfs.
//
// It is an entry point only. The command line lives in cmd, and everything
// below it in internal.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/thomaslaurenson/smount/cmd"
)

func main() {
	root := cmd.NewRootCmd(os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		// A cancelled prompt is not a failure. Backing out of a menu is a
		// normal way to finish, so it is reported and exits zero rather than
		// being printed as an error.
		if errors.Is(err, cmd.ErrCancelled) {
			fmt.Fprintln(os.Stderr, "[*] cancelled")
			return
		}
		fmt.Fprintf(os.Stderr, "smount: %v\n", err)
		os.Exit(1)
	}
}
