// Package msgs holds the messages views send to the application model.
package msgs

import "github.com/raven-clown/bridge/forge"

type (
	OpenThread   struct{ ID forge.ItemID }
	OpenTriage   struct{ IDs []forge.ItemID }
	NewIssue     struct{}
	Refresh      struct{}
	OpenURL      struct{ URL string }
	Back         struct{}
	Status       struct{ Text string }
	IssueCreated struct {
		ID  forge.ItemID
		URL string
	}
	TriageDone struct{}
)
