## 2024-05-23 - Missing Security Headers
**Vulnerability:** The web server was missing standard security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy, HSTS).
**Learning:** Even in internal tools or Go backend services, default http.ServeMux does not include modern security headers.
**Prevention:** Always wrap the main handler with a security middleware that sets these headers. Conditional headers like HSTS should check configuration.
