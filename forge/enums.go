package forge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

var (
	checkNames     = [...]string{"none", "pending", "awaiting-approval", "failing", "passing"}
	reviewNames    = [...]string{"none", "commented", "approved", "changes-requested"}
	mergeableNames = [...]string{"unknown", "clean", "conflicting", "blocked"}
)

func enumName(names []string, v int) string {
	if v < 0 || v >= len(names) {
		return strconv.Itoa(v)
	}
	return names[v]
}

// decodeEnum takes a name, or the integer older versions wrote, since
// encoding/json never passes a number to UnmarshalText.
func decodeEnum(data []byte, names []string, what string) (int, error) {
	if bytes.HasPrefix(data, []byte(`"`)) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return 0, err
		}
		for i, n := range names {
			if n == s {
				return i, nil
			}
		}
		return 0, fmt.Errorf("unknown %s %q", what, s)
	}
	n, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("bad %s %s", what, data)
	}
	return n, nil
}

func (k InboxKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

func (k *InboxKind) UnmarshalJSON(data []byte) error {
	n, err := decodeEnum(data, kindNames[:], "kind")
	*k = InboxKind(n)
	return err
}

func (c CheckState) String() string { return enumName(checkNames[:], int(c)) }

func (c CheckState) MarshalText() ([]byte, error) { return []byte(c.String()), nil }

func (c *CheckState) UnmarshalJSON(data []byte) error {
	n, err := decodeEnum(data, checkNames[:], "check state")
	*c = CheckState(n)
	return err
}

func (r ReviewState) String() string { return enumName(reviewNames[:], int(r)) }

func (r ReviewState) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

func (r *ReviewState) UnmarshalJSON(data []byte) error {
	n, err := decodeEnum(data, reviewNames[:], "review state")
	*r = ReviewState(n)
	return err
}

func (m Mergeable) String() string { return enumName(mergeableNames[:], int(m)) }

func (m Mergeable) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

func (m *Mergeable) UnmarshalJSON(data []byte) error {
	n, err := decodeEnum(data, mergeableNames[:], "mergeable state")
	*m = Mergeable(n)
	return err
}
