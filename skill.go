package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	flag "github.com/spf13/pflag"
)

// skillDoc is the agent skill for this version of gh-attention, so the
// flags it describes always match the binary.
//
//go:embed skill/SKILL.md
var skillDoc string

// skillMarker is a line of its own in a SKILL.md written by `skill install`,
// which may be overwritten; any other SKILL.md is someone's own and needs
// --force. A skill that merely quotes it mid-line doesn't count.
const skillMarker = "<!-- installed by gh attention skill install; local edits are overwritten -->"

func hasSkillMarker(doc string) bool {
	for _, line := range strings.Split(doc, "\n") {
		if strings.TrimSpace(line) == skillMarker {
			return true
		}
	}
	return false
}

const skillUsage = `Print or install the agent skill for gh attention.

USAGE
  gh attention skill              Print the skill (SKILL.md)
  gh attention skill install      Install it for Claude Code

FLAGS
%s
EXAMPLES
  gh attention skill install
  gh attention skill install --dir .github/skills/gh-attention
  gh attention skill > SKILL.md
`

// runSkill handles `gh attention skill [install]`.
func runSkill(args []string, stdout io.Writer, home func() (string, error)) error {
	var dir string
	var force bool
	fs := flag.NewFlagSet("gh attention skill", flag.ContinueOnError)
	fs.StringVar(&dir, "dir", "", "Install into `DIR` (default ~/.claude/skills/gh-attention)")
	fs.BoolVar(&force, "force", false, "Overwrite a SKILL.md that skill install didn't write")
	fs.Usage = func() { fmt.Fprintf(os.Stderr, skillUsage, fs.FlagUsages()) }
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch {
	case fs.NArg() == 0:
		if fs.Changed("dir") || force {
			return errors.New("--dir and --force only apply to skill install")
		}
		_, err := io.WriteString(stdout, skillDoc)
		return err
	case fs.NArg() == 1 && fs.Arg(0) == "install":
		if dir == "" {
			h, err := home()
			if err != nil {
				return err
			}
			dir = filepath.Join(h, ".claude", "skills", "gh-attention")
		}
		path, err := installSkill(dir, force)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "Installed", path)
		return err
	default:
		return fmt.Errorf("unexpected argument: %s (want install)", strings.Join(fs.Args(), " "))
	}
}

// installSkill writes skillDoc to dir/SKILL.md. It won't replace a SKILL.md
// it didn't write unless force is set.
func installSkill(dir string, force bool) (string, error) {
	path := filepath.Join(filepath.Clean(dir), "SKILL.md")
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if !force && !hasSkillMarker(string(existing)) {
			return "", fmt.Errorf("%s exists and wasn't written by skill install; move it or pass --force", path)
		}
	case !errors.Is(err, fs.ErrNotExist):
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(skillDoc), 0o600)
}
