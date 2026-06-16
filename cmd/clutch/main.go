// Command clutch is the agent-neutral CLI gateway to the Task+Board store.
package main

import (
	"os"

	"github.com/unsafe9/clutch/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
