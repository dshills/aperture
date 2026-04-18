// Package version exposes the Aperture build identity. Values are set at
// link time via `-ldflags "-X github.com/dshills/aperture/internal/version.X=Y"`.
// Plain `go build` leaves them at their development defaults.
package version

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Full returns the formatted version string used by `aperture version`.
func Full() string {
	return "aperture " + Version + " (" + Commit + " @ " + BuildDate + ")"
}
