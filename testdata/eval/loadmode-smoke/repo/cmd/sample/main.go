// Package main — smoke fixture for aperture eval loadmode.
package main

// Greet prints a greeting used by the trivial loadmode check.
func Greet(name string) string { return "hello " + name }

func main() { _ = Greet("world") }
