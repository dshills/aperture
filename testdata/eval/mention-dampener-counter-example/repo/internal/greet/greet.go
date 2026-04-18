// Package greet holds the Greet function referenced by the fixture
// task. Both s_mention (filename agreement) and s_symbol / s_filename
// (Greet exported here) agree — the v1.1 dampener MUST NOT penalize
// this case.
package greet

import "fmt"

// Greet prints "hello <name>".
func Greet(name string) {
	fmt.Println("hello", name)
}
