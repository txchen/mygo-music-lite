package plugin

import (
	"net/url"
	"testing"
)

// Audio streams carry the plugin's network permission snapshot into player requests and redirects.
func TestStreamNetworkAuthorization(t *testing.T) {
	for _, c := range []struct {
		name, manifest string
		granted        bool
		allow          []string
		deny           []string
	}{
		{name: "none", manifest: `{"id":"x"}`, deny: []string{"https://audio.example/a", "http://127.0.0.1/a"}},
		{name: "declared", manifest: `{"id":"x","permissions":{"network":["audio.example"]}}`, allow: []string{"https://audio.example/a"}, deny: []string{"https://cdn.example/a"}},
		{name: "process denied", manifest: `{"id":"x","permissions":{"process":true}}`, deny: []string{"http://127.0.0.1/a", "http://localhost/a"}},
		{name: "process granted", manifest: `{"id":"x","permissions":{"process":true}}`, granted: true, allow: []string{"http://127.0.0.1/a", "http://localhost/a"}, deny: []string{"https://audio.example/a"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := load(t, c.manifest, `export function stream(){return {url:"https://audio.example/a",type:"mp3"}}`)
			p.mu.Lock()
			p.granted = c.granted
			p.mu.Unlock()
			st, err := p.Stream("a", "")
			if err != nil {
				t.Fatal(err)
			}
			if st.AuthorizeURL == nil {
				t.Fatal("stream lost permission callback")
			}
			// Later manifest or grant changes must not broaden an already resolved stream.
			p.mu.Lock()
			p.granted = true
			p.manifest.Permissions.Network = []string{"*"}
			p.mu.Unlock()
			for _, raw := range c.allow {
				u, _ := url.Parse(raw)
				if err := st.AuthorizeURL(u); err != nil {
					t.Errorf("denied %s: %v", raw, err)
				}
			}
			for _, raw := range c.deny {
				u, _ := url.Parse(raw)
				if err := st.AuthorizeURL(u); err == nil {
					t.Errorf("allowed %s", raw)
				}
			}
		})
	}
}
