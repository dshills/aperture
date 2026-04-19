package eval

import "fmt"

// AdvisoryThresholdDeltaPP is the v1.1 §7.5.1 / §7.5.2 trigger:
// when Plan_A's aggregate agent-check pass rate is at least this
// many percentage points below Plan_B's across all fixtures that
// declared agent_check, emit a recommendation to raise the §7.5.0
// avg_size_kb threshold. Advisory only; never auto-applied.
const AdvisoryThresholdDeltaPP = 10.0

// AdvisoryBumpPercent is the fixed recommendation magnitude:
// raise avg_size_kb by 25%, rounded to the nearest integer KB.
const AdvisoryBumpPercent = 25

// Advisory is the §7.5.1 threshold-tuning recommendation. When
// Emit is false every other field is zero-valued — no
// recommendation fired this run.
type Advisory struct {
	Emit        bool
	PassRateAPP float64 // Plan_A pass-rate across fixtures declaring agent_check (0-100)
	PassRateBPP float64 // Plan_B same
	DeltaPP     float64 // B - A
	Message     string  // human-readable summary for the Markdown emitter
	BumpPercent int     // the recommended percent increase (always AdvisoryBumpPercent when Emit)
}

// ComputeAdvisory aggregates the agent_check outcomes across every
// per-fixture record that declared an agent_check and returns the
// §7.5.1 advisor verdict. Records without agent_check declarations
// are ignored; a run where no fixture declared agent_check yields
// Emit=false.
//
// Input shape: one `classifiedDelta` value per fixture, tagged with
// whether agent_check was declared. The caller constructs this
// from its per-fixture result rows.
func ComputeAdvisory(records []AdvisoryInput) Advisory {
	var aPass, bPass, total int
	for _, r := range records {
		if !r.HasAgentCheck {
			continue
		}
		total++
		if r.PlanAPass {
			aPass++
		}
		if r.PlanBPass {
			bPass++
		}
	}
	if total == 0 {
		return Advisory{}
	}
	aRate := float64(aPass) * 100.0 / float64(total)
	bRate := float64(bPass) * 100.0 / float64(total)
	delta := bRate - aRate

	adv := Advisory{
		PassRateAPP: aRate,
		PassRateBPP: bRate,
		DeltaPP:     delta,
	}
	if delta >= AdvisoryThresholdDeltaPP {
		adv.Emit = true
		adv.BumpPercent = AdvisoryBumpPercent
		adv.Message = fmt.Sprintf(
			"Plan_A agent-check pass rate %.1f%% is %.1f pp below Plan_B (%.1f%%); consider raising §7.5.0 avg_size_kb by %d%% (advisory only).",
			aRate, delta, bRate, AdvisoryBumpPercent,
		)
	}
	return adv
}

// AdvisoryInput is one per-fixture signal for ComputeAdvisory.
type AdvisoryInput struct {
	HasAgentCheck bool
	PlanAPass     bool
	PlanBPass     bool
}
