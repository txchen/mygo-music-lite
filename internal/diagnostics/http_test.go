package diagnostics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Network tracing keeps timings while excluding request credentials and addresses.
func TestHTTPPrivacy(t *testing.T) {
	Set(false)
	Set(true)
	defer Set(false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("private response")) }))
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/private-path?token=secret-token", nil)
	req.Header.Set("Authorization", "Bearer private-credential")
	res, err := (Transport{}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	data, _ := Snapshot("test")
	for _, secret := range []string{server.URL, "private-path", "secret-token", "private-credential", "private response"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("log contains private data: %q", secret)
		}
	}
	for _, event := range []string{"plugin_http", "http_headers", "http_first_byte"} {
		if !strings.Contains(string(data), event) {
			t.Fatalf("missing %s", event)
		}
	}
}
