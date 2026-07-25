// Command naira is the entrypoint for the Naira AI companion robot
// orchestrator. See RFC.md for the full technical design.
package main

import (
	"fmt"
	"os"

	"naira/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
