package src

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func zipFiles(sourceFiles []string, target string) error {
	zipfile, err := os.Create(target)
	if err != nil {
		return err
	}
	defer zipfile.Close()

	archive := zip.NewWriter(zipfile)
	defer archive.Close()

	for _, source := range sourceFiles {
		info, err := os.Stat(source)
		if err != nil {
			return nil
		}

		var baseDir string
		if info.IsDir() {
			baseDir = filepath.Base(System.Folder.Data)
		}

		walkErr := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}

			if baseDir != "" {
				header.Name = filepath.Join(strings.TrimPrefix(path, System.Folder.Config))
			}

			if info.IsDir() {
				header.Name += string(os.PathSeparator)
			} else {
				header.Method = zip.Deflate
			}

			writer, err := archive.CreateHeader(header)
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(writer, file)
			return err
		})
		// If filepath.Walk itself had an error (e.g. source path issue)
		// or if the last error from the walkFn was not handled inside.
		if walkErr != nil {
			return walkErr
		}
	}
	// The original 'err' was from os.Stat(source) which is inside the loop.
	// It should be nil here if the loop completed or returned earlier.
	// If sourceFiles is empty, err would be from the last os.Stat, which is incorrect.
	// The function should return nil if everything is successful.
	return nil
}

func extractZIP(archive, target string) (err error) {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}

	for _, file := range reader.File {
		path := filepath.Join(target, file.Name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(path, file.Mode()); err != nil {
				// Log the error and continue with other files/dirs
				// log.Printf("Error creating directory %s during ZIP extraction: %v", path, err)
				// Depending on desired strictness, could accumulate errors and return at the end
			}
			continue
		}

		fileReader, err := file.Open()
		if err != nil {
			return err
		}
		defer fileReader.Close()

		targetFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return err
		}
		defer targetFile.Close()

		if _, err := io.Copy(targetFile, fileReader); err != nil {
			return err
		}
	}
	return
}

func extractGZIP(gzipBody []byte, fileSource string) (body []byte, err error) {
	var b = bytes.NewBuffer(gzipBody)

	var r io.Reader
	r, err = gzip.NewReader(b)
	if err != nil {
		// Not a gzip file
		body = gzipBody
		err = nil
		return
	}

	showInfo("Extract gzip:" + fileSource)

	var resB bytes.Buffer
	_, err = resB.ReadFrom(r)
	if err != nil {
		body = gzipBody
		err = nil
		return
	}

	body = resB.Bytes()
	return
}

func compressGZIP(data *[]byte, file string) (err error) {
	if len(file) != 0 {
		f, err := os.Create(file)
		if err != nil {
			return err
		}

		w := gzip.NewWriter(f)
		if _, errWrite := w.Write(*data); errWrite != nil {
			// Attempt to close f even if write failed, then return the write error
			_ = f.Close() // Best effort close, ignore error as we're returning writeErr
			return errWrite
		}
		if errClose := w.Close(); errClose != nil {
			// Attempt to close f even if gzip close failed, then return the close error
			_ = f.Close() // Best effort close
			return errClose
		}
		// Ensure underlying file is also closed
		if errFileClose := f.Close(); errFileClose != nil {
			return errFileClose
		}
	}
	return
}
