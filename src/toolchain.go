package src

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log" // Added for log.Printf
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/avfs/avfs"
	"github.com/samber/lo"
)

// --- System Tools ---

// Checks whether the Folder exists, if not, the Folder is created
func checkFolder(path string) (err error) {
	var debug string
	_, err = os.Stat(filepath.Dir(path))

	if os.IsNotExist(err) {
		// Folder does not exist, will now be created
		err = os.MkdirAll(getPlatformPath(path), 0755)
		if err == nil {
			debug = fmt.Sprintf("Create Folder:%s", path)
			showDebug(debug, 1)
		} else {
			return err
		}
		return nil
	}
	return nil
}

// checkVFSFolder : Checks whether the Folder exists in provided virtual filesystem, if not, the Folder is created
func checkVFSFolder(path string, vfs avfs.VFS) (err error) {
	var debug string
	_, err = vfs.Stat(filepath.Dir(path))

	if fsIsNotExistErr(err) {
		// Folder does not exist, will now be created
		err = vfs.MkdirAll(getPlatformPath(path), 0755)
		if err == nil {
			debug = fmt.Sprintf("Create virtual filesystem Folder:%s", path)
			showDebug(debug, 1)
		} else {
			return err
		}
		return nil
	}
	return nil
}

// fsIsNotExistErr : Returns true whether the <err> is known to report that a file or directory does not exist,
// including virtual file system errors
func fsIsNotExistErr(err error) bool {
	if errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, avfs.ErrWinPathNotFound) ||
		errors.Is(err, avfs.ErrNoSuchFileOrDir) ||
		errors.Is(err, avfs.ErrWinFileNotFound) {
		return true
	}
	return false
}

// Checks whether the File exists in the Filesystem
func checkFile(filename string) (err error) {
	var file = getPlatformFile(filename)

	if _, err = os.Stat(file); os.IsNotExist(err) {
		return err
	}

	fi, err := os.Stat(file)
	if err != nil {
		return err
	}

	switch mode := fi.Mode(); {
	case mode.IsDir():
		err = fmt.Errorf("%s: %s", file, getErrMsg(1072))
		// case mode.IsRegular():
		// 	break
	}
	return
}

func allFilesExist(list ...string) bool {
	for _, f := range list {
		if err := checkFile(f); err != nil {
			return false
		}
	}
	return true
}

// GetUserHomeDirectory : User Home Directory
func GetUserHomeDirectory() (userHomeDirectory string) {
	usr, err := user.Current()

	if err != nil {
		for _, name := range []string{"HOME", "USERPROFILE"} {
			if dir := os.Getenv(name); dir != "" {
				userHomeDirectory = dir
				break
			}
		}
	} else {
		userHomeDirectory = usr.HomeDir
	}
	return
}

// Checks File Permissions
func checkFilePermission(dir string) (err error) {
	var filename = dir + "permission.test"

	err = os.WriteFile(filename, []byte(""), 0644)
	if err == nil {
		err = os.RemoveAll(filename)
	}
	return
}

// Generate folder path for the running OS
func getPlatformPath(path string) string {
	return filepath.Dir(path) + string(os.PathSeparator)
}

// getDefaultTempDir returns default temporary folder path + application name, e.g.: "/tmp/xteve/" or %Tmp%\xteve.
//
// Function assumes default OS temporary folder exists and writable.
func getDefaultTempDir() string {
	return os.TempDir() + string(os.PathSeparator) + System.AppName + string(os.PathSeparator)
}

// getValidTempDir returns standartized temporary folder <path> with trailing path separator:
//
// Slashes will be replaced with OS specific ones and duplicated slashes removed.
//
// On Windows, "/tmp" will be replaced with expanded system environment variable %Tmp%.
func getValidTempDir(path string) string {
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(path, "/tmp") {
			path = strings.Replace(path, "/tmp", os.TempDir(), 1)
		}
	}
	path = filepath.Clean(path)
	path = path + string(os.PathSeparator)

	err := checkFolder(path)
	if err == nil {
		err = checkFilePermission(path)
	}

	if err != nil {
		ShowError(err, 1015)
		path = getDefaultTempDir()
	}
	return path
}

// Generate File Path for the running OS
func getPlatformFile(filename string) (osFilePath string) {
	path, file := filepath.Split(filename)
	var newPath = filepath.Dir(path)
	osFilePath = newPath + string(os.PathSeparator) + file
	return
}

// Output Filenames from the File Path
func getFilenameFromPath(path string) (file string) {
	return filepath.Base(path)
}

// Searches for a File in the OS
func searchFileInOS(file string) (path string) {
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd":
		var args = file
		var cmd = exec.Command("which", strings.Split(args, " ")...)

		out, err := cmd.CombinedOutput()
		if err == nil {
			var slice = strings.Split(strings.Replace(string(out), "\r\n", "\n", -1), "\n")

			if len(slice) > 0 {
				path = strings.Trim(slice[0], "\r\n")
			}
		}
	default:
		return
	}
	return
}

func removeChildItems(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return err
	}

	for _, file := range files {
		err = os.RemoveAll(file)
		if err != nil {
			return err
		}
	}
	return nil
}

// JSON
func mapToJSON(tmpMap any) string {
	jsonString, err := json.MarshalIndent(tmpMap, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(jsonString)
}

func jsonToMap(content string) map[string]any {
	var tmpMap = make(map[string]any)
	if err := json.Unmarshal([]byte(content), &tmpMap); err != nil {
		log.Printf("Error unmarshalling JSON to map: %v. Content: %s", err, content)
		// Return an empty map or handle as appropriate for the callers
		return make(map[string]any)
	}
	return tmpMap
}

func jsonToInterface(content string) (tmpMap any, err error) {
	err = json.Unmarshal([]byte(content), &tmpMap)
	return
}

func saveMapToJSONFile(file string, tmpMap any) error {
	var filename = getPlatformFile(file)
	jsonString, err := json.MarshalIndent(tmpMap, "", "  ")

	if err != nil {
		return err
	}

	err = os.WriteFile(filename, []byte(jsonString), 0644)
	if err != nil {
		return err
	}
	return nil
}

func loadJSONFileToMap(file string) (tmpMap map[string]any, err error) {
	f, err := os.Open(getPlatformFile(file))
	if err != nil {
		return nil, err // Return error instead of panic
	}
	defer f.Close()

	content, err := io.ReadAll(f)

	if err == nil {
		err = json.Unmarshal([]byte(content), &tmpMap)
	}

	if closeErr := f.Close(); closeErr != nil {
		log.Printf("Error closing file %s: %v", file, closeErr)
		// If err is nil at this point, we should return closeErr.
		// If err is not nil, the original error is probably more important.
		if err == nil {
			return tmpMap, closeErr
		}
	}
	return
}

// Binary
func readByteFromFile(file string) (content []byte, err error) {
	f, err := os.Open(getPlatformFile(file))
	if err != nil {
		return nil, err // Return error instead of panic
	}
	defer f.Close()

	content, err = io.ReadAll(f)
	// The deferred f.Close() will run. If an explicit Close is needed before return,
	// it should be handled carefully with the deferred one.
	// In this case, io.ReadAll reads until EOF or error, so the file is likely fully read or errored.
	// The deferred close is generally sufficient. If we want to capture a close error specifically
	// *before* returning a potential io.ReadAll error, the logic would be more complex.
	// Given the original structure, we'll assume the deferred close is the primary mechanism.
	// However, the explicit f.Close() was there. Let's handle its error if err is nil.
	if err == nil { // If ReadAll was successful
		if closeErr := f.Close(); closeErr != nil {
			log.Printf("Error closing file %s after read: %v", file, closeErr)
			return content, closeErr // Return the close error as the primary error now
		}
	} else {
		// If ReadAll failed, we still rely on the deferred close.
		// We could try to close explicitly and log, but it might mask the original ReadAll error.
		// For now, let's stick to the deferred close for the error path of ReadAll.
	}
	return
}

func writeByteToFile(file string, data []byte) (err error) {
	var filename = getPlatformFile(file)
	err = os.WriteFile(filename, data, 0644)
	return
}

func readStringFromFile(file string) (str string, err error) {
	var content []byte
	var filename = getPlatformFile(file)

	err = checkFile(filename)
	if err != nil {
		return
	}

	content, err = os.ReadFile(filename)
	if err != nil {
		ShowError(err, 0)
		return
	}

	str = string(content)
	return
}

// Network
func resolveHostIP() (err error) {
	netInterfaceAddresses, err := net.InterfaceAddrs()
	if err != nil {
		return
	}

	for _, netInterfaceAddress := range netInterfaceAddresses {
		networkIP, ok := netInterfaceAddress.(*net.IPNet)
		System.IPAddressesList = append(System.IPAddressesList, networkIP.IP.String())

		if ok {
			var ip = networkIP.IP.String()

			if networkIP.IP.To4() != nil {
				System.IPAddressesV4 = append(System.IPAddressesV4, ip)
				System.IPAddressesV4Raw = append(System.IPAddressesV4Raw, networkIP.IP)

				if !networkIP.IP.IsLoopback() && ip[0:7] != "169.254" {
					System.IPAddressesV4Host = append(System.IPAddressesV4Host, ip)
				}
			} else {
				System.IPAddressesV6 = append(System.IPAddressesV6, ip)
			}
		}
	}

	// If IP previously set in settings (including the default, empty) is not available anymore
	if !lo.Contains(System.IPAddressesV4Host, Settings.HostIP) {
		Settings.HostIP = System.IPAddressesV4Host[0]
	}

	if len(Settings.HostIP) == 0 {
		switch len(System.IPAddressesV4) {
		case 0:
			if len(System.IPAddressesV6) > 0 {
				Settings.HostIP = System.IPAddressesV6[0]
			}
		default:
			Settings.HostIP = System.IPAddressesV4[0]
		}
	}

	System.Hostname, err = os.Hostname()
	if err != nil {
		return
	}
	return
}

// Miscellaneous
func randomString(n int) string {
	const alphanum = "AB1CD2EF3GH4IJ5KL6MN7OP8QR9ST0UVWXYZ"

	var bytes = make([]byte, n)

	if _, err := rand.Read(bytes); err != nil {
		log.Printf("Error reading random bytes for randomString: %v", err)
		// Fallback to a less random or empty string, or panic if this is critical
		// For now, returning a fixed string or empty to avoid panic,
		// but this might have security implications depending on usage.
		return "error_generating_random_string"
	}

	for i, b := range bytes {
		bytes[i] = alphanum[b%byte(len(alphanum))]
	}
	return string(bytes)
}

func parseTemplate(content string, tmpMap map[string]any) (result string) {
	t := template.Must(template.New("template").Parse(content))

	var tpl bytes.Buffer

	if err := t.Execute(&tpl, tmpMap); err != nil {
		ShowError(err, 0)
	}
	result = tpl.String()
	return
}

func getMD5(str string) string {
	md5Hasher := md5.New()
	if _, err := md5Hasher.Write([]byte(str)); err != nil {
		log.Printf("Error writing to md5 hasher: %v", err)
		// Return an empty string or a fixed error indicator.
		// This depends on how callers handle an MD5 generation failure.
		return "" // Or a specific error string like "md5_error"
	}
	return hex.EncodeToString(md5Hasher.Sum(nil))
}
