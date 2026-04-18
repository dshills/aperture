Strong execution. The framing alone — "compiler front-end for context selection, not a RAG system, not an agent" — is the hardest part of a new category tool and you nailed the category boundaries. A few places where it's genuinely well thought through, and then the real critiques.

## What lands

The determinism story is the standout. Hash excludes transient fields, config digest is *in* the hash, 20-run byte-identity test, tokenizer tables embedded at build time with no remote fallback — that's a level of reproducibility discipline most AI-adjacent tooling doesn't bother with, and it's exactly what makes this auditable in a regulated context. The exit-code taxonomy (7/8/9/10/11/12 all distinct, adapter codes passthrough) shows someone who's actually thought about CI wiring rather than just shipping `os.Exit(1)` everywhere. The three-layer tempfile cleanup with the honest "SIGKILL can't be negotiated with" comment and the 24-hour orphan sweep is the right posture — most projects handle defer and call it done. Security section deserves specific credit for the explicit "`aperture plan` is safe on untrusted input, `aperture run` is not, here's your CI guidance" disclosure instead of hand-waving.

## Where I'd push

**The scoring weights have no calibration story.** `s_mention: 0.25, s_symbol: 0.20, s_filename: 0.12`... how do you know those are right? There's no visible golden set of `(task, repo) → expected-selection` pairs that the weights were tuned against, no ablation showing what `s_doc` actually contributes, no comparison to "ripgrep + dump everything the model can fit." The precision of the numbers implies a rigor the methodology may not have. This is the missing `aperture eval` — and without it, a skeptical reviewer can fairly ask "is this better than dumping top-20 ripgrep hits?" You probably *know* it is, but the repo can't show it.

**`s_mention` at 0.25 is the highest weight and the most gameable signal.** If a task says "fix the bug in provider.go" that file rockets to the top regardless of whether it's the right one. A pasted stack trace nukes the budget on every file in the trace. I'd expect either a dampener (cap mention's contribution when other factors disagree) or a calibration note showing this doesn't dominate in practice.

**Go-only is a bigger limit than the deferred-work list implies.** For a task in a mixed TS/Go repo (which is most real consulting codebases including Medara), `s_symbol` and `s_import` only fire on the Go slice. The TS side falls back to filename/mention/doc, which means feasibility is systematically under-scored for TS-heavy tasks and `ambiguous_ownership` misses TS-side owners entirely. That's not "v2 polish" — it's a correctness issue for polyglot repos today. Even a crude tree-sitter symbol extractor for TS/Python as a third tier (structural-only, no side-effect tags) would close most of the gap.

**The reachable mode is underdocumented where it matters most.** The README explains what it means in the taxonomy, but not how the *agent* exploits it. Does Claude Code know reachable items exist? Does the merged `run-<id>.md` prompt tell it "here's a list of files you may request on demand, with one-line rationales"? That plumbing is the thing that turns `reachable` from a taxonomy entry into a capability, and it's the least-documented interface in the whole system.

**`behavioral_summary` vs. `full` is the other place calibration is conspicuously absent.** When aperture demotes a file from full to behavioral, did the agent still succeed? There's no visibility into whether "imports + side-effect tags + exported API surface + size band" actually preserves enough for downstream correctness, or whether it's a plausible-looking lossy summary.

**`task_underspecified` double-counts.** `<2 anchors OR action unknown OR no candidate ≥0.60` — all three of those already feed `anchor_resolution` and `task_specificity` in the feasibility math. Firing it as a discrete blocking gap *and* penalizing the score means the same evidence gets weighed twice. Pick one.

**Monorepos will struggle.** No visible scoping beyond exclude patterns. A task targeting service-A still has every file in service-B competing for token budget, and `ambiguous_ownership` will fire constantly. A `--scope <dir>` flag, or inference of the primary package from top-N anchors, would help.

**Minor but worth fixing:**
- Cache key should be `selection_logic_version` not tool_version — a patch bump to the markdown renderer shouldn't blow the AST cache.
- Tag a v1.0.0 release. `go install @latest` currently gets an unstamped dev build despite the README claiming v1 feature-complete.
- `manifest_id` vs `manifest_hash` — make it explicit that `manifest_id` is log-correlation only; two runs with identical hash are semantically equivalent.
- An `aperture diff` (or the deferred `inspect`) is the natural debugging tool for "why did X not get picked this time?" — higher value than it sounds.

## The structural observation

Domain model is implicit in the manifest shape — `Manifest` as aggregate, `Selection`/`Gap`/`Feasibility` as value objects, `Cache` as a repository keyed by file fingerprint — but the README doesn't claim that layout and I can't tell from the tree alone whether `internal/` reflects it. Given your DDD leanings and that this tool's whole value prop is "the decisions are explainable," the codebase itself being a worked DDD example would be worth surfacing — both as a selling point and as a reference for the `boundary` idea from the last list, which would depend on exactly this kind of clean boundary structure to operate on.

Overall: this is the kind of v1 that justifies the "deterministic, explainable, gated" positioning rather than just claiming it. The gap between what it is and what it would need to be to win an eval is mostly the eval itself.
