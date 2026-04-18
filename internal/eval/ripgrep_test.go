package eval

import "testing"

func TestRenderAnchorPattern_Dedup(t *testing.T) {
	pat, dedup := renderAnchorPattern([]string{"Foo", "foo", "Bar", "BAR"})
	if pat != "Foo|Bar" {
		t.Errorf("pattern=%q, want 'Foo|Bar'", pat)
	}
	if len(dedup) != 2 {
		t.Errorf("dedup=%v, want 2 entries", dedup)
	}
}

func TestRenderAnchorPattern_Empty(t *testing.T) {
	pat, dedup := renderAnchorPattern(nil)
	if pat != "" || len(dedup) != 0 {
		t.Errorf("empty anchors should yield empty pat and dedup: pat=%q dedup=%v", pat, dedup)
	}
}

func TestRenderAnchorPattern_EscapesRegex(t *testing.T) {
	pat, _ := renderAnchorPattern([]string{"a.b"})
	if pat != "a\\.b" {
		t.Errorf("pat=%q, want 'a\\\\.b'", pat)
	}
}

func TestParseCountMatches_SortsByCountThenPath(t *testing.T) {
	raw := []byte(`/repo/z.go:5
/repo/a.go:10
/repo/b.go:5
`)
	paths, err := parseCountMatches(raw, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "b.go", "z.go"}
	if len(paths) != 3 {
		t.Fatalf("len=%d, want 3", len(paths))
	}
	for i, p := range paths {
		if p != want[i] {
			t.Errorf("paths[%d]=%q, want %q", i, p, want[i])
		}
	}
}

func TestParseCountMatches_FiltersZero(t *testing.T) {
	raw := []byte("/repo/z.go:0\n/repo/a.go:1\n")
	paths, err := parseCountMatches(raw, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "a.go" {
		t.Errorf("paths=%v, want [a.go]", paths)
	}
}

func TestScoreRipgrepBaseline_PerfectMatch(t *testing.T) {
	fx := Fixture{Expected: Expected{Selections: []ExpectedSelection{{Path: "a.go"}, {Path: "b.go"}}}}
	m := ScoreRipgrepBaseline(fx, []string{"a.go", "b.go"})
	if m.F1 != 1.0 {
		t.Errorf("F1=%v, want 1.0", m.F1)
	}
}
