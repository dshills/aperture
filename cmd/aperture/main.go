// Aperture CLI entry point.
package main

import (
	"os"

	"github.com/dshills/aperture/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
