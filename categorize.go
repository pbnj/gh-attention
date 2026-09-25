package main

import (
	"encoding/json"
	"slices"
	"sort"
	"time"
)

// Section keys, in display order.
const (
	sectionApproved  = "approved"
	sectionReview    = "review"
	sectionFeedback  = "feedback"
	sectionFailing   = "failing"
	sectionRequested = "requested"
)

var allSections = []string{sectionApproved, sectionReview, sectionFeedback, sectionFailing, sectionRequested}

// failingConclusions are CheckRun conclusions that count as a failed check.
var failingConclusions = []string{"FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE"}

// enum is a GraphQL enum value that GitHub may return as null; it encodes an
// absent value back to null so JSON consumers can test `.ciState == null`.
type enum string

func (e enum) MarshalJSON() ([]byte, error) {
	if e == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(e))
}

// PR is the flattened view of a pull request used for categorizing and output.
type PR struct {
	Repo               string    `json:"repo"`
	Number             int       `json:"number"`
	Title              string    `json:"title"`
	URL                string    `json:"url"`
	Draft              bool      `json:"draft"`
	Author             string    `json:"author"`
	UpdatedAt          time.Time `json:"updatedAt"`
	ReviewDecision     enum      `json:"reviewDecision"`
	Conflicting        bool      `json:"conflicting"`
	Approved           bool      `json:"approved"`
	ApprovedBy         []string  `json:"approvedBy"`
	ChangesRequestedBy []string  `json:"changesRequestedBy"`
	PendingReviewers   []string  `json:"pendingReviewers"`
	UnresolvedThreads  int       `json:"unresolvedThreads"`
	CIState            enum      `json:"ciState"`
	FailingChecks      []string  `json:"failingChecks"`
}

func summarize(n prNode) PR {
	pr := PR{
		Repo:               n.Repository.NameWithOwner,
		Number:             n.Number,
		Title:              n.Title,
		URL:                n.URL,
		Draft:              n.IsDraft,
		Author:             "ghost",
		UpdatedAt:          n.UpdatedAt,
		ReviewDecision:     enum(n.ReviewDecision),
		Conflicting:        n.Mergeable == "CONFLICTING",
		ApprovedBy:         []string{},
		ChangesRequestedBy: []string{},
		PendingReviewers:   []string{},
		FailingChecks:      []string{},
	}
	if n.Author != nil {
		pr.Author = n.Author.Login
	}

	for _, r := range n.LatestOpinionatedReviews.Nodes {
		who := "ghost"
		if r.Author != nil {
			who = r.Author.Login
		}
		switch r.State {
		case "APPROVED":
			pr.ApprovedBy = append(pr.ApprovedBy, who)
		case "CHANGES_REQUESTED":
			pr.ChangesRequestedBy = append(pr.ChangesRequestedBy, who)
		}
	}

	for _, rr := range n.ReviewRequests.Nodes {
		if r := rr.RequestedReviewer; r != nil {
			if r.Login != "" {
				pr.PendingReviewers = append(pr.PendingReviewers, r.Login)
			} else if r.Slug != "" {
				pr.PendingReviewers = append(pr.PendingReviewers, r.Slug)
			}
		}
	}

	for _, t := range n.ReviewThreads.Nodes {
		if !t.IsResolved {
			pr.UnresolvedThreads++
		}
	}

	if len(n.Commits.Nodes) > 0 {
		if ci := n.Commits.Nodes[0].Commit.StatusCheckRollup; ci != nil {
			pr.CIState = enum(ci.State)
			for _, c := range ci.Contexts.Nodes {
				switch {
				case c.Typename == "CheckRun" && slices.Contains(failingConclusions, c.Conclusion):
					pr.FailingChecks = append(pr.FailingChecks, c.Name)
				case c.Typename == "StatusContext" && (c.State == "FAILURE" || c.State == "ERROR"):
					pr.FailingChecks = append(pr.FailingChecks, c.Context)
				}
			}
		}
	}

	// Repos without required reviews report a null decision; fall back to the reviews themselves.
	pr.Approved = pr.ReviewDecision == "APPROVED" ||
		(pr.ReviewDecision == "" && len(pr.ApprovedBy) > 0 && len(pr.ChangesRequestedBy) == 0)

	return pr
}

func (pr PR) ciFailing() bool { return pr.CIState == "FAILURE" || pr.CIState == "ERROR" }

func (pr PR) awaitingReview() bool {
	return !pr.Approved && !pr.Draft &&
		pr.ReviewDecision != "CHANGES_REQUESTED" &&
		(pr.ReviewDecision == "REVIEW_REQUIRED" || len(pr.PendingReviewers) > 0)
}

func (pr PR) hasFeedback() bool {
	return pr.ReviewDecision == "CHANGES_REQUESTED" || pr.UnresolvedThreads > 0
}

// categorize sorts PRs into every section they qualify for. Only the requested
// sections are present in the result; each is ordered most recently updated first.
func categorize(authored, requested []prNode, includeDrafts bool, sections []string) map[string][]PR {
	out := make(map[string][]PR, len(sections))
	for _, s := range sections {
		out[s] = []PR{}
	}
	add := func(section string, pr PR) {
		if list, ok := out[section]; ok {
			out[section] = append(list, pr)
		}
	}

	for _, n := range authored {
		if n.IsDraft && !includeDrafts {
			continue
		}
		pr := summarize(n)
		if pr.Approved && !pr.Draft {
			add(sectionApproved, pr)
		}
		if pr.awaitingReview() {
			add(sectionReview, pr)
		}
		if pr.hasFeedback() {
			add(sectionFeedback, pr)
		}
		if pr.ciFailing() {
			add(sectionFailing, pr)
		}
	}
	for _, n := range requested {
		if n.IsDraft && !includeDrafts {
			continue
		}
		add(sectionRequested, summarize(n))
	}

	for _, list := range out {
		sort.SliceStable(list, func(i, j int) bool { return list[i].UpdatedAt.After(list[j].UpdatedAt) })
	}
	return out
}
