package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func botRuleFor(section string, actions ...string) rule {
	r := rule{Name: "bots", Section: section, Actions: actions, MergeMethod: "squash", source: "cfg"}
	r.Match.Authors = []string{"app/dependabot", "snyk-*"}
	return r
}

func TestRuleMatches(t *testing.T) {
	r := botRuleFor(sectionRequested, actionApprove)
	r.Match.Repos = []string{"octo-org/*"}
	for _, tt := range []struct {
		pr   PR
		want bool
	}{
		{PR{Author: "dependabot", Repo: "octo-org/api"}, true},
		{PR{Author: "Dependabot[bot]", Repo: "Octo-Org/API"}, true},
		{PR{Author: "snyk-bot", Repo: "octo-org/web"}, true},
		{PR{Author: "octocat", Repo: "octo-org/api"}, false},
		{PR{Author: "dependabot", Repo: "other/api"}, false},
		{PR{Author: "dependabot", Repo: "octo-org/api", Draft: true}, false},
	} {
		if got := r.matches(tt.pr); got != tt.want {
			t.Errorf("matches(%+v) = %v, want %v", tt.pr, got, tt.want)
		}
	}
}

func TestRuleMatchesTitles(t *testing.T) {
	r := rule{Section: sectionRequested}
	r.Match.Authors = []string{"github-actions"}
	r.Match.Titles = []string{"chore: roll Wolfi package pins forward*", "exact?title"}
	for _, tt := range []struct {
		title string
		want  bool
	}{
		{"chore: roll Wolfi package pins forward for nodejs/24", true}, // * spans the slash
		{"CHORE: Roll wolfi package pins forward", true},               // case-insensitive
		{"exactXtitle", true},
		{"exact title (x)", false},
		{"fix: roll Wolfi package pins forward", false},
		{"chore: roll Wolfi package pins", false},
	} {
		if got := r.matches(PR{Author: "app/github-actions", Title: tt.title}); got != tt.want {
			t.Errorf("matches(%q) = %v, want %v", tt.title, got, tt.want)
		}
	}
	// Regexp metacharacters in a pattern are literal.
	if titleGlob("deps (major) [x]", "deps (major) [x]") != true || titleGlob("a.c", "abc") {
		t.Error("regexp metacharacters should match literally")
	}
}

func TestPlanActions(t *testing.T) {
	result := map[string][]PR{
		sectionRequested: {
			{URL: "u1", Author: "dependabot", Repo: "o/a", Number: 1, ViewerApproved: true},
			{URL: "u2", Author: "octocat", Repo: "o/a", Number: 2},
			{URL: "u3", Author: "snyk-bot", Repo: "o/a", Number: 3, AutoMerge: true},
		},
		sectionFailing: {{URL: "u1", Author: "dependabot", Repo: "o/a", Number: 1}},
	}
	rules := []rule{
		botRuleFor(sectionRequested, actionAutoMerge, actionApprove, actionApproveWorkflows),
		botRuleFor(sectionRequested, actionApprove), // duplicate action, runs once
	}
	plan := planActions([]string{sectionFailing, sectionRequested}, result, rules)

	var got []string
	for _, p := range plan {
		s := p.PR.URL + " " + p.Action
		if p.Skip != "" {
			s += " (" + p.Skip + ")"
		}
		got = append(got, s)
	}
	want := []string{
		"u1 approve-workflows",
		"u1 approve (already approved)",
		"u1 auto-merge",
		"u3 approve-workflows",
		"u3 approve",
		"u3 auto-merge (auto-merge already enabled)",
	}
	if !slices.Equal(got, want) {
		t.Errorf("plan:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if pr := result[sectionRequested][0]; !slices.Equal(pr.Actions, allActions) || !slices.Equal(pr.Rules, []string{"bots", "bots"}) {
		t.Errorf("matched PR = rules %v actions %v", pr.Rules, pr.Actions)
	}
	if pr := result[sectionFailing][0]; pr.Rules != nil {
		t.Errorf("a rule for one section shouldn't mark PRs in another: %v", pr.Rules)
	}
}

type fakeActor struct {
	runs  int
	calls []string
	fail  string
}

func (f *fakeActor) do(name string) error {
	f.calls = append(f.calls, name)
	if name == f.fail {
		return errors.New("boom")
	}
	return nil
}
func (f *fakeActor) approveWorkflows(_ context.Context, pr PR) (int, error) {
	return f.runs, f.do(actionApproveWorkflows)
}
func (f *fakeActor) approve(context.Context, PR) error { return f.do(actionApprove) }
func (f *fakeActor) enableAutoMerge(_ context.Context, _ PR, method string) error {
	return f.do(actionAutoMerge + ":" + method)
}

func TestExecute(t *testing.T) {
	pr := PR{Repo: "o/a", Number: 1}
	plan := []plannedAction{
		{PR: pr, Action: actionApproveWorkflows},
		{PR: pr, Action: actionApprove, Skip: "already approved"},
		{PR: pr, Action: actionAutoMerge, MergeMethod: "rebase"},
	}

	a := &fakeActor{fail: actionAutoMerge + ":rebase"}
	res := execute(context.Background(), a, plan)
	if !slices.Equal(a.calls, []string{actionApproveWorkflows, "auto-merge:rebase"}) {
		t.Errorf("calls = %v", a.calls)
	}
	if res[0].Skip != "no workflow runs awaiting approval" || res[1].Done != "" || res[2].Err == nil {
		t.Errorf("results = %+v", res)
	}

	res = execute(context.Background(), &fakeActor{runs: 2}, plan[:1])
	if res[0].Done != "approved 2 workflow runs" {
		t.Errorf("Done = %q", res[0].Done)
	}
}

func TestRenderActions(t *testing.T) {
	pr := PR{Repo: "o/a", Number: 7}
	results := []actionResult{
		{plannedAction: plannedAction{PR: pr, Action: actionAutoMerge, MergeMethod: "squash", Rule: "bots", Source: "cfg"}},
	}
	var b bytes.Buffer
	if err := (renderer{w: &b, now: time.Now()}).renderActions(results, false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"dry run", "o/a#7", "would auto-merge (squash)", "(bots, cfg)", "--apply"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// fakeREST serves action_required runs: each GET returns the runs not yet
// approved, up to pageSize, as GitHub does once approved runs move on.
type fakeREST struct {
	pending  []int64
	pageSize int
	stuck    bool // approved runs keep showing up
	approved []int64
}

func (f *fakeREST) DoWithContext(_ context.Context, method, p string, _ io.Reader, resp interface{}) error {
	if method == "POST" {
		var id int64
		if _, err := fmt.Sscanf(p[strings.LastIndex(p, "runs/")+len("runs/"):], "%d", &id); err != nil {
			return err
		}
		f.approved = append(f.approved, id)
		if !f.stuck {
			f.pending = slices.DeleteFunc(f.pending, func(x int64) bool { return x == id })
		}
		return nil
	}
	var runs []string
	for _, id := range f.pending[:min(f.pageSize, len(f.pending))] {
		runs = append(runs, fmt.Sprintf(`{"id":%d}`, id))
	}
	return json.Unmarshal([]byte(`{"workflow_runs":[`+strings.Join(runs, ",")+`]}`), resp)
}

func TestApproveWorkflowRunsPages(t *testing.T) {
	pr := PR{Repo: "o/a", HeadSHA: "abc"}

	f := &fakeREST{pending: []int64{1, 2, 3, 4, 5}, pageSize: 2}
	n, err := approveWorkflowRuns(context.Background(), f, pr)
	if err != nil || n != 5 || len(f.approved) != 5 {
		t.Errorf("got n=%d approved=%v err=%v, want all 5 once", n, f.approved, err)
	}

	f = &fakeREST{pending: []int64{1, 2}, pageSize: 100, stuck: true}
	n, err = approveWorkflowRuns(context.Background(), f, pr)
	if err != nil || n != 2 || len(f.approved) != 2 {
		t.Errorf("runs that stay listed should be approved once: n=%d approved=%v err=%v", n, f.approved, err)
	}
}
