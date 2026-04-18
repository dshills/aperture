package diff

import (
	"fmt"
	"strings"
)

// EmitMarkdown returns the human-readable Markdown rendering of d.
// Sections are emitted in the fixed §4.5 order; empty sections render
// an `_unchanged_` marker (so consumers can tell "unchanged" apart
// from "not checked" per §7.6.2).
//
// The output is deterministic: repeated calls on the same Diff produce
// byte-identical bytes. The top line is the semantic_equivalent banner
// so a reader can scan it at a glance.
func EmitMarkdown(d *Diff) []byte {
	var sb strings.Builder
	writeBanner(&sb, d)
	writeSection(&sb, "Hash and ID", renderHash(d))
	writeSection(&sb, "Task", renderTask(d))
	writeSection(&sb, "Repo", renderRepo(d))
	writeSection(&sb, "Budget", renderBudget(d))
	writeSection(&sb, "Scope", renderScope(d))
	writeSection(&sb, "Selections", renderSelections(d))
	writeSection(&sb, "Reachable", renderReachable(d))
	writeSection(&sb, "Gaps", renderGaps(d))
	writeSection(&sb, "Feasibility", renderFeasibility(d))
	writeSection(&sb, "Generation metadata", renderGeneration(d))
	return []byte(sb.String())
}

func writeBanner(sb *strings.Builder, d *Diff) {
	fmt.Fprintln(sb, "# aperture diff")
	fmt.Fprintln(sb)
	if d.SemanticEquivalent {
		fmt.Fprintln(sb, "**semantic_equivalent: true** — manifest_hash matches on both sides.")
	} else {
		fmt.Fprintln(sb, "**semantic_equivalent: false** — manifest_hash differs.")
	}
	if len(d.ToolBugDiagnostic) > 0 {
		fmt.Fprintln(sb)
		fmt.Fprintln(sb, "> **Tool-bug diagnostic (hash agreement + content disagreement):**")
		for _, line := range d.ToolBugDiagnostic {
			fmt.Fprintf(sb, "> - %s\n", line)
		}
	}
	fmt.Fprintln(sb)
}

func writeSection(sb *strings.Builder, name string, body []string) {
	fmt.Fprintf(sb, "## %s\n\n", name)
	if len(body) == 0 {
		fmt.Fprintln(sb, "_unchanged_")
		fmt.Fprintln(sb)
		return
	}
	for _, line := range body {
		fmt.Fprintln(sb, line)
	}
	fmt.Fprintln(sb)
}

// renderHash returns body lines for the Hash section. Returns nil when
// fully unchanged (signals the "_unchanged_" marker).
func renderHash(d *Diff) []string {
	if d.HashEqual && d.ManifestIDEqual && d.ConfigDigestA == d.ConfigDigestB {
		return nil
	}
	var out []string
	out = append(out, fmt.Sprintf("- manifest_hash A: `%s`", d.HashA))
	out = append(out, fmt.Sprintf("- manifest_hash B: `%s`", d.HashB))
	out = append(out, fmt.Sprintf("- manifest_hash equal: %v", d.HashEqual))
	out = append(out, fmt.Sprintf("- manifest_id equal: %v", d.ManifestIDEqual))
	out = append(out, fmt.Sprintf("- config_digest A: `%s`", d.ConfigDigestA))
	out = append(out, fmt.Sprintf("- config_digest B: `%s`", d.ConfigDigestB))
	if d.ConfigDigestA != d.ConfigDigestB {
		out = append(out, "- _config_digest differs_: the resolved config was not identical on the two sides. The manifest does not embed the full resolved config; digest divergence is the authoritative signal (v1.1 §4.5).")
	}
	return out
}

func renderTask(d *Diff) []string {
	if len(d.TaskAnchorsAdded) == 0 && len(d.TaskAnchorsRemoved) == 0 &&
		d.TaskTypeA == d.TaskTypeB && d.TaskTextFirstDiff == "" {
		return nil
	}
	var out []string
	if d.TaskTypeA != d.TaskTypeB {
		out = append(out, fmt.Sprintf("- type: `%s` → `%s`", d.TaskTypeA, d.TaskTypeB))
	}
	if len(d.TaskAnchorsAdded) > 0 {
		out = append(out, fmt.Sprintf("- anchors added: %s", joinCode(d.TaskAnchorsAdded)))
	}
	if len(d.TaskAnchorsRemoved) > 0 {
		out = append(out, fmt.Sprintf("- anchors removed: %s", joinCode(d.TaskAnchorsRemoved)))
	}
	if d.TaskTextFirstDiff != "" {
		out = append(out, fmt.Sprintf("- first differing line: `%s`", d.TaskTextFirstDiff))
	}
	return out
}

func renderRepo(d *Diff) []string {
	if d.FingerprintEqual && len(d.LanguageHintsAdded) == 0 && len(d.LanguageHintsDropd) == 0 {
		return nil
	}
	var out []string
	if !d.FingerprintEqual {
		out = append(out, fmt.Sprintf("- fingerprint A: `%s`", d.FingerprintA))
		out = append(out, fmt.Sprintf("- fingerprint B: `%s`", d.FingerprintB))
	}
	if len(d.LanguageHintsAdded) > 0 {
		out = append(out, fmt.Sprintf("- language hints added: %s", joinCode(d.LanguageHintsAdded)))
	}
	if len(d.LanguageHintsDropd) > 0 {
		out = append(out, fmt.Sprintf("- language hints removed: %s", joinCode(d.LanguageHintsDropd)))
	}
	return out
}

func renderBudget(d *Diff) []string {
	if d.BudgetModelA == d.BudgetModelB &&
		d.TokenCeilingA == d.TokenCeilingB &&
		d.EffectiveContextA == d.EffectiveContextB &&
		d.EstimatorA == d.EstimatorB {
		return nil
	}
	var out []string
	if d.BudgetModelA != d.BudgetModelB {
		out = append(out, fmt.Sprintf("- model: `%s` → `%s`", d.BudgetModelA, d.BudgetModelB))
	}
	if d.TokenCeilingA != d.TokenCeilingB {
		out = append(out, fmt.Sprintf("- token_ceiling: %d → %d", d.TokenCeilingA, d.TokenCeilingB))
	}
	if d.EffectiveContextA != d.EffectiveContextB {
		out = append(out, fmt.Sprintf("- effective_context_budget: %d → %d", d.EffectiveContextA, d.EffectiveContextB))
	}
	if d.EstimatorA != d.EstimatorB {
		out = append(out, fmt.Sprintf("- estimator: `%s` → `%s`", d.EstimatorA, d.EstimatorB))
	}
	return out
}

func renderScope(d *Diff) []string {
	if d.ScopeA == d.ScopeB {
		return nil
	}
	return []string{fmt.Sprintf("- scope: `%s` → `%s`", orNone(d.ScopeA), orNone(d.ScopeB))}
}

func orNone(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func renderSelections(d *Diff) []string {
	if len(d.SelectionsAdded) == 0 && len(d.SelectionsRemoved) == 0 && len(d.SelectionsLoadChanged) == 0 {
		return nil
	}
	var out []string
	if len(d.SelectionsAdded) > 0 {
		out = append(out, "### Added")
		for _, s := range d.SelectionsAdded {
			out = append(out, fmt.Sprintf("- `%s` (load=%s, score=%.4f)", s.Path, s.LoadMode, s.RelevanceScore))
		}
	}
	if len(d.SelectionsRemoved) > 0 {
		out = append(out, "### Removed")
		for _, s := range d.SelectionsRemoved {
			out = append(out, fmt.Sprintf("- `%s` (load=%s, score=%.4f)", s.Path, s.LoadMode, s.RelevanceScore))
		}
	}
	if len(d.SelectionsLoadChanged) > 0 {
		out = append(out, "### Load-mode changed")
		for _, c := range d.SelectionsLoadChanged {
			out = append(out, fmt.Sprintf("- `%s`: %s → %s (score %.4f → %.4f)",
				c.Path, c.LoadModeA, c.LoadModeB, c.RelevanceA, c.RelevanceB))
		}
	}
	return out
}

func renderReachable(d *Diff) []string {
	if len(d.ReachableAdded) == 0 && len(d.ReachableRemoved) == 0 && len(d.ReachablePromoted) == 0 {
		return nil
	}
	var out []string
	if len(d.ReachableAdded) > 0 {
		out = append(out, "### Added")
		for _, r := range d.ReachableAdded {
			out = append(out, fmt.Sprintf("- `%s` (score=%.4f, reason=%s)", r.Path, r.RelevanceScore, r.Reason))
		}
	}
	if len(d.ReachableRemoved) > 0 {
		out = append(out, "### Removed")
		for _, r := range d.ReachableRemoved {
			out = append(out, fmt.Sprintf("- `%s` (score=%.4f, reason=%s)", r.Path, r.RelevanceScore, r.Reason))
		}
	}
	if len(d.ReachablePromoted) > 0 {
		out = append(out, "### Promoted to selections")
		for _, p := range d.ReachablePromoted {
			out = append(out, fmt.Sprintf("- `%s`", p))
		}
	}
	return out
}

func renderGaps(d *Diff) []string {
	if len(d.GapsAdded) == 0 && len(d.GapsResolved) == 0 && len(d.GapsSeverityChanged) == 0 {
		return nil
	}
	var out []string
	if len(d.GapsAdded) > 0 {
		out = append(out, "### Added")
		for _, g := range d.GapsAdded {
			out = append(out, fmt.Sprintf("- %s (%s)", g.Type, g.Severity))
		}
	}
	if len(d.GapsResolved) > 0 {
		out = append(out, "### Resolved")
		for _, g := range d.GapsResolved {
			out = append(out, fmt.Sprintf("- %s (%s)", g.Type, g.Severity))
		}
	}
	if len(d.GapsSeverityChanged) > 0 {
		out = append(out, "### Severity changed")
		for _, c := range d.GapsSeverityChanged {
			out = append(out, fmt.Sprintf("- %s: %s → %s", c.Type, c.SeverityA, c.SeverityB))
		}
	}
	return out
}

func renderFeasibility(d *Diff) []string {
	if d.FeasibilityScoreA == d.FeasibilityScoreB && len(d.FeasibilityDeltas) == 0 {
		return nil
	}
	out := []string{
		fmt.Sprintf("- score: %.4f → %.4f", d.FeasibilityScoreA, d.FeasibilityScoreB),
	}
	if len(d.FeasibilityDeltas) > 0 {
		out = append(out, "### Sub-signal deltas")
		for _, s := range d.FeasibilityDeltas {
			out = append(out, fmt.Sprintf("- %s: %.4f → %.4f", s.Name, s.ValueA, s.ValueB))
		}
	}
	return out
}

func renderGeneration(d *Diff) []string {
	if d.ApertureVersionA == d.ApertureVersionB && d.SelectionLogicA == d.SelectionLogicB {
		return nil
	}
	var out []string
	if d.ApertureVersionA != d.ApertureVersionB {
		out = append(out, fmt.Sprintf("- aperture_version: `%s` → `%s`", d.ApertureVersionA, d.ApertureVersionB))
	}
	if d.SelectionLogicA != d.SelectionLogicB {
		out = append(out, fmt.Sprintf("- selection_logic_version: `%s` → `%s`", d.SelectionLogicA, d.SelectionLogicB))
	}
	return out
}

func joinCode(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = "`" + s + "`"
	}
	return strings.Join(q, ", ")
}
