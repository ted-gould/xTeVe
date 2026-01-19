package src

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDataImagesCSP verifies that the /data_images/ endpoint serves SVG files with
// a restrictive Content-Security-Policy to prevent Stored XSS.
func TestDataImagesCSP(t *testing.T) {
	// Setup temporary directory structure for testing
	tempDir := t.TempDir()
	imagesUploadDir := filepath.Join(tempDir, "images_upload")
	if err := os.Mkdir(imagesUploadDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Mock System.Folder.ImagesUpload
	originalImagesUpload := System.Folder.ImagesUpload
	originalXML := System.ServerProtocol.XML
	originalDomain := System.Domain

	defer func() {
		System.Folder.ImagesUpload = originalImagesUpload
		System.ServerProtocol.XML = originalXML
		System.Domain = originalDomain
	}()

	System.Folder.ImagesUpload = imagesUploadDir + string(os.PathSeparator)
	System.ServerProtocol.XML = "http"
	System.Domain = "localhost:34400"

	// Create a dummy SVG file
	svgFilename := "xss.svg"
	svgContent := `<?xml version="1.0" standalone="no"?>
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
<svg version="1.1" baseProfile="full" xmlns="http://www.w3.org/2000/svg">
   <script type="text/javascript">alert("XSS");</script>
</svg>`

	if err := os.WriteFile(filepath.Join(imagesUploadDir, svgFilename), []byte(svgContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a request to the handler
	req, err := http.NewRequest("GET", "/data_images/"+svgFilename, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder to record the response
	rr := httptest.NewRecorder()

	// We need to use the full handler stack including middleware because CSP is set in middleware
	handler := newHTTPHandler()

	handler.ServeHTTP(rr, req)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check the Content-Type
	expectedContentType := "image/svg+xml"
	if contentType := rr.Header().Get("Content-Type"); contentType != expectedContentType {
		t.Errorf("handler returned wrong content type: got %v want %v", contentType, expectedContentType)
	}

	// Check the Content-Security-Policy header
	csp := rr.Header().Get("Content-Security-Policy")

	// We expect the CSP to be strict for images, specifically containing 'sandbox'
	if csp == "" {
		t.Errorf("Content-Security-Policy header is missing")
	} else {
		// Positive assertion: Must contain "sandbox"
		if !strings.Contains(csp, "sandbox") {
			t.Errorf("VULNERABILITY CONFIRMED: Content-Security-Policy header does not contain 'sandbox': %s", csp)
		}
	}
	fmt.Printf("CSP Header: %s\n", csp)
}
