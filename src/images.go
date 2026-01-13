package src

import (
	b64 "encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func uploadLogo(input, filename string) (logoURL string, err error) {
	b64data := input[strings.IndexByte(input, ',')+1:]

	// Convert Base64 into bytes and save
	sDec, err := b64.StdEncoding.DecodeString(b64data)
	if err != nil {
		return
	}

	// Sanitize filename to prevent path traversal
	filename = filepath.Base(filename)

	// Security: Validate file extension to prevent uploading malicious files (e.g., HTML for XSS)
	allowedExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".svg":  true,
		".ico":  true,
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowedExts[ext] {
		err = errors.New("invalid file extension: only image files are allowed")
		return
	}

	var file = fmt.Sprintf("%s%s", System.Folder.ImagesUpload, filename)

	err = writeByteToFile(file, sDec)
	if err != nil {
		return
	}

	logoURL = fmt.Sprintf("%s://%s/data_images/%s", System.ServerProtocol.XML, System.Domain, filename)
	return
}
