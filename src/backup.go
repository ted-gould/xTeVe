package src

import (
	b64 "encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func xTeVeAutoBackup() (err error) {
	var archive = "xteve_auto_backup_" + time.Now().Format("20060102_1504") + ".zip"
	var target string
	var sourceFiles = make([]string, 0)
	var oldBackupFiles = make([]string, 0)
	var debug string

	if len(Settings.BackupPath) > 0 {
		System.Folder.Backup = Settings.BackupPath
	}

	showInfo("Backup Path:" + System.Folder.Backup)

	err = checkFolder(System.Folder.Backup)
	if err != nil {
		ShowError(err, 1070)
		return
	}

	// Delete Old Backups
	files, err := os.ReadDir(System.Folder.Backup)

	if err == nil {
		for _, file := range files {
			if filepath.Ext(file.Name()) == ".zip" && strings.Contains(file.Name(), "xteve_auto_backup") {
				oldBackupFiles = append(oldBackupFiles, file.Name())
			}
		}

		// Delete All Backups
		var end int
		switch Settings.BackupKeep {
		case 0:
			end = 0
		default:
			end = Settings.BackupKeep - 1
		}

		for i := 0; i < len(oldBackupFiles)-end; i++ { // Corrected loop condition
			backupFileToDelete := System.Folder.Backup + oldBackupFiles[i]
			if errRemove := os.RemoveAll(backupFileToDelete); errRemove != nil {
				// Log the error, but continue trying to delete other old backups
				// log.Printf("Error deleting old backup file %s: %v", backupFileToDelete, errRemove)
				// Potentially, update 'err' to return a generic error indicating some cleanup failed
			}
			debug = fmt.Sprintf("Delete backup file:%s", oldBackupFiles[i])
			showDebug(debug, 1)
		}

		if Settings.BackupKeep == 0 {
			return
		}
	} else {
		return
	}

	// Create a Backup
	target = System.Folder.Backup + archive

	for _, i := range SystemFiles {
		sourceFiles = append(sourceFiles, System.Folder.Config+i)
	}

	sourceFiles = append(sourceFiles, System.Folder.ImagesUpload)
	if Settings.TLSMode {
		sourceFiles = append(sourceFiles, System.Folder.Certificates)
	}

	err = zipFiles(sourceFiles, target)

	if err == nil {
		debug = fmt.Sprintf("Create backup file:%s", target)
		showDebug(debug, 1)

		showInfo("Backup file:" + target)
	}

	return
}

func xteveBackup() (archive string, err error) {
	err = checkFolder(System.Folder.Temp)
	if err != nil {
		return
	}

	archive = "xteve_backup_" + time.Now().Format("20060102_1504") + ".zip"

	var target = System.Folder.Temp + archive
	var sourceFiles = make([]string, 0)

	for _, i := range SystemFiles {
		sourceFiles = append(sourceFiles, System.Folder.Config+i)
	}

	sourceFiles = append(sourceFiles, System.Folder.Data)
	if Settings.TLSMode {
		sourceFiles = append(sourceFiles, System.Folder.Certificates)
	}

	err = zipFiles(sourceFiles, target)
	if err != nil {
		ShowError(err, 0)
		return
	}

	return
}

func xteveRestore(archive string) (newWebURL string, err error) {
	var newPort, oldPort, backupVersion, tmpRestore string

	tmpRestore = System.Folder.Temp + "restore" + string(os.PathSeparator)

	defer os.RemoveAll(tmpRestore)
	defer os.Remove(archive)

	err = checkFolder(tmpRestore)
	if err != nil {
		return
	}

	// Unpack the ZIP Archive in tmp
	err = extractZIP(archive, tmpRestore)
	if err != nil {
		return
	}

	// Load a new Config to check the Port and Version
	newConfig, err := loadJSONFileToMap(tmpRestore + "settings.json")
	if err != nil {
		ShowError(err, 0)
		return
	}

	backupVersion = newConfig["version"].(string)
	if backupVersion < System.Compatibility {
		err = errors.New(getErrMsg(1013))
		return
	}

	if err = removeChildItems(getPlatformPath(System.Folder.Config)); err != nil {
		ShowError(err, 1073)
		return // Propagate the error
	}

	// Extract the ZIP Archive into the Config Folder
	err = extractZIP(archive, System.Folder.Config)
	if err != nil {
		return
	}

	// Load a new Config to check the Port and Version
	newConfig, err = loadJSONFileToMap(System.Folder.Config + "settings.json")
	if err != nil {
		ShowError(err, 0)
		return
	}

	newPort = newConfig["port"].(string)
	oldPort = Settings.Port

	if newPort == oldPort {
		if err != nil {
			ShowError(err, 0)
			// Even if newPort == oldPort, err might have been set by a previous operation.
			// We should return it if it's not nil.
			if err != nil {
				return "", err
			}
		}

		// loadSettings likely returns (SettingsStruct, error)
		// We only care about the error here as Settings is a global variable modified by loadSettings.
		if _, err = loadSettings(); err != nil {
			ShowError(err, 0) // Or choose to propagate it
			return "", err // Propagating seems more appropriate for a restore failure
		}

		err = Init()
		if err != nil {
			ShowError(err, 0)
			return "", err
		}

		err = StartSystem(true)
		if err != nil {
			ShowError(err, 0)
			return "", err
		}

		return "", err
	}

	var url = System.URLBase + "/web/"
	newWebURL = strings.Replace(url, ":"+oldPort, ":"+newPort, 1)

	return
}

func xteveRestoreFromWeb(input string) (newWebURL string, err error) {
	// Convert base64 JSON string to base64
	b64data := input[strings.IndexByte(input, ',')+1:]

	// Convert Base64 into bytes and save
	sDec, err := b64.StdEncoding.DecodeString(b64data)

	if err != nil {
		return
	}

	var archive = System.Folder.Temp + "restore.zip"

	err = writeByteToFile(archive, sDec)
	if err != nil {
		return
	}

	newWebURL, err = xteveRestore(archive)

	return
}

// XteveRestoreFromCLI : Recovery from the Command Line
func XteveRestoreFromCLI(archive string) (err error) {
	var confirm string

	println()
	showInfo(fmt.Sprintf("Version:%s Build: %s", System.Version, System.Build))
	showInfo(fmt.Sprintf("Backup File:%s", archive))
	showInfo(fmt.Sprintf("System Folder:%s", getPlatformPath(System.Folder.Config)))
	println()

	fmt.Print("All data will be replaced with those from the backup. Should the files be restored? [yes|no]:")

	if _, errScan := fmt.Scanln(&confirm); errScan != nil {
		fmt.Println("Error reading input:", errScan)
		return errScan // Propagate the error
	}

	switch strings.ToLower(confirm) {
	case "yes":
		break

	case "no":
		return
	default:
		fmt.Println("Invalid input")
		return
	}

	if len(System.Folder.Config) > 0 {
		err = checkFilePermission(System.Folder.Config)
		if err != nil {
			return
		}

		_, err = xteveRestore(archive)
		if err != nil {
			return
		}

		showHighlight(fmt.Sprintf("Restor:Backup was successfully restored. %s can now be started normally", System.Name))
	}
	return
}
