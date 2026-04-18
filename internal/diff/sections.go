package diff

import (
	"sort"
	"strings"

	"github.com/dshills/aperture/internal/manifest"
)

// diffStringSets returns (added-in-B, removed-from-A), each sorted.
func diffStringSets(a, b []string) (added, removed []string) {
	setA := make(map[string]struct{}, len(a))
	setB := make(map[string]struct{}, len(b))
	for _, s := range a {
		setA[s] = struct{}{}
	}
	for _, s := range b {
		setB[s] = struct{}{}
	}
	for s := range setB {
		if _, ok := setA[s]; !ok {
			added = append(added, s)
		}
	}
	for s := range setA {
		if _, ok := setB[s]; !ok {
			removed = append(removed, s)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// firstDiffLine returns the first differing line between a and b,
// trimmed to 200 chars for legibility. Returns "" when a == b.
func firstDiffLine(a, b string) string {
	if a == b {
		return ""
	}
	la := strings.Split(a, "\n")
	lb := strings.Split(b, "\n")
	n := min(len(la), len(lb))
	for i := 0; i < n; i++ {
		if la[i] != lb[i] {
			return trimForDisplay("A:" + la[i] + " -> B:" + lb[i])
		}
	}
	// One side is a prefix of the other. Use plain ASCII arrows so
	// terminals without full Unicode font coverage render the output
	// intact.
	switch {
	case len(la) > n:
		return trimForDisplay("A:" + la[n] + " -> B:<eof>")
	case len(lb) > n:
		return trimForDisplay("A:<eof> -> B:" + lb[n])
	}
	return ""
}

func trimForDisplay(s string) string {
	const maxRunes = 200
	// Byte-length fast path: if the byte length is already within
	// the rune limit, no slicing is needed (every byte is at most
	// one rune).
	if len(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "..."
}

// diffSelections computes (added-in-B, removed-from-A, load-mode-changed).
// The result lists are sorted lexicographically by path for determinism.
func diffSelections(a, b []manifest.Selection) (added, removed []SelectionEntry, changed []SelectionChange) {
	byPathA := make(map[string]manifest.Selection, len(a))
	byPathB := make(map[string]manifest.Selection, len(b))
	for _, s := range a {
		byPathA[s.Path] = s
	}
	for _, s := range b {
		byPathB[s.Path] = s
	}
	for path, sb := range byPathB {
		sa, ok := byPathA[path]
		if !ok {
			added = append(added, selectionEntry(sb))
			continue
		}
		if sa.LoadMode != sb.LoadMode {
			changed = append(changed, SelectionChange{
				Path:       path,
				LoadModeA:  string(sa.LoadMode),
				LoadModeB:  string(sb.LoadMode),
				RelevanceA: sa.RelevanceScore,
				RelevanceB: sb.RelevanceScore,
			})
		}
	}
	for path, sa := range byPathA {
		if _, ok := byPathB[path]; !ok {
			removed = append(removed, selectionEntry(sa))
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Path < removed[j].Path })
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })
	return added, removed, changed
}

func selectionEntry(s manifest.Selection) SelectionEntry {
	return SelectionEntry{
		Path:           s.Path,
		LoadMode:       string(s.LoadMode),
		RelevanceScore: s.RelevanceScore,
		Rationale:      s.Rationale,
	}
}

// diffReachable computes reachable deltas PLUS the set of paths that
// moved from reachable-in-A to selection-in-B ("promoted"). The
// inverse (selection-in-A, reachable-in-B) shows up naturally in
// SelectionsRemoved + ReachableAdded, so we don't name it separately.
func diffReachable(a, b *manifest.Manifest) (added, removed []ReachableEntry, promoted []string) {
	byPathA := make(map[string]manifest.Reachable, len(a.Reachable))
	byPathB := make(map[string]manifest.Reachable, len(b.Reachable))
	for _, r := range a.Reachable {
		byPathA[r.Path] = r
	}
	for _, r := range b.Reachable {
		byPathB[r.Path] = r
	}
	selBPaths := make(map[string]struct{}, len(b.Selections))
	for _, s := range b.Selections {
		selBPaths[s.Path] = struct{}{}
	}

	for path, rb := range byPathB {
		if _, ok := byPathA[path]; !ok {
			added = append(added, reachableEntry(rb))
		}
	}
	for path, ra := range byPathA {
		switch {
		case existsInMap(byPathB, path):
			// Present on both sides — no reachable-level delta.
		case existsSet(selBPaths, path):
			// Was reachable in A; became a selection in B.
			promoted = append(promoted, path)
		default:
			removed = append(removed, reachableEntry(ra))
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Path < added[j].Path })
	sort.Slice(removed, func(i, j int) bool { return removed[i].Path < removed[j].Path })
	sort.Strings(promoted)
	return added, removed, promoted
}

func reachableEntry(r manifest.Reachable) ReachableEntry {
	return ReachableEntry{Path: r.Path, RelevanceScore: r.RelevanceScore, Reason: r.Reason}
}

func existsInMap[V any](m map[string]V, k string) bool { _, ok := m[k]; return ok }
func existsSet(m map[string]struct{}, k string) bool   { _, ok := m[k]; return ok }

// diffGaps computes gap additions, resolutions, and severity changes.
//
// Gaps are matched on (Type, Description) — the v1 manifest attaches
// synthetic gap-N IDs derived from emission order, so matching on ID
// would produce false positives whenever the gap list shifts. Two
// distinct gaps that happen to share a type (e.g. multiple
// `missing_tests` gaps, one per missing test) must NOT collapse
// together; the Description field carries the per-instance context
// (the file or package that triggered the gap) and therefore makes a
// stable compound key.
//
// Within (type, description) equivalence classes, a severity change
// on the same composite key is reported once, even if the side with
// more entries has multiple matching gaps — the spec contract is
// per-kind, not per-file-instance.
func diffGaps(a, b []manifest.Gap) (added, resolved []GapEntry, changed []GapSeverityChange) {
	type key struct {
		Type        string
		Description string
	}
	byKeyA := make(map[key]manifest.Gap, len(a))
	byKeyB := make(map[key]manifest.Gap, len(b))
	for _, g := range a {
		byKeyA[key{Type: string(g.Type), Description: g.Description}] = g
	}
	for _, g := range b {
		byKeyB[key{Type: string(g.Type), Description: g.Description}] = g
	}
	for k, gb := range byKeyB {
		ga, ok := byKeyA[k]
		if !ok {
			added = append(added, gapEntry(gb))
			continue
		}
		if ga.Severity != gb.Severity {
			changed = append(changed, GapSeverityChange{
				Type:      k.Type,
				SeverityA: string(ga.Severity),
				SeverityB: string(gb.Severity),
			})
		}
	}
	for k, ga := range byKeyA {
		if _, ok := byKeyB[k]; !ok {
			resolved = append(resolved, gapEntry(ga))
		}
	}
	sort.Slice(added, func(i, j int) bool {
		if added[i].Type != added[j].Type {
			return added[i].Type < added[j].Type
		}
		return added[i].ID < added[j].ID
	})
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].Type != resolved[j].Type {
			return resolved[i].Type < resolved[j].Type
		}
		return resolved[i].ID < resolved[j].ID
	})
	sort.Slice(changed, func(i, j int) bool { return changed[i].Type < changed[j].Type })
	return added, resolved, changed
}

func gapEntry(g manifest.Gap) GapEntry {
	return GapEntry{ID: g.ID, Type: string(g.Type), Severity: string(g.Severity)}
}

// diffSubSignals computes per-named-signal deltas. Sub-signals that
// appear on only one side are reported with the missing side's value
// as 0.
func diffSubSignals(a, b map[string]float64) []SubSignalDelta {
	names := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		names[k] = struct{}{}
	}
	for k := range b {
		names[k] = struct{}{}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	out := make([]SubSignalDelta, 0, len(sorted))
	for _, n := range sorted {
		va, vb := a[n], b[n]
		if va == vb {
			continue
		}
		out = append(out, SubSignalDelta{Name: n, ValueA: va, ValueB: vb})
	}
	return out
}
