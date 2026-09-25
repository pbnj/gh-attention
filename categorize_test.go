package main

import (
	"encoding/json"
	"slices"
	"testing"
)

func node(t *testing.T, raw string) prNode {
	t.Helper()
	var n prNode
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sectionsOf(result map[string][]PR, number int) []string {
	var in []string
	for _, s := range allSections {
		for _, pr := range result[s] {
			if pr.Number == number {
				in = append(in, s)
			}
		}
	}
	return in
}

func TestCategorize(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		drafts bool
		want   []string
	}{
		{
			name: "approved by decision",
			raw:  `{"number":1,"reviewDecision":"APPROVED"}`,
			want: []string{sectionApproved},
		},
		{
			name: "approved without required reviews",
			raw:  `{"number":2,"latestOpinionatedReviews":{"nodes":[{"state":"APPROVED","author":{"login":"a"}}]}}`,
			want: []string{sectionApproved},
		},
		{
			name: "approval with outstanding change request is not approved",
			raw: `{"number":3,"latestOpinionatedReviews":{"nodes":[
				{"state":"APPROVED","author":{"login":"a"}},
				{"state":"CHANGES_REQUESTED","author":{"login":"b"}}]}}`,
			want: nil,
		},
		{
			name: "review required",
			raw:  `{"number":4,"reviewDecision":"REVIEW_REQUIRED"}`,
			want: []string{sectionReview},
		},
		{
			name: "reviewer requested without required reviews",
			raw:  `{"number":5,"reviewRequests":{"nodes":[{"requestedReviewer":{"slug":"team"}}]}}`,
			want: []string{sectionReview},
		},
		{
			name: "changes requested is feedback, not pending review",
			raw:  `{"number":6,"reviewDecision":"CHANGES_REQUESTED"}`,
			want: []string{sectionFeedback},
		},
		{
			name: "unresolved threads alongside approval",
			raw:  `{"number":7,"reviewDecision":"APPROVED","reviewThreads":{"nodes":[{"isResolved":true},{"isResolved":false}]}}`,
			want: []string{sectionApproved, sectionFeedback},
		},
		{
			name: "failing CI",
			raw:  `{"number":8,"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}}`,
			want: []string{sectionFailing},
		},
		{
			name: "pending CI is not failing",
			raw:  `{"number":9,"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"PENDING"}}}]}}`,
			want: nil,
		},
		{
			name:   "draft is never approved or awaiting review",
			raw:    `{"number":10,"isDraft":true,"reviewDecision":"REVIEW_REQUIRED","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"ERROR"}}}]}}`,
			drafts: true,
			want:   []string{sectionFailing},
		},
		{
			name: "drafts excluded",
			raw:  `{"number":11,"isDraft":true,"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"ERROR"}}}]}}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := node(t, tt.raw)
			got := sectionsOf(categorize([]prNode{n}, nil, tt.drafts, allSections), n.Number)
			if !slices.Equal(got, tt.want) {
				t.Errorf("sections = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSummarizeFailingChecks(t *testing.T) {
	pr := summarize(node(t, `{"number":1,"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE","contexts":{"nodes":[
		{"__typename":"CheckRun","name":"lint","conclusion":"FAILURE"},
		{"__typename":"CheckRun","name":"test","conclusion":"SUCCESS"},
		{"__typename":"CheckRun","name":"build","conclusion":"TIMED_OUT"},
		{"__typename":"CheckRun","name":"running","conclusion":""},
		{"__typename":"StatusContext","context":"ci/jenkins","state":"ERROR"},
		{"__typename":"StatusContext","context":"ci/ok","state":"SUCCESS"}]}}}}]}}`))
	want := []string{"lint", "build", "ci/jenkins"}
	if !slices.Equal(pr.FailingChecks, want) {
		t.Errorf("FailingChecks = %v, want %v", pr.FailingChecks, want)
	}
}

func TestCategorizeOnlyRequestedSections(t *testing.T) {
	n := node(t, `{"number":1,"reviewDecision":"APPROVED"}`)
	got := categorize([]prNode{n}, nil, true, []string{sectionFailing})
	if _, ok := got[sectionApproved]; ok || len(got) != 1 {
		t.Errorf("got sections %v, want only %q", got, sectionFailing)
	}
}

func TestParseFlagsSections(t *testing.T) {
	opts, err := parseFlags([]string{"-s", "requested,failing"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{sectionFailing, sectionRequested}; !slices.Equal(opts.sections, want) {
		t.Errorf("sections = %v, want %v", opts.sections, want)
	}
	if !opts.reviews {
		t.Error("requesting the requested section should enable the review search")
	}
	if _, err := parseFlags([]string{"-s", "bogus"}); err == nil {
		t.Error("expected an error for an unknown section")
	}
}

func TestTruncate(t *testing.T) {
	for _, tt := range []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is t…"},
		{"bookworm→trixie upgrade", 12, "bookworm→tr…"},
	} {
		if got := truncate(tt.width, tt.in); got != tt.want {
			t.Errorf("truncate(%d, %q) = %q, want %q", tt.width, tt.in, got, tt.want)
		}
	}
}
