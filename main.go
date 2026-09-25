// Command gh-attention is a gh extension that lists open pull requests needing
// your attention: approved but unmerged, pending reviewer approval, unresolved
// feedback, and failing CI. A PR appears in every section it qualifies for.
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

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/term"
	flag "github.com/spf13/pflag"
)

const usage = `List open pull requests that need your attention.

USAGE
  gh attention [flags]

FLAGS
%s
EXAMPLES
  gh attention
  gh attention -o octo-org -r
  gh attention -s failing,feedback
  gh attention --json | jq '.failing[].url'
`

type options struct {
	orgs, repos   []string
	author        string
	reviews       bool
	sections      []string
	includeDrafts bool
	json          bool
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
	opts, err := parseFlags(args)
	if err != nil {
		return err
	}

	client, err := api.DefaultGraphQLClient()
	if err != nil {
		return err
	}

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

	if opts.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(categorized)
	}

	t := term.FromEnv()
	return renderer{w: t.Out(), color: t.IsColorEnabled(), now: time.Now()}.render(opts.sections, categorized)
}

func parseFlags(args []string) (options, error) {
	var opts options
	var noDrafts bool

	fs := flag.NewFlagSet("gh attention", flag.ContinueOnError)
	fs.SortFlags = false
	fs.StringArrayVarP(&opts.orgs, "org", "o", nil, "Only PRs in this `ORG` (repeatable)")
	fs.StringArrayVarP(&opts.repos, "repo", "R", nil, "Only PRs in this `OWNER/REPO` (repeatable)")
	fs.StringVarP(&opts.author, "author", "a", "@me", "Inspect PRs authored by `USER`")
	fs.BoolVarP(&opts.reviews, "reviews", "r", false, "Also list PRs where your review is requested")
	fs.StringSliceVarP(&opts.sections, "section", "s", nil,
		"Comma-separated `SECTIONS` to show: "+strings.Join(allSections, ","))
	fs.BoolVar(&noDrafts, "no-drafts", false, "Exclude draft PRs")
	fs.BoolVar(&opts.json, "json", false, "Print categorized JSON instead of text")
	fs.Usage = func() { fmt.Fprintf(os.Stderr, usage, fs.FlagUsages()) }

	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	opts.includeDrafts = !noDrafts

	if len(opts.sections) == 0 {
		opts.sections = []string{sectionApproved, sectionReview, sectionFeedback, sectionFailing}
		if opts.reviews {
			opts.sections = append(opts.sections, sectionRequested)
		}
	} else {
		for _, s := range opts.sections {
			if !slices.Contains(allSections, s) {
				return opts, fmt.Errorf("unknown section %q (want one of %s)", s, strings.Join(allSections, ", "))
			}
		}
		// Asking for the requested section implies fetching it.
		if slices.Contains(opts.sections, sectionRequested) {
			opts.reviews = true
		}
		// Display in canonical order, not flag order.
		want := opts.sections
		opts.sections = slices.DeleteFunc(slices.Clone(allSections), func(s string) bool { return !slices.Contains(want, s) })
	}
	return opts, nil
}
