package repo

import (
	"os"
	"time"
)

// mtimeRFC3339UTC returns the file's modification time formatted as RFC 3339
// in UTC, matching the manifest-wide timestamp convention (SPEC §7.2.1,
// §11.1).
func mtimeRFC3339UTC(fi os.FileInfo) string {
	return fi.ModTime().UTC().Format(time.RFC3339)
}
