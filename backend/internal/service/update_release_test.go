package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReleasesAPIURL(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	got := svc.releasesAPIURL()
	want := "https://api.github.com/repos/shenwei234/inkstone/releases/latest"
	if got != want {
		t.Fatalf("默认地址 = %q，期望 %q", got, want)
	}

	svc.cfg.UpdateReleasesAPI = "{api}/repos/{owner}/{name}/releases?per_page=1"
	got = svc.releasesAPIURL()
	want = "https://api.github.com/repos/shenwei234/inkstone/releases?per_page=1"
	if got != want {
		t.Fatalf("模板地址 = %q，期望 %q", got, want)
	}
}

func TestFetchLatestReleaseAndCheck(t *testing.T) {
	release := map[string]any{
		"tag_name":     "v1.28.0",
		"name":         "v1.28.0 镜像包",
		"body":         "## 更新\n- PoW 验证码",
		"html_url":     "https://github.com/shenwei234/inkstone/releases/tag/v1.28.0",
		"published_at": "2026-10-01T10:00:00Z",
		"prerelease":   false,
		"draft":        false,
		"assets": []map[string]any{{
			"name":                 "inkstone-images-v1.28.0.tar",
			"size":                 800,
			"browser_download_url": "https://example.com/inkstone-images-v1.28.0.tar",
			"content_type":         "application/x-tar",
		}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/shenwei234/inkstone/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(release)
	}))
	defer server.Close()

	source := t.TempDir()
	svc := newTestUpdateService(t, source)
	svc.cfg.UpdateSource = "releases" // 测试走 Releases 源
	svc.cfg.UpdateGitHubAPI = server.URL

	// 本地版本 v1.27.0 → 应有更新
	if err := svc.writeDeployedVersion("v1.27.0"); err != nil {
		t.Fatalf("写部署版本失败：%v", err)
	}
	status, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("检查更新失败：%v", err)
	}
	if !status.Version.UpdateAvail {
		t.Fatal("本地 v1.27.0 < 上游 v1.28.0，应提示有更新")
	}
	if status.Version.LatestVersion != "v1.28.0" {
		t.Fatalf("latest_version = %q，期望 v1.28.0", status.Version.LatestVersion)
	}
	if status.Source != "releases" {
		t.Fatalf("source = %q，期望 releases", status.Source)
	}
	if status.Version.Release == nil || len(status.Version.Release.Assets) != 1 {
		t.Fatal("应带出 Release 资产信息")
	}

	// 本地版本即上游最新 → 无更新
	if err := svc.writeDeployedVersion("v1.28.0"); err != nil {
		t.Fatalf("写部署版本失败：%v", err)
	}
	status, err = svc.Check(context.Background())
	if err != nil {
		t.Fatalf("检查更新失败：%v", err)
	}
	if status.Version.UpdateAvail {
		t.Fatal("本地 v1.28.0 = 上游最新，不应提示有更新")
	}
}

func TestLocalVersionFallbackToAppVersion(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	version, from := svc.localVersion()
	if version != AppVersion || from != "app_version" {
		t.Fatalf("无版本记录时应回落到 AppVersion：got %q（%s）", version, from)
	}

	// 更新目录下的版本文件优先（writeDeployedVersion 会建目录，与代理写回路径一致）
	if err := svc.writeDeployedVersion("v1.28.0"); err != nil {
		t.Fatalf("写部署版本失败：%v", err)
	}
	version, from = svc.localVersion()
	if version != "v1.28.0" || from != "version_file" {
		t.Fatalf("应读到版本文件：got %q（%s）", version, from)
	}
}

func TestStaleSnapshotIgnoredAfterSourceSwitch(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	svc.cfg.UpdateSource = "releases"

	// commits 模式的旧快照（没有版本号）不能被 releases 模式采信，
	// 否则页面会显示「有更新」却给不出目标版本
	svc.state.Check = &VersionInfo{
		Kind:        pendingKindSource,
		Latest:      CommitBrief{Hash: "ba0677b8c7defa22c0a296630f5e63199174bd79"},
		UpdateAvail: true,
	}
	info := svc.buildVersionInfo()
	if info.UpdateAvail {
		t.Fatal("releases 模式不应采信 commits 的旧快照")
	}
	if info.LatestVersion != "" || info.Release != nil {
		t.Fatal("旧快照的版本字段不应透传")
	}

	// 同代（release_image）快照正常采信。
	//
	// 目标版本必须**动态高于当前 AppVersion**：buildVersionInfo 会用
	// compareVersions 重算 UpdateAvail，写死一个具体版本号的话，
	// 每次给 AppVersion 升版本都可能让「更高版本」变成「同代/更低」而
	// 莫名失败（Beta1.27 → Beta1.28 时 v1.28.0 就从"更新"变成了"同代"）。
	cur, ok := parseVersionValue(AppVersion)
	if !ok {
		t.Fatalf("AppVersion %q 无法解析为版本号，测试前提不成立", AppVersion)
	}
	newerVersion := fmt.Sprintf("v%d.%d.0", cur.major, cur.minor+1)
	svc.state.Check = &VersionInfo{
		Kind:          pendingKindReleaseImage,
		LatestVersion: newerVersion,
		UpdateAvail:   true,
	}
	info = svc.buildVersionInfo()
	if !info.UpdateAvail || info.LatestVersion != newerVersion {
		t.Fatalf("releases 模式的检查快照应被采信：%+v", info)
	}
}

func TestParseVersionValue(t *testing.T) {
	cases := []struct {
		raw    string
		want   versionValue
		wantOK bool
	}{
		{"Beta1.27", versionValue{major: 1, minor: 27}, true},
		{"beta1.27", versionValue{major: 1, minor: 27}, true},
		{"v1.28.0", versionValue{major: 1, minor: 28}, true},
		{"V1.28.0", versionValue{major: 1, minor: 28}, true},
		{"1.28", versionValue{major: 1, minor: 28}, true},
		{"1.28.3", versionValue{major: 1, minor: 28, patch: 3}, true},
		{"v1.28.0-rc1", versionValue{major: 1, minor: 28, pre: "rc1"}, true},
		{"v1.28.0b3", versionValue{major: 1, minor: 28}, true},
		{"", versionValue{}, false},
		{"v", versionValue{}, false},
		{"v1.x", versionValue{}, false},
		{"v1.2.3.4", versionValue{}, false},
	}
	for _, c := range cases {
		got, ok := parseVersionValue(c.raw)
		if ok != c.wantOK {
			t.Fatalf("parseVersionValue(%q) ok=%v，期望 %v", c.raw, ok, c.wantOK)
		}
		if ok && got != c.want {
			t.Fatalf("parseVersionValue(%q)=%+v，期望 %+v", c.raw, got, c.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // -1 / 0 / 1
	}{
		{"Beta1.27", "v1.28.0", -1}, // 老写法 vs 语义化 tag：小版本落后
		{"v1.28.0", "Beta1.28", 0},  // 同代版本（Beta1.28 ≡ v1.28.0）
		{"v1.29.0", "Beta1.28", 1},  // 新小版本领先
		{"v1.27.0", "v1.27.0", 0},
		{"1.28", "v1.28.0", 0},
		{"v1.28.1", "v1.28.0", 1},
		{"v2.0.0", "v1.99.99", 1},
		{"v1.28.0-rc1", "v1.28.0", -1}, // 预发布低于正式版
		{"v1.28.0", "v1.28.0-rc1", 1},
	}
	for _, c := range cases {
		va, _ := parseVersionValue(c.a)
		vb, _ := parseVersionValue(c.b)
		if got := compareVersions(va, vb); got != c.want {
			t.Fatalf("compareVersions(%q,%q)=%d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSameVersion(t *testing.T) {
	if !sameVersion("v1.28.0", "1.28.0") {
		t.Fatal("v1.28.0 应等价于 1.28.0")
	}
	if !sameVersion("Beta1.27", "v1.27.0") {
		t.Fatal("Beta1.27 应等价于 v1.27.0")
	}
	if sameVersion("v1.28.0", "v1.27.0") {
		t.Fatal("v1.28.0 不应等价于 v1.27.0")
	}
}

func TestParseReleaseResponse(t *testing.T) {
	raw := []byte(`{
	  "tag_name": "v1.28.0",
	  "name": "v1.28.0 镜像包",
	  "body": "## 更新\n- 新增 PoW 验证码",
	  "html_url": "https://github.com/shenwei234/inkstone/releases/tag/v1.28.0",
	  "published_at": "2026-10-01T10:00:00Z",
	  "prerelease": false,
	  "assets": [
	    {"name": "inkstone-images-v1.28.0.tar", "size": 734003200,
	     "browser_download_url": "https://github.com/shenwei234/inkstone/releases/download/v1.28.0/inkstone-images-v1.28.0.tar",
	     "content_type": "application/x-tar"},
	    {"name": "checksums.txt", "size": 96,
	     "browser_download_url": "https://example.com/checksums.txt"}
	  ]
	}`)
	rel, ok := parseReleaseResponse(raw)
	if !ok {
		t.Fatal("应能解析标准 Release JSON")
	}
	if rel.Tag != "v1.28.0" || rel.Body == "" || len(rel.Assets) != 2 {
		t.Fatalf("解析结果异常：%+v", rel)
	}
	if rel.Assets[0].Size != 734003200 || rel.Assets[0].URL == "" {
		t.Fatalf("资产字段解析异常：%+v", rel.Assets[0])
	}

	// 草稿 release 不能作为更新目标
	draft := []byte(`{"tag_name":"v9.9.9","draft":true,"assets":[]}`)
	if _, ok := parseReleaseResponse(draft); !ok {
		t.Fatal("草稿 release 应该仍可解析（由调用方拒绝草稿）")
	}

	// 没有 tag 的响应视为不可解析
	if _, ok := parseReleaseResponse([]byte(`{"message":"Not Found"}`)); ok {
		t.Fatal("缺少 tag_name 的响应不应解析成功")
	}
}

func TestPickImageAsset(t *testing.T) {
	rel := ReleaseBrief{
		Tag: "v1.28.0",
		Assets: []ReleaseAssetBrief{
			{Name: "checksums.txt", Size: 100, URL: "https://example.com/c"},
			{Name: "inkstone-images-v1.27.0.tar", Size: 700, URL: "https://example.com/old"},
			{Name: "inkstone-images-v1.28.0.tar", Size: 800, URL: "https://example.com/new"},
		},
	}
	svc := newTestUpdateService(t, t.TempDir())
	asset, err := svc.pickImageAsset(rel)
	if err != nil {
		t.Fatalf("应能挑出镜像包：%v", err)
	}
	if asset.Name != "inkstone-images-v1.28.0.tar" {
		t.Fatalf("应优先挑版本号匹配的镜像包，实际 %s", asset.Name)
	}

	noAsset := ReleaseBrief{Tag: "v1.28.0", Assets: []ReleaseAssetBrief{{Name: "checksums.txt", URL: "https://x"}}}
	if _, err := svc.pickImageAsset(noAsset); err == nil {
		t.Fatal("没有镜像包资产时应报错")
	}
}
