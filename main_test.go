package main

import (
	"errors"
	"slices"
	"testing"
)

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
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%q) = nil error, want conflict", args)
		}
	}
}
