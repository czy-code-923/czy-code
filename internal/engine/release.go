// Package engine installs verified official releases in a private, versioned
// directory. Neither the API nor a task may supply a URL, path or shell command.
package engine

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"vps-control/internal/executor"
)

var tagPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-.][a-zA-Z0-9.-]+)?$`)

func Repository(name string) string {
	switch name {
	case "sing-box":
		return "SagerNet/sing-box"
	case "xray":
		return "XTLS/Xray-core"
	}
	return ""
}
func ValidVersion(version string) bool {
	return len(version) <= 64 && (version == "latest" || tagPattern.MatchString(version))
}

type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	Published  string  `json:"published_at"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}
type cachedRelease struct {
	value Release
	at    time.Time
}

var releaseCache = struct {
	sync.Mutex
	items map[string]cachedRelease
}{items: map[string]cachedRelease{}}

func officialURL(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func Client() *http.Client {
	return &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !officialURL(r.URL.String()) {
			return errors.New("unexpected official release redirect")
		}
		return nil
	}}
}
func request(ctx context.Context, client *http.Client, raw string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "Starhub-official-engine-manager")
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("official release connection failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("official release HTTP %d", res.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("official release exceeds size limit")
	}
	return data, e
}
func Resolve(ctx context.Context, name, version string) (Release, error) {
	repo := Repository(name)
	if repo == "" || !ValidVersion(version) {
		return Release{}, errors.New("unsupported engine or version")
	}
	if version != "latest" && !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	key := name + "/" + version
	releaseCache.Lock()
	cached, ok := releaseCache.items[key]
	releaseCache.Unlock()
	if ok && time.Since(cached.at) < time.Minute {
		return cached.value, nil
	}
	path := "tags/" + version
	if version == "latest" {
		path = "latest"
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	raw, e := request(ctx, Client(), "https://api.github.com/repos/"+repo+"/releases/"+path, 4<<20)
	if e != nil {
		return Release{}, e
	}
	var r Release
	if e = json.Unmarshal(raw, &r); e != nil {
		return r, e
	}
	if !tagPattern.MatchString(r.Tag) || (version == "latest" && r.Prerelease) || version != "latest" && r.Tag != version {
		return Release{}, errors.New("unexpected release version")
	}
	releaseCache.Lock()
	if len(releaseCache.items) >= 64 {
		releaseCache.items = map[string]cachedRelease{}
	}
	releaseCache.items[key] = cachedRelease{r, time.Now()}
	releaseCache.Unlock()
	return r, nil
}
func (r Release) Asset(name, platform string) (Asset, error) {
	parts := strings.Split(strings.ReplaceAll(platform, "/", "-"), "-")
	if len(parts) != 2 || (parts[0] != "linux" && parts[0] != "windows") || (parts[1] != "amd64" && parts[1] != "arm64") {
		return Asset{}, errors.New("unsupported platform")
	}
	wanted := ""
	if name == "sing-box" {
		extension := "tar.gz"
		if parts[0] == "windows" {
			extension = "zip"
		}
		wanted = "sing-box-" + strings.TrimPrefix(r.Tag, "v") + "-" + parts[0] + "-" + parts[1] + "." + extension
	} else if name == "xray" {
		arch := "64"
		if parts[1] == "arm64" {
			arch = "arm64-v8a"
		}
		wanted = "Xray-" + parts[0] + "-" + arch + ".zip"
	} else {
		return Asset{}, errors.New("unsupported engine")
	}
	for _, a := range r.Assets {
		if a.Name == wanted {
			if a.Size < 1 || a.Size > 160<<20 || len(a.Digest) != 71 || !strings.HasPrefix(a.Digest, "sha256:") || !strings.HasPrefix(a.URL, "https://github.com/"+Repository(name)+"/releases/download/"+r.Tag+"/") {
				return Asset{}, errors.New("official asset has no verified digest or URL")
			}
			if _, e := hex.DecodeString(a.Digest[7:]); e != nil {
				return Asset{}, errors.New("invalid official digest")
			}
			return a, nil
		}
	}
	return Asset{}, errors.New("version has no official binary for this platform")
}

func ExtractBinary(data []byte, archiveName, binary string) ([]byte, error) {
	matches := 0
	var result []byte
	take := func(name string, size int64, reader io.Reader) error {
		if filepath.Base(strings.ReplaceAll(name, "\\", "/")) != binary {
			return nil
		}
		matches++
		if matches != 1 || size <= 0 || size > 160<<20 {
			return errors.New("ambiguous or oversized engine archive")
		}
		raw, e := io.ReadAll(io.LimitReader(reader, (160<<20)+1))
		if len(raw) > 160<<20 {
			return errors.New("engine exceeds limit")
		}
		result = raw
		return e
	}
	if strings.HasSuffix(archiveName, ".zip") {
		z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, e
		}
		for _, f := range z.File {
			if f.Mode().IsRegular() && filepath.Base(f.Name) == binary {
				reader, e := f.Open()
				if e != nil {
					return nil, e
				}
				e = take(f.Name, int64(f.UncompressedSize64), reader)
				reader.Close()
				if e != nil {
					return nil, e
				}
			}
		}
	} else {
		gz, e := gzip.NewReader(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if h.Typeflag == tar.TypeReg {
				if e = take(h.Name, h.Size, tr); e != nil {
					return nil, e
				}
			}
		}
	}
	if matches != 1 {
		return nil, errors.New("engine binary missing from official archive")
	}
	return result, nil
}
func Stage(ctx context.Context, dir, name, version, platform string) (executor.Binary, error) {
	release, e := Resolve(ctx, name, version)
	if e != nil {
		return executor.Binary{}, e
	}
	a, e := release.Asset(name, platform)
	if e != nil {
		return executor.Binary{}, e
	}
	binary := name
	if strings.HasPrefix(platform, "windows") {
		binary += ".exe"
	}
	target := filepath.Join(dir, name, release.Tag+"-"+strings.TrimPrefix(a.Digest, "sha256:"))
	var receipt struct {
		Archive string
		Binary  string
	}
	if raw, err := os.ReadFile(filepath.Join(target, "verified.json")); err == nil && json.Unmarshal(raw, &receipt) == nil && receipt.Archive == a.Digest {
		path := filepath.Join(target, binary)
		if hash, err := executor.FileDigest(path); err == nil && hash == receipt.Binary {
			return executor.Binary{Path: path, SHA256: hash}, nil
		}
	}
	data, e := request(ctx, Client(), a.URL, 160<<20)
	if e != nil {
		return executor.Binary{}, e
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != a.Size || "sha256:"+hex.EncodeToString(sum[:]) != a.Digest {
		return executor.Binary{}, errors.New("official archive digest mismatch")
	}
	raw, e := ExtractBinary(data, a.Name, binary)
	if e != nil {
		return executor.Binary{}, e
	}
	if e = os.MkdirAll(target, 0700); e != nil {
		return executor.Binary{}, e
	}
	// OpenRoot keeps all writes beneath the private version store, including links.
	root, e := os.OpenRoot(dir)
	if e != nil {
		return executor.Binary{}, e
	}
	defer root.Close()
	relative, _ := filepath.Rel(dir, filepath.Join(target, binary))
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	existing := filepath.Join(dir, relative)
	actual, existingError := executor.FileDigest(existing)
	if existingError != nil || actual != digest {
		candidate := relative + ".candidate"
		f, e := root.OpenFile(candidate, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
		if e != nil {
			return executor.Binary{}, e
		}
		_, e = f.Write(raw)
		if e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return executor.Binary{}, e
		}
		if e = root.Rename(candidate, relative); e != nil {
			return executor.Binary{}, e
		}
	}
	if name == "xray" {
		for _, asset := range []string{"geoip.dat", "geosite.dat"} {
			body, err := ExtractBinary(data, a.Name, asset)
			if err != nil {
				continue
			}
			file, err := root.OpenFile(filepath.Join(filepath.Dir(relative), asset), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				return executor.Binary{}, err
			}
			_, err = file.Write(body)
			ce := file.Close()
			if err == nil {
				err = ce
			}
			if err != nil {
				return executor.Binary{}, err
			}
		}
	}
	receipt.Archive = a.Digest
	receipt.Binary = digest
	provenance, _ := json.Marshal(receipt)
	file, e := root.OpenFile(filepath.Join(filepath.Dir(relative), "verified.json"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return executor.Binary{}, e
	}
	_, e = file.Write(provenance)
	ce := file.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return executor.Binary{}, e
	}
	return executor.Binary{Path: filepath.Join(dir, relative), SHA256: hex.EncodeToString(hash[:])}, nil
}
