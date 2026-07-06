// Command darkubectl provides kubectl-like access to the Hamravesh Darkube platform.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rahacloud/darkubectl/cmd"
)

func main() {
	if err := cmd.NewApp().Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "darkubectl:", err)
		os.Exit(1)
	}
}
