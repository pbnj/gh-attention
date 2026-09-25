# gh-attention

A [GitHub CLI](https://cli.github.com/) extension that lists your open pull
requests that need your attention:

1. **Approved, not merged**
2. **Pending reviewer approval**
3. **Unresolved PR feedback**: changes requested, or unresolved review threads
4. **Failing CI checks**

Optionally, it also lists PRs where **your review is requested**.

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
check that GitHub Actions built it from this repo:

```sh
gh attestation verify ~/.local/share/gh/extensions/gh-attention/gh-attention -R pbnj/gh-attention
```

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
| `-a, --author USER`      | Inspect PRs authored by `USER` (default `@me`) |
| `-r, --reviews`          | Also list PRs where your review is requested   |
| `-s, --section SECTIONS` | Comma-separated sections to show (see below)   |
| `--no-drafts`            | Exclude draft PRs                              |
| `--json`                 | Print categorized JSON instead of text         |
| `-h, --help`             | Show help                                      |

### Examples

```sh
# Everything that needs you, across every repo you can see
gh attention

# One org, including PRs waiting on your review
gh attention -o octo-org -r

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
| 👀 Your review requested    | `requested`   | Your review is requested, directly or through a team. Shown with `-r`, or when named in `-s`.                                                                        | PR author                                                 |

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
      "failingChecks": ["lint / tflint", "plan (staging)"]
    }
  ]
}
```

`reviewDecision` and `ciState` are `null` when GitHub has no value for them,
for example in a repo with no required reviews or on a PR with no checks.

```sh
gh attention --json | jq -r '.failing[].url'
gh attention --json | jq '[.[][]] | unique_by(.url) | length'   # distinct PRs needing attention
```

## How it works

gh-attention uses GitHub's GraphQL API in two steps:

1. A search (`is:pr is:open archived:false author:<you>`, plus any `org:` and
   `repo:` qualifiers) that returns only PR ids. This is cheap.
2. The review and CI details for those ids, fetched in batches of 20, with up
   to 4 requests at a time.

The review decision and CI status are the expensive fields. GitHub computes
them per PR on every request, so asking for them in a large search page runs
into GitHub's ~10s GraphQL timeout. Batching keeps each request around 4–5s.
Requests that still time out (a 502/504, or an empty 200 response) are retried
up to 3 times with backoff.

A run takes about 6–8s for ~65 open PRs.

## Troubleshooting

| Message                                                                | Cause                                                                                                                     |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `HTTP 403: You have exceeded a secondary rate limit`                   | Too many heavy GraphQL requests in a short time, usually from running the tool repeatedly in a loop. Wait about a minute. |
| `HTTP 502` / `HTTP 504` / `unexpected end of JSON input` after retries | GitHub is timing out or degraded. Try again, or narrow the scope with `-o` or `-R`.                                       |
| `unknown section "…"`                                                  | `-s` accepts only `approved`, `review`, `feedback`, `failing` and `requested`.                                            |

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

Push a version tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

`.github/workflows/release.yml` then uses
[`cli/gh-extension-precompile`](https://github.com/cli/gh-extension-precompile)
to build a binary for every platform gh supports, attach build-provenance
attestations, and publish a GitHub release. `gh extension install` and
`gh extension upgrade` pick up the new release.

| File                 | Purpose                                                                |
| -------------------- | ---------------------------------------------------------------------- |
| `main.go`            | Flag parsing and the concurrent authored and review-requested searches |
| `github.go`          | GraphQL queries, batching, concurrency limit and retries               |
| `categorize.go`      | Flattening PRs and the rules that place them in sections               |
| `render.go`          | Terminal output                                                        |
| `categorize_test.go` | Section rules, failing-check detection, flags and truncation           |

## License

[MIT](LICENSE)
