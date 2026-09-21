<!-- GENERATED from claude-standards/ALTERION.md in AlterionAI/infra — do not edit here; overwritten by the sync-claude-standards workflow. -->

<!-- Canonical source of org-wide engineering standards for AI coding agents.
     Synced into each repo as .claude/alterion-standards.md by
     .github/workflows/sync-claude-standards.yaml in AlterionAI/infra.
     Edit ONLY this file; synced copies are overwritten. -->

# Alterion Engineering Standards (org-wide)

These rules apply to every Alterion repo that imports this file. Repo-specific
conventions live in each repo's own CLAUDE.md and take precedence on conflict.
When in doubt, orion is the reference implementation — copy what orion does.

## Communicating with the requester

These rules govern every user-visible message — chat replies, reports, "PR is ready" notices, background-job updates.

1. **Language a 14-year-old engineer can follow**, while staying accurate. Plain words before technical terms; introduce a term only if the reader needs it. Where accuracy forces complexity, open that passage with **Complex idea:** in bold and write the whole passage in *italics*, so the reader slows down. Do not fence it with marker lines.
2. **Delineate what the requester must decide or do.** End every user-visible message with a `**Decisions needed:**` and/or `**Actions for you:**` block listing them one per line — omit the block entirely when there are none. Never bury a decision or action item mid-paragraph.
3. **Minimize update text.** Status updates are 1–2 sentences leading with the outcome; give conclusions, not reasoning or alternatives. If the requester wants detail they'll ask. (Background-job completion signals such as `result:` / `needs input:` / `failed:` lines are still required — brevity never drops the completion signal.)
4. **Every link is clickable.** Any mention of a ticket, PR, issue, doc, or URL is a markdown link — a bare identifier is never enough:
   - Linear: `[ALT-XXXX](https://linear.app/alterion/issue/ALT-XXXX)`, plus a few words saying what the ticket is, unless the surrounding text already made that clear.
   - GitHub: `[repo#N](https://github.com/AlterionAI/<repo>/pull/N)`; bare `#N` refers to the current repo; non-AlterionAI orgs get the full `owner/repo#N` URL; issues use `/issues/N`.

## Minimize output tokens

Chat output is token-constrained. Keep every response as short as it can be while staying accurate — cut fluff, not substance.

- **Lead with the answer.** No greetings, pleasantries, conversational filler, preamble, or restating the question.
- **Conclusions, not reasoning.** Don't explain your thought process, give step-by-step reasoning, or survey alternatives unless explicitly asked.
- **Code answers are code.** When generating or fixing code, output the code changes and where they land — not a prose walkthrough of them.
- **Accuracy always wins over brevity.** Never drop a caveat, a failing test, or a skipped step to save tokens.
- PR bodies, docs, commit messages, and agent reports keep the format their own sections below define.

## Use the right model to give high ROI

- Say which model is doing what as you go. The main session orchestrates, architects, and judges output; implementer agents make all code edits plus their immediate tests; a senior reviewer agent does review/hardening passes.
- Overlap review with the next implementation: start PR N+1's implementer while PR N is under review, accepting occasional rework on shared surfaces.
- **Subagent reports follow the [agent-report convention](https://github.com/AlterionAI/orion/blob/main/scripts/claude/README.md#agent-report-convention):** a `VERDICT:` line, ≤~15 lines of key facts, and a pointer to a detail file — never a narrative dump back to the orchestrator.
- **Don't restate standing instructions in spawn prompts.** Anything true of every task an agent runs (codebase-memory-mcp usage, repo conventions, the report format) belongs in that agent's definition; the prompt carries only this task's scope, files, and acceptance criteria.
- **Never send mid-run status messages** to peers or the parent — they arrive late and double-bill tokens. One consolidated message at idle or completion.
- **Mechanical work** (renames, boilerplate, broad/repeated searches, formatting, running an already-written test suite) → the cheapest model tier, not the implementer or reviewer tier. Reserve the implementer for actual implementation and the reviewer for review/hardening — don't reach for the expensive tier on work that doesn't need judgment.
- **Spawn independent agents in one message when their work doesn't depend on each other**, each named so it's addressable and each given a complete, self-contained prompt — wait for them to report back rather than polling.

## PR workflow

1. **Branch:** `<type>/alt-<ticket>`, lowercase, tied to the Linear ticket number (not Linear's descriptive slug) — e.g. `fix/alt-2757`, `feat/alt-2702`.
2. **Commits & PR title:** Conventional Commits — `<type>(scope): subject`; types `feat|fix|docs|refactor|perf|test|chore|ci`.
3. **Never add `Co-Authored-By` lines** to commits.
4. **PR body:** Summary (what & why, 1–2 sentences) + checklist: CI passed; every CodeRabbit thread closed per step 10 — critical/major fixed or dismissed with adversarial-reviewer concurrence, minor/nit replied to and resolved; self-reviewed the diff; tests added/updated; docs updated. Include a staging test plan.
5. **Labels:** every PR gets the GitHub label `fix` (default) or `feat` (new capability only).
6. **After opening:** move the Linear ticket to "In Review".
7. **Review flow:** post the PR to Slack **#eng-reviews** as `[PR url] [@reviewer] [1 sentence on why it's needed]`. CodeRabbit reviews every **ready** PR — it does not review drafts, so a PR left in draft never accrues a review at all (see §Multi-agent coordination item 3). Close its critical/major findings before merge per step 10 — fixed, or dismissed with recorded adversarial-reviewer concurrence.
8. **Monitor until green:** watch CI checks AND late CodeRabbit findings (they lag CI); fix problems right away and report clearly when the PR is ready to merge. Never merge a PR unless explicitly asked to.
9. **While remediating, put it back in draft.** The moment CodeRabbit or CI turns up a finding that needs more than a trivial one-line fix, convert the PR back to draft immediately. Only mark it "ready for review" again once the fix is actually pushed and verified. CodeRabbit doesn't review drafts, so this isn't just a status flag — flipping back to ready re-triggers a fresh CodeRabbit pass against the fixed code, closing the loop properly. Draft is the one machine-readable "not actually done yet" signal: a reviewer or a merge button can't tell "green checks, fully fixed" apart from "green checks, fix still uncommitted" any other way, and a PR merged in that second state ships the unfixed version with no warning. **And never mark a PR ready for review if you did not put it into draft yourself.** Read its comments first. A draft you did not create is almost always someone else's hold on that PR, and in these repos it is the *only* hold they have: GitHub will not let an account review a pull request it authored, and effectively every PR here is authored by the same account, so a blocking `REQUEST_CHANGES` review is not available. If the comments do not explain the draft, ask whoever drafted it before flipping it. Be aware too that draft is a signal rather than enforcement — a pr-deck authorization is bound to the head SHA, and converting to draft does not change that SHA, so a ticked PR stays mergeable while it sits in draft. When you find a real defect in someone else's PR and cannot reach them, getting any commit pushed to the branch is the move that actually holds it.
10. **Resolve CodeRabbit threads yourself — never leave them dangling.** Every CodeRabbit thread ends in one of two ways, and you do both without asking:
    - **You fixed it:** reply with what changed (one line, commit SHA), then resolve the thread.
    - **You think CodeRabbit is wrong:** don't resolve on your own judgment. Get a second opinion from an adversarial reviewer — the senior-reviewer agent or a second model — with the finding and your reasoning. If it agrees with you, reply with the reasoning (and the reviewer's concurrence) and resolve the thread. If it sides with CodeRabbit, fix the finding per the bullet above. If it's unsure, fix it — a disputed critical/major finding is never resolved unfixed.
    The reply is the audit trail: a resolved thread with no reply is indistinguishable from one dismissed by hand, so always reply before resolving. Minor/nit threads follow the same two paths but never block merge (see Merge flow below).

## CI every PR must pass

- **gitleaks** secret scan — the universal gate in all repos.
- **lint + typecheck + tests** on `pull_request` (tool names vary by repo).
- Larger service repos add a Trivy dependency scan, Cosign image signing, and a Syft SBOM.

## Standard workflow for fixing a problem

The default sequence for any bug fix or problem report. Follow it in order unless the requester explicitly says otherwise:

1. **Create a Linear ticket** for the work first; its number names the branch (`fix/alt-<ticket>`).
2. **Write a failing test** that reproduces the problem and will run in CI — in the repo's normal test framework and location, so the `pull_request` test job picks it up automatically (e.g. Vitest in orion, pytest in orion-mlops).
3. **Confirm the test fails** against the unfixed code, and say so explicitly.
4. **Implement the fix, using the model and effort level that will achieve high quality at low cost.**
5. **Confirm the fix makes the test pass**, then run every locally runnable check from the repo's CI gate before pushing — lint + typecheck + full test suite, plus a gitleaks scan where available. Checks that only run in CI (Trivy/Cosign/Syft image pipelines) are covered by the monitoring in step 7.
6. **Use codebase-memory-mcp and qmd as much as possible** throughout — the code graph (`search_graph`, `trace_path`, `get_code_snippet`, …) for code structure, qmd for docs/prose — before falling back to raw grep/file reading. Pass this instruction on to every subagent.
7. **Mark the draft PR ready for review** — the one opened at the start of work (see Multi-agent coordination); if none exists yet, open it now per the PR workflow above. Then **monitor it until every check passes** — CI green *and* CodeRabbit critical/major findings closed per PR workflow step 10 — fixing problems right away as they surface.
8. **Report the PR ready for review** only after everything above is green.

## New-feature workflow (design-first)

The sequence above covers bug fixes. For a **new feature** (net-new capability, not a fix), replace its steps 2–5 (failing test → confirm → fix → confirm) with the sequence below, in order unless the requester says otherwise. Everything outside those steps still applies unchanged: Linear ticket and branch naming first, the claim and scope-check in Multi-agent coordination, a draft PR opened at the start of work, the full local CI gate before pushing, and monitoring to green including CodeRabbit critical/major findings.

1. Clarify scope with the requester until the problem and solution are unambiguous.
2. Write the design in a `DESIGN.md` (or the PR description for small features).
3. Get an adversarial design review — a senior-reviewer pass, or a second model — before implementing; fold its findings back into the design.
4. Implement per "Use the right model to give high ROI" above, using as many independently-spawnable agents as the work genuinely parallelizes into.
5. Build out unit/integration/e2e coverage as part of implementation (not after) — thorough, but optimized so the suite stays fast at every level.
6. Close with an adversarial review pass on the whole change, optimizing for simplicity, performance, maintainability, and reliability — including the test suite itself.

## Multi-agent coordination

Many engineers run coding agents in the same repos. Collisions and duplicated effort are prevented by deriving "who is touching what" from systems every agent already reads — never by authoring a second source of truth. The rule: **never write coordination state that git or Linear already knows.**

1. **Claim before the first edit.** Assign yourself the Linear ticket and move it to "In Progress" *before* writing any code. If it's already claimed, coordinate instead of starting — this is the duplicate-effort check. **Comment the same owner token on the ticket as you claim it — `Owner: <workstream>/<session>`, the value item 3 puts in the PR body.** Assignee alone cannot separate two sessions running under one person, and there is a window between claiming a ticket and opening the draft PR where nothing else can either. So: a ticket already "In Progress" under your own identity, with no comment naming *this* session, belongs to another session — stand down per item 6 rather than assuming it is yours.
2. **Check for overlap before the first edit.** Compare your full *intended* working set — every file you plan to touch, not just the first commit — against the files touched by all open PRs, re-run the check whenever the working set grows, and run it once more immediately before pushing or marking the PR ready — both the working set and the open-PR list drift between kickoff and push. In orion, run `scripts/claude/scope-check.sh [paths…]` (skill `/scope-check`) — the reference implementation. In repos without the script, sweep open PRs' changed files with `gh` at the same three points: before the first edit, whenever the working set grows, and immediately before pushing or marking the PR ready. **This answers "will these collide in merge?", not "is this mine to touch?"** Two disjoint file sets can belong to one person working one cluster of related PRs — a clean overlap result is never authorization. Ownership is item 1 and item 6; check both.
3. **Open a draft PR at the start of work, not the end.** Push the branch with the first commit and open the draft immediately, so your file scope is visible to everyone else's overlap checks from day one. When implementation is complete, mark that same draft ready for review (step 7 of the standard workflow below) — don't open a second PR. **Name the owner in the PR body as its first line: `Owner: <workstream>/<session> · worktree: <path>`.** Within one person's identity every agent session commits as the same author, so `git log --format=%an` and `gh pr list --author @me` cannot tell two sessions apart. The ticket comment from item 1 is the earliest session-specific record; this line is the record from PR creation onward, where anyone checking a branch will actually look. It costs one line, lives beside the branch, dies with it, and is queryable (`gh pr view <n> --json body`). `<workstream>` is a stable name so the value means the same thing across repos and weeks — `perf-and-load-tests`, `aquila`, `orion-gateway`, `draco`, or a short descriptive name for one-off work. **`<session>` must be unique among sessions running concurrently in that workstream** — a shared token distinguishes nothing, which is the whole failure being fixed. `<path>` is the worktree the session works in, so item 6's reflog check has a target.

   **A draft is a promise to flip it ready, and CodeRabbit does not review drafts** — so a draft nobody flips is waiting for a review that can never arrive, and the wait is invisible. Before a session ends or goes idle, every draft PR it opened must be left in exactly one of two states: **flipped ready**, or **carrying a comment saying what is still missing and who owns the next step**. A session that has stopped running is not "still working on it". Measured 2026-08-31 across all AlterionAI repos: 22 open drafts, every one already human-approved, median age ~63h, worst case 154h. And the wait is genuinely invisible from the outside: when CodeRabbit reviews a PR and finds nothing it posts no review object and no inline comment, only a summary comment — so "has CodeRabbit reviewed this?" cannot be answered by counting reviews, and a draft that was never reviewed looks exactly like one that was reviewed and cleared (see [ALT-6095](https://linear.app/alterion/issue/ALT-6095) for the correct way to tell them apart).

   **Never flip a draft ready automatically on "green + approved".** The flip is the author's claim that their work is finished, and an approved, green PR may still be deliberately held — an ordering constraint, or a dependency that must land first. Automate the noticing, never the claim: a green, approved draft older than 4h with **no documented hold** is a deadlock, and whoever is shepherding reports it to the requester by name instead of flipping it. One that does carry a documented hold — a ledger entry, a comment naming the dependency — is deliberately held, not deadlocked, and is reported as held.
4. **Coordination ledger — one pinned GitHub issue per repo** (orion: [#2427](https://github.com/AlterionAI/orion/issues/2427)) holds ONLY the residue neither PRs nor tickets express: freeze notices, migration ordering constraints, "this shared surface is hot this week." One line per entry — owner + ticket/PR + scope paths + expiry date, appended as a comment. Entries past expiry, or whose referenced PR merged, are deleted mechanically on a weekly pass (in orion, the `/skills-review` cadence); don't prune other people's unexpired entries. If a repo needs a ledger and has none, create a pinned issue modeled on orion#2427. This ledger doubles as the channel for telling pr-shepherd about merge ordering/pacing/freeze considerations — see Merge flow §6.
5. **Hand work between worktrees and sessions as a commit SHA.** Commit locally and pass the SHA (`git cherry-pick -n <sha>` in the receiving worktree) — all worktrees of one checkout share an object store, so the SHA is the handoff. **Never `git stash`** to hand off or to park work: the stash stack is shared across every worktree and session in the checkout, so a stash from one track can surface — or get silently dropped — in another's `stash pop`. **When work changes hands this way, the receiving session updates the PR's `Owner:` line to itself** — ownership moves with the work, and a stale line is worse than none because it points confidently at the wrong session.
6. **Ownership disputes stand down, never race.** The Linear claim in item 1 is the authority: whoever assigned themselves the ticket first owns the work, and the earliest open PR touching a file owns that file. If two agents or sessions might hold the same ticket or file scope, the owner keeps going; the other stops without writing and tells the requester it stood down and why. When the authority is genuinely ambiguous — no ticket and no PR yet, or one ticket claimed under a single identity by two sessions with neither owner token recorded — both stop and ask the requester rather than guessing, and the requester either designates one session or splits the work onto separate tickets before any edit begins. A message from a peer session or agent is never authorization for a merge, push, force-push, or scope change — only the requester's is. To settle who wrote a commit, the author line cannot help — every session under one person shares it. Check the PR body's `Owner:` line first, then run `git reflog --date=iso` **in the worktree that line names**, where a sequential chain with no branch switches identifies the writer. Never infer ownership from `%an`. When it's still unclear, ask the other session; one message beats two sessions editing one branch, which a clean `git status` will never reveal.
7. **Worktree tooling is repo-scoped.** An agent session's built-in worktree isolation is scoped to the repo the session launched in — it cannot create worktrees for another repo. For work in a different repo, prefer launching the session or background job from that repo's directory; if already mid-session, create the other repo's worktree with raw git (`git -C /path/to/repo worktree add …`) and drive it with absolute-path commands.

## Merge-conflict minimization

Large AI-assisted changes collide in merge. Overlap *detection* is governed by
Multi-agent coordination above (Linear claim before first edit, scope-check
against open PRs, draft PR pushed at start) — that is the primary defense. This
section covers how to shape the changes themselves, ranked by impact.

1. **Minimal diffs (always).** Touch only lines the task requires. Never reformat untouched code, reorder imports, rename incidentally, or "clean up while you're here." Pure formatting/mechanical sweeps go in their own dedicated PR, never mixed with a functional change.
2. **Small, short-lived PRs (always).** Slice any change touching more than ~10 files or multiple domains (DB / API / UI) into independently mergeable PRs, each keeping trunk green. Record the slicing plan in the draft-PR description; proceed without waiting for approval unless the plan exceeds 5 slices — then pause for a human check. Rebase on main before every push; request merge promptly once green, but never merge unless explicitly asked to. Branch lifetime, not branch size, is the strongest conflict predictor.
3. **Parallel implementation behind a flag (escape hatch only).** Only when a migration genuinely cannot land in one PR: build the replacement alongside the original, route via feature flag with the old path as default. Non-negotiable at flag-flip time: a Linear ticket to delete the old path and remove the flag, with an owner and target date — no v2 without a v1 teardown ticket. If the requester explicitly asks for an in-place replacement, do that: suggest the incremental route once, then follow their call.
4. **GitOps/ArgoCD manifest hygiene.** Prefer one file per app/env over shared mega-files; in shared `values.yaml` / kustomize overlays, make additive key changes by default — narrowly scoped updates or removals are allowed for flag teardown and stale-config cleanup — and never reorder existing YAML.

## Merge flow — tick early, the shepherd merges

How ready PRs actually reach main (ruling 2026-08-20). This implements "request
merge promptly once green" above without anyone sitting on a finished PR.

1. **The shepherd is the merger.** Multi-PR merging routes through orion's pr-shepherd engine (`scripts/claude/pr-shepherd-run.sh`), which independently re-verifies every gate at the moment of merge — agents never run ad-hoc `gh pr merge`. "Never merge unless explicitly asked" stands: a pr-deck tick or an invoked shepherd run is the standing authorization for non-consequential PRs; consequential data-flow PRs always require the explicit per-PR tick.
2. **Tick early instead of waiting.** A tick means "merge once fully ready," not "ready now." Tick at push time and move on — the shepherd merges the moment CI and review are green and critical/major CodeRabbit findings (the only blocking severities) are fixed. Ticks are SHA-bound: every later push automatically invalidates the tick (the shepherd merges only the exact ticked commit), so scope added after a tick — consequential or not — always requires a fresh tick. Nobody sits watching CodeRabbit; its latency costs wall-clock on an unattended branch, not engineer attention.
3. **The CodeRabbit gate is severity-triaged and stays pre-merge for critical/major.** Under continuous deployment a merged defect is a deployed defect, and a post-merge fix costs a full extra PR cycle — so critical/major findings are always closed before merge — fixed, or dismissed with an adversarial reviewer's concurrence recorded in the thread (PR workflow step 10); merge-before-fix was considered and rejected (ALT-4990). Minor/nit findings never block; remediate them post-merge or not at all, but still close their threads per step 10.
4. **Cross-runner merge serialization is live.** `pacing.crossRunner` ([ALT-4991](https://linear.app/alterion/issue/ALT-4991), shipped 2026-08-22 as [orion#3369](https://github.com/AlterionAI/orion/pull/3369), on by default) makes main's own tip + CI state the lock: a foreign tip that isn't green, or is younger than the quiet window, holds every merge candidate for that repo until it clears — no lock file, no cross-machine registry, because coordination state git already knows is never duplicated. This is what makes multiple engineers' manual/one-shot `--execute --merge` passes safe to run concurrently today.
5. **Standing loops stay off a while longer.** Item 4 governs *safety*, not *authorization* — a per-engineer standing `/loop` runner ([ALT-4992](https://linear.app/alterion/issue/ALT-4992): FIFO ordering, CodeRabbit-latency bound) is still blocked: its flaky-triage prerequisite (orion#3330, ALT-5153) was closed unmerged rather than landed, so unfixed flakes would still spuriously stall a standing queue. Manual/one-shot engine passes are always fine. Queue build-out: ALT-4992, [ALT-4993](https://linear.app/alterion/issue/ALT-4993) (non-orion deploy verification + batch verification).
6. **Tell the shepherd about ordering, pacing, or freeze considerations via the coordination ledger.** If your work needs a merge held, sequenced, or paced against something else, append it to the repo's pinned coordination-ledger issue (Multi-agent coordination §4) using the existing entry format — that IS the "tell pr-shepherd" channel; don't invent a second one (a Slack DM, a PR-body-only note). Whoever runs pr-shepherd for that repo — a human today, the engine once [ALT-5234](https://linear.app/alterion/issue/ALT-5234) lands — reads the ledger before merging there. Scope your entry to a path the shepherd can match against a PR's changed files, or it isn't actionable.

## Cost discipline

Two sessions, profiled for ALT-8423, produced most of this section:

| Session | What it was doing | Duration | What it cost |
|---|---|---|---|
| 1 | Standing loop mixing crons, log reads, an Opus reviewer | 30 hours | ~$4,870 |
| 2 | "Get screenshots for Aquila" (job `671860da`) | 4.5 days | 17,418 Fable turns, 8.09B cache-read tokens |

No single choice in either session looked wrong at the time — a 15-minute
cron, reading a whole run log, an inbound agent status message, one more
`Monitor` call. Nine hooks, synced into every repo's `.claude/settings.json`
the same way this file syncs today (see infra's `claude-standards/hooks/`),
warn at the moment one of these choices is made:

| # | Pattern | Measured in the profiled session(s) |
|---|---|---|
| 1 | Expensive recurring loop (cron/wakeup under 45 min, or any interval on Fable/Opus) | part of session 1's $4,870/30h |
| 2 | Raw log/output dump (`gh run view --log`, unbounded `kubectl logs`/`psql`/`curl`, or an 8 KB+ Bash result) | part of session 1's $4,870/30h |
| 3 | Wrong model tier for a mechanical task (Opus/`deep-reviewer` to poll/search/list/summarize) | part of session 1's $4,870/30h |
| 4 | Idle wait that never wakes (backgrounded Bash, or "wait for"/"until it finishes") | part of session 1's $4,870/30h |
| 5 | Session cost checkpoints ($100/$250/$500/+$500) | session 1's running total |
| 7 | Agent chatter (mid-run status to `main`/`team-lead` before the final report) | session 2: ~1,460 inbound agent messages |
| 8 | Monitor/wait polling (`Monitor`, `TaskOutput` with `block`, `ScheduleWakeup`) | session 2: 531 `Monitor` calls |
| 9 | Compaction and session-age checkpoint | session 2: 21 compactions over 4.5 days |
| 10 | Task-list churn (`TaskCreate`/`TaskUpdate`) | session 2: 1,201 calls |

(#6 in the ticket's own numbering is this doc section, not a hook — numbers 1–10
above match `claude-standards/hooks/DESIGN.md`'s hook numbering exactly.)

1. **An expensive recurring loop.** Scheduling a recurring wakeup under 45
   minutes, or at any interval while the session is running Fable or Opus,
   warns; a second one in the same session denies. Use the **cheap-loop
   runner** in internal-tools (`cheap-loop/README.md`, landing as
   [internal-tools#163](https://github.com/AlterionAI/internal-tools/pull/163))
   for the check, and page a full session only when it finds something new.
2. **A raw log/output dump.** A command known to print a large blob with no
   mitigation already applied (`gh run view --log`, `kubectl logs` with no
   `--tail`, `cat`/`less` on a `.log` file, `psql` with no `LIMIT`, `curl`
   with neither `-s` nor `-o`) warns before it runs; a Bash result over 8 KB
   warns once after. Write it to a file under the job tmp dir and read only
   the lines you need.
3. **The wrong model tier for the task.** Spawning `deep-reviewer` or an Opus
   agent to poll, watch, wait, search, list, or summarize — or spawning any
   agent with no model tier for a task that reads as mechanical — warns.
   Haiku for scouting, polling and log summaries; Sonnet to implement; Opus
   only to review a finished diff.
4. **An idle wait that never wakes.** A backgrounded Bash call, or a subagent
   prompt that says "wait for" or "until it finishes," warns: a subagent
   waiting on a background job never wakes on its own. Poll in the
   foreground with a bounded loop, or hand the wait to cheap-loop.
5. **Session cost checkpoints.** With the optional per-machine bridge
   installed (`claude-standards/hooks/install-local.sh`, run once per
   machine), a session is reminded at $100, $250, $500, and every $500
   after: split long-running work into a cheap-loop job and short sessions
   instead of running one session indefinitely.
7. **Agent chatter.** A subagent sending a status message to its parent
   (`main` or `team-lead`) before its final report warns once, then denies a
   second one in the same agent — that wakes the parent and makes it
   re-read its whole context. Send one consolidated report (starting with
   `VERDICT:`) at completion instead.
8. **Monitor/wait polling.** Repeatedly re-arming a `Monitor` call,
   `TaskOutput` with `block`, or `ScheduleWakeup` instead of one long-lived
   watch warns on every call past the 10th in a session — each wake
   re-reads the whole context. Hand the wait to cheap-loop, or use one
   `Monitor` call with an until-condition.
9. **Compaction and session-age checkpoint.** A session that has compacted
   3 times, or whose transcript is over 48 hours old, is warned once: its
   context is being re-created over and over, or it's been running far too
   long. Finish the current item, write a handoff, and start fresh.
10. **Task-list churn.** Past 200 `TaskCreate`/`TaskUpdate` calls in a
    session, a one-time warning: each call is a full model turn, and the
    task list is not a log.

None of these block real work except the two explicit second-offence cases
(#1 and #7) — the rest print a short warning and a link to the right tool.
To silence all nine for one session: `touch ~/.claude/cost-guard-off`. Full
design: infra's `claude-standards/hooks/DESIGN.md`.

## Repos that do NOT follow this standard

`codebase-memory-mcp` follows its upstream OSS conventions (DCO, CodeQL, upstream maintainers) — do not apply this document there.

`orion-gateway` DOES follow this standard (joined 2026-09-18 — it is part of the Alterion platform and holds to the same bar), with upstream-fork overrides recorded in its own CLAUDE.md that take precedence on conflict: DCO sign-off on every commit, CodeQL, and its fork-specific build/format mechanics (never bare `cargo fmt`; `make gen` for schema regeneration).
