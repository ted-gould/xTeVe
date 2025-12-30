package snap

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/go-snapctl"
)

// LoadEnv loads environment variables from a file in the snap configuration directory.
func LoadEnv(filename string) error {
	// If we're not running in a snap environment, do nothing.
	if os.Getenv("SNAP_NAME") == "" {
		return nil
	}

	snapCommon := os.Getenv("SNAP_COMMON")
	if snapCommon == "" {
		return nil
	}

	path := filepath.Join(snapCommon, filename)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			os.Setenv(key, value)
		}
	}

	return scanner.Err()
}

// Get returns the value of a snap configuration option.
func Get(key string) (string, error) {
	// If we're not running in a snap environment, fall back to environment variables.
	if os.Getenv("SNAP_NAME") == "" {
		return os.Getenv(key), nil
	}

	return snapctl.Get(key).Run()
}
