package httpmw

import (
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy exhausted") }

func TestNewULID_ProducesParseableULID(t *testing.T) {
	id, err := newULID()
	require.NoError(t, err)
	_, err = ulid.ParseStrict(id)
	require.NoError(t, err)
}

func TestNewULIDFrom_EntropyFailureIsAnErrorNotAPanic(t *testing.T) {
	var id string
	var err error
	require.NotPanics(t, func() { id, err = newULIDFrom(failingReader{}) })
	require.Error(t, err)
	require.Empty(t, id)
}
