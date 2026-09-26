# Contributing to PlumeBot

This is a short guide on how to contribute to PlumeBot. For the Chinese version see
[CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md).

Before you start, please read the project's two governing documents:

- [CLAUDE.md](CLAUDE.md) — the **development contract**. It pins the tech stack (incl. the "explicitly not used" list), layer rules, directory layout, and the principle *one task at a time*.
- [docs/roadmap.md](docs/roadmap.md) — the **task ledger**. Work is driven by phases and pending items (B ledger). Do **not** implement features scheduled for a later phase.

## Reporting a bug

If you are unsure whether it is a bug, or just have a question, open a discussion instead of an issue.

When filing an issue, include the following if possible:

- PlumeBot version (commit hash you are running, e.g. from `git log -1 --oneline`) and the config file **`config.yaml` with all `api_key` values obscured** (or at least the `llm.models` shape, without the key);
- OS and architecture (e.g. Windows 11 amd64, Linux x86_64);
- Repro steps: what you did, what you expected, what happened;
- Relevant log lines from `~/.plumebot/logs/` — each message produces exactly one *entry* line (`收到消息`) and one *outcome* line (`消息结局`); when possible attach the `info.log` entry/outcome pair for the failing message plus the surrounding `warn.log` / `error.log`. If the failure is inside one request chain, attach the `trace_id` (`group:<GroupID>` / `private:<QQ>`) so maintainers can join the full chain.

## Submitting a new feature or bug fix

Big feature? Open an issue first so it can be discussed. Small fixes and docs-only changes can go straight to a pull request.

1. **Read the contract.** Start with [CLAUDE.md](CLAUDE.md) and the [documentation home](docs/README.md). Check [roadmap](docs/roadmap.md) that the work isn't already tracked, and that it does not belong to a future phase.

2. **Fork and clone.**

   ```console
   git clone https://github.com/plumebot/plumeBot.git
   cd plumeBot
   git remote rename origin upstream
   git remote add origin git@github.com:YOURUSER/plumeBot.git
   ```

3. **Make a branch and get hacking.**

   ```console
   go build ./...
   git checkout -b my-feature
   ```

   Take a look at the [code layout](#code-layout--architecture) and follow the [layer rules](#layer-rules) below.

4. **Test what you changed** before committing — for a simple fix the package tests are enough, otherwise the [full test suite](#testing):

   ```console
   go test ./...
   ```

5. Make sure your change also includes:
   - unit tests for new behaviour,
   - documentation updates (bilingual, see [Writing documentation](#writing-documentation)),
   - a commit message following the [guidelines](#commit-messages).

6. Push and open a pull request:

   ```console
   git push -u origin my-feature
   ```

Your change will be reviewed. If you are asked to adjust, commit and push again on the same branch. You may be asked to [rebase onto latest master](#keeping-your-branch-in-sync) or [squash](#squashing-your-commits) your commits.

## AI-assisted contributions

AI coding assistants (Claude Code, Codex, Cursor, Gemini CLI, and similar) are welcome here — in fact `CLAUDE.md` is written precisely so you can point a tool at it and get idiomatic PlumeBot code. From the executing agent's viewpoint it *is* the spec: layer rules, one task at a time, no implementing ahead of the roadmap.

The same standard applies to every pull request whether or not a tool was involved: **you are responsible for the code you submit.** Before opening a PR please make sure that:

- You understand every line of the change and can explain why it is correct. If a reviewer asks, you should be able to answer without going back to the tool;
- You have actually built and run it. At a minimum `go build ./...` and `go test ./...` must pass, and for config / migration changes you should also verify the second config file stays in sync (see [config changes](#config-changes));
- The change is a genuine fix or feature you verified solves the problem, not a plausible-looking guess. Unverified, AI-generated PRs that don't compile, don't pass tests, invent non-existent APIs, or don't do what the description claims waste maintainer time and are likely to be closed;
- You have trimmed the comments. AI tools tend to add verbose comments that restate the code; match the surrounding code's commenting density instead.

In short: an AI assistant helps *you* contribute; it is not a substitute for understanding and testing your own work.

## Using Git and GitHub

### Committing your changes

```console
git checkout my-feature
git status          # see new/changed files
git add FILENAME    # stage one file at a time
git commit          # follow the commit-message guidelines
```

To rework the last commit: `git commit --amend`.

### Pushing after a rewrite

If you amended or rebased commits already pushed, replace the old ones:

```console
git push --force origin my-feature
```

Rewriting pushed history is a good moment to keep collaborators informed.

### Keeping your branch in sync

```console
git fetch upstream
git checkout main && git merge --ff-only
git checkout my-feature && git rebase main
git push --force origin my-feature
```

### Squashing your commits

```console
git reset --soft HEAD~N    # undo the last N commits, keep changes staged
git commit
```

Or `git rebase -i main` for more complex situations.

## Commit messages

Use [conventional commits](https://www.conventionalcommits.org/): a single-line subject

```text
<type>(<scope>): <summary>
```

- `type`: `feat` / `fix` / `refactor` / `docs` / `test` / `perf` / `chore`;
- `scope`: the package or domain the change is in — examples from this repo: `domain`, `onebot`, `memory`, `control`, `admin`, `ai`, `sqlite`;
- `summary`: one concise line describing *what* user-visible behaviour changed (repo convention is Chinese subjects, English is also fine).

If the change fixes an issue, add `Fixes #123` to the message.

**Do not append any `Co-Authored-By` / `Co-authored-by` or other attribution trailer** to commit messages.

The first line should stand alone well: the project's changelog is assembled from these subjects. If more context is needed, add a blank line and a body explaining *why* the change was needed, comparing behaviour before and after.

## Code layout & architecture

```
cmd/              entry point; dependency injection (only main package)
internal/
  domain/         interfaces + entities + sentinel errors (entity/errors.go); ZERO third-party imports
    entity/       shared entities (Message, GroupConfig, ...) + errors
  service/        orchestration; depends only on domain interfaces, never on infra
  handler/        thin glue; message/notice events → services
  infra/          implementations of domain interfaces (onebot, ai, sqlite, logfile)
plugin-sdk/       (remote module github.com/plumebot/plumebot-sdk) standalone plugin SDK
pkg/              reusable utilities (config, logger, jwt, ahocorasick, ...)
docs/             bilingual docs (zh/ + en/), architecture, roadmap, plans
plugins/          runtime third-party plugins (separate processes)
```

See the full layout in [CLAUDE.md §4](CLAUDE.md) and [docs/features.md §9](docs/en/features.md).

### Layer rules

1. `domain` imports **no third-party libraries** (stdlib only);
2. `service` imports **no `infra`** packages — it orchestrates against domain interfaces;
3. interfaces are defined in `domain`, implemented in `infra`; dependencies are injected via constructors in `cmd` (no globals);
4. sentinel / validation errors live in `internal/domain/entity/errors.go` — do not define ad-hoc sentinels in infra packages;
5. SQL: keep DML as package-level `const`s in each infra package's `queries.go`; DDL goes into `migrations/*.sql` (loaded via `//go:embed`), versioned via `schema_migrations` — never edit an old migration in place; add `00N_*.sql`;
6. **don't implement ahead of the roadmap**: if the feature belongs to a later phase or a deferred/B item, discuss it first.

### Logging

The logging convention (exactly one entry line + one outcome line per message, level semantics, audit rules, `trace_id`) is specified in [architecture.md §17](docs/architecture.md). New log statements should follow it; audit lines for the same fact belong in exactly one place.

## Testing

Run from the project root:

```console
go build ./...
go vet ./...
go test ./...
```

- Tests use only the Go standard library `testing` — **no testify**;
- Tests exist for most packages (see the list in [CLAUDE.md §0](CLAUDE.md)); changes should come with tests for the affected package;
- The live-LLM smoke test is gated behind `PLUMEBOT_TEST_LLM=1` (requires a real endpoint) and is not part of the default suite;
- The full end-to-end stability pass (memory/goroutine leaks, API rate) is a pending phase (`P6-003`) — not yet part of the default flow.

## Adding or updating a dependency

The allowed stack is a **closed whitelist** — see the tech stack and "explicitly not used" list in [CLAUDE.md §3](CLAUDE.md) (notably: no `stretchr/testify`, no cgo-dependent libraries, SQLite via `modernc.org/sqlite`, no external vector DB, no Redis/message queues). Confirm a candidate fits before adding it.

```console
go get github.com/example/new_dependency
```

Commit the resulting `go.mod` and `go.sum` together with your other changes, in the same commit.

## Writing documentation

- Our docs are **bilingual**: any change to `docs/zh/*` must be mirrored in `docs/en/*` (and vice versa). Keep both in the same PR;
- `docs/architecture.md` is the single source of truth for design decisions — code follows it, not the other way around;
- `docs/roadmap.md` is the task ledger: **add a row for new work, delete a row when it's done**;
- `README.md` / `README.zh-CN.md` are the **facade**: a concise pitch + links. Put details in `docs/` (e.g. the five-page site under `docs/zh|en/`);
- Plugin SDK changes are documented at the `github.com/plumebot/plumebot-sdk` repo and referenced from `docs/zh|en/plugin-dev.md`.

### Config changes

New config fields must be added in **two places** (obligation B-006): `pkg/config/config.default.yaml` (the embedded template) **and** the root `config.yaml` (your local file — remember `config.yaml` is gitignored, so only the default template is committed). Update the [configuration reference](docs/zh/configuration.md) too.

## Making a release

Not yet formalized. Releases so far are built manually (`build.bat` produces `bot.exe` / `bot-linux`); tag the repo with a plain version (no `v` prefix required) and cross-check with [roadmap](docs/roadmap.md). Versioned release mechanics can be discussed in an issue if you need them.

## License

By contributing you agree that your contributions are licensed under the [MIT license](LICENSE), consistent with the project.