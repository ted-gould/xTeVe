package snap

import (
	"os"

	"github.com/canonical/go-snapctl"
)

// Get returns the value of a snap configuration option.
func Get(key string) (string, error) {
	// If we're not running in a snap environment, fall back to environment variables.
	if os.Getenv("SNAP_NAME") == "" {
		return os.Getenv(key), nil
	}

	return snapctl.Get(key).Run()
}
