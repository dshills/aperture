package relevance

import "testing"

func TestDampen_DisabledIsPassThrough(t *testing.T) {
	for _, otherMax := range []float64{0, 0.25, 0.5, 0.75, 1.0} {
		got := Dampen(otherMax, DampenerConfig{Enabled: false, Floor: 0.3, Slope: 0.7})
		if got != 1.0 {
			t.Errorf("disabled dampener at otherMax=%v = %v, want 1.0", otherMax, got)
		}
	}
}

func TestDampen_FloorWhenOtherMaxZero(t *testing.T) {
	got := Dampen(0, DampenerConfig{Enabled: true, Floor: 0.3, Slope: 0.7})
	if got != 0.3 {
		t.Errorf("otherMax=0 → %v, want floor 0.3", got)
	}
}

func TestDampen_UnityWhenOtherMaxOne(t *testing.T) {
	got := Dampen(1, DampenerConfig{Enabled: true, Floor: 0.3, Slope: 0.7})
	if got != 1.0 {
		t.Errorf("otherMax=1 → %v, want 1.0", got)
	}
}

func TestDampen_LinearRamp(t *testing.T) {
	// With floor=0.30 slope=0.70, otherMax=0.5 → 0.65 (to 1e-9 tol;
	// float arithmetic produces ~0.6499999999999999).
	cfg := DampenerConfig{Enabled: true, Floor: 0.3, Slope: 0.7}
	got := Dampen(0.5, cfg)
	if !nearlyEqual(got, 0.65) {
		t.Errorf("otherMax=0.5 → %v, want 0.65", got)
	}
}

func nearlyEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

func TestDampen_Monotone(t *testing.T) {
	cfg := DampenerConfig{Enabled: true, Floor: 0.3, Slope: 0.7}
	prev := -1.0
	for i := 0; i <= 100; i++ {
		x := float64(i) / 100.0
		got := Dampen(x, cfg)
		if got < prev {
			t.Fatalf("non-monotone: Dampen(%v)=%v < prev %v", x, got, prev)
		}
		prev = got
	}
}

func TestDampen_SlopeZeroIsFloorClamp(t *testing.T) {
	cfg := DampenerConfig{Enabled: true, Floor: 0.5, Slope: 0.0}
	for _, otherMax := range []float64{0, 0.4, 1.0} {
		got := Dampen(otherMax, cfg)
		if got != 0.5 {
			t.Errorf("slope=0 at otherMax=%v → %v, want 0.5", otherMax, got)
		}
	}
}

func TestDampen_FloorPlusSlopeBelowOne_NeverReachesUnity(t *testing.T) {
	// floor=0.4 slope=0.5 → max output at otherMax=1 is 0.9, never 1.0.
	cfg := DampenerConfig{Enabled: true, Floor: 0.4, Slope: 0.5}
	got := Dampen(1.0, cfg)
	if got != 0.9 {
		t.Errorf("got %v, want 0.9", got)
	}
}

func TestDampen_ClampsDefensivelyOnOutOfRangeInputs(t *testing.T) {
	// Safety net for unit tests that pass synthetic values; the config
	// validator (§7.2.3) rejects these up front in the real pipeline.
	got := Dampen(2.0, DampenerConfig{Enabled: true, Floor: -0.5, Slope: 2.0})
	if got != 1.0 {
		t.Errorf("out-of-range clamp result = %v, want 1.0", got)
	}
}

func TestOtherMaxForDampener_IgnoresOtherFactors(t *testing.T) {
	// mention, test, doc, config are NOT part of other_max (§7.2.2).
	signals := map[string]float64{
		"mention":  1.0,
		"test":     1.0,
		"doc":      1.0,
		"config":   1.0,
		"symbol":   0.0,
		"filename": 0.2,
		"import":   0.1,
		"package":  0.0,
	}
	got := OtherMaxForDampener(signals)
	if got != 0.2 {
		t.Errorf("OtherMaxForDampener=%v, want 0.2", got)
	}
}

func TestOtherMaxForDampener_AllFourFactorsChecked(t *testing.T) {
	cases := []struct {
		name   string
		winner string
		signal map[string]float64
		want   float64
	}{
		{"symbol winner", "symbol", map[string]float64{"symbol": 0.9}, 0.9},
		{"filename winner", "filename", map[string]float64{"filename": 0.8}, 0.8},
		{"import winner", "import", map[string]float64{"import": 0.7}, 0.7},
		{"package winner", "package", map[string]float64{"package": 0.6}, 0.6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := OtherMaxForDampener(c.signal); got != c.want {
				t.Errorf("other_max=%v, want %v", got, c.want)
			}
		})
	}
}
