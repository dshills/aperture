// Package main is the fixture application entry point.
package main

import (
	"fmt"
	"os"

	"example.com/small/internal/oauth"
)

func main() {
	p := oauth.NewProvider("github")
	fmt.Fprintln(os.Stdout, p.Name())
}
