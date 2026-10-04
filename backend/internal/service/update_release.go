package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// GitHub Releases 更新源（UPDATE_SOURCE=releases）
//
// 与 commits 模式的区别：上游不再要求「提交可下载源码包」，而是在 GitHub
// Releases 发布：
//   - tag_name：语义化版本号（如 v1.28.0）
//   - body：发布说明（后台「检查更新」直接展示）
//   - assets：镜像包（inkstone-images-<tag>.tar），由宿主代理 docker load
//
// 版本比较以「版本号」为基准，不再以 commit 哈希为基准；本地当前版本来自
// data/deployed-version.json（镜像部署没有源码树，拿不到 commit）。
// ---------------------------------------------------------------------------

// ReleaseBrief 是 GitHub Release 的规范化展示信息。
type ReleaseBrief struct {
	Tag         string              `json:"tag"`
	Name        string              `json:"name"`
	Body        string              `json:"body"`
	URL         string              `json:"url"`
	PublishedAt string              `json:"published_at"`
	Prerelease  bool                `json:"prerelease"`
	Draft       bool                `json:"draft"` // 草稿中间态不应作为更新目标
	Assets      []ReleaseAssetBrief `json:"assets"`
}

// ReleaseAssetBrief 是 Release 附件（镜像包）的展示与下载信息。
type ReleaseAssetBrief struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`          // browser_download_url
	ContentType string `json:"content_type"` // 如 application/x-tar
}

// releaseAssetPattern 缓存编译后的资产名正则（UPDATE_IMAGE_ASSET）。
func (s *UpdateService) releaseAssetPattern() (*regexp.Regexp, error) {
	pattern := strings.TrimSpace(s.cfg.UpdateImageAsset)
	if pattern == "" {
		pattern = defaultReleaseAssetPatternFallback
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("UPDATE_IMAGE_ASSET 不是合法正则：%w", err)
	}
	return re, nil
}

// defaultReleaseAssetPatternFallback 与 config.defaultImageAssetPattern 保持一致，
// 在配置异常时兜底（config 已保证非空，这里只防御空串）。
const defaultReleaseAssetPatternFallback = `inkstone-images-.*\.tar$`

// releasesAPIURL 拼出「最新 Release」接口地址。
// 默认 {UPDATE_GITHUB_API}/repos/{owner}/{name}/releases/latest；
// 配置了 UPDATE_RELEASES_API 时按模板展开（{owner} {name} {repo} {branch} {api}）。
func (s *UpdateService) releasesAPIURL() string {
	owner, name := splitRepo(s.cfg.UpdateRepoURL)
	if tmpl := strings.TrimSpace(s.cfg.UpdateReleasesAPI); tmpl != "" {
		repl := strings.NewReplacer(
			"{owner}", owner,
			"{name}", name,
			"{repo}", s.cfg.UpdateRepoURL,
			"{branch}", s.cfg.UpdateBranch,
			"{api}", strings.TrimRight(s.cfg.UpdateGitHubAPI, "/"),
		)
		return repl.Replace(tmpl)
	}
	return strings.TrimRight(s.cfg.UpdateGitHubAPI, "/") + "/repos/" + owner + "/" + name + "/releases/latest"
}

// releaseByTagAPIURL 拼出「按 tag 取 Release」接口地址（更新到指定旧版本时用）。
func (s *UpdateService) releaseByTagAPIURL(tag string) string {
	owner, name := splitRepo(s.cfg.UpdateRepoURL)
	tag = url.PathEscape(strings.TrimSpace(tag))
	return strings.TrimRight(s.cfg.UpdateGitHubAPI, "/") + "/repos/" + owner + "/" + name + "/releases/tags/" + tag
}

// fetchLatestRelease 拉取上游最新 Release（不含草稿；GitHub 的 latest 端点
// 本身也会跳过 draft 与 prerelease）。
func (s *UpdateService) fetchLatestRelease(ctx context.Context) (ReleaseBrief, error) {
	return s.fetchReleaseByURL(ctx, s.releasesAPIURL())
}

// fetchRelease 取指定版本的 Release；tag 为空或等于最新时走 latest 端点。
func (s *UpdateService) fetchRelease(ctx context.Context, tag string) (ReleaseBrief, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return s.fetchLatestRelease(ctx)
	}
	latest, err := s.fetchLatestRelease(ctx)
	if err != nil {
		return ReleaseBrief{}, err
	}
	if sameVersion(latest.Tag, tag) {
		return latest, nil
	}
	return s.fetchReleaseByURL(ctx, s.releaseByTagAPIURL(tag))
}

func (s *UpdateService) fetchReleaseByURL(ctx context.Context, rawURL string) (ReleaseBrief, error) {
	body, _, err := s.client.getJSON(ctx, rawURL)
	if err != nil {
		return ReleaseBrief{}, fmt.Errorf("获取 GitHub Releases 失败（%s）：%w", redactURL(rawURL), err)
	}
	rel, ok := parseReleaseResponse(body)
	if !ok {
		return ReleaseBrief{}, fmt.Errorf("无法解析 GitHub Releases 响应（%s）", redactURL(rawURL))
	}
	if rel.Draft {
		return ReleaseBrief{}, fmt.Errorf("最新 Release（%s）仍是草稿，请先在 GitHub 发布", rel.Tag)
	}
	return rel, nil
}

// parseReleaseResponse 解析 Release JSON：既接受单个对象，也接受列表（取第一个）。
// 解析是纯函数，便于单元测试；字段名同时兼容 GitHub 官方与常见镜像。
func parseReleaseResponse(raw []byte) (ReleaseBrief, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		// 兼容 {"release": {...}} / {"data": {...}} 包装
		for _, key := range []string{"release", "data", "result", "latest"} {
			if inner, ok := obj[key]; ok {
				if m, ok := inner.(map[string]any); ok {
					if rel, ok := parseReleaseObject(m); ok {
						return rel, true
					}
				}
			}
		}
		if rel, ok := parseReleaseObject(obj); ok {
			return rel, true
		}
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		for _, item := range list {
			if item["draft"] == true {
				continue
			}
			if rel, ok := parseReleaseObject(item); ok {
				return rel, true
			}
		}
	}
	return ReleaseBrief{}, false
}

func parseReleaseObject(item map[string]any) (ReleaseBrief, bool) {
	tag := firstString(item, "tag_name", "tag", "version", "name")
	if tag == "" {
		return ReleaseBrief{}, false
	}
	rel := ReleaseBrief{
		Tag:         strings.TrimSpace(tag),
		Name:        firstString(item, "name", "title", "tag_name"),
		Body:        firstString(item, "body", "notes", "description"),
		URL:         firstString(item, "html_url", "url"),
		PublishedAt: firstString(item, "published_at", "created_at", "released_at"),
	}
	if v, ok := item["prerelease"].(bool); ok {
		rel.Prerelease = v
	}
	if v, ok := item["draft"].(bool); ok {
		rel.Draft = v
	}
	if assetsRaw, ok := item["assets"].([]any); ok {
		for _, a := range assetsRaw {
			m, ok := a.(map[string]any)
			if !ok {
				continue
			}
			name := firstString(m, "name", "label")
			url := firstString(m, "browser_download_url", "url")
			if name == "" || url == "" {
				continue
			}
			rel.Assets = append(rel.Assets, ReleaseAssetBrief{
				Name:        name,
				Size:        firstInt(m, "size"),
				URL:         url,
				ContentType: firstString(m, "content_type"),
			})
		}
	}
	return rel, true
}

// pickImageAsset 从 Release 资产里挑镜像包：先按 UPDATE_IMAGE_ASSET 正则过滤，
// 多个命中时优先名字里带本版本号的（防止旧版本残留资产被误选）。
func (s *UpdateService) pickImageAsset(rel ReleaseBrief) (ReleaseAssetBrief, error) {
	re, err := s.releaseAssetPattern()
	if err != nil {
		return ReleaseAssetBrief{}, err
	}
	var matches []ReleaseAssetBrief
	for _, asset := range rel.Assets {
		if re.MatchString(asset.Name) {
			matches = append(matches, asset)
		}
	}
	if len(matches) == 0 {
		return ReleaseAssetBrief{}, fmt.Errorf("Release %s 里没有找到镜像包资产（匹配规则：%s）",
			rel.Tag, strings.TrimSpace(s.cfg.UpdateImageAsset))
	}
	if len(matches) > 1 {
		bare := strings.TrimPrefix(strings.ToLower(rel.Tag), "v")
		for _, asset := range matches {
			if strings.Contains(strings.ToLower(asset.Name), bare) {
				return asset, nil
			}
		}
	}
	return matches[0], nil
}

// ---------------------------------------------------------------------------
// 版本号解析与比较
//
// 同时兼容本项目的 AppVersion 写法（Beta1.27）与语义化 tag（v1.28.0）：
//   Beta1.27     → 1.27.0
//   v1.28.0      → 1.28.0
//   v1.28.0-rc1  → 1.28.0 预发布（低于正式版）
// ---------------------------------------------------------------------------

type versionValue struct {
	major, minor, patch int
	pre                 string // 预发布后缀（空 = 正式版）
}

// parseVersionValue 解析版本号；无法识别时返回 false（调用方按「无法比较」处理）。
func parseVersionValue(raw string) (versionValue, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return versionValue{}, false
	}
	// 去掉常见前缀写法：v1.2.3 / V1.2.3 / Beta1.27 / beta 1.27 / rc1.2
	lower := strings.ToLower(s)
	for _, prefix := range []string{"beta", "alpha", "rc", "release", "ver"} {
		if strings.HasPrefix(lower, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			lower = strings.ToLower(s)
			break
		}
	}
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	s = strings.TrimSpace(s)
	if s == "" {
		return versionValue{}, false
	}

	// 拆预发布后缀：1.28.0-rc1 / 1.28.0 rc1
	pre := ""
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		pre = strings.TrimSpace(s[i+1:])
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return versionValue{}, false
	}
	nums := make([]int, 3)
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return versionValue{}, false
		}
		// 允许 1.28.0b3 这类后缀：只取前导数字
		end := 0
		for end < len(part) && part[end] >= '0' && part[end] <= '9' {
			end++
		}
		if end == 0 {
			return versionValue{}, false
		}
		n, err := strconv.Atoi(part[:end])
		if err != nil {
			return versionValue{}, false
		}
		nums[i] = n
	}
	return versionValue{major: nums[0], minor: nums[1], patch: nums[2], pre: pre}, true
}

// compareVersions 比较两个已解析版本：-1 a<b，0 相等，1 a>b。
func compareVersions(a, b versionValue) int {
	if a.major != b.major {
		return cmpInt(a.major, b.major)
	}
	if a.minor != b.minor {
		return cmpInt(a.minor, b.minor)
	}
	if a.patch != b.patch {
		return cmpInt(a.patch, b.patch)
	}
	// 同号版本：预发布低于正式版
	if a.pre == b.pre {
		return 0
	}
	if a.pre == "" {
		return 1
	}
	if b.pre == "" {
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// sameVersion 判断两个版本号写法是否等价（v1.28.0 == 1.28.0）。
func sameVersion(a, b string) bool {
	va, oka := parseVersionValue(a)
	vb, okb := parseVersionValue(b)
	if !oka || !okb {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return compareVersions(va, vb) == 0
}

// normalizeVersionTag 规范展示：统一保留 v 前缀（与 Release tag 习惯一致）。
func normalizeVersionTag(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	if !strings.HasPrefix(s, "v") && !strings.HasPrefix(s, "V") {
		return "v" + s
	}
	return s
}

// ---------------------------------------------------------------------------
// 本地版本记录（镜像部署没有源码树，用版本号而非 commit 记录部署状态）
// ---------------------------------------------------------------------------

// deployedVersionFile 是镜像更新的部署记录：宿主代理 docker load + compose
// 重建成功后写回，后端据此判断「当前跑的是哪一版」。
type deployedVersionFile struct {
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// versionCandidates 覆盖几种运行布局（前面的优先）：
//   - 更新目录：宿主代理写回的位置
//   - 容器内数据卷：/app/data
//   - 仓库根 / 老式单目录部署
func (s *UpdateService) versionCandidates() []string {
	var candidates []string
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			candidates = append(candidates, path)
		}
	}
	add(s.cfg.UpdateVersionFile)
	if s.cfg.UpdateDir != "" {
		add(filepath.Join(s.cfg.UpdateDir, "deployed-version.json"))
	}
	wd, _ := os.Getwd()
	for _, base := range []string{wd, s.cfg.UpdateSourceDir} {
		if base == "" {
			continue
		}
		add(filepath.Join(base, "data", "deployed-version.json"))
		add(filepath.Join(base, "backend", "data", "deployed-version.json"))
	}
	return candidates
}

// localVersion 返回本地当前版本号与来源（version_file / app_version）。
func (s *UpdateService) localVersion() (version, from string) {
	for _, path := range s.versionCandidates() {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec deployedVersionFile
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if v := strings.TrimSpace(rec.Version); v != "" {
			return v, "version_file"
		}
	}
	return AppVersion, "app_version"
}

// versionPath 返回版本记录的写入路径：沿用已读到的文件，否则回落到
// 更新目录（宿主代理默认写回处）。
func (s *UpdateService) versionPath() string {
	for _, path := range s.versionCandidates() {
		if isFile(path) {
			return path
		}
	}
	if strings.TrimSpace(s.cfg.UpdateVersionFile) != "" {
		return s.cfg.UpdateVersionFile
	}
	if s.cfg.UpdateDir != "" {
		return filepath.Join(s.cfg.UpdateDir, "deployed-version.json")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "data", "deployed-version.json")
	}
	return ""
}

// writeDeployedVersion 原子写回部署版本记录。
func (s *UpdateService) writeDeployedVersion(version string) error {
	path := s.versionPath()
	if path == "" {
		return fmt.Errorf("版本记录路径未知")
	}
	return writeJSONFile(path, deployedVersionFile{
		Version:   normalizeVersionTag(version),
		UpdatedAt: time.Now().Format(time.RFC3339),
	})
}

// ---------------------------------------------------------------------------
// 镜像包下载
// ---------------------------------------------------------------------------

// maxImageBytes 镜像包大小上限（配置化，默认 2 GB）。
func (s *UpdateService) maxImageBytes() int64 {
	return s.cfg.UpdateImageMaxMB << 20
}

// downloadImageAsset 流式下载镜像包到 UPDATE_DIR/images/，边下边算 SHA-256，
// 写完后与 Release 资产声明的 size 对账（ asset 给了 size 才校验）。
func (s *UpdateService) downloadImageAsset(ctx context.Context, version string, asset ReleaseAssetBrief, report func(progress int, message string)) (path string, size int64, sum string, err error) {
	if err := os.MkdirAll(s.imageDir(), 0o755); err != nil {
		return "", 0, "", err
	}
	dest := filepath.Join(s.imageDir(), sanitizeFileName(version)+"-"+sanitizeFileName(asset.Name))
	report(20, "下载镜像包："+redactURL(asset.URL))
	res, err := s.client.download(ctx, asset.URL, dest, func(done, total int64) {
		if total <= 0 {
			total = asset.Size
		}
		progress := 20
		message := fmt.Sprintf("下载镜像包 %.1f MB", float64(done)/1024/1024)
		if total > 0 {
			progress = 20 + int(float64(done)/float64(total)*55)
			if progress > 75 {
				progress = 75
			}
			message = fmt.Sprintf("下载镜像包 %.1f / %.1f MB", float64(done)/1024/1024, float64(total)/1024/1024)
		}
		report(progress, message)
	})
	if err != nil {
		return "", 0, "", err
	}
	if asset.Size > 0 && res.Bytes != asset.Size {
		_ = os.Remove(res.Path)
		return "", 0, "", fmt.Errorf("镜像包大小与 Release 资产不一致：期望 %d 字节，实际 %d 字节", asset.Size, res.Bytes)
	}
	report(76, "镜像包下载完成，开始安装")
	return res.Path, res.Bytes, res.SHA256, nil
}

// imageDir 是镜像包的存放目录（UPDATE_DIR/images）。
func (s *UpdateService) imageDir() string {
	return filepath.Join(s.cfg.UpdateDir, "images")
}

// sanitizeFileName 去掉路径分隔符等危险字符，防止Release 资产名带目录穿越。
func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "unnamed"
	}
	return name
}

// releaseHistoryItem 记录一次镜像安装，供回滚使用（保留本地镜像包路径）。
type releaseHistoryItem struct {
	Version   string `json:"version"`
	ImagePath string `json:"image_path"`
	ImageName string `json:"image_name"`
	SHA256    string `json:"sha256"`
	UpdatedAt string `json:"updated_at"`
}

// releaseHistoryPath 是镜像安装历史的文件路径。
func (s *UpdateService) releaseHistoryPath() string {
	return filepath.Join(s.cfg.UpdateDir, "release-history.json")
}

// loadReleaseHistory 读取镜像安装历史（新 → 旧）。
func (s *UpdateService) loadReleaseHistory() []releaseHistoryItem {
	raw, err := os.ReadFile(s.releaseHistoryPath())
	if err != nil {
		return nil
	}
	var items []releaseHistoryItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return items
}

// appendReleaseHistory 写入一条安装历史（最多保留 recentReleaseHistory 条）。
func (s *UpdateService) appendReleaseHistory(item releaseHistoryItem) error {
	if item.Version == "" {
		return fmt.Errorf("版本号为空")
	}
	if item.UpdatedAt == "" {
		item.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	items := s.loadReleaseHistory()
	// 同版本只保留最新一条
	filtered := items[:0]
	for _, old := range items {
		if sameVersion(old.Version, item.Version) {
			continue
		}
		filtered = append(filtered, old)
	}
	items = append([]releaseHistoryItem{item}, filtered...)
	if len(items) > recentReleaseHistory {
		items = items[:recentReleaseHistory]
	}
	return writeJSONFile(s.releaseHistoryPath(), items)
}

// recentReleaseHistory 镜像安装历史保留条数：够回滚到最近几个版本即可。
const recentReleaseHistory = 10

// previousReleaseImage 找最近一个「非当前版本」的镜像包，供回滚使用。
func (s *UpdateService) previousReleaseImage(current string) (releaseHistoryItem, bool) {
	for _, item := range s.loadReleaseHistory() {
		if sameVersion(item.Version, current) {
			continue
		}
		if isFile(item.ImagePath) {
			return item, true
		}
	}
	return releaseHistoryItem{}, false
}
