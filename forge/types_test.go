package forge

import "testing"

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
