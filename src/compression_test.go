package src

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZipAndExtract_Files(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	// 1. Create files to zip
	file1Path := filepath.Join(tempDir, "file1.txt")
	err := os.WriteFile(file1Path, []byte("hello world"), 0644)
	require.NoError(t, err)

	file2Path := filepath.Join(tempDir, "file2.txt")
	err = os.WriteFile(file2Path, []byte("foo bar"), 0644)
	require.NoError(t, err)

	// 2. Zip the files
	zipFilePath := filepath.Join(tempDir, "archive.zip")
	err = zipFiles([]string{file1Path, file2Path}, zipFilePath)
	require.NoError(t, err)

	// Check if the zip file was created
	_, err = os.Stat(zipFilePath)
	require.NoError(t, err, "zip file should be created")

	// 3. Extract the zip file to a destination directory
	destDir := filepath.Join(tempDir, "destination")
	err = extractZIP(zipFilePath, destDir)
	require.NoError(t, err)

	// 4. Verify the extracted files
	extractedFile1Path := filepath.Join(destDir, "file1.txt")
	content1, err := os.ReadFile(extractedFile1Path)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(content1))

	extractedFile2Path := filepath.Join(destDir, "file2.txt")
	content2, err := os.ReadFile(extractedFile2Path)
	require.NoError(t, err)
	require.Equal(t, "foo bar", string(content2))
}

func TestZipAndExtract_Directory(t *testing.T) {
	tempDir := t.TempDir()

	// zipFiles has a dependency on the global System object.
	// We need to set the config folder to the temp directory
	// so the zip file paths are created correctly.
	System.Folder.Config = tempDir

	// The function also has a dependency on System.Folder.Data but its value is not used.
	// We set it just in case.
	System.Folder.Data = filepath.Join(tempDir, "data")
	err := os.Mkdir(System.Folder.Data, 0755)
	require.NoError(t, err)


	sourceDir := filepath.Join(tempDir, "source")
	err = os.Mkdir(sourceDir, 0755)
	require.NoError(t, err)

	file1Path := filepath.Join(sourceDir, "file1.txt")
	err = os.WriteFile(file1Path, []byte("content1"), 0644)
	require.NoError(t, err)

	subDir := filepath.Join(sourceDir, "subdir")
	err = os.Mkdir(subDir, 0755)
	require.NoError(t, err)

	file2Path := filepath.Join(subDir, "file2.txt")
	err = os.WriteFile(file2Path, []byte("content2"), 0644)
	require.NoError(t, err)

	zipFilePath := filepath.Join(tempDir, "archive.zip")
	err = zipFiles([]string{sourceDir}, zipFilePath)
	require.NoError(t, err)

	destDir := filepath.Join(tempDir, "destination")
	err = extractZIP(zipFilePath, destDir)
	require.NoError(t, err)

	// The file paths in the zip are relative to System.Folder.Config
	// So the extracted path will be destDir/source/file1.txt
	extractedFile1Path := filepath.Join(destDir, filepath.Base(sourceDir), "file1.txt")
	content1, err := os.ReadFile(extractedFile1Path)
	require.NoError(t, err)
	require.Equal(t, "content1", string(content1))

	extractedFile2Path := filepath.Join(destDir, filepath.Base(sourceDir), "subdir", "file2.txt")
	content2, err := os.ReadFile(extractedFile2Path)
	require.NoError(t, err)
	require.Equal(t, "content2", string(content2))
}

func TestGzipAndExtract(t *testing.T) {
	// 1. Define test data
	originalContent := "this is a test string for gzip"
	data := []byte(originalContent)

	// 2. Compress the data to a temporary file
	tempDir := t.TempDir()
	gzipFilePath := filepath.Join(tempDir, "test.gz")

	err := compressGZIP(&data, gzipFilePath)
	require.NoError(t, err)

	// 3. Read the compressed data
	compressedData, err := os.ReadFile(gzipFilePath)
	require.NoError(t, err)

	// 4. Extract the data
	extractedData, err := extractGZIP(compressedData, gzipFilePath)
	require.NoError(t, err)

	// 5. Verify the content
	require.Equal(t, originalContent, string(extractedData))
}

func TestExtractGZIP_UncompressedData(t *testing.T) {
	originalContent := "this is not gzipped"
	data := []byte(originalContent)

	extractedData, err := extractGZIP(data, "dummy.txt")
	require.NoError(t, err)
	require.Equal(t, originalContent, string(extractedData))
}
