package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// repoConfigFile is the repo file, relative to the checkout root, that is
// merged over the user's config when run inside a checkout.
const repoConfigFile = ".github/attention.yaml"

var mergeMethods = []string{"merge", "squash", "rebase"}

// config is one config file. Pointer and nil-slice fields are unset, so a
// file that doesn't mention a setting leaves the one below it in place.
type config struct {
	Sections []string `yaml:"sections"`
	Orgs     []string `yaml:"orgs"`
	Repos    []string `yaml:"repos"`
	All      *bool    `yaml:"all"`
	Author   *string  `yaml:"author"`
	Drafts   *bool    `yaml:"drafts"`
	Rules    []rule   `yaml:"rules"`
}

// rule runs actions on PRs in one section that match its filters.
type rule struct {
	Name    string `yaml:"name"`
	Section string `yaml:"section"`
	Match   struct {
		Authors []string `yaml:"authors"`
		Repos   []string `yaml:"repos"`
	} `yaml:"match"`
	Actions     []string `yaml:"actions"`
	MergeMethod string   `yaml:"merge_method"`

	// source is the file the rule came from, for display.
	source string
}

// hasScope reports whether c sets any of the scope settings, which are
// replaced together: an org from one layer and a repo from another would
// otherwise combine into a search neither asked for.
func (c config) hasScope() bool { return c.Orgs != nil || c.Repos != nil || c.All != nil }

// merge layers over on top of base: settings over sets replace base's, and
// its rules are added after base's.
func merge(base, over config) config {
	out := base
	if over.Sections != nil {
		out.Sections = over.Sections
	}
	if over.hasScope() {
		out.Orgs, out.Repos, out.All = over.Orgs, over.Repos, over.All
	}
	if over.Author != nil {
		out.Author = over.Author
	}
	if over.Drafts != nil {
		out.Drafts = over.Drafts
	}
	out.Rules = append(slices.Clone(base.Rules), over.Rules...)
	return out
}

// parseConfig decodes and validates one config file. source names the file
// in errors and on its rules.
func parseConfig(data []byte, source string) (config, error) {
	var c config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return c, fmt.Errorf("%s: %w", source, err)
	}
	if err := c.validate(); err != nil {
		return c, fmt.Errorf("%s: %w", source, err)
	}
	for i := range c.Rules {
		c.Rules[i].source = source
		if c.Rules[i].Name == "" {
			c.Rules[i].Name = fmt.Sprintf("rule %d", i+1)
		}
		if c.Rules[i].MergeMethod == "" {
			c.Rules[i].MergeMethod = "squash"
		}
	}
	return c, nil
}

func (c config) validate() error {
	for _, s := range c.Sections {
		if !slices.Contains(allSections, s) {
			return fmt.Errorf("unknown section %q (want one of %s)", s, strings.Join(allSections, ", "))
		}
	}
	if c.All != nil && *c.All && (len(c.Orgs) > 0 || len(c.Repos) > 0) {
		return errors.New("all can't be combined with orgs or repos")
	}
	for i, r := range c.Rules {
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("%d", i+1)
		}
		if err := r.validate(); err != nil {
			return fmt.Errorf("rule %s: %w", name, err)
		}
	}
	return nil
}

func (r rule) validate() error {
	// The name is printed as-is; keep a repo file from writing terminal escapes.
	if strings.ContainsFunc(r.Name, unicode.IsControl) {
		return errors.New("name can't contain control characters")
	}
	if !slices.Contains(allSections, r.Section) {
		return fmt.Errorf("section %q must be one of %s", r.Section, strings.Join(allSections, ", "))
	}
	if len(r.Actions) == 0 {
		return errors.New("no actions")
	}
	for _, a := range r.Actions {
		if !slices.Contains(allActions, a) {
			return fmt.Errorf("unknown action %q (want one of %s)", a, strings.Join(allActions, ", "))
		}
	}
	if slices.Contains(r.Actions, actionApprove) && r.Section != sectionRequested {
		return fmt.Errorf("action %q only works in section %q: GitHub doesn't let you approve your own PRs", actionApprove, sectionRequested)
	}
	// Someone else's PRs: name whose, so a rule can't act on every PR you're asked to review.
	if r.Section == sectionRequested && len(r.Match.Authors) == 0 {
		return fmt.Errorf("section %q needs match.authors", sectionRequested)
	}
	if r.MergeMethod != "" && !slices.Contains(mergeMethods, r.MergeMethod) {
		return fmt.Errorf("merge_method %q must be one of %s", r.MergeMethod, strings.Join(mergeMethods, ", "))
	}
	for _, p := range append(slices.Clone(r.Match.Authors), r.Match.Repos...) {
		if _, err := path.Match(p, ""); err != nil {
			return fmt.Errorf("bad pattern %q: %w", p, err)
		}
	}
	return nil
}

// configEnv is what loadConfigs reads from the environment, swappable in tests.
type configEnv struct {
	getenv      func(string) string
	home        func() (string, error)
	toplevel    func() (string, error)
	currentRepo func() (string, error)
}

var defaultConfigEnv = configEnv{
	getenv:      os.Getenv,
	home:        os.UserHomeDir,
	toplevel:    gitToplevel,
	currentRepo: currentRepo,
}

// loadConfigs reads the user's config, then, inside a checkout, the repo's
// .github/attention.yaml on top. An explicit path (--config or
// GH_ATTENTION_CONFIG) must exist and is the only file read.
func loadConfigs(explicit string, env configEnv) (config, error) {
	if explicit == "" {
		explicit = env.getenv("GH_ATTENTION_CONFIG")
	}
	if explicit != "" {
		c, _, err := readConfig(explicit, explicit, true)
		return c, err
	}

	var c config
	if userPath, err := userConfigPath(env); err == nil {
		if c, _, err = readConfig(userPath, tildePath(userPath, env), false); err != nil {
			return c, err
		}
	}

	root, err := env.toplevel()
	if err != nil {
		return c, nil // not in a checkout
	}
	repoCfg, found, err := readConfig(filepath.Join(root, repoConfigFile), repoConfigFile, false)
	if err != nil || !found {
		return c, err
	}
	// A repo file mustn't widen the search, or your own rules would act on
	// PRs in repositories it chose.
	if repoCfg.hasScope() {
		return c, fmt.Errorf("%s: orgs, repos and all can only be set in your own config or with flags", repoConfigFile)
	}
	// Pin the repo file's rules to its repo, so a committed file can't act on PRs elsewhere.
	if len(repoCfg.Rules) > 0 {
		repo, err := env.currentRepo()
		if err != nil {
			return c, fmt.Errorf("%s: can't tell which GitHub repository its rules belong to: %w", repoConfigFile, err)
		}
		for i := range repoCfg.Rules {
			repoCfg.Rules[i].Match.Repos = []string{repo}
		}
	}
	return merge(c, repoCfg), nil
}

// userConfigPath is $XDG_CONFIG_HOME/gh-attention/config.yaml, defaulting to
// ~/.config on every platform (os.UserConfigDir picks ~/Library on macOS).
func userConfigPath(env configEnv) (string, error) {
	dir := env.getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := env.home()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "gh-attention", "config.yaml"), nil
}

// readConfig reads and parses p. A missing file reports found=false, or an
// error when the file is required.
func readConfig(p, source string, required bool) (c config, found bool, err error) {
	data, err := os.ReadFile(filepath.Clean(p))
	if errors.Is(err, fs.ErrNotExist) && !required {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	c, err = parseConfig(data, source)
	return c, true, err
}

func tildePath(p string, env configEnv) string {
	if home, err := env.home(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func gitToplevel() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
