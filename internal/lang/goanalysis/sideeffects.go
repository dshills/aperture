// Package goanalysis parses Go source files with the standard library
// go/parser + go/ast machinery and produces the symbol, import, and
// side-effect data that later phases (relevance, summaries, gaps) depend
// on. Per SPEC §7.2.3, regex-based extraction on Go source is forbidden;
// everything here runs on a real AST.
package goanalysis

import (
	"sort"
	"strings"
)

// sideEffectEntry is one row of the §12.2 table.
type sideEffectEntry struct {
	Tag      string
	Prefix   string   // import-path prefix, matched on segment boundary
	Excludes []string // sub-paths that should NOT inherit the tag
}

// sideEffectTables is the v1 canonical set from SPEC §12.2. The ordering
// here does not affect matching — emission is sorted lexicographically.
var sideEffectTables = []sideEffectEntry{
	// io:filesystem
	{"io:filesystem", "os", []string{"os/exec"}},
	{"io:filesystem", "io/fs", nil},
	{"io:filesystem", "io/ioutil", nil},
	{"io:filesystem", "path", nil},
	{"io:filesystem", "path/filepath", nil},
	{"io:filesystem", "embed", nil},

	// io:process
	{"io:process", "os/exec", nil},
	{"io:process", "syscall", nil},
	{"io:process", "golang.org/x/sys", nil},

	// io:network
	{"io:network", "net", nil},
	{"io:network", "net/http", nil},
	{"io:network", "net/url", nil},
	{"io:network", "net/rpc", nil},
	{"io:network", "net/smtp", nil},
	{"io:network", "net/textproto", nil},
	{"io:network", "net/mail", nil},
	{"io:network", "net/httptest", nil},
	{"io:network", "google.golang.org/grpc", nil},
	{"io:network", "nhooyr.io/websocket", nil},

	// io:database
	{"io:database", "database/sql", nil},
	{"io:database", "database/sql/driver", nil},
	{"io:database", "github.com/jackc/pgx", nil},
	{"io:database", "github.com/lib/pq", nil},
	{"io:database", "github.com/go-sql-driver/mysql", nil},
	{"io:database", "github.com/mattn/go-sqlite3", nil},
	{"io:database", "go.mongodb.org/mongo-driver", nil},
	{"io:database", "github.com/redis/go-redis", nil},
	{"io:database", "github.com/redis/redis", nil},

	// io:time
	{"io:time", "time", nil},

	// io:randomness
	{"io:randomness", "math/rand", nil},
	{"io:randomness", "math/rand/v2", nil},
	{"io:randomness", "crypto/rand", nil},

	// io:logging
	{"io:logging", "log", nil},
	{"io:logging", "log/slog", nil},
	{"io:logging", "go.uber.org/zap", nil},
	{"io:logging", "github.com/sirupsen/logrus", nil},
	{"io:logging", "github.com/rs/zerolog", nil},
}

// SideEffectsFor returns the sorted, deduped tag set produced by matching
// the import list against the §12.2 table. Matching semantics:
//
//  1. Entry-prefix matches on path-segment boundaries — `net/http` matches
//     `net/http/httputil` but NOT `net/httptest`.
//  2. `Excludes` is applied after the prefix test — `os` matches `os` and
//     `os/*` EXCEPT `os/exec` and its descendants.
//  3. A single import may carry multiple tags across tables.
//  4. Matching uses canonical (non-aliased) Go import paths.
func SideEffectsFor(imports []string) []string {
	set := map[string]struct{}{}
	for _, imp := range imports {
		for _, row := range sideEffectTables {
			if !matchesPrefix(imp, row.Prefix) {
				continue
			}
			excluded := false
			for _, ex := range row.Excludes {
				if matchesPrefix(imp, ex) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			set[row.Tag] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// matchesPrefix reports whether imp equals prefix or is a descendant on a
// path-segment boundary. Crucially, `net/http` does NOT match `net/httptest`.
func matchesPrefix(imp, prefix string) bool {
	if imp == prefix {
		return true
	}
	if strings.HasPrefix(imp, prefix+"/") {
		return true
	}
	return false
}
