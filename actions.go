package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

const approveMutation = `
mutation($id: ID!) {
  addPullRequestReview(input: {pullRequestId: $id, event: APPROVE}) { clientMutationId }
}`

const autoMergeMutation = `
mutation($id: ID!, $method: PullRequestMergeMethod!) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method}) { clientMutationId }
}`

// githubActor runs rule actions: GraphQL for reviews and auto-merge, REST
// for workflow runs, which GraphQL doesn't expose.
type githubActor struct {
	f    *fetcher
	rest *api.RESTClient
}

func (g githubActor) approve(ctx context.Context, pr PR) error {
	return g.f.mutate(ctx, approveMutation, map[string]interface{}{"id": pr.ID})
}

func (g githubActor) enableAutoMerge(ctx context.Context, pr PR, method string) error {
	return g.f.mutate(ctx, autoMergeMutation, map[string]interface{}{
		"id": pr.ID, "method": strings.ToUpper(method),
	})
}

// maxWorkflowPages bounds approveWorkflows, in case approved runs keep
// reporting action_required.
const maxWorkflowPages = 10

// approveWorkflows approves the PR head commit's workflow runs that are
// waiting for approval, and returns how many it approved.
func (g githubActor) approveWorkflows(ctx context.Context, pr PR) (int, error) {
	return approveWorkflowRuns(ctx, g.rest, pr)
}

// restDoer is the part of api.RESTClient approveWorkflowRuns uses.
type restDoer interface {
	DoWithContext(ctx context.Context, method, path string, body io.Reader, response interface{}) error
}

// approveWorkflowRuns approves runs a page at a time. Approved runs leave the
// action_required list, so it re-reads the first page until no new run
// appears, rather than paging past runs that moved.
func approveWorkflowRuns(ctx context.Context, rest restDoer, pr PR) (int, error) {
	q := url.Values{"head_sha": {pr.HeadSHA}, "status": {"action_required"}, "per_page": {"100"}}
	list := fmt.Sprintf("repos/%s/actions/runs?%s", pr.Repo, q.Encode())
	approved := map[int64]bool{}
	for range maxWorkflowPages {
		var resp struct {
			WorkflowRuns []struct {
				ID int64 `json:"id"`
			} `json:"workflow_runs"`
		}
		if err := rest.DoWithContext(ctx, "GET", list, nil, &resp); err != nil {
			return len(approved), err
		}
		fresh := 0
		for _, run := range resp.WorkflowRuns {
			if approved[run.ID] {
				continue
			}
			fresh++
			if err := rest.DoWithContext(ctx, "POST", fmt.Sprintf("repos/%s/actions/runs/%d/approve", pr.Repo, run.ID), nil, nil); err != nil {
				return len(approved), fmt.Errorf("run %d: %w", run.ID, err)
			}
			approved[run.ID] = true
		}
		if fresh == 0 {
			return len(approved), nil
		}
	}
	return len(approved), fmt.Errorf("still finding runs awaiting approval after %d pages", maxWorkflowPages)
}
