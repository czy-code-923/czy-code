package engine

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"vps-control/internal/executor"
)

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
