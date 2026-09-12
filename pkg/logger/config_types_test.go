package logger

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestLevel_JSONRoundTrip pins Level's TextMarshaler/TextUnmarshaler wiring
// through encoding/json, since that's how Level actually reaches config files
// in practice, not just through ParseLevel directly.
func TestLevel_JSONRoundTrip(t *testing.T) {
	type wrapper struct {
		Level Level `json:"level"`
	}

	encoded, err := json.Marshal(wrapper{Level: LevelWarn})
	require.NoError(t, err)
	assert.JSONEq(t, `{"level":"warn"}`, string(encoded))

	var decoded wrapper
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, LevelWarn, decoded.Level)
}

func TestLevel_JSONRoundTrip_RejectsInvalidValue(t *testing.T) {
	type wrapper struct {
		Level Level `json:"level"`
	}

	var decoded wrapper
	err := json.Unmarshal([]byte(`{"level":"trace"}`), &decoded)
	require.Error(t, err)
}

// TestFormat_YAMLRoundTrip pins the same contract through YAML, since
// gopkg.in/yaml.v3 also drives unmarshaling off TextUnmarshaler.
func TestFormat_YAMLRoundTrip(t *testing.T) {
	type wrapper struct {
		Format Format `yaml:"format"`
	}

	encoded, err := yaml.Marshal(wrapper{Format: FormatJSON})
	require.NoError(t, err)

	var decoded wrapper
	require.NoError(t, yaml.Unmarshal(encoded, &decoded))
	assert.Equal(t, FormatJSON, decoded.Format)
}

// TestFormat_YAMLRoundTrip_RejectsInvalidValue pins that "logfmt" -- a format
// kit-logger has never supported -- fails to unmarshal instead of silently
// falling back to FormatText.
func TestFormat_YAMLRoundTrip_RejectsInvalidValue(t *testing.T) {
	type wrapper struct {
		Format Format `yaml:"format"`
	}

	var decoded wrapper
	err := yaml.Unmarshal([]byte("format: logfmt\n"), &decoded)
	require.Error(t, err)
}
