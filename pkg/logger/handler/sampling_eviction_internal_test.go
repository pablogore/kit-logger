package handler

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEvictionTarget pins the eviction rule as arithmetic, separately from the
// map surgery that applies it.
//
// The defect this replaces was a one-line arithmetic mistake that only showed
// up three call layers away, as a map one entry too large. Naming the rule and
// tabulating it makes the small bounds -- the only ones that were ever wrong --
// readable at a glance instead of inferable from integer division.
func TestEvictionTarget(t *testing.T) {
	tests := []struct {
		maxKeys int
		want    int
	}{
		{1, 0},
		{2, 1},
		{3, 2},
		{4, 3},
		{5, 4},
		{7, 6},
		{8, 6},
		{16, 12},
		{4096, 3072},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("maxKeys=%d", tc.maxKeys), func(t *testing.T) {
			require.Equal(t, tc.want, evictionTarget(tc.maxKeys))
		})
	}
}

// TestEvictionTargetAlwaysLeavesRoom is the property the bound rests on: the
// caller inserts one key right after the pass, so a target equal to maxKeys
// would hand it a full map. Checking it across the whole small range is what
// stops a future "optimisation" from reintroducing the integer-division hole.
func TestEvictionTargetAlwaysLeavesRoom(t *testing.T) {
	for maxKeys := 1; maxKeys <= 1024; maxKeys++ {
		target := evictionTarget(maxKeys)
		require.GreaterOrEqual(t, target, 0,
			"a negative target would index past the sorted buffer (maxKeys=%d)", maxKeys)
		require.LessOrEqual(t, target, maxKeys-1,
			"eviction must free at least one slot for the insert that follows (maxKeys=%d)", maxKeys)
	}
}
