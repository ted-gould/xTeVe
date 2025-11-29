//go:build snap
// +build snap

package snap

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnapGet(t *testing.T) {
	t.Setenv("SNAP_NAME", "xteve")
	// Set a mock value using snapctl
	err := exec.Command("snapctl", "set", "test-key=test-value").Run()
	assert.NoError(t, err)

	// Get the value using snap.Get
	value, err := Get("test-key")
	assert.NoError(t, err)

	// Check that the value is correct
	assert.Equal(t, "test-value", value)
}
