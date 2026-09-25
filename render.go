package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/text"
)

const titleWidth = 60

type sectionMeta struct {
	icon, title, color string
}

var sectionInfo = map[string]sectionMeta{
	sectionApproved:  {"✔", "Approved, not merged", "32"},
	sectionReview:    {"◷", "Pending reviewer approval", "33"},
	sectionFeedback:  {"✎", "Unresolved PR feedback", "35"},
	sectionFailing:   {"✘", "Failing CI checks", "31"},
	sectionRequested: {"👀", "Your review requested", "36"},
}

type renderer struct {
	w     io.Writer
	color bool
	now   time.Time
}

func (r renderer) paint(code, s string) string {
	if !r.color || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// render writes the report in a single write, so a failed write (e.g. a
// closed pipe) surfaces as one error instead of being dropped per line.
func (r renderer) render(order []string, result map[string][]PR) error {
	var b strings.Builder
	writeln := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	flush := func() error {
		_, err := io.WriteString(r.w, b.String())
		return err
	}

	total, refWidth := 0, 0
	for _, s := range order {
		for _, pr := range result[s] {
			total++
			refWidth = max(refWidth, text.DisplayWidth(ref(pr)))
		}
	}
	if total == 0 {
		writeln(r.paint("32", "Nothing needs your attention."))
		return flush()
	}

	for _, s := range order {
		meta, prs := sectionInfo[s], result[s]
		writeln(r.paint("1;"+meta.color, fmt.Sprintf("%s %s (%d)", meta.icon, meta.title, len(prs))))
		if len(prs) == 0 {
			writeln("  " + r.paint("2", "none"))
		}
		for _, pr := range prs {
			line := "  " + r.paint("1", text.PadRight(refWidth, ref(pr))) +
				"  " + text.PadRight(titleWidth, truncate(titleWidth, pr.Title)) +
				"  " + r.paint("2", text.PadRight(4, age(r.now, pr.UpdatedAt))) +
				"  " + r.paint(meta.color, detail(s, pr))
			if pr.Draft {
				line += " " + r.paint("2", "[draft]")
			}
			writeln(line)
			writeln("    " + r.paint("2;4", pr.URL))
		}
		writeln("")
	}
	return flush()
}

// truncate shortens s to width display columns, marking the cut with a
// single-column ellipsis (text.Truncate spends three columns on "...").
func truncate(width int, s string) string {
	if text.DisplayWidth(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := text.DisplayWidth(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

func ref(pr PR) string { return fmt.Sprintf("%s#%d", pr.Repo, pr.Number) }

func age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// list joins up to three names, summarizing the rest as "+N".
func list(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s +%d", strings.Join(names[:3], ", "), len(names)-3)
}

func detail(section string, pr PR) string {
	switch section {
	case sectionApproved:
		parts := []string{"by " + list(pr.ApprovedBy)}
		if pr.Conflicting {
			parts = append(parts, "conflicts")
		}
		switch {
		case pr.ciFailing():
			parts = append(parts, "CI failing")
		case pr.CIState == "PENDING" || pr.CIState == "EXPECTED":
			parts = append(parts, "CI pending")
		}
		return strings.Join(parts, " · ")
	case sectionReview:
		if len(pr.PendingReviewers) == 0 {
			return "no reviewer requested"
		}
		return "waiting on " + list(pr.PendingReviewers)
	case sectionFeedback:
		var parts []string
		if len(pr.ChangesRequestedBy) > 0 {
			parts = append(parts, "changes requested by "+list(pr.ChangesRequestedBy))
		}
		if pr.UnresolvedThreads > 0 {
			parts = append(parts, text.Pluralize(pr.UnresolvedThreads, "unresolved thread"))
		}
		return strings.Join(parts, " · ")
	case sectionFailing:
		if len(pr.FailingChecks) == 0 {
			return "status: " + strings.ToLower(string(pr.CIState))
		}
		return list(pr.FailingChecks)
	case sectionRequested:
		return "from " + pr.Author
	}
	return ""
}
