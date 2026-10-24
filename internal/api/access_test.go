package api_test

import (
	"fmt"
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/stokaro/ptah-console/internal/api"
)

func namespaces(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("team-%02d", i)
	}
	return names
}

// A console is never started with candidates it would cut short or could not
// read: every one of them goes into one request.
func TestAScopeOffersNamespacesTheConsoleCanRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		candidates []string
	}{
		{name: "none, so the identity's own are offered", candidates: nil},
		{name: "as many as one request reads", candidates: namespaces(api.MaxNamespaces)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			err := api.Scope{Candidates: test.candidates}.Validate()

			c.Assert(err, qt.IsNil)
		})
	}
}

func TestAScopeRefusesNamespacesTheConsoleCannotRead(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		candidates []string
		want       string
	}{
		{name: "one more than a request reads", candidates: namespaces(api.MaxNamespaces + 1),
			want: fmt.Sprintf("%d namespaces named; the console reads at most %d", api.MaxNamespaces+1, api.MaxNamespaces)},
		{name: "a name no namespace can have", candidates: []string{"application", "Billing"},
			want: `"Billing" is not a namespace name`},
		{name: "an empty name", candidates: []string{""}, want: `"" is not a namespace name`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c := qt.New(t)

			err := api.Scope{Candidates: test.candidates}.Validate()

			c.Assert(err, qt.ErrorMatches, test.want)
		})
	}
}
