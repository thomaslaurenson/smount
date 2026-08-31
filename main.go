// Command smount mounts remote directories over SSH using sshfs.
//
// It is an entry point only. The command line lives in cmd, and everything
// below it in internal.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/thomaslaurenson/smount/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := cmd.NewRootCmd(os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		// Asked of the context rather than the error, because a cancelled
		// operation reports its own symptom instead of the cancellation: a
		// killed sshfs says "signal: killed". Only the context knows why the
		// work stopped, and an interrupt the user asked for needs no message.
		if errors.Is(ctx.Err(), context.Canceled) {
			os.Exit(130)
		}
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
