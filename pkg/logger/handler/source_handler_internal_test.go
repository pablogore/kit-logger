package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShortFuncName(t *testing.T) {
	cases := map[string]string{
		"github.com/acme/svc/orders.(*Service).Reserve": "orders.(*Service).Reserve",
		"github.com/acme/svc/httpmw.New.func1.1.1":      "httpmw.New",
		"main.main.func1": "main.main",
		"main.main":       "main.main",
		"pkg.Func1":       "pkg.Func1",
		"pkg.func":        "pkg.func",
		"":                "",
	}
	for in, want := range cases {
		assert.Equal(t, want, shortFuncName(in), in)
	}
}
