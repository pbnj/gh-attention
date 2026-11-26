package main

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/cli/go-gh/v2/pkg/text"
)

// Rule actions, in the order they run on a PR: workflows first so CI can
// start, then the approval, then auto-merge, which waits on both.
const (
	actionApproveWorkflows = "approve-workflows"
	actionApprove          = "approve"
	actionAutoMerge        = "auto-merge"
)

var allActions = []string{actionApproveWorkflows, actionApprove, actionAutoMerge}

// plannedAction is one action a rule wants to run on one PR. Skip, when
// set, says why it won't run.
type plannedAction struct {
	PR          PR
	Action      string
	Rule        string
	Source      string
	MergeMethod string
	Skip        string
}

// actionResult is a plannedAction after --apply ran it.
type actionResult struct {
	plannedAction
	Done string // what happened, on success
	Err  error
}

// normalizeLogin compares bot logins the way people write them: GraphQL
// reports "dependabot", search wants "app/dependabot", REST says
// "dependabot[bot]".
func normalizeLogin(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimPrefix(s, "app/")
	return strings.TrimSuffix(s, "[bot]")
}

func globAny(patterns []string, s string, norm func(string) string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(norm(p), norm(s)); ok {
			return true
		}
	}
	return false
}

// matches reports whether r applies to pr, a PR in r's section. Drafts never match.
func (r rule) matches(pr PR) bool {
	if pr.Draft {
		return false
	}
	if len(r.Match.Authors) > 0 && !globAny(r.Match.Authors, pr.Author, normalizeLogin) {
		return false
	}
	if len(r.Match.Repos) > 0 && !globAny(r.Match.Repos, pr.Repo, strings.ToLower) {
		return false
	}
	return true
}

// planActions matches rules against the categorized PRs, records the rule
// names and actions on each matched PR in result, and returns the actions
// grouped by PR (in display order) and in run order within each PR. An
// action named by more than one rule runs once, for the first rule.
func planActions(order []string, result map[string][]PR, rules []rule) []plannedAction {
	type key struct{ url, action string }
	seen := map[key]bool{}
	var prOrder []string
	byPR := map[string][]plannedAction{}

	for _, section := range order {
		prs := result[section]
		for i := range prs {
			pr := &prs[i]
			for _, r := range rules {
				if r.Section != section || !r.matches(*pr) {
					continue
				}
				pr.Rules = append(pr.Rules, r.Name)
				for _, a := range r.Actions {
					k := key{pr.URL, a}
					if seen[k] {
						continue
					}
					seen[k] = true
					if !slices.Contains(pr.Actions, a) {
						pr.Actions = append(pr.Actions, a)
					}
					if _, ok := byPR[pr.URL]; !ok {
						prOrder = append(prOrder, pr.URL)
					}
					byPR[pr.URL] = append(byPR[pr.URL], plannedAction{
						PR: *pr, Action: a, Rule: r.Name, Source: r.source,
						MergeMethod: r.MergeMethod, Skip: skipReason(*pr, a),
					})
				}
			}
			slices.SortStableFunc(pr.Actions, func(a, b string) int {
				return slices.Index(allActions, a) - slices.Index(allActions, b)
			})
		}
	}

	var plan []plannedAction
	for _, url := range prOrder {
		acts := byPR[url]
		slices.SortStableFunc(acts, func(a, b plannedAction) int {
			return slices.Index(allActions, a.Action) - slices.Index(allActions, b.Action)
		})
		plan = append(plan, acts...)
	}
	return plan
}

// skipReason is why an action needn't run, as far as the fetched PR shows.
func skipReason(pr PR, action string) string {
	switch {
	case action == actionApprove && pr.ViewerApproved:
		return "already approved"
	case action == actionAutoMerge && pr.AutoMerge:
		return "auto-merge already enabled"
	}
	return ""
}

// actor performs rule actions against GitHub.
type actor interface {
	approveWorkflows(ctx context.Context, pr PR) (int, error)
	approve(ctx context.Context, pr PR) error
	enableAutoMerge(ctx context.Context, pr PR, method string) error
}

// execute runs every planned action that isn't skipped, carrying on after
// failures so one bad PR doesn't block the rest.
func execute(ctx context.Context, a actor, plan []plannedAction) []actionResult {
	results := make([]actionResult, 0, len(plan))
	for _, p := range plan {
		res := actionResult{plannedAction: p}
		if p.Skip == "" {
			switch p.Action {
			case actionApproveWorkflows:
				var n int
				if n, res.Err = a.approveWorkflows(ctx, p.PR); res.Err == nil {
					if n == 0 {
						res.Skip = "no workflow runs awaiting approval"
					} else {
						res.Done = "approved " + text.Pluralize(n, "workflow run")
					}
				}
			case actionApprove:
				if res.Err = a.approve(ctx, p.PR); res.Err == nil {
					res.Done = "approved"
				}
			case actionAutoMerge:
				if res.Err = a.enableAutoMerge(ctx, p.PR, p.MergeMethod); res.Err == nil {
					res.Done = "enabled auto-merge (" + p.MergeMethod + ")"
				}
			}
		}
		results = append(results, res)
	}
	return results
}
