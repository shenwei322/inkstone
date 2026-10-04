package service

import (
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
	"strconv"
	"strings"
	"time"
)

// 一次更新请求的硬上限：防止异常响应把内存/磁盘打满。
const (
	maxJSONBytes    = 4 << 20 // 元数据接口响应上限 4 MB
	maxArchiveBytes = 1 << 30 // 源码包上限 1 GB
)

// downloadTimeout 是**下载源码包**的客户端超时，必须远大于元数据接口：
// 源码包几十 MB、经国内代理可能只有几十 KB/s，20 秒必然超时。
// 真实踩过：apply 跑到 1.5 MB 就 context deadline exceeded。
const downloadTimeout = 30 * time.Minute

type httpClient struct {
	client *http.Client // 元数据接口：短超时，避免检查更新卡住
	// downloader 只用于下载源码包：长超时（进度回调依赖它不被打断）
	downloader *http.Client
	token      string // 私有仓库 / 提高 API 限额
}

func newHTTPClient(timeout time.Duration, token string) *httpClient {
	return &httpClient{
		client:     &http.Client{Timeout: timeout},
		downloader: &http.Client{Timeout: downloadTimeout},
		token:      strings.TrimSpace(token),
	}
}

// getJSON 拉取并解析 JSON。返回原始字节与响应头，便于调用方按需二次解析。
func (c *httpClient) getJSON(ctx context.Context, rawURL string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, err
	}
	c.applyHeaders(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONBytes))
	if err != nil {
		return nil, resp.Header, err
	}
	return body, resp.Header, nil
}

// downloadResult 是一次镜像包下载的产物。
type downloadResult struct {
	Path   string
	Bytes  int64
	SHA256 string
	Source string
}

// download 流式下载到 destPath，边下边算 SHA-256，并在写完后校验 Content-Length。
func (c *httpClient) download(ctx context.Context, rawURL, destPath string, onProgress func(done, total int64)) (downloadResult, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return downloadResult{Source: rawURL}, err
	}
	out, err := os.Create(destPath)
	if err != nil {
		return downloadResult{Source: rawURL}, err
	}
	// 失败路径统一清理半截文件
	success := false
	defer func() {
		out.Close()
		if !success {
			_ = os.Remove(destPath)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return downloadResult{Source: rawURL}, err
	}
	c.applyHeaders(req)
	resp, err := c.downloader.Do(req)
	if err != nil {
		return downloadResult{Source: rawURL}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return downloadResult{Source: rawURL}, fmt.Errorf("下载源码包失败：HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total > maxArchiveBytes {
		return downloadResult{Source: rawURL}, fmt.Errorf("源码包过大（%d 字节），已超过 1 GB 上限", total)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)
	var written int64
	buf := make([]byte, 256<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if written+int64(n) > maxArchiveBytes {
				return downloadResult{Source: rawURL}, errors.New("源码包超过 1 GB 上限，已中止下载")
			}
			if _, err := writer.Write(buf[:n]); err != nil {
				return downloadResult{Source: rawURL}, err
			}
			written += int64(n)
			if onProgress != nil {
				onProgress(written, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return downloadResult{Source: rawURL}, readErr
		}
	}
	if total > 0 && written != total {
		return downloadResult{Source: rawURL}, fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", total, written)
	}
	if err := out.Sync(); err != nil {
		return downloadResult{Source: rawURL}, err
	}
	success = true
	return downloadResult{
		Path:   destPath,
		Bytes:  written,
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Source: rawURL,
	}, nil
}

// applyHeaders 统一请求头：GitHub API 需要 UA 与 Accept；
// token 走 Bearer（私有仓库 / 提高 API 限额）。
func (c *httpClient) applyHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "inkstone-updater/"+AppVersion)
	req.Header.Set("Accept", "application/vnd.github+json, application/json;q=0.9, */*;q=0.8")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// ---------------------------------------------------------------------------
// 更新源响应解析
//
// 兼容三类来源：GitHub REST API、GitHub 兼容镜像、自建极简 JSON 接口。
// 解析全部是纯函数，便于单元测试。
// ---------------------------------------------------------------------------

// remoteCommit 是「上游一次提交」的规范化结果。
type remoteCommit struct {
	Hash    string
	Message string
	Author  string
	Date    string
	URL     string
}

// parseCommitsResponse 解析提交列表接口：
// GitHub 形状 [{sha, commit:{message, author:{name, date}}, html_url}, ...]
func parseCommitsResponse(raw []byte) ([]remoteCommit, bool) {
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		// 有些镜像会包一层 {"data": [...]} 或 {"commits": [...]}
		var wrapped map[string]json.RawMessage
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil {
			return nil, false
		}
		for _, key := range []string{"commits", "data", "items", "list"} {
			if inner, ok := wrapped[key]; ok {
				if err3 := json.Unmarshal(inner, &list); err3 == nil {
					break
				}
			}
		}
		if len(list) == 0 {
			return nil, false
		}
	}
	out := make([]remoteCommit, 0, len(list))
	for _, item := range list {
		if c, ok := parseCommitObject(item); ok {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseLatestResponse 解析「仅返回最新提交」的接口：既接受单个对象，
// 也接受只有一条元素的列表。
func parseLatestResponse(raw []byte, branch string) (remoteCommit, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		// 可能是 {"commit": {...}} / {"latest": {...}} 这类包装
		for _, key := range []string{"latest", "commit", "data", "result"} {
			if inner, ok := obj[key]; ok {
				if m, ok := inner.(map[string]any); ok {
					if c, ok := parseCommitObject(m); ok {
						return c, true
					}
				}
				// 有些实现把 hash 直接放在包装层里（{"commit":"abc","message":"..."}）
				if s, ok := inner.(string); ok && looksLikeHash(s) {
					obj["sha"] = s
					break
				}
			}
		}
		// 分支维度：{"branches":{"main":{"sha":"..."}}} / {"main":"..."}
		if branches, ok := obj["branches"].(map[string]any); ok && branch != "" {
			if entry, ok := branches[branch]; ok {
				switch v := entry.(type) {
				case string:
					if looksLikeHash(v) {
						obj["sha"] = v
					}
				case map[string]any:
					if c, ok := parseCommitObject(v); ok {
						return c, true
					}
				}
			}
		}
		if c, ok := parseCommitObject(obj); ok {
			return c, true
		}
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		if c, ok := parseCommitObject(list[0]); ok {
			return c, true
		}
	}
	return remoteCommit{}, false
}

// parseCommitObject 从单个提交对象里取字段，兼容多种键名。
func parseCommitObject(item map[string]any) (remoteCommit, bool) {
	hash := firstString(item, "sha", "hash", "commit_hash", "commitId", "id", "commit")
	if !looksLikeHash(hash) {
		return remoteCommit{}, false
	}
	c := remoteCommit{Hash: strings.ToLower(strings.TrimSpace(hash))}

	// 嵌套的 commit 对象（GitHub 形状）
	nested, _ := item["commit"].(map[string]any)
	if nested != nil {
		if s, ok := nested["message"].(string); ok {
			c.Message = s
		}
		if author, ok := nested["author"].(map[string]any); ok {
			c.Author = firstString(author, "name", "login", "username")
			c.Date = firstString(author, "date", "timestamp", "created_at")
		}
		if committer, ok := nested["committer"].(map[string]any); ok {
			if c.Date == "" {
				c.Date = firstString(committer, "date", "timestamp")
			}
		}
	}
	if c.Message == "" {
		c.Message = firstString(item, "message", "commit_message", "title", "subject")
	}
	if c.Author == "" {
		if author, ok := item["author"].(map[string]any); ok {
			c.Author = firstString(author, "login", "name", "username")
		}
		if c.Author == "" {
			c.Author = firstString(item, "author", "author_name", "committer_name")
		}
	}
	if c.Date == "" {
		c.Date = firstString(item, "date", "timestamp", "created_at", "committed_at", "time")
	}
	c.URL = firstString(item, "html_url", "url", "web_url")
	c.Date = normalizeTimestamp(c.Date)
	return c, true
}

// firstString 依次尝试多个键，返回第一个非空字符串（数字时间戳也会被转成字符串）。
func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		case float64:
			return strconv.FormatInt(int64(v), 10)
		case json.Number:
			return v.String()
		}
	}
	return ""
}

// firstInt 依次尝试多个键，返回第一个可解析的整数（GitHub 的 size 等字段）。
func firstInt(obj map[string]any, keys ...string) int64 {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case float64:
			return int64(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return n
			}
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}

// normalizeTimestamp 把秒/毫秒时间戳统一成 RFC3339；已是字符串的原样返回。
func normalizeTimestamp(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		switch {
		case n > 1e12: // 毫秒
			return time.UnixMilli(n).Format(time.RFC3339)
		case n > 1e9: // 秒
			return time.Unix(n, 0).Format(time.RFC3339)
		}
		return ""
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.Format(time.RFC3339)
	}
	// 常见变体：2026-09-26 20:49:28 +0800 / 2026-09-26T20:49:28Z 已被上面覆盖
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return v
}

// looksLikeHash 判断是否像 git 提交哈希（7-64 位十六进制），过滤掉数字 ID 之类的噪声。
func looksLikeHash(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, ch := range s {
		switch {
		case ch >= '0' && ch <= '9':
		case ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

// escapeURLPath 转义 URL 路径片段（提交哈希是十六进制，正常无需转义，做一层兜底）。
func escapeURLPath(s string) string {
	return url.PathEscape(strings.TrimSpace(s))
}

// escapeQueryValue 转义查询参数（分支名可能含 / 与 #）。
func escapeQueryValue(s string) string {
	return url.QueryEscape(strings.TrimSpace(s))
}

// normalizeRepoURL 归一化仓库标识：去 .git 后缀、去空白、统一小写。
func normalizeRepoURL(raw string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), ".git"))
}

// sameRepo 判断两个「owner/name」标识是否指向同一个仓库。
//
// 上游 API 可能因为仓库改名/转移而返回**历史作者**的仓库名
// （实测：配置 shenwei234/inkstone，返回的 html_url 却是 shenwei322/inkstone），
// 这种情况要沿用本地配置生成链接，否则页面会把「查看提交」链到别人的仓库。
func sameRepo(a, b string) bool {
	na, nb := normalizeRepoURL(a), normalizeRepoURL(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb
}

// splitRepo 从仓库地址里拆出 owner / name，支持：
//
//	https://github.com/owner/name.git
//	git@github.com:owner/name.git
//	https://gitee.com/owner/name
//	https://git.example.com/group/owner/name   （取最后两段）
func splitRepo(repoURL string) (owner, name string) {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return "", ""
	}
	raw = strings.TrimSuffix(raw, ".git")
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
		if j := strings.Index(raw, "/"); j >= 0 {
			raw = raw[j+1:]
		} else {
			return "", ""
		}
	} else if i := strings.Index(raw, "@"); i >= 0 {
		// git@host:owner/name
		raw = raw[i+1:]
		if j := strings.IndexAny(raw, ":/"); j >= 0 {
			raw = raw[j+1:]
		}
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[len(parts)-2], parts[len(parts)-1]
}

// repoFullName 返回 owner/name 形式的展示名。
func repoFullName(repoURL string) string {
	owner, name := splitRepo(repoURL)
	if owner == "" || name == "" {
		return strings.TrimSpace(repoURL)
	}
	return owner + "/" + name
}
