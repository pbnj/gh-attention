package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

// idQuery is deliberately cheap: search pages cost ~1.5s per 100 bare PRs.
const idQuery = `
query($q: String!, $endCursor: String) {
  search(query: $q, type: ISSUE, first: 100, after: $endCursor) {
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest { id } }
  }
}`

// detailQuery fetches everything needed to categorize a batch of PRs.
// reviewDecision and statusCheckRollup are computed per PR on every request
// (~4.5s per 50 PRs together), which is why they are not in the search:
// a 50-PR search page ran into GitHub's ~10s GraphQL timeout.
const detailQuery = `
query($ids: [ID!]!) {
  nodes(ids: $ids) {
    ... on PullRequest {
      number
      title
      url
      isDraft
      updatedAt
      author { login }
      repository { nameWithOwner }
      reviewDecision
      mergeable
      reviewRequests(first: 20) {
        nodes {
          requestedReviewer {
            ... on User { login }
            ... on Team { slug }
            ... on Bot { login }
            ... on Mannequin { login }
          }
        }
      }
      latestOpinionatedReviews(first: 20) {
        nodes { state author { login } }
      }
      reviewThreads(first: 100) {
        nodes { isResolved }
      }
      commits(last: 1) {
        nodes {
          commit {
            statusCheckRollup {
              state
              contexts(first: 100) {
                nodes {
                  __typename
                  ... on CheckRun { name conclusion }
                  ... on StatusContext { context state }
                }
              }
            }
          }
        }
      }
    }
  }
}`

const (
	// detailBatch PRs per detailQuery keeps each request around 4s.
	detailBatch = 20
	// maxInFlight bounds concurrent requests across all searches, staying
	// well clear of GitHub's secondary rate limits.
	maxInFlight = 4
)

type login struct {
	Login string `json:"login"`
}

// prNode mirrors a PullRequest node from searchQuery.
type prNode struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	IsDraft   bool      `json:"isDraft"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    *login    `json:"author"`

	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`

	ReviewDecision string `json:"reviewDecision"`
	Mergeable      string `json:"mergeable"`

	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *struct {
				Login string `json:"login"`
				Slug  string `json:"slug"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`

	LatestOpinionatedReviews struct {
		Nodes []struct {
			State  string `json:"state"`
			Author *login `json:"author"`
		} `json:"nodes"`
	} `json:"latestOpinionatedReviews"`

	ReviewThreads struct {
		Nodes []struct {
			IsResolved bool `json:"isResolved"`
		} `json:"nodes"`
	} `json:"reviewThreads"`

	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State    string `json:"state"`
					Contexts struct {
						Nodes []checkContext `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// checkContext is either a CheckRun (Name, Conclusion) or a StatusContext (Context, State).
type checkContext struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Context    string `json:"context"`
	State      string `json:"state"`
}

type idResponse struct {
	Search struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []struct {
			ID string `json:"id"`
		} `json:"nodes"`
	} `json:"search"`
}

type detailResponse struct {
	Nodes []prNode `json:"nodes"`
}

// fetcher runs GraphQL requests with bounded concurrency and retries.
type fetcher struct {
	client *api.GraphQLClient
	sem    chan struct{}
}

func newFetcher(client *api.GraphQLClient) *fetcher {
	return &fetcher{client: client, sem: make(chan struct{}, maxInFlight)}
}

// searchPRs returns every pull request matching q: ids from the search, then
// details in concurrent batches.
func (f *fetcher) searchPRs(ctx context.Context, q string) ([]prNode, error) {
	var ids []string
	vars := map[string]interface{}{"q": q, "endCursor": nil}
	for {
		var resp idResponse
		if err := f.do(ctx, idQuery, vars, &resp); err != nil {
			return nil, err
		}
		for _, n := range resp.Search.Nodes {
			// Non-PullRequest search hits decode with an empty id.
			if n.ID != "" {
				ids = append(ids, n.ID)
			}
		}
		if !resp.Search.PageInfo.HasNextPage {
			break
		}
		vars["endCursor"] = resp.Search.PageInfo.EndCursor
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	batches := make([][]prNode, (len(ids)+detailBatch-1)/detailBatch)
	var wg sync.WaitGroup
	for i := range batches {
		batch := ids[i*detailBatch : min((i+1)*detailBatch, len(ids))]
		wg.Add(1)
		go func() {
			defer wg.Done()
			var resp detailResponse
			if err := f.do(ctx, detailQuery, map[string]interface{}{"ids": batch}, &resp); err != nil {
				cancel(err)
				return
			}
			batches[i] = resp.Nodes
		}()
	}
	wg.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}

	var prs []prNode
	for _, b := range batches {
		for _, n := range b {
			if n.Number != 0 {
				prs = append(prs, n)
			}
		}
	}
	return prs, nil
}

// do runs one query, retrying what GitHub returns when a request runs into its
// GraphQL timeout: a 502/504, or a 200 with an empty or truncated body
// (surfacing as "unexpected end of JSON input").
func (f *fetcher) do(ctx context.Context, query string, vars map[string]interface{}, resp interface{}) error {
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	case <-ctx.Done():
		return context.Cause(ctx)
	}

	const attempts = 3
	backoff := time.Second
	for i := 1; ; i++ {
		err := f.client.DoWithContext(ctx, query, vars, resp)
		if err == nil || i == attempts || !retryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-time.After(backoff):
		}
		backoff *= 2
	}
}

func retryable(err error) bool {
	var httpErr *api.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode >= 500
	}
	var syntaxErr *json.SyntaxError
	return errors.As(err, &syntaxErr) || errors.Is(err, io.ErrUnexpectedEOF)
}
