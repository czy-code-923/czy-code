package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vps-control/internal/executor"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfficialMetadataUsesCITokenWithoutSendingItToAssets(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-ci-token")
	for _, raw := range []string{"https://api.github.com/repos/SagerNet/sing-box/releases/latest", "https://api.github.com/repos/XTLS/Xray-core/releases/tags/v1.0.0", "https://github.com/SagerNet/sing-box/releases/download/v1.14.4/archive.tar.gz", "https://release-assets.githubusercontent.com/archive", "https://api.github.com.evil.test/repos/SagerNet/sing-box/releases/latest"} {
		client := Client()
		client.Transport = releaseTransport(func(r *http.Request) (*http.Response, error) {
			expected := ""
			if r.URL.Host == "api.github.com" {
				expected = "Bearer test-ci-token"
			}
			if r.Header.Get("Authorization") != expected {
				t.Fatal("unexpected release credential destination", r.URL.Host)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
		})
		if _, err := request(context.Background(), client, raw, 100); err != nil {
			t.Fatal(err)
		}
	}
	r, _ := http.NewRequest("GET", "https://release-assets.githubusercontent.com/archive", nil)
	r.Header.Set("Authorization", "Bearer test-ci-token")
	if err := Client().CheckRedirect(r, nil); err != nil || r.Header.Get("Authorization") != "" {
		t.Fatal("redirect retained API credential", err)
	}
}

func TestOfficialSelectionRejectsURLsAndUnverifiedAssets(t *testing.T) {
	for _, v := range []string{"https://evil.test/file", "../../bin", "1.14.4;id", "latest/other"} {
		if ValidVersion(v) {
			t.Fatal(v)
		}
	}
	r := Release{Tag: "v1.14.4", Assets: []Asset{{Name: "sing-box-1.14.4-linux-amd64.tar.gz", URL: "https://github.com/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-amd64.tar.gz", Digest: "sha256:" + strings.Repeat("a", 64), Size: 1024}}}
	if _, e := r.Asset("sing-box", "linux/amd64"); e != nil {
		t.Fatal(e)
	}
	r.Assets[0].URL = "https://evil.test/download"
	if _, e := r.Asset("sing-box", "linux/amd64"); e == nil {
		t.Fatal("custom URL accepted")
	}
	r.Assets[0].URL = "https://github.com/SagerNet/sing-box/releases/download/v1.14.4/file"
	r.Assets[0].Digest = ""
	if _, e := r.Asset("sing-box", "linux/amd64"); e == nil {
		t.Fatal("missing digest accepted")
	}
	for _, u := range []string{"http://github.com/file", "https://github.com.evil.test/file", "https://user:pass@github.com/file", "https://127.0.0.1/file"} {
		if officialURL(u) {
			t.Fatal(u)
		}
	}
}
func TestArchiveExtractsOnlyTheExpectedBinary(t *testing.T) {
	build := func(duplicate bool) []byte {
		var raw bytes.Buffer
		z := zip.NewWriter(&raw)
		f, _ := z.Create("package/sing-box")
		f.Write([]byte("binary"))
		f, _ = z.Create("../../outside")
		f.Write([]byte("ignored"))
		if duplicate {
			f, _ = z.Create("other/sing-box")
			f.Write([]byte("different"))
		}
		z.Close()
		return raw.Bytes()
	}
	body, e := ExtractBinary(build(false), "official.zip", "sing-box")
	if e != nil || string(body) != "binary" {
		t.Fatal(string(body), e)
	}
	if _, e = ExtractBinary(build(true), "official.zip", "sing-box"); e == nil {
		t.Fatal("ambiguous binaries accepted")
	}
}
func TestPersistedVersionIsDigestBoundAndRejectsTampering(t *testing.T) {
	dir := t.TempDir()
	m, e := NewManager(dir)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(m.dir, "sing-box", "v1.14.4", "sing-box")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte("fixture"), 0700)
	h := sha256.Sum256([]byte("fixture"))
	r := Request{ID: strings.Repeat("a", 32), Engine: "sing-box", Version: "v1.14.4"}
	m.state.Reports = append(m.state.Reports, Report{Request: r, State: "validating"})
	if e = m.Finish(r, executor.Binary{Path: path, SHA256: hex.EncodeToString(h[:])}, ""); e != nil {
		t.Fatal(e)
	}
	restored, e := NewManager(dir)
	if e != nil || restored.Overrides()["sing-box"].Path != path {
		t.Fatal(restored, e)
	}
	os.WriteFile(path, []byte("tampered"), 0700)
	if _, e = NewManager(dir); e == nil {
		t.Fatal("modified active engine accepted")
	}
}
