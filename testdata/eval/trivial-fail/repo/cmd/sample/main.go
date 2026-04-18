// Package main is a trivial fixture used by the eval harness to prove
// the failure-detection path works end-to-end. The fixture's expected
// selection points at a path that does not exist in this snapshot, so
// the harness's precision/recall numbers are deliberately lower than
// 1.0 — and will stay that way regardless of future planner
// improvements, which is the §11 requirement for this fixture.
package main

func main() {}
