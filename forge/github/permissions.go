package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/raven-clown/bridge/forge"
)

const reposPerPermissionQuery = 50

var writePermissions = []string{"ADMIN", "MAINTAIN", "WRITE"}

// Writable asks for viewerPermission on many repositories per query, using
// one alias per repository. Repositories that no longer exist are dropped.
func (c *Client) Writable(ctx context.Context, repos []forge.RepoID) ([]forge.RepoID, error) {
	var out []forge.RepoID
	for chunk := range slices.Chunk(repos, reposPerPermissionQuery) {
		var params, fields []string
		vars := map[string]any{}
		for i, r := range chunk {
			params = append(params, fmt.Sprintf("$o%d: String!, $n%d: String!", i, i))
			fields = append(fields, fmt.Sprintf("r%d: repository(owner: $o%d, name: $n%d) { viewerPermission }", i, i, i))
			vars[fmt.Sprintf("o%d", i)], vars[fmt.Sprintf("n%d", i)] = r.Owner, r.Name
		}
		query := "query(" + strings.Join(params, ", ") + ") {\n" + strings.Join(fields, "\n") + "\n}"

		var data map[string]*struct {
			ViewerPermission string `json:"viewerPermission"`
		}
		err := c.graphql(ctx, query, vars, &data)
		var gqlErrs GraphQLErrors
		if err != nil && (!errors.As(err, &gqlErrs) || !onlyNotFound(gqlErrs)) {
			return nil, err
		}
		for i, r := range chunk {
			if p := data[fmt.Sprintf("r%d", i)]; p != nil && slices.Contains(writePermissions, p.ViewerPermission) {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func onlyNotFound(errs GraphQLErrors) bool {
	for _, e := range errs {
		if e.Type != "NOT_FOUND" {
			return false
		}
	}
	return true
}
