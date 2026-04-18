package eval

import "os"

// lookupEnv is a tiny wrapper around os.LookupEnv so env-isolation tests
// can stub it if needed. Shared by ripgrep_exec.go and future agent_check
// wiring (Phase 6).
func lookupEnv(key string) (string, bool) { return os.LookupEnv(key) }
