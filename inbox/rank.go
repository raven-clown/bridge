package inbox

import "github.com/raven-clown/bridge/forge"

type Urgency int

const (
	UrgencyOther                   Urgency = 10
	UrgencyOwnPRWaiting            Urgency = 20
	UrgencyPushedBranch            Urgency = 30
	UrgencyOwnPRReadyToMerge       Urgency = 40
	UrgencyIncomingPRGreen         Urgency = 50
	UrgencyUntriagedIssue          Urgency = 60
	UrgencyReviewRequested         Urgency = 70
	UrgencyOwnPRFailing            Urgency = 80
	UrgencyUnanswered              Urgency = 90
	UrgencyForkRunAwaitingApproval Urgency = 100
)

func checkIs(it Item, s forge.CheckState) bool { return it.Checks != nil && it.Checks.State == s }

func reviewIs(it Item, r forge.ReviewState) bool {
	if it.Review == nil {
		return r == forge.ReviewNone
	}
	return *it.Review == r
}

// Rank applies the design's rules in order; the first match wins.
func Rank(it Item) Urgency {
	switch {
	case it.Kind == forge.IncomingPR && checkIs(it, forge.ChecksAwaitingApproval):
		return UrgencyForkRunAwaitingApproval
	case it.Unanswered > 0:
		return UrgencyUnanswered
	case it.Kind == forge.OwnPR && checkIs(it, forge.ChecksFailing):
		return UrgencyOwnPRFailing
	case it.Kind == forge.ReviewRequest:
		return UrgencyReviewRequested
	case it.Kind == forge.IncomingIssue && len(it.Labels) == 0 && len(it.Assignees) == 0:
		return UrgencyUntriagedIssue
	case it.Kind == forge.IncomingPR && checkIs(it, forge.ChecksPassing) && reviewIs(it, forge.ReviewNone):
		return UrgencyIncomingPRGreen
	case it.Kind == forge.OwnPR && checkIs(it, forge.ChecksPassing) && reviewIs(it, forge.ReviewApproved):
		return UrgencyOwnPRReadyToMerge
	case it.Kind == forge.PushedBranch:
		return UrgencyPushedBranch
	case it.Kind == forge.OwnPR:
		return UrgencyOwnPRWaiting
	}
	return UrgencyOther
}
