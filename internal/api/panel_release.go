package api

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"vps-control/internal/buildinfo"
)

var panelTag = regexp.MustCompile(`^v([0-9]{1,5})\.([0-9]{1,5})\.([0-9]{1,5})-distribution$`)

func panelNewer(latest, current string) bool {
	parse := func(s string) []string {
		return strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "v"), "-distribution"), ".")
	}
	a, b := parse(latest), parse(current)
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		x, e := strconv.Atoi(a[i])
		y, f := strconv.Atoi(b[i])
		if e != nil || f != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func (s *Server) panelRelease(w http.ResponseWriter, r *http.Request) error {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if s.panelReleaseClient != nil {
		client = s.panelReleaseClient
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://api.github.com/repos/czy-code-923/czy-code/releases/latest", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Starhub-release-check")
	response, err := client.Do(req)
	if err != nil {
		return problem{502, "无法连接 GitHub 检查版本"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return problem{502, "GitHub 版本查询失败"}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return problem{502, "GitHub 版本响应无效"}
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(raw, &release) != nil || release.Draft || release.Prerelease || !panelTag.MatchString(release.Tag) {
		return problem{502, "GitHub 正式版本格式无效"}
	}
	send(w, 200, map[string]any{"current": buildinfo.Version, "latest": strings.TrimSuffix(strings.TrimPrefix(release.Tag, "v"), "-distribution"), "update_available": panelNewer(release.Tag, buildinfo.Version), "release_url": "https://github.com/czy-code-923/czy-code/releases/tag/" + release.Tag, "automatic_update": false})
	return nil
}
