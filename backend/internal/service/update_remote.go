package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	// allowPrivate 放行内网目标（UPDATE_ALLOW_PRIVATE_HOSTS=true）。
	allowPrivate bool
}

func newHTTPClient(timeout time.Duration, token string) *httpClient {
	return newHTTPClientWithGuard(timeout, token, false)
}

// newHTTPClientWithGuard 是带 SSRF 防护开关的真实构造入口。
// allowPrivate 对应 UPDATE_ALLOW_PRIVATE_HOSTS（默认 false = 校验公网地址）。
func newHTTPClientWithGuard(timeout time.Duration, token string, allowPrivate bool) *httpClient {
	hc := &httpClient{
		client:       &http.Client{Timeout: timeout},
		downloader:   &http.Client{Timeout: downloadTimeout},
		token:        strings.TrimSpace(token),
		allowPrivate: allowPrivate,
	}
	hc.applySSRFGuard(hc.client)
	hc.applySSRFGuard(hc.downloader)
	return hc
}

// applySSRFGuard 给客户端装上「拨号前校验 + 逐跳重定向校验」。
//
// 为什么需要：更新链路上的下载地址并不都来自可信配置——Release 资产的
// browser_download_url 来自上游 API 响应，镜像模板里的部分占位符同理。上游
// 一旦被污染或被中间人，就能借更新通道让后端去请求内网服务（云元数据
// 169.254.169.254、本机管理端口等）。
//
// 注意不能无条件封禁内网：UPDATE_GITHUB_API 等允许指向自建内网代理，
// 因此由 cfg.AllowPrivateHosts 显式放行，默认严格。
func (c *httpClient) applySSRFGuard(client *http.Client) {
	client.Transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if c.privateAllowed() {
				return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !isPublicIP(ip) {
					return nil, fmt.Errorf("更新下载目标 %s 解析到非公网地址 %s，已拒绝（如需使用内网更新源，请设置 UPDATE_ALLOW_PRIVATE_HOSTS=true）", host, ip)
				}
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, addr)
		},
	}
	// CheckRedirect 属于 http.Client 而非 Transport。默认最多 10 跳且逐跳不
	// 校验，等于第一跳合法就放行后续任意跳转，这里补上每一跳的地址校验。
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("重定向次数过多")
		}
		if c.privateAllowed() {
			return nil
		}
		host := req.URL.Hostname()
		if ip := net.ParseIP(host); ip != nil {
			if !isPublicIP(ip) {
				return fmt.Errorf("重定向目标 %s 不是公网地址，已拒绝", host)
			}
			return nil
		}
		ips, err := net.LookupIP(host)
		if err != nil {
			return err
		}
		for _, ip := range ips {
			if !isPublicIP(ip) {
				return fmt.Errorf("重定向目标 %s 解析到非公网地址 %s，已拒绝", host, ip)
			}
		}
		return nil
	}
}

// privateAllowed 汇报是否放行内网地址（配置 UPDATE_ALLOW_PRIVATE_HOSTS）。
// 之所以做成方法而不是读全局变量：便于测试注入，也避免包级可变状态。
func (c *httpClient) privateAllowed() bool {
	return c.allowPrivate
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

// downloadResumeMaxAttempts 是单次 download 内部的重试次数上限。
//
// 为什么必须重试：镜像包 265 MB 级别，国内网络下「下到一半被掐断」是常态
// 而不是异常。改前失败即整体报错并删掉半截文件，等于每次都要从 0 开始，
// 实测 58.5 MB 就 context deadline exceeded，反复重来永远到不了终点。
const downloadResumeMaxAttempts = 6

// downloadRetryBaseDelay 是首次重试前的等待，之后按 2 的幂退避并封顶 30s。
// 断流往往是瞬时的，稍等一下成功率明显更高；但也不能等太久，否则
// 用户看到进度条长时间不动会以为卡死。
const downloadRetryBaseDelay = 2 * time.Second

// download 流式下载到 destPath，边下边算 SHA-256，并在写完后校验 Content-Length。
//
// 支持**断点续传**：中途断流时保留半截文件，下一轮用 Range 头从断点继续，
// 而不是丢弃重来。判定与限制：
//   - 只有服务器返回 Accept-Ranges/206 才续传，否则回退为整包重下；
//   - 续传前比对 ETag/Last-Modified，资源变了就放弃半截文件重下
//     （否则会拼出「前半段旧版 + 后半段新版」的损坏包）；
//   - 续传时 SHA-256 无法沿用（哈希器不支持从中间状态恢复），改为对
//     已落盘的半截文件重新摘要后再接着算，保证最终摘要覆盖全文件。
func (c *httpClient) download(ctx context.Context, rawURL, destPath string, onProgress func(done, total int64)) (downloadResult, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return downloadResult{Source: rawURL}, err
	}

	var lastErr error
	for attempt := 1; attempt <= downloadResumeMaxAttempts; attempt++ {
		res, err := c.downloadOnce(ctx, rawURL, destPath, onProgress)
		if err == nil {
			return res, nil
		}
		lastErr = err

		// 上下文被取消（用户点了取消 / 进程退出）不必再试
		if ctx.Err() != nil {
			break
		}
		// 明确的「不该重试」错误直接返回：4xx/5xx、超限等，重试只会
		// 浪费时间并刷日志。
		//
		// 不删半截文件：调用方（downloadImageAsset）可能换一个等价地址
		// 继续下，保留已下的字节就能续传。安全性由 ETag/If-Range 保证——
		// 换到不同源时校验值不同，downloadOnce 会自行放弃续传重下。
		var noRetry *noRetryError
		if errors.As(err, &noRetry) {
			return downloadResult{Source: rawURL}, err
		}
		if attempt == downloadResumeMaxAttempts {
			break
		}

		delay := downloadRetryBaseDelay << (attempt - 1)
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		// 半截文件保留给下一轮续传；这里只等待退避
		select {
		case <-ctx.Done():
			lastErr = ctx.Err()
		case <-time.After(delay):
		}
	}
	// 重试耗尽：保留半截文件交给调用方。
	//
	// 这里刻意不删——downloadImageAsset 会换下一个地址继续，同源时能靠
	// ETag/Range 接着下完，删掉等于白下。真正的收尾由它在所有地址都
	// 失败后完成（删文件 + 删续传元数据）。
	// 续传元数据也一并留着，否则换地址后就失去 If-Range 的比对依据，
	// 而缺依据时 downloadOnce 会拒绝续传、退回整包重下。
	return downloadResult{Source: rawURL}, lastErr
}

// noRetryError 标记「重试也没有意义」的失败，避免对 4xx 之类反复重连。
type noRetryError struct{ err error }

func (e *noRetryError) Error() string { return e.err.Error() }
func (e *noRetryError) Unwrap() error { return e.err }

// downloadOnce 是一次完整的下载尝试（可能从半截文件续传）。
func (c *httpClient) downloadOnce(ctx context.Context, rawURL, destPath string, onProgress func(done, total int64)) (downloadResult, error) {
	// 检查是否已有半截文件可供续传
	var resumeFrom int64
	var partETag, partModTime string
	if st, err := os.Stat(destPath); err == nil && st.Size() > 0 {
		resumeFrom = st.Size()
		partETag, partModTime = readResumeMeta(destPath)
		// 没有 ETag / Last-Modified 就不能安全续传。
		//
		// 缺了比对依据，If-Range 无从下手，服务器会按范围直接给内容——
		// 万一这期间 Release 资产被换成另一个版本（重新构建、重传同名包），
		// 就会拼出「前半段旧 + 后半段新」的包。它长度正确、能通过大小
		// 检查，只有在 docker load 时才炸，甚至悄悄装上错的镜像。
		// 相比之下重下一次只是慢，所以这里一律选择重下。
		if partETag == "" && partModTime == "" {
			if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
				return downloadResult{Source: rawURL}, err
			}
			resumeFrom = 0
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return downloadResult{Source: rawURL}, err
	}
	c.applyHeaders(req)
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
		// 条件请求：资源若已变化，服务器回 200 而非 206，下面按「重下」处理
		if partETag != "" {
			req.Header.Set("If-Range", partETag)
		} else if partModTime != "" {
			req.Header.Set("If-Range", partModTime)
		}
	}

	resp, err := c.downloader.Do(req)
	if err != nil {
		return downloadResult{Source: rawURL}, err
	}
	defer resp.Body.Close()

	// 4xx 是确定性失败，重试无意义（403 可能是限流，但也会一直 403）
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return downloadResult{Source: rawURL}, &noRetryError{
			err: fmt.Errorf("下载镜像包失败：HTTP %d", resp.StatusCode),
		}
	}
	// 5xx 也直接换地址，不在原地退避重试。
	//
	// 动机是实测出来的：镜像加速站返回 502 时，原地重试 6 次要耗掉
	// 一分钟（2+4+8+16+30+30），而这期间该做的其实是立刻试下一个地址。
	// 「服务器挂了」不是瞬时抖动，重试同一个地址几乎没有意义。
	if resp.StatusCode >= 500 {
		return downloadResult{Source: rawURL}, &noRetryError{
			err: fmt.Errorf("下载镜像包失败：HTTP %d（服务端错误，换地址重试）", resp.StatusCode),
		}
	}

	// 服务器没答应续传（返回 200 而非 206）：放弃半截文件，从头下，
	// 否则会把整包内容追加到半截文件后面，拼出一个更大的坏包。
	if resumeFrom > 0 && resp.StatusCode != http.StatusPartialContent {
		resumeFrom = 0
		partETag, partModTime = "", ""
		if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
			return downloadResult{Source: rawURL}, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return downloadResult{Source: rawURL}, fmt.Errorf("下载镜像包失败：HTTP %d", resp.StatusCode)
	}

	// 总长度 = 已下载 + 本次剩余（206 的 Content-Length 只是剩余部分）
	var total int64
	if resp.StatusCode == http.StatusPartialContent {
		if cr, ok := parseContentRange(resp.Header.Get("Content-Range")); ok {
			total = cr
		} else if resp.ContentLength > 0 {
			total = resumeFrom + resp.ContentLength
		}
	} else {
		total = resp.ContentLength
	}
	if total > maxArchiveBytes {
		return downloadResult{Source: rawURL}, &noRetryError{
			err: fmt.Errorf("镜像包过大（%d 字节），已超过上限", total),
		}
	}

	hasher := sha256.New()
	if resumeFrom > 0 {
		// 哈希器无法从中间状态恢复：先对已落盘的半截内容重算摘要，
		// 再接着往下算，最终摘要才能真正覆盖整个文件。
		// 这也是为什么不能简单地「保留 hasher 接着写」。
		existing, err := os.Open(destPath)
		if err != nil {
			return downloadResult{Source: rawURL}, err
		}
		if _, err := io.Copy(hasher, existing); err != nil {
			existing.Close()
			// 半截文件读不动（如被截断）——删掉重下，不留坏状态
			_ = os.Remove(destPath)
			return downloadResult{Source: rawURL}, err
		}
		existing.Close()
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resumeFrom > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(destPath, flags, 0o644)
	if err != nil {
		return downloadResult{Source: rawURL}, err
	}

	// 只在本次尝试失败时保留半截文件供下次续传；成功才关闭。
	attemptOK := false
	defer func() {
		out.Close()
		if !attemptOK {
			// 关键区别：这里**不删**文件，留给下一轮 Range 续传
			writeResumeMeta(destPath, resp.Header)
		}
	}()

	writer := io.MultiWriter(out, hasher)
	written := resumeFrom
	buf := make([]byte, 256<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if written+int64(n) > maxArchiveBytes {
				return downloadResult{Source: rawURL}, &noRetryError{
					err: errors.New("镜像包超过上限，已中止下载"),
				}
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
			// 断流：把已收内容落盘，下一轮从 written 处续
			_ = out.Sync()
			return downloadResult{Source: rawURL}, readErr
		}
	}
	if total > 0 && written != total {
		_ = out.Sync()
		return downloadResult{Source: rawURL}, fmt.Errorf("下载不完整：期望 %d 字节，实际 %d 字节", total, written)
	}
	if err := out.Sync(); err != nil {
		return downloadResult{Source: rawURL}, err
	}
	attemptOK = true
	clearResumeMeta(destPath)
	return downloadResult{
		Path:   destPath,
		Bytes:  written,
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Source: rawURL,
	}, nil
}

// parseContentRange 从 "bytes 100-999/1000" 里取出总长度 1000。
func parseContentRange(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	slash := strings.LastIndex(v, "/")
	if slash < 0 || slash == len(v)-1 {
		return 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(v[slash+1:]), 10, 64)
	if err != nil || total <= 0 {
		return 0, false
	}
	return total, true
}

// ---------------------------------------------------------------------------
// 断点续传的元数据
//
// 续传前必须确认「远端资源还是同一份」。否则内容一变，前半段是旧版、
// 后半段是新版，拼出来的是个能通过长度检查却完全损坏的包——比直接失败
// 更危险（它会一路走到 docker load 才炸，甚至悄悄装上错的镜像）。
// 因此把上一轮的 ETag/Last-Modified 存成旁路文件，下一轮用 If-Range 送出去。
// ---------------------------------------------------------------------------

// resumeMetaPath 返回半截文件对应的元数据路径。
func resumeMetaPath(destPath string) string { return destPath + ".resume-meta" }

// writeResumeMeta 记录本次响应的 ETag/Last-Modified，供下一轮 If-Range 使用。
func writeResumeMeta(destPath string, header http.Header) {
	etag := header.Get("ETag")
	mod := header.Get("Last-Modified")
	if etag == "" && mod == "" {
		return
	}
	_ = os.WriteFile(resumeMetaPath(destPath), []byte(etag+"\n"+mod), 0o644)
}

// readResumeMeta 读回上一轮记录的校验值。返回空串表示没有可用的比对依据。
func readResumeMeta(destPath string) (etag, modTime string) {
	b, err := os.ReadFile(resumeMetaPath(destPath))
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(string(b), "\n", 2)
	if len(parts) > 0 {
		etag = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		modTime = strings.TrimSpace(parts[1])
	}
	return etag, modTime
}

// clearResumeMeta 在下载完成后清掉旁路文件，避免残留误导下一轮。
func clearResumeMeta(destPath string) {
	_ = os.Remove(resumeMetaPath(destPath))
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
