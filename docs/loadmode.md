# Load-mode calibration: `aperture eval loadmode`

`aperture eval loadmode` is the v1.1 §7.5.1 empirical gate on
the `behavioral_summary` vs `full` demotion rule. For each
fixture the command runs the planner twice — once normally
(Plan_A) and once with every full-eligible candidate pinned at
`full` regardless of budget (Plan_B, "forced-full") — then
reports what changed.

## Symbolic differences (always reported)

Every fixture produces the structural delta:

- **Demoted in A, held at full in B** — files that the §7.5.0
  demotion rule downgraded from full in the normal plan but
  that Plan_B kept at full. Each entry carries its Plan_A
  score and token count.
- **Tokens gained by forcing** — Plan_B's
  `estimated_selected_tokens` minus Plan_A's. Always ≥ 0.
- **Feasibility delta** — Plan_B feasibility score minus
  Plan_A's. Positive means forcing helped.
- **Gaps fired in A only / B only** — gap types that
  appeared in one plan but not the other as a consequence of
  the load-mode forcing.
- **`ForcedFullWouldUnderflow`** — boolean data point.
  Plan_B tolerates overflow and records the excess on
  `BudgetOverflowTokens` rather than raising an error.

## Agent-check deltas (when declared)

A fixture may declare `agent_check` in its YAML:

```yaml
agent_check:
  command: scripts/check.sh
  timeout: 30s
```

The script receives the full §7.1.1 env-var contract:

- `APERTURE_MANIFEST_PATH`
- `APERTURE_MANIFEST_MARKDOWN_PATH`
- `APERTURE_PROMPT_PATH`
- `APERTURE_TASK_PATH`
- `APERTURE_REPO_ROOT`
- `APERTURE_MANIFEST_HASH`
- `APERTURE_VERSION`

**Nothing else propagates from the parent environment** apart
from `PATH`. Credential-bearing variables (`GITHUB_TOKEN`,
`AWS_*`, etc.) are explicitly stripped.

Exit-0 = pass; any non-zero = fail; timeout exceeded = fail;
command-not-found = abort the whole eval run (exit 1).

The harness runs the script twice per fixture (once per plan
variant) and classifies the pair:

- `IMPROVEMENT` — Plan_A failed, Plan_B passed
- `REGRESSION` — Plan_A passed, Plan_B failed
- `NO_CHANGE_PASS` — both passed
- `NO_CHANGE_FAIL` — both failed

## §7.5.2 threshold advisor

After every fixture with a declared `agent_check`, the command
aggregates pass rates and emits **one advisory line** when
Plan_B's rate is at least 10 percentage points above Plan_A's.
The recommendation suggests raising the §7.5.0 `avg_size_kb`
threshold by 25% — **advisory only**, never auto-applied.
Adjusting the threshold remains a release-gate decision that
requires human review and a reviewer-issued `aperture eval
baseline`.

## Ctrl+C and timeouts

Per-fixture timeouts are enforced by SIGKILLing the
subprocess's entire process group (so shell-spawned `sleep` or
`find` grandchildren don't hold pipes open). A 5-second grace
period caps the worst case if the child is in a D-state. Ctrl+C
on the CLI cancels in-flight subprocesses immediately and
propagates as an `AgentCheckCanceled` outcome that aborts the
run.
