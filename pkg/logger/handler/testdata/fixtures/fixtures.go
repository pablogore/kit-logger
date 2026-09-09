// Package fixtures aggregates the call-site fixtures used to prove that source
// attribution no longer depends on a denylist of directory or file names.
//
// It lives inside the testdata tree so that it may import the internal/
// fixture, which only code rooted at testdata/ is allowed to import.
package fixtures

import (
	"log/slog"

	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata/cmd/api"
	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata/internal/orders"
	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata/mw"
	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata/pkg/billing"
	"github.com/pablogore/kit-logger/pkg/logger/handler/testdata/transport"
)

// Call is one fixture: a path shape plus the function that logs from it.
type Call struct {
	// Name describes the path shape under test.
	Name string
	// Log emits one record and returns the file base name and line that the
	// record must be attributed to.
	Log func(*slog.Logger) (file string, line int)
}

// All returns one fixture per path shape that the removed denylist excluded.
func All() []Call {
	return []Call{
		{Name: "internal directory and service.go", Log: orders.Log},
		{Name: "pkg directory and handler.go", Log: billing.Log},
		{Name: "cmd directory and controller.go", Log: api.Log},
		{Name: "middleware.go", Log: mw.Log},
		{Name: "client.go", Log: transport.Log},
	}
}
