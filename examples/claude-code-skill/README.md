# Claude Code skill — example integration

One example of wiring aperture into a coding-agent harness. This is **not canonical** — aperture is harness-neutral by design, and the CLI (`aperture plan`, `aperture explain`, `aperture run`) is the contract. This directory just shows how that contract is consumed from Claude Code.

## What's here

- [`SKILL.md`](./SKILL.md) — a Claude Code [skill](https://docs.claude.com/en/docs/claude-code/skills) that teaches Claude when and how to invoke aperture before it starts a coding task. References the CLI surface documented in the project's top-level `README.md` and `SPEC.md`.

## Install

Copy `SKILL.md` into your Claude Code skills directory:

```bash
mkdir -p ~/.claude/skills/aperture
cp examples/claude-code-skill/SKILL.md ~/.claude/skills/aperture/SKILL.md
```

Claude Code discovers the skill on next launch. The skill triggers on task-kickoff phrases ("let's implement this", "start coding", "run the agent") and on any explicit request to gate feasibility.

## Verifying it works

The repo ships an opt-in smoke test that drives `aperture run claude` against the live `claude` CLI:

```bash
make smoke
```

The test auto-skips when `claude` is not on `$PATH`. See `internal/cli/smoke_real_claude_test.go`.

## Keeping it in sync

The skill references specific CLI flags and exit codes. If you change the surface in `internal/cli/`, update the relevant sections of `SKILL.md`:

- Flag names / defaults → *Workflow* and *Common invocations*
- Exit-code semantics → *Exit codes*
- Gap categories → *Gap remediation*
- Config keys → *Configuration*

## Other harnesses

Any harness that can shell out to a CLI can integrate the same way — aperture's contract is a command, a set of exit codes, and a manifest JSON schema (see `schema/`). A Codex adapter, a shell pre-commit hook, or a CI gate all consume the same surface.
