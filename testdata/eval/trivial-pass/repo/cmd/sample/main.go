// Package main is a trivial fixture binary the Aperture eval harness
// points its `trivial-pass` fixture at.
package main

import "fmt"

// Greet prints a greeting for name. Named in the fixture task text so
// the planner's s_symbol and s_mention both fire on this file.
func Greet(name string) {
	fmt.Println("hello", name)
}

func main() {
	Greet("world")
}
