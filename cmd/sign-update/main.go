// sign-update signs the final notarized archive for MyGo's update protocol.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func run() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: sign-update ARCHIVE VERSION KEY_FILE")
	}
	archive, version := os.Args[1], os.Args[2]
	encoded, err := os.ReadFile(os.Args[3])
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid update private key")
	}
	pub := ed25519.PrivateKey(key).Public().(ed25519.PublicKey)
	if base64.StdEncoding.EncodeToString(pub) != "XnQ//4wwe7Mh4wyzOCtmlhFM5uAgl5vXsmTvZj/m8jI=" {
		return fmt.Errorf("key does not match the app's update public key")
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	manifest := struct {
		Version   string `json:"version"`
		Date      string `json:"date"`
		URL       string `json:"url"`
		Size      int    `json:"size"`
		Signature string `json:"signature"`
	}{version, time.Now().UTC().Format(time.RFC3339), "https://github.com/txchen/mygo-music-lite/releases/download/v" + version + "/" + filepath.Base(archive), len(data), base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest[:]))}
	result, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(filepath.Dir(archive), "update-darwin-arm64.json"), append(result, '\n'), 0644)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
