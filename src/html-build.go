package src

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io" // Added for io.EOF
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

var htmlFolder string
var goFile string
var mapName string
var packageName string

var blankMap = make(map[string]any)

// HTMLInit : Define file paths
// mapName = Name of the map to be created
// htmlFolder: HTML Files Folder
// packageName: Name of the package
func HTMLInit(name, pkg, folder, file string) {
	htmlFolder = folder
	goFile = file
	mapName = name
	packageName = pkg
}

// BuildGoFile : Creates the GO Document
func BuildGoFile() error {
	var err = checkHTMLFile(htmlFolder)

	if err != nil {
		return err
	}

	var content string
	content += `package ` + packageName + "\n\n"
	content += `var ` + mapName + ` = make(map[string]interface{})` + "\n\n"
	content += "func loadHTMLMap() {" + "\n\n"

	content += createMapFromFiles(htmlFolder) + "\n"

	content += "}" + "\n\n"
	// Propagate the error from writeStringToFile
	return writeStringToFile(goFile, content)
}

// GetHTMLString : base64 -> string
func GetHTMLString(base string) string {
	content, _ := base64.StdEncoding.DecodeString(base)
	return string(content)
}

func createMapFromFiles(folder string) string {
	var path = getLocalPath(folder)

	err := filepath.Walk(path, readFilesToMap)
	if err != nil {
		checkErr(err)
	}

	var content string

	// Sort map keys before writing to file to prevent git mark webUI.go as modified when no real changes has been made
	keys := make([]string, 0, len(blankMap))
	for k := range blankMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		var newKey = key
		content += `  ` + mapName + `["` + newKey + `"` + `] = "` + blankMap[key].(string) + `"` + "\n"
	}
	return content
}

func readFilesToMap(path string, info os.FileInfo, err error) error {
	if !info.IsDir() {
		var base64Str = fileToBase64(getLocalPath(path))
		blankMap[filepath.ToSlash(path)] = base64Str
	}
	return nil
}

func fileToBase64(file string) string {
	imgFile, err := os.Open(file)
	if err != nil {
		log.Printf("Error opening file %s: %v", file, err)
		return "" // Return empty string or handle error as appropriate
	}
	defer imgFile.Close()

	// create a new buffer base on file size
	fInfo, err := imgFile.Stat()
	if err != nil {
		log.Printf("Error stating file %s: %v", file, err)
		return "" // Return empty string or handle error as appropriate
	}
	var size = fInfo.Size()
	buf := make([]byte, int64(size))

	// read file content into buffer
	fReader := bufio.NewReader(imgFile)
	n, err := fReader.Read(buf)
	if err != nil && err != io.EOF { // EOF is not an error for Read, bufio.EOF is not a thing
		log.Printf("Error reading file %s: %v", file, err)
		return "" // Return empty string or handle error as appropriate
	}
	// It's good practice to use the actual number of bytes read
	imgBase64Str := base64.StdEncoding.EncodeToString(buf[:n])
	return imgBase64Str
}

func getLocalPath(filename string) string {
	path, file := filepath.Split(filename)
	var newPath = filepath.ToSlash(filepath.Dir(path))

	var newFileName = newPath + "/" + file
	return newFileName
}

func writeStringToFile(filename, content string) error {
	err := os.WriteFile(getPlatformFile(filename), []byte(content), 0644)
	if err != nil {
		checkErr(err)
		return err
	}
	return nil
}

func checkHTMLFile(filename string) error {
	if _, err := os.Stat(getLocalPath(filename)); os.IsNotExist(err) {
		fmt.Println(filename)
		checkErr(err)
		return err
	}
	return nil
}

func checkErr(err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		log.Println("ERROR: [", err, "] in ", file, line)
	}
}
