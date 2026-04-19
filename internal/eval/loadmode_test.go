package eval

import (
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

func TestClassifyAgentCheckDelta_AllFourBuckets(t *testing.T) {
	cases := []struct {
		name string
		a, b AgentCheckOutcome
		want LoadmodeDelta
	}{
		{"both pass", AgentCheckPass, AgentCheckPass, LoadmodeNoChangePass},
		{"both fail", AgentCheckFail, AgentCheckFail, LoadmodeNoChangeFail},
		{"a fail, b pass", AgentCheckFail, AgentCheckPass, LoadmodeImprovement},
		{"a pass, b fail", AgentCheckPass, AgentCheckFail, LoadmodeRegression},
		{"a timeout collapses to fail", AgentCheckTimeout, AgentCheckPass, LoadmodeImprovement},
		{"b timeout collapses to fail", AgentCheckPass, AgentCheckTimeout, LoadmodeRegression},
		{"both timeout → no_change_fail", AgentCheckTimeout, AgentCheckTimeout, LoadmodeNoChangeFail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyAgentCheckDelta(c.a, c.b); got != c.want {
				t.Errorf("delta(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestComputeSymbolicDiff_DemotionTracking(t *testing.T) {
	planA := &manifest.Manifest{
		Selections: []manifest.Selection{
			{Path: "a.go", LoadMode: manifest.LoadModeBehavioralSummary, RelevanceScore: 0.85, EstimatedTokens: 500},
			{Path: "b.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.70, EstimatedTokens: 200},
		},
		Budget:      manifest.Budget{EstimatedSelectedTokens: 700, EffectiveContextBudget: 1000},
		Feasibility: manifest.Feasibility{Score: 0.6},
	}
	planB := &manifest.Manifest{
		Selections: []manifest.Selection{
			{Path: "a.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.85, EstimatedTokens: 1200},
			{Path: "b.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.70, EstimatedTokens: 200},
		},
		Budget:      manifest.Budget{EstimatedSelectedTokens: 1400, EffectiveContextBudget: 1000},
		Feasibility: manifest.Feasibility{Score: 0.7},
	}
	d := ComputeSymbolicDiff(planA, planB, 400)
	if len(d.DemotedInA) != 1 || d.DemotedInA[0].Path != "a.go" {
		t.Errorf("DemotedInA = %+v, want [a.go]", d.DemotedInA)
	}
	if d.TokensGainedByForcing != 700 {
		t.Errorf("TokensGainedByForcing = %d, want 700", d.TokensGainedByForcing)
	}
	if d.FeasibilityDelta <= 0 {
		t.Errorf("FeasibilityDelta should be positive, got %v", d.FeasibilityDelta)
	}
	if !d.ForcedFullWouldUnderflow {
		t.Errorf("ForcedFullWouldUnderflow should be true when overflow > 0")
	}
	if d.BudgetOverflowTokens != 400 {
		t.Errorf("BudgetOverflowTokens = %d, want 400", d.BudgetOverflowTokens)
	}
}

func TestComputeSymbolicDiff_GapSets(t *testing.T) {
	planA := &manifest.Manifest{
		Gaps: []manifest.Gap{
			{Type: manifest.GapMissingTests, Severity: manifest.GapSeverityWarning},
			{Type: manifest.GapOversizedPrimaryContext, Severity: manifest.GapSeverityBlocking},
		},
		Budget: manifest.Budget{EffectiveContextBudget: 1000, EstimatedSelectedTokens: 800},
	}
	planB := &manifest.Manifest{
		Gaps: []manifest.Gap{
			{Type: manifest.GapMissingTests, Severity: manifest.GapSeverityWarning},
			{Type: manifest.GapMissingSpec, Severity: manifest.GapSeverityInfo},
		},
		Budget: manifest.Budget{EffectiveContextBudget: 1000, EstimatedSelectedTokens: 800},
	}
	d := ComputeSymbolicDiff(planA, planB, 0)
	if len(d.GapsFiredInAOnly) != 1 || d.GapsFiredInAOnly[0] != string(manifest.GapOversizedPrimaryContext) {
		t.Errorf("A-only gaps = %v", d.GapsFiredInAOnly)
	}
	if len(d.GapsFiredInBOnly) != 1 || d.GapsFiredInBOnly[0] != string(manifest.GapMissingSpec) {
		t.Errorf("B-only gaps = %v", d.GapsFiredInBOnly)
	}
}

func TestComputeAdvisory_EmitsAboveThreshold(t *testing.T) {
	// 4 fixtures, A passes 1/4 (25%), B passes 4/4 (100%).
	// Delta = 75 pp, well above the 10 pp trigger.
	inputs := []AdvisoryInput{
		{HasAgentCheck: true, PlanAPass: false, PlanBPass: true},
		{HasAgentCheck: true, PlanAPass: false, PlanBPass: true},
		{HasAgentCheck: true, PlanAPass: false, PlanBPass: true},
		{HasAgentCheck: true, PlanAPass: true, PlanBPass: true},
	}
	adv := ComputeAdvisory(inputs)
	if !adv.Emit {
		t.Fatalf("advisor should emit; got %+v", adv)
	}
	if adv.BumpPercent != AdvisoryBumpPercent {
		t.Errorf("BumpPercent = %d, want %d", adv.BumpPercent, AdvisoryBumpPercent)
	}
}

func TestComputeAdvisory_QuietWhenNoAgentCheckFixtures(t *testing.T) {
	inputs := []AdvisoryInput{
		{HasAgentCheck: false, PlanAPass: false, PlanBPass: true},
	}
	adv := ComputeAdvisory(inputs)
	if adv.Emit {
		t.Errorf("advisor should NOT emit when no fixtures declare agent_check: %+v", adv)
	}
}

func TestComputeAdvisory_QuietBelowThreshold(t *testing.T) {
	// 11 fixtures: A passes 9/11 (~81.8%), B passes 10/11 (~90.9%).
	// Delta ~9.09 pp, strictly below the 10 pp trigger.
	inputs := make([]AdvisoryInput, 0, 11)
	for i := 0; i < 9; i++ {
		inputs = append(inputs, AdvisoryInput{HasAgentCheck: true, PlanAPass: true, PlanBPass: true})
	}
	inputs = append(inputs,
		AdvisoryInput{HasAgentCheck: true, PlanAPass: false, PlanBPass: true},  // improvement
		AdvisoryInput{HasAgentCheck: true, PlanAPass: false, PlanBPass: false}, // both fail
	)
	adv := ComputeAdvisory(inputs)
	if adv.Emit {
		t.Errorf("advisor should not emit under 10pp: %+v", adv)
	}
}
