# trivial-pass fixture

A minimal Go repo used by the Aperture eval harness to confirm end-to-end
plumbing works. The fixture's `task` asks the planner to modify `Greet`
in `cmd/sample/main.go`; the expected selection is that single file.
