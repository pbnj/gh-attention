package main

import (
	"errors"
	"slices"
	"testing"
)

// parseArgs parses args and resolves them against cfg, as run does.
func parseArgs(t *testing.T, cfg config, args ...string) (options, error) {
	t.Helper()
	opts, fs, err := parseFlags(args)
	if err != nil {
		return opts, err
	}
	return resolve(opts, fs.Changed, cfg)
}

func TestDefaultRepos(t *testing.T) {
	inRepo := func() (string, error) { return "octo-org/api", nil }
	outside := func() (string, error) { return "", errors.New("not a git repository") }

	tests := []struct {
		name    string
		opts    options
		current func() (string, error)
		want    []string
	}{
		{name: "inside a repo", current: inRepo, want: []string{"octo-org/api"}},
		{name: "outside a repo", current: outside, want: nil},
		{name: "--all", opts: options{all: true}, current: inRepo, want: nil},
		{name: "--repo wins", opts: options{repos: []string{"octo-org/web"}}, current: inRepo, want: []string{"octo-org/web"}},
		{name: "--org wins", opts: options{orgs: []string{"octo-org"}}, current: inRepo, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultRepos(tt.opts, tt.current); !slices.Equal(got, tt.want) {
				t.Errorf("defaultRepos() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseFlagsAllConflicts(t *testing.T) {
	for _, args := range [][]string{{"--all", "-R", "octo-org/api"}, {"-A", "-o", "octo-org"}} {
		if _, err := parseArgs(t, config{}, args...); err == nil {
			t.Errorf("parseArgs(%q) = nil error, want conflict", args)
		}
	}
}

func TestRequestedSection(t *testing.T) {
	for _, tt := range []struct {
		name        string
		cfg         config
		args        []string
		wantReviews bool
	}{
		{name: "shown by default", wantReviews: true},
		{name: "-s without requested skips the search", args: []string{"-s", "failing"}},
		{name: "-s with requested", args: []string{"-s", "failing,requested"}, wantReviews: true},
		{name: "config sections without requested", cfg: config{Sections: []string{"failing"}}},
		{name: "config sections with requested", cfg: config{Sections: []string{"requested"}}, wantReviews: true},
		{name: "-s beats config sections", cfg: config{Sections: []string{"failing"}}, args: []string{"-s", "requested"}, wantReviews: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseArgs(t, tt.cfg, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if opts.reviews != tt.wantReviews || slices.Contains(opts.sections, sectionRequested) != tt.wantReviews {
				t.Errorf("reviews = %v, sections = %v, want reviews %v", opts.reviews, opts.sections, tt.wantReviews)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestResolveConfig(t *testing.T) {
	cfg := config{
		Sections: []string{sectionFailing, sectionFeedback},
		Orgs:     []string{"octo-org"},
		Author:   ptr("octocat"),
		Drafts:   ptr(false),
	}

	t.Run("config fills unset flags", func(t *testing.T) {
		opts, err := parseArgs(t, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{sectionFeedback, sectionFailing}; !slices.Equal(opts.sections, want) {
			t.Errorf("sections = %v, want %v", opts.sections, want)
		}
		if !slices.Equal(opts.orgs, []string{"octo-org"}) || opts.author != "octocat" || opts.includeDrafts {
			t.Errorf("got orgs=%v author=%q drafts=%v", opts.orgs, opts.author, opts.includeDrafts)
		}
	})

	t.Run("flags override config", func(t *testing.T) {
		opts, err := parseArgs(t, cfg, "-s", "approved", "-a", "@me")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(opts.sections, []string{sectionApproved}) || opts.author != "@me" {
			t.Errorf("got sections=%v author=%q", opts.sections, opts.author)
		}
	})

	t.Run("a scope flag replaces the whole config scope", func(t *testing.T) {
		opts, err := parseArgs(t, cfg, "-R", "other/repo")
		if err != nil {
			t.Fatal(err)
		}
		if opts.orgs != nil || !slices.Equal(opts.repos, []string{"other/repo"}) {
			t.Errorf("got orgs=%v repos=%v", opts.orgs, opts.repos)
		}
		if _, err := parseArgs(t, cfg, "--all"); err != nil {
			t.Errorf("--all with config orgs: %v", err)
		}
	})
}

func TestRemovedReviewsOptions(t *testing.T) {
	if _, err := parseArgs(t, config{}, "-r"); err == nil {
		t.Error("-r should no longer be accepted")
	}
	if _, err := parseConfig([]byte("reviews: true"), "cfg"); err == nil {
		t.Error("the reviews config key should no longer be accepted")
	}
}
