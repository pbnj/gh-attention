// Command gh-attention is a gh extension that lists open pull requests needing
// your attention: your PRs that are approved but unmerged, pending reviewer
// approval, carrying unresolved feedback or failing CI, and other people's PRs
// awaiting your review. A PR appears in every section it qualifies for.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/cli/go-gh/v2/pkg/text"
	flag "github.com/spf13/pflag"
)

const usage = `List open pull requests that need your attention.

USAGE
  gh attention [flags]

FLAGS
%s
Inside a git repository with a GitHub remote, only that repository's PRs are
listed; elsewhere, PRs across every repository. -o, -R and --all override this.

Defaults and rules are read from ~/.config/gh-attention/config.yaml
($XDG_CONFIG_HOME), then the checkout's .github/attention.yaml. Flags win.
Rule actions only run with --apply; without it they are listed.

EXAMPLES
  gh attention
  gh attention --all
  gh attention -o octo-org
  gh attention -s failing,feedback
  gh attention --json | jq '.failing[].url'
  gh attention --apply
`

type options struct {
	orgs, repos   []string
	all           bool
	author        string
	reviews       bool // derived: the requested section is shown
	sections      []string
	includeDrafts bool
	json          bool
	configPath    string
	apply         bool
	rules         []rule
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "gh attention:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, fs, err := parseFlags(args)
	if err != nil {
		return err
	}
	cfg, err := loadConfigs(opts.configPath, defaultConfigEnv)
	if err != nil {
		return err
	}
	if opts, err = resolve(opts, fs.Changed, cfg); err != nil {
		return err
	}

	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return err
	}

	opts.repos = defaultRepos(opts, currentRepo)

	scope := []string{"is:pr", "is:open", "archived:false"}
	for _, o := range opts.orgs {
		scope = append(scope, "org:"+o)
	}
	for _, r := range opts.repos {
		scope = append(scope, "repo:"+r)
	}
	base := strings.Join(scope, " ")

	f := newFetcher(client)

	// Run the authored and review-requested searches concurrently.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		prs []prNode
		err error
	}
	search := func(q string) <-chan result {
		ch := make(chan result, 1)
		go func() {
			prs, err := f.searchPRs(ctx, q)
			ch <- result{prs, err}
		}()
		return ch
	}

	authoredCh := search(base + " author:" + opts.author)
	var requestedCh <-chan result
	if opts.reviews {
		requestedCh = search(base + " review-requested:@me")
	}

	authored := <-authoredCh
	if authored.err != nil {
		return authored.err
	}
	var requested []prNode
	if requestedCh != nil {
		res := <-requestedCh
		if res.err != nil {
			return res.err
		}
		requested = res.prs
	}

	categorized := categorize(authored.prs, requested, opts.includeDrafts, opts.sections)
	plan := planActions(opts.sections, categorized, opts.rules)

	var results []actionResult
	if opts.apply && len(plan) > 0 {
		rest, err := api.DefaultRESTClient()
		if err != nil {
			return err
		}
		results = execute(ctx, githubActor{f: f, rest: rest}, plan)
	} else {
		for _, p := range plan {
			results = append(results, actionResult{plannedAction: p})
		}
	}

	t := term.FromEnv()
	r := renderer{w: t.Out(), color: t.IsColorEnabled(), now: time.Now()}
	if opts.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(categorized); err != nil {
			return err
		}
		// Keep stdout valid JSON: report actions on stderr.
		r.w = t.ErrOut()
	} else if err := r.render(opts.sections, categorized); err != nil {
		return err
	}
	if err := r.renderActions(results, opts.apply); err != nil {
		return err
	}

	failed := 0
	for _, res := range results {
		if res.Err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%s failed", text.Pluralize(failed, "action"))
	}
	return nil
}

// parseFlags parses the command line. Settings are only final after resolve
// merges in the config files.
func parseFlags(args []string) (options, *flag.FlagSet, error) {
	var opts options
	var noDrafts bool

	fs := flag.NewFlagSet("gh attention", flag.ContinueOnError)
	fs.SortFlags = false
	fs.StringArrayVarP(&opts.orgs, "org", "o", nil, "Only PRs in this `ORG` (repeatable)")
	fs.StringArrayVarP(&opts.repos, "repo", "R", nil, "Only PRs in this `OWNER/REPO` (repeatable)")
	fs.BoolVarP(&opts.all, "all", "A", false, "Search every repository, even inside a git repository")
	fs.StringVarP(&opts.author, "author", "a", "@me", "Inspect PRs authored by `USER`")
	fs.StringSliceVarP(&opts.sections, "section", "s", nil,
		"Comma-separated `SECTIONS` to show: "+strings.Join(allSections, ","))
	fs.BoolVar(&noDrafts, "no-drafts", false, "Exclude draft PRs")
	fs.BoolVar(&opts.json, "json", false, "Print categorized JSON instead of text")
	fs.StringVar(&opts.configPath, "config", "", "Read config only from `PATH`")
	fs.BoolVar(&opts.apply, "apply", false, "Run the config rules' actions; without it, only list them")
	fs.Usage = func() { fmt.Fprintf(os.Stderr, usage, fs.FlagUsages()) }

	if err := fs.Parse(args); err != nil {
		return opts, fs, err
	}
	if fs.NArg() > 0 {
		return opts, fs, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	opts.includeDrafts = !noDrafts
	return opts, fs, nil
}

// resolve fills every setting not given as a flag from cfg, then validates
// and normalizes the result.
func resolve(opts options, changed func(string) bool, cfg config) (options, error) {
	// Scope settings go together: any scope flag replaces the config's whole scope.
	if !changed("org") && !changed("repo") && !changed("all") {
		opts.orgs, opts.repos = cfg.Orgs, cfg.Repos
		if cfg.All != nil {
			opts.all = *cfg.All
		}
	}
	if !changed("section") && cfg.Sections != nil {
		opts.sections = slices.Clone(cfg.Sections)
	}
	if !changed("author") && cfg.Author != nil {
		opts.author = *cfg.Author
	}
	if !changed("no-drafts") && cfg.Drafts != nil {
		opts.includeDrafts = *cfg.Drafts
	}
	opts.rules = cfg.Rules

	if opts.all && (len(opts.orgs) > 0 || len(opts.repos) > 0) {
		return opts, errors.New("--all can't be combined with --org or --repo")
	}

	if len(opts.sections) == 0 {
		opts.sections = slices.Clone(allSections)
	} else {
		for _, s := range opts.sections {
			if !slices.Contains(allSections, s) {
				return opts, fmt.Errorf("unknown section %q (want one of %s)", s, strings.Join(allSections, ", "))
			}
		}
		// Display in canonical order, not flag order.
		want := opts.sections
		opts.sections = slices.DeleteFunc(slices.Clone(allSections), func(s string) bool { return !slices.Contains(want, s) })
	}
	// Search for review requests only when their section is shown.
	opts.reviews = slices.Contains(opts.sections, sectionRequested)
	return opts, nil
}

// defaultRepos scopes the search to the current repository when no scope was
// given. Outside a git repository, or one without a GitHub remote, current
// fails and the search spans every repository.
func defaultRepos(opts options, current func() (string, error)) []string {
	if opts.all || len(opts.orgs) > 0 || len(opts.repos) > 0 {
		return opts.repos
	}
	r, err := current()
	if err != nil {
		return nil
	}
	return []string{r}
}

// currentRepo asks gh for the current repository rather than using go-gh's
// repository.Current, which ignores scp-style remotes without a user (e.g. an
// SSH alias like "work:owner/repo"). gh also honors GH_REPO and set-default.
func currentRepo() (string, error) {
	stdout, _, err := gh.Exec("repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}
