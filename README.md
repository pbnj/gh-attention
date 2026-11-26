# gh-attention

A [GitHub CLI](https://cli.github.com/) extension that lists the open pull
requests that need your attention:

1. **Approved, not merged**: your PRs that are ready to merge
2. **Pending reviewer approval**: your PRs waiting on reviewers
3. **Unresolved PR feedback**: changes requested, or unresolved review threads
   on your PRs
4. **Failing CI checks**: your PRs with failing checks
5. **Your review requested**: other people's PRs waiting on your review

```text
✔ Approved, not merged (1)
  octo-org/api#482   feat: add cursor pagination to the /v2/orders endpoint        3h    by monalisa
    https://github.com/octo-org/api/pull/482

◷ Pending reviewer approval (2)
  octo-org/web#1290  fix: debounce search input so every keystroke stops refetch…  1d    waiting on hubot, frontend
    https://github.com/octo-org/web/pull/1290
  octo-org/infra#77  chore: bump terraform aws provider to 6.x                     4d    no reviewer requested
    https://github.com/octo-org/infra/pull/77

✎ Unresolved PR feedback (1)
  octo-org/api#482   feat: add cursor pagination to the /v2/orders endpoint        3h    2 unresolved threads
    https://github.com/octo-org/api/pull/482

✘ Failing CI checks (1)
  octo-org/infra#77  chore: bump terraform aws provider to 6.x                     4d    lint / tflint, plan (staging)
    https://github.com/octo-org/infra/pull/77

👀 Your review requested (1)
  octo-org/api#490   fix: retry idempotent order writes on 409                     2h    from hubot
    https://github.com/octo-org/api/pull/490
```

## Requirements

- [`gh`](https://cli.github.com/), logged in (`gh auth login`). The extension
  reuses gh's credentials and host.
- [mise](https://mise.jdx.dev/) (recommended), which installs Go and the dev
  tools pinned in `mise.toml` (betterleaks, golangci-lint, govulncheck,
  oxfmt, markdownlint). Or [Go](https://go.dev/dl/) 1.25 or newer, to
  build without mise.

## Install

```sh
gh extension install pbnj/gh-attention
```

This downloads the prebuilt binary for your platform from the latest
[release](https://github.com/pbnj/gh-attention/releases). Upgrade with
`gh extension upgrade attention`.

Each release binary carries a signed build-provenance attestation, so you can
check that GitHub Actions built it from this repo. Verify the asset for your
platform (`darwin-arm64`, `linux-amd64`, `windows-amd64.exe`, …) as downloaded:

```sh
gh release download -R pbnj/gh-attention -p darwin-arm64 -D /tmp
gh attestation verify /tmp/darwin-arm64 -R pbnj/gh-attention
```

Verify the download, not the installed copy. On macOS the installed binary is
re-signed during install, so its hash no longer matches the attestation and
verification fails with `HTTP 404`.

### From source

With mise:

```sh
git clone https://github.com/pbnj/gh-attention.git
cd gh-attention
mise install        # Go, Node and the dev tools
mise run install    # build, then install the checkout as a gh extension
```

Without mise:

```sh
go build -o gh-attention .
gh extension install .
```

`gh extension install .` symlinks the directory into gh's extensions folder, so
it runs whatever `gh-attention` binary is in the checkout. After pulling
changes, rebuild with `mise run build` (or `go build -o gh-attention .`); you
don't need to reinstall. `mise run install` is safe to re-run.

To uninstall:

```sh
gh extension remove attention
```

## Usage

```text
gh attention [flags]
```

| Flag                     | Description                                    |
| ------------------------ | ---------------------------------------------- |
| `-o, --org ORG`          | Only PRs in this organization (repeatable)     |
| `-R, --repo OWNER/REPO`  | Only PRs in this repository (repeatable)       |
| `-A, --all`              | Every repository, even inside a git repository |
| `-a, --author USER`      | Inspect PRs authored by `USER` (default `@me`) |
| `-s, --section SECTIONS` | Comma-separated sections to show (see below)   |
| `--no-drafts`            | Exclude draft PRs                              |
| `--json`                 | Print categorized JSON instead of text         |
| `--config PATH`          | Read settings only from this file              |
| `--apply`                | Run the actions in your config's rules         |
| `-h, --help`             | Show help                                      |

Run inside a git repository with a GitHub remote, `gh attention` lists only
that repository's PRs. Anywhere else, it lists PRs across every repository you
can see. `-o`, `-R` and `--all` override this. The current repository is the one
`gh repo view` reports, so `gh repo set-default` applies.

Default flags, and rules that act on PRs, can be set in a
[config file](#configuration).

### Examples

```sh
# What needs you in the current repo (inside a checkout), or everywhere (outside one)
gh attention

# Everything that needs you, across every repo you can see, even inside a checkout
gh attention --all

# One org
gh attention -o octo-org

# Only your own PRs, without the PRs waiting on your review
gh attention -s approved,review,feedback,failing

# Just one repo
gh attention -R octo-org/api

# Only the things you can fix right now
gh attention -s failing,feedback

# Someone else's PRs
gh attention -a octocat
```

## Sections

A PR is listed in **every** section it qualifies for. For example, an approved
PR with an unresolved thread appears under both _Approved_ and _Feedback_.
Each section is sorted by most recently updated first.

| Section                     | Name for `-s` | A PR is listed when…                                                                                                                                                 | Detail column                                             |
| --------------------------- | ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| ✔ Approved, not merged      | `approved`    | GitHub reports it approved. In repos that don't require reviews, it also counts when at least one reviewer approved and none requested changes. Drafts are excluded. | Approvers, plus `conflicts`, `CI failing` or `CI pending` |
| ◷ Pending reviewer approval | `review`      | Review is required or reviewers are requested, it isn't approved, and no one requested changes. Drafts are excluded.                                                 | Pending reviewers and teams                               |
| ✎ Unresolved PR feedback    | `feedback`    | Changes are requested, or it has at least one unresolved review thread.                                                                                              | Who requested changes; unresolved thread count            |
| ✘ Failing CI checks         | `failing`     | The latest commit's combined check status is failing or errored.                                                                                                     | Names of the failing checks                               |
| 👀 Your review requested    | `requested`   | Your review is requested, directly or through a team. Hidden only when `-s` leaves it out.                                                                           | PR author                                                 |

Drafts are included by default, marked `[draft]`; `--no-drafts` hides them.

## JSON output

`--json` prints an object keyed by section name. Each PR has these fields:

```json
{
  "failing": [
    {
      "repo": "octo-org/infra",
      "number": 77,
      "title": "chore: bump terraform aws provider to 6.x",
      "url": "https://github.com/octo-org/infra/pull/77",
      "draft": false,
      "author": "octocat",
      "updatedAt": "2026-09-21T13:02:11Z",
      "reviewDecision": "REVIEW_REQUIRED",
      "conflicting": false,
      "approved": false,
      "approvedBy": [],
      "changesRequestedBy": [],
      "pendingReviewers": [],
      "unresolvedThreads": 0,
      "ciState": "FAILURE",
      "failingChecks": ["lint / tflint", "plan (staging)"],
      "autoMerge": false,
      "rules": [],
      "actions": []
    }
  ]
}
```

`reviewDecision` and `ciState` are `null` when GitHub has no value for them,
for example in a repo with no required reviews or on a PR with no checks.
`rules` names the [config rules](#rules) that matched the PR in that section,
and `actions` lists what they run on it. With `--json`, the rule actions report
goes to stderr, so stdout stays valid JSON.

```sh
gh attention --json | jq -r '.failing[].url'
gh attention --json | jq '[.[][]] | unique_by(.url) | length'   # distinct PRs needing attention
```

## Configuration

gh-attention reads YAML settings from these files, each layered over the one
before. Command-line flags override them all.

1. `~/.config/gh-attention/config.yaml`, or
   `$XDG_CONFIG_HOME/gh-attention/config.yaml` when that variable is set.
2. Inside a git checkout, the repository's `.github/attention.yaml`.

`--config PATH` or `GH_ATTENTION_CONFIG=PATH` replaces both: only that file is
read, and it must exist. Missing files are otherwise skipped, and an unknown key
is an error, so typos don't go unnoticed.

Every key is optional:

| Key        | Same as                  | Example               |
| ---------- | ------------------------ | --------------------- |
| `sections` | `-s`                     | `[feedback, failing]` |
| `orgs`     | `-o`                     | `[octo-org]`          |
| `repos`    | `-R`                     | `[octo-org/api]`      |
| `all`      | `--all`                  | `true`                |
| `author`   | `-a`                     | `"@me"`               |
| `drafts`   | `--no-drafts` (inverted) | `false`               |
| `rules`    |                          | See [Rules](#rules)   |

`orgs`, `repos` and `all` are the search scope and are replaced together: a
scope flag such as `-R` replaces the whole configured scope. They can only be
set in your own config file, not in a repository's `.github/attention.yaml`.
A configured scope works like the flags, so it applies even inside a checkout.

To see only the problems you can fix yourself, everywhere:

```yaml
# ~/.config/gh-attention/config.yaml
sections: [feedback, failing]
```

### Rules

A rule picks PRs in one section and names actions to run on them. Actions run
only with `--apply`. Without it, gh-attention lists what it would do under
**⚙ Rule actions (dry run)**.

```yaml
rules:
  - name: dependency-bots
    section: requested
    match:
      authors: [dependabot, snyk-bot]
      repos: ["octo-org/*"]
    actions: [approve-workflows, approve, auto-merge]
    merge_method: squash
```

```sh
gh attention            # lists the matching PRs and the actions it would run
gh attention --apply    # runs them
```

| Key             | Meaning                                                                                                                         |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `name`          | Shown next to matching PRs and in the actions report. Defaults to `rule N`.                                                     |
| `section`       | The section whose PRs the rule looks at. Rules only see sections that are shown, so `-s failing` skips a `requested` rule.      |
| `match.authors` | PR author globs, case-insensitive. `dependabot`, `app/dependabot` and `dependabot[bot]` are the same. Required for `requested`. |
| `match.repos`   | Optional `OWNER/REPO` globs, such as `octo-org/*`.                                                                              |
| `match.titles`  | Optional PR title globs, case-insensitive. `*` matches any text, including `/`, and `?` one character: `chore(deps): bump *`.   |
| `actions`       | Any of `approve-workflows`, `approve` and `auto-merge`. They run in that order, whatever order you list them in.                |
| `merge_method`  | `merge`, `squash` (default) or `rebase`, used by `auto-merge`.                                                                  |

| Action              | What it does                                                                                                     | Skipped when             |
| ------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------------ |
| `approve-workflows` | Approves the head commit's workflow runs that are waiting for approval (`action_required`), so CI runs.          | No run is waiting        |
| `approve`           | Submits an approving review. Only allowed in `requested`, because GitHub doesn't let you approve your own PRs.   | You've already approved  |
| `auto-merge`        | Enables auto-merge, so GitHub merges the PR once its requirements are met. The repository must allow auto-merge. | Auto-merge is already on |

Draft PRs never match a rule. When several rules name the same action for a PR,
it runs once. A failed action doesn't stop the others; gh-attention exits with
status 1 after trying them all.

Rules in a repository's `.github/attention.yaml` only apply to that repository:
their `match.repos` is replaced with the current repository, so a file someone
commits can't act on PRs elsewhere. The file also can't set `orgs`, `repos` or
`all`, which would widen the search your own rules run over. The actions report
names the file each rule came from.

## How it works

gh-attention uses GitHub's GraphQL API in two steps:

1. Searches that return only PR ids, which is cheap. One finds your PRs
   (`is:pr is:open archived:false author:<you>`, plus any `org:` and `repo:`
   qualifiers, or the current repository). When the `requested` section is
   shown, a second search with `review-requested:@me` runs alongside it.
2. The review and CI details for those ids, fetched in batches of 20, with up
   to 4 requests at a time across both searches.

The review decision and CI status are the expensive fields. GitHub computes
them per PR on every request, so asking for them in a large search page runs
into GitHub's ~10s GraphQL timeout. Batching keeps each request around 4–5s.
Requests that still time out (a 502/504, or an empty 200 response) are retried
up to 3 times with backoff.

A run takes about 6–8s for ~65 open PRs. Leaving `requested` out of `-s` skips
the second search.

With `--apply`, rule actions run after the PRs are categorized, one PR at a
time: workflow runs through the REST API (`GET …/actions/runs`, then
`POST …/runs/{id}/approve`), reviews and auto-merge through the GraphQL
mutations `addPullRequestReview` and `enablePullRequestAutoMerge`. Mutations
aren't retried, because a request that timed out may still have gone through.

## Troubleshooting

| Message                                                                | Cause                                                                                                                                     |
| ---------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `HTTP 403: You have exceeded a secondary rate limit`                   | Too many heavy GraphQL requests in a short time, usually from running the tool repeatedly in a loop. Wait about a minute.                 |
| `HTTP 502` / `HTTP 504` / `unexpected end of JSON input` after retries | GitHub is timing out or degraded. Try again, or narrow the scope with `-o` or `-R`.                                                       |
| `unknown section "…"`                                                  | `-s` accepts only `approved`, `review`, `feedback`, `failing` and `requested`.                                                            |
| `unknown shorthand flag: 'r'` / `unknown flag: --reviews`              | `-r` was removed: review requests are shown by default. Drop the flag, and use `-s` to hide them.                                         |
| `config.yaml: …` or `.github/attention.yaml: …`                        | The config file has an unknown key or an invalid value. The message names the key or rule. `reviews:` was removed; use `sections:`.       |
| `.github/attention.yaml: orgs, repos and all can only be set in…`      | A repository's file can't change the search scope. Move the setting to your own config file, or pass `-o`, `-R` or `--all`.               |
| `✘ auto-merge …: Auto merge is not allowed for this repository`        | Turn on **Allow auto-merge** in the repository's settings, or remove `auto-merge` from the rule for that repository.                      |
| `✘ approve-workflows …: HTTP 403` / `HTTP 404`                         | Your token needs write access to Actions in that repository, or the run can't be approved (only some runs, such as ones from forks, can). |

## Development

Tools and tasks are defined in `mise.toml`. Run `mise tasks` to list them.

| Task                     | What it does                                                                                                          |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| `mise run test`          | Unit tests (`go test ./...`)                                                                                          |
| `mise run build`         | Build `./gh-attention`; skipped when no Go source changed                                                             |
| `mise run install`       | Build, then install the checkout as a gh extension                                                                    |
| `mise run secrets`       | betterleaks scan of the working tree and git history                                                                  |
| `mise run keywords`      | Check files and file names for forbidden keywords                                                                     |
| `mise run lint`          | Both linters below                                                                                                    |
| `mise run lint:go`       | golangci-lint: `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `gosec` and `gofmt` (see `.golangci.yml`) |
| `mise run lint:docs`     | markdownlint, plus an oxfmt check that Markdown is formatted                                                          |
| `mise run fmt`           | Both formatters below                                                                                                 |
| `mise run fmt:go`        | Format Go with `gofmt` (via `golangci-lint fmt`)                                                                      |
| `mise run fmt:docs`      | Format Markdown with oxfmt, then apply markdownlint's fixes                                                           |
| `mise run vuln`          | govulncheck: known vulnerabilities in dependencies and the Go toolchain that the code actually calls                  |
| `mise run pre-commit`    | Every pre-commit check, against the staged changes                                                                    |
| `mise run hooks:install` | Install the git pre-commit hook                                                                                       |

### Pre-commit hook

After cloning, set up the keyword list and install the hook once:

```sh
printf 'my-employer\n' > .forbidden-keywords
mise run hooks:install
```

Every `git commit` then runs `mise run pre-commit`, which blocks the commit if:

1. **betterleaks** finds a secret in the staged changes.
2. A staged file's contents or name contains a keyword from
   `.forbidden-keywords`: one per line, case-insensitive, `#` for comments.
   The file is gitignored on purpose, because committing it would publish the
   words it exists to keep out. If it's missing, the check fails. An empty
   file turns the check off.
3. **golangci-lint** reports a lint or security issue in the Go code. This
   includes gosec's static security analysis.
4. **markdownlint** reports a problem in a Markdown file, or **oxfmt** finds
   one that isn't formatted. Run `mise run fmt` to fix formatting.

Checks 3 and 4 read the working tree, so unstaged edits are checked too.

`mise run vuln` isn't part of the hook because it needs network access to
Go's vulnerability database. Run it before a release or on a schedule.

To bypass the hook in an emergency, run `git commit --no-verify`.

### CI

`.github/workflows/ci.yml` runs `lint`, `test`, `vuln` and `secrets` on every
push to `main` and every pull request, using the tool versions pinned in
`mise.toml`. The keyword check runs only locally, because its blocklist is
never committed.

### Releasing

Releases are cut by
[release-please](https://github.com/googleapis/release-please). Every push to
`main` opens or updates a release PR that bumps the version from the
Conventional Commit types since the last release and updates `CHANGELOG.md`:

| Commit type                                  | Bump                  |
| -------------------------------------------- | --------------------- |
| `feat`                                       | minor (0.1.0 → 0.2.0) |
| `fix`, `perf`, `revert`                      | patch (0.1.0 → 0.1.1) |
| `!` after the type, or `BREAKING CHANGE:`    | major (minor pre-1.0) |
| `docs`, `chore`, `ci`, `test`, `refactor`, … | none on their own     |

Merging the release PR tags the commit and creates the GitHub release.
`.github/workflows/release.yml` then uses
[`cli/gh-extension-precompile`](https://github.com/cli/gh-extension-precompile)
to build a binary for every platform gh supports, attach build-provenance
attestations, and upload them to the release. `gh extension install` and
`gh extension upgrade` pick up the new release.

The release PR is opened with `GITHUB_TOKEN`, so CI doesn't run on it. It only
touches `CHANGELOG.md` and `.release-please-manifest.json`.

To force a specific version, add a `Release-As: 1.0.0` line to a commit
message body on `main`.

| File            | Purpose                                                              |
| --------------- | -------------------------------------------------------------------- |
| `main.go`       | Flag parsing, merging config with flags, and the concurrent searches |
| `config.go`     | Loading, validating and layering the config files                    |
| `github.go`     | GraphQL queries, batching, concurrency limit and retries             |
| `categorize.go` | Flattening PRs and the rules that place them in sections             |
| `rules.go`      | Matching config rules to PRs and planning and running their actions  |
| `actions.go`    | The GitHub calls behind rule actions                                 |
| `render.go`     | Terminal output                                                      |
| `*_test.go`     | Unit tests for each of the above                                     |

## License

[MIT](LICENSE)
