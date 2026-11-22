package inbox

import (
	"testing"

	"github.com/raven-clown/bridge/forge"
)

func checks(s forge.CheckState) *forge.Checks { return &forge.Checks{State: s} }

func review(r forge.ReviewState) *forge.ReviewState { return &r }

func TestRank(t *testing.T) {
	label := []forge.Label{{Name: "bug"}}
	tests := []struct {
		name string
		it   Item
		want Urgency
	}{
		{"fork run awaiting approval", Item{Kind: forge.IncomingPR, Checks: checks(forge.ChecksAwaitingApproval)}, UrgencyForkRunAwaitingApproval},
		{"unanswered", Item{Kind: forge.IncomingIssue, Labels: label, Unanswered: 2}, UrgencyUnanswered},
		{"own pr failing", Item{Kind: forge.OwnPR, Checks: checks(forge.ChecksFailing)}, UrgencyOwnPRFailing},
		{"review requested", Item{Kind: forge.ReviewRequest}, UrgencyReviewRequested},
		{"untriaged issue", Item{Kind: forge.IncomingIssue}, UrgencyUntriagedIssue},
		{"incoming pr green", Item{Kind: forge.IncomingPR, Checks: checks(forge.ChecksPassing), Review: review(forge.ReviewNone)}, UrgencyIncomingPRGreen},
		{"incoming pr green no review field", Item{Kind: forge.IncomingPR, Checks: checks(forge.ChecksPassing)}, UrgencyIncomingPRGreen},
		{"own pr ready", Item{Kind: forge.OwnPR, Checks: checks(forge.ChecksPassing), Review: review(forge.ReviewApproved)}, UrgencyOwnPRReadyToMerge},
		{"pushed branch", Item{Kind: forge.PushedBranch}, UrgencyPushedBranch},
		{"own pr pending", Item{Kind: forge.OwnPR, Checks: checks(forge.ChecksPending)}, UrgencyOwnPRWaiting},
		{"labelled issue", Item{Kind: forge.IncomingIssue, Labels: label}, UrgencyOther},
		{"assigned issue", Item{Kind: forge.IncomingIssue, Assignees: []forge.User{{Login: "x"}}}, UrgencyOther},
		{"incoming pr reviewed", Item{Kind: forge.IncomingPR, Checks: checks(forge.ChecksPassing), Review: review(forge.ReviewApproved)}, UrgencyOther},
	}
	for _, tt := range tests {
		if got := Rank(tt.it); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestRankFirstMatchWins(t *testing.T) {
	it := Item{Kind: forge.IncomingPR, Checks: checks(forge.ChecksAwaitingApproval), Unanswered: 3}
	if got := Rank(it); got != UrgencyForkRunAwaitingApproval {
		t.Fatalf("got %d", got)
	}
}

func TestUrgencyOrder(t *testing.T) {
	order := []Urgency{
		UrgencyForkRunAwaitingApproval, UrgencyUnanswered, UrgencyOwnPRFailing, UrgencyReviewRequested,
		UrgencyUntriagedIssue, UrgencyIncomingPRGreen, UrgencyOwnPRReadyToMerge, UrgencyPushedBranch,
		UrgencyOwnPRWaiting, UrgencyOther,
	}
	for i := 1; i < len(order); i++ {
		if order[i-1] <= order[i] {
			t.Fatalf("rule %d (%d) is not above rule %d (%d)", i, order[i-1], i+1, order[i])
		}
	}
}
