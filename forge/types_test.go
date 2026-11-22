package forge

import (
	"encoding/json"
	"testing"
)

func TestParseRepoID(t *testing.T) {
	tests := []struct {
		in      string
		want    RepoID
		wantErr bool
	}{
		{in: "a/b", want: RepoID{Host: "github.com", Owner: "a", Name: "b"}},
		{in: "ghe.corp/a/b", want: RepoID{Host: "ghe.corp", Owner: "a", Name: "b"}},
		{in: "a", wantErr: true},
		{in: "a/b/c/d", wantErr: true},
		{in: "a/", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseRepoID(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseRepoID(%q) = %v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestRepoIDString(t *testing.T) {
	r := RepoID{Host: "github.com", Owner: "a", Name: "b"}
	if r.String() != "a/b" {
		t.Fatalf("got %q", r.String())
	}
	id := ItemID{Repo: r, Number: 12}
	if id.String() != "a/b#12" {
		t.Fatalf("got %q", id.String())
	}
}

func TestEnumsMarshalAsNames(t *testing.T) {
	review, merge := ReviewChangesRequested, MergeableConflicting
	b, err := json.Marshal(struct {
		Kind      InboxKind    `json:"kind"`
		Checks    Checks       `json:"checks"`
		Review    *ReviewState `json:"review"`
		Mergeable *Mergeable   `json:"mergeable"`
	}{ReviewRequest, Checks{State: ChecksAwaitingApproval}, &review, &merge})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"review-request","checks":{"state":"awaiting-approval"},"review":"changes-requested","mergeable":"conflicting"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func TestEnumsUnmarshalOldIntegers(t *testing.T) {
	var v struct {
		Kind      InboxKind   `json:"kind"`
		State     CheckState  `json:"state"`
		Review    ReviewState `json:"review"`
		Mergeable Mergeable   `json:"mergeable"`
	}
	if err := json.Unmarshal([]byte(`{"kind":3,"state":4,"review":2,"mergeable":1}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Kind != ReviewRequest || v.State != ChecksPassing || v.Review != ReviewApproved || v.Mergeable != MergeableClean {
		t.Fatalf("numbers: %+v", v)
	}
	if err := json.Unmarshal([]byte(`{"kind":"review-request","state":"passing","review":"approved","mergeable":"clean"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Kind != ReviewRequest || v.State != ChecksPassing {
		t.Fatalf("names: %+v", v)
	}
	if err := json.Unmarshal([]byte(`{"kind":"nope"}`), &v); err == nil {
		t.Fatal("unknown name: want error")
	}
}

func TestInboxKindString(t *testing.T) {
	want := map[InboxKind]string{
		OwnPR:         "own-pr",
		IncomingPR:    "incoming-pr",
		IncomingIssue: "incoming-issue",
		ReviewRequest: "review-request",
		Mention:       "mention",
		PushedBranch:  "pushed-branch",
	}
	for k, s := range want {
		if k.String() != s {
			t.Errorf("%d: got %q, want %q", k, k.String(), s)
		}
		back, ok := ParseInboxKind(s)
		if !ok || back != k {
			t.Errorf("ParseInboxKind(%q) = %v, %v", s, back, ok)
		}
	}
}
