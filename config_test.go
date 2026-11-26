package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const botRule = `
rules:
  - name: bot-deps
    section: requested
    match:
      authors: [dependabot, snyk-bot]
    actions: [auto-merge, approve, approve-workflows]
`

func TestParseConfig(t *testing.T) {
	c, err := parseConfig([]byte("sections: [feedback, failing]\ndrafts: false\n"+botRule), "cfg")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Sections, []string{"feedback", "failing"}) || c.Drafts == nil || *c.Drafts {
		t.Errorf("got sections=%v drafts=%v", c.Sections, c.Drafts)
	}
	if c.Author != nil || c.Orgs != nil {
		t.Error("unset settings should stay unset")
	}
	r := c.Rules[0]
	if r.Name != "bot-deps" || r.MergeMethod != "squash" || r.source != "cfg" {
		t.Errorf("got rule %+v", r)
	}

	if _, err := parseConfig(nil, "empty"); err != nil {
		t.Errorf("empty file: %v", err)
	}
}

func TestParseConfigErrors(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown key":      "sectons: [failing]",
		"unknown section":  "sections: [bogus]",
		"all with orgs":    "all: true\norgs: [octo-org]",
		"unknown action":   "rules: [{section: requested, match: {authors: [a]}, actions: [merge]}]",
		"no actions":       "rules: [{section: requested, match: {authors: [a]}}]",
		"approve own PRs":  "rules: [{section: approved, actions: [approve]}]",
		"requested no who": "rules: [{section: requested, actions: [auto-merge]}]",
		"merge method":     "rules: [{section: approved, actions: [auto-merge], merge_method: fast}]",
		"bad glob":         "rules: [{section: requested, match: {authors: ['[']}, actions: [approve]}]",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseConfig([]byte(raw), "my.yaml")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.HasPrefix(err.Error(), "my.yaml: ") {
				t.Errorf("error %q should name the file", err)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	base := config{Sections: []string{"failing"}, Orgs: []string{"octo-org"}, Author: ptr("me"), Rules: []rule{{Name: "a"}}}
	over := config{Repos: []string{"octo-org/api"}, Rules: []rule{{Name: "b"}}}
	got := merge(base, over)
	if !slices.Equal(got.Sections, []string{"failing"}) || *got.Author != "me" {
		t.Errorf("unset settings should fall through: %+v", got)
	}
	if got.Orgs != nil || !slices.Equal(got.Repos, []string{"octo-org/api"}) {
		t.Errorf("scope should be replaced whole: orgs=%v repos=%v", got.Orgs, got.Repos)
	}
	if len(got.Rules) != 2 || got.Rules[0].Name != "a" || got.Rules[1].Name != "b" {
		t.Errorf("rules = %+v, want a then b", got.Rules)
	}
}

func writeFile(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testEnv(home, checkout string, vars map[string]string) configEnv {
	return configEnv{
		getenv: func(k string) string { return vars[k] },
		home:   func() (string, error) { return home, nil },
		toplevel: func() (string, error) {
			if checkout == "" {
				return "", errors.New("not a git repository")
			}
			return checkout, nil
		},
		currentRepo: func() (string, error) { return "octo-org/api", nil },
	}
}

func TestLoadConfigs(t *testing.T) {
	home, checkout := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(home, ".config/gh-attention/config.yaml"), "sections: [failing]\nauthor: me\n"+botRule)
	writeFile(t, filepath.Join(checkout, ".github/attention.yaml"), "sections: [feedback]\n"+
		"rules: [{name: local, section: requested, match: {authors: [x], repos: ['*']}, actions: [approve]}]")

	t.Run("user and repo files", func(t *testing.T) {
		c, err := loadConfigs("", testEnv(home, checkout, nil))
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(c.Sections, []string{"feedback"}) || *c.Author != "me" {
			t.Errorf("got sections=%v author=%v", c.Sections, c.Author)
		}
		if len(c.Rules) != 2 {
			t.Fatalf("rules = %+v", c.Rules)
		}
		if c.Rules[0].source != "~/.config/gh-attention/config.yaml" || c.Rules[0].Match.Repos != nil {
			t.Errorf("user rule = %+v", c.Rules[0])
		}
		local := c.Rules[1]
		if local.source != repoConfigFile || !slices.Equal(local.Match.Repos, []string{"octo-org/api"}) {
			t.Errorf("repo rule should be pinned to its repo: %+v", local)
		}
	})

	t.Run("XDG_CONFIG_HOME", func(t *testing.T) {
		xdg := t.TempDir()
		writeFile(t, filepath.Join(xdg, "gh-attention/config.yaml"), "author: xdg")
		c, err := loadConfigs("", testEnv(home, "", map[string]string{"XDG_CONFIG_HOME": xdg}))
		if err != nil || *c.Author != "xdg" {
			t.Errorf("got %+v, %v", c, err)
		}
	})

	t.Run("no files", func(t *testing.T) {
		c, err := loadConfigs("", testEnv(t.TempDir(), "", nil))
		if err != nil || c.Sections != nil || c.Rules != nil {
			t.Errorf("got %+v, %v", c, err)
		}
	})

	t.Run("explicit path is the only file and must exist", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "only.yaml")
		writeFile(t, p, "author: explicit")
		for _, env := range []configEnv{
			testEnv(home, checkout, nil),
			testEnv(home, checkout, map[string]string{"GH_ATTENTION_CONFIG": p}),
		} {
			explicit := p
			if env.getenv("GH_ATTENTION_CONFIG") != "" {
				explicit = ""
			}
			c, err := loadConfigs(explicit, env)
			if err != nil || *c.Author != "explicit" || c.Sections != nil {
				t.Errorf("got %+v, %v", c, err)
			}
		}
		if _, err := loadConfigs(filepath.Join(t.TempDir(), "missing.yaml"), testEnv(home, checkout, nil)); err == nil {
			t.Error("a missing explicit config should be an error")
		}
	})
}

func TestRepoConfigCantWidenScope(t *testing.T) {
	home, checkout := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(home, ".config/gh-attention/config.yaml"), botRule)
	for _, scope := range []string{"all: true", "orgs: [other]", "repos: [other/repo]"} {
		writeFile(t, filepath.Join(checkout, ".github/attention.yaml"), scope)
		if _, err := loadConfigs("", testEnv(home, checkout, nil)); err == nil {
			t.Errorf("repo file with %q should be rejected", scope)
		}
	}
}

func TestRuleNameControlCharacters(t *testing.T) {
	raw := "rules: [{name: \"evil\\e[2J\", section: approved, actions: [auto-merge]}]"
	if _, err := parseConfig([]byte(raw), "cfg"); err == nil {
		t.Error("a rule name with an escape sequence should be rejected")
	}
}
