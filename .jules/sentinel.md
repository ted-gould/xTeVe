# Sentinel Journal

## 2026-01-13 - Stored XSS in Logo Upload
**Vulnerability:** Arbitrary File Upload leading to Stored XSS. The `uploadLogo` function in `src/images.go` relied solely on `filepath.Base` for sanitization, which prevents path traversal but does not validate file types. This allowed attackers to upload HTML files with malicious scripts. These files could then be served by the webserver with `Content-Type: text/html` (via `src/webserver.go:getContentType`), executing the script in the context of the application.
**Learning:** File upload features must always validate the file content and extension against a strict allowlist. Reliance on frontend validation or assuming benign input is dangerous. Even if the file is saved to a specific directory, if that directory is served by the webserver, the file type matters.
**Prevention:**
1.  Implement strict allowlist validation for file extensions (e.g., `.jpg`, `.png`).
2.  (Ideally) Validate the file content (magic numbers/MIME type) to ensure it matches the extension.
3.  Serve user-uploaded content with `Content-Type: application/octet-stream` or `Content-Disposition: attachment` if possible, or from a separate domain (sandbox domain) to prevent XSS on the main application domain.
4.  Use a strict Content Security Policy (CSP) that restricts script execution (though `unsafe-inline` was required here for legacy reasons, making this vulnerability more critical).
