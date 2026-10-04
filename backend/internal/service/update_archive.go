package service

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// 解压保护上限：源码包正常只有几 MB，这些阈值只拦异常包（zip 炸弹）。
const (
	maxArchiveFiles   = 20000
	maxExtractedBytes = 512 << 20 // 512 MB
	maxEntryBytes     = 256 << 20 // 单文件 256 MB
)

// 源码包必须包含的文件：用来判断「下下来的到底是不是本站源码」，
// 避免镜像返回一个 HTML 错误页却当成源码包替换进去。
var requiredArchiveFiles = []string{
	"backend/cmd/server/main.go",
	"backend/go.mod",
	"frontend/package.json",
}

// archiveURLs 生成候选下载地址，按顺序尝试：
// 1. 配置的镜像模板（默认 gh-proxy，国内可直连）
// 2. codeload 官方地址（作为兜底，国内可能不通）
func (s *UpdateService) archiveURLs(commit string) []string {
	replace := func(tmpl string) string {
		owner, name := splitRepo(s.cfg.UpdateRepoURL)
		repl := strings.NewReplacer(
			"{repo}", s.cfg.UpdateRepoURL,
			"{owner}", owner,
			"{name}", name,
			"{commit}", commit,
			"{short}", ShortCommit(commit),
			"{ref}", firstNonEmpty(commit, s.cfg.UpdateBranch),
			"{branch}", s.cfg.UpdateBranch,
			// 兼容「区间下载」型自建地址：{to} 即目标提交，{from} 无意义留空
			"{to}", commit,
			"{from}", "",
		)
		return repl.Replace(tmpl)
	}

	var urls []string
	seen := map[string]bool{}
	add := func(raw string) {
		u := strings.TrimSpace(raw)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		urls = append(urls, u)
	}
	if s.cfg.UpdateMirror != "" {
		add(replace(s.cfg.UpdateMirror))
	}
	add(replace(defaultFallbackArchive))
	return urls
}

// downloadAndExtract 把目标提交的源码包下载到 UPDATE_DIR，校验后解压到暂存目录。
// 返回暂存目录、包大小、SHA256 与实际使用的下载地址。
func (s *UpdateService) downloadAndExtract(ctx context.Context, commit string, report func(progress int, message string)) (string, int64, string, string, error) {
	if commit == "" {
		return "", 0, "", "", NewValidationError("缺少目标提交")
	}
	stagingRoot := filepath.Join(s.cfg.UpdateDir, "staging")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return "", 0, "", "", err
	}

	urls := s.archiveURLs(commit)
	if len(urls) == 0 {
		return "", 0, "", "", NewValidationError("没有可用的源码包地址（检查 UPDATE_MIRROR / UPDATE_REPO_URL）")
	}

	var lastErr error
	for _, url := range urls {
		report(25, "下载源码包："+redactURL(url))
		archivePath := filepath.Join(stagingRoot, commit+"-"+shortHashOf(url)+".pkg")
		res, err := s.client.download(ctx, url, archivePath, func(done, total int64) {
			progress := 30
			message := fmt.Sprintf("下载源码包 %.1f MB（总大小未知）", float64(done)/1024/1024)
			if total > 0 {
				progress = 30 + int(float64(done)/float64(total)*40)
				if progress > 70 {
					progress = 70
				}
				message = fmt.Sprintf("下载源码包 %.1f / %.1f MB", float64(done)/1024/1024, float64(total)/1024/1024)
			}
			report(progress, message)
		})
		if err != nil {
			lastErr = err
			continue
		}

		report(75, "校验并解压源码包…")
		stagedDir := filepath.Join(stagingRoot, commit)
		if err := os.RemoveAll(stagedDir); err != nil {
			return "", 0, "", "", err
		}
		if err := os.MkdirAll(stagedDir, 0o755); err != nil {
			return "", 0, "", "", err
		}
		if err := extractArchive(res.Path, stagedDir); err != nil {
			// 包坏了就换下一个地址重试，不留半解压目录
			_ = os.RemoveAll(stagedDir)
			lastErr = fmt.Errorf("%s 解压失败：%w", redactURL(url), err)
			continue
		}
		if err := validateStagedSource(stagedDir); err != nil {
			_ = os.RemoveAll(stagedDir)
			lastErr = fmt.Errorf("%s：%w", redactURL(url), err)
			continue
		}
		// 包文件用完即弃：磁盘上只留解压后的源码与备份
		_ = os.Remove(res.Path)
		report(78, "源码包校验通过")
		return stagedDir, res.Bytes, res.SHA256, url, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("源码包下载失败")
	}
	return "", 0, "", "", lastErr
}

// extractArchive 按魔数识别 tar.gz / zip 并解压到 destRoot。
//
// 安全策略：
//   - 拒绝绝对路径与 .. 穿越（防写到仓库外）；
//   - 跳过符号链接 / 硬链接 / 设备节点（不给上游包在宿主上放链接的机会）；
//   - 统一剥离压缩包的顶层目录（GitHub 源码包是 owner-repo-hash/）；
//   - 限制文件数与总体积（防 zip 炸弹）。
func extractArchive(archivePath, destRoot string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	head := make([]byte, 4)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}
	head = head[:n]
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}

	switch {
	case len(head) >= 2 && head[0] == 0x1f && head[1] == 0x8b:
		gz, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("gzip 头无效：%w", err)
		}
		defer gz.Close()
		return extractTar(gz, destRoot)
	case len(head) >= 4 && head[0] == 'P' && head[1] == 'K':
		// zip 需要随机读取，先整体读进临时文件由 zip.NewReader 处理
		info, err := file.Stat()
		if err != nil {
			return err
		}
		zr, err := zip.NewReader(file, info.Size())
		if err != nil {
			return fmt.Errorf("zip 头无效：%w", err)
		}
		return extractZip(zr, destRoot)
	default:
		return fmt.Errorf("无法识别的源码包格式（既不是 tar.gz 也不是 zip）")
	}
}

func extractTar(reader io.Reader, destRoot string) error {
	tr := tar.NewReader(reader)
	count := 0
	var total int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			rel, err := safeArchivePath(header.Name)
			if err != nil {
				return err
			}
			if rel == "" {
				continue
			}
			if err := os.MkdirAll(filepath.Join(destRoot, filepath.FromSlash(rel)), 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			rel, err := safeArchivePath(header.Name)
			if err != nil {
				return err
			}
			if rel == "" {
				continue
			}
			if header.Size > maxEntryBytes {
				return fmt.Errorf("条目 %s 超过单文件大小上限", header.Name)
			}
			count++
			if count > maxArchiveFiles {
				return fmt.Errorf("源码包文件数超过 %d 上限", maxArchiveFiles)
			}
			size, err := writeArchiveFileLimited(destRoot, rel, tr, header.Size, total)
			if err != nil {
				return err
			}
			total += size
		default:
			// 符号链接 / 硬链接 / 设备 / FIFO：一律忽略
			continue
		}
	}
	return nil
}

func extractZip(reader *zip.Reader, destRoot string) error {
	count := 0
	var total int64
	for _, entry := range reader.File {
		name := entry.Name
		if entry.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			rel, err := safeArchivePath(name)
			if err != nil {
				return err
			}
			if rel == "" {
				continue
			}
			if err := os.MkdirAll(filepath.Join(destRoot, filepath.FromSlash(rel)), 0o755); err != nil {
				return err
			}
			continue
		}
		// 符号链接在 zip 里带 unix 模式高位，直接按普通文件跳过
		if entry.Mode()&os.ModeSymlink != 0 {
			continue
		}
		rel, err := safeArchivePath(name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		if int64(entry.UncompressedSize64) > maxEntryBytes {
			return fmt.Errorf("条目 %s 超过单文件大小上限", name)
		}
		count++
		if count > maxArchiveFiles {
			return fmt.Errorf("源码包文件数超过 %d 上限", maxArchiveFiles)
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		// 体积按**实际写入**累计：UncompressedSize64 是包内声明值，可以被伪造成 0，
		// 只看声明值挡不住「两万个 256MB 条目」这种聚合炸弹。
		written, err := writeArchiveFileLimited(destRoot, rel, rc, int64(entry.UncompressedSize64), total)
		rc.Close()
		if err != nil {
			return err
		}
		total += written
	}
	return nil
}

// safeArchivePath 归一化压缩包内的条目路径：
// 返回相对路径（斜杠分隔，已剥离顶层目录），目录条目可能得到空串表示「无需创建」。
//
// 检查顺序很重要：先按原始路径判断穿越（`..` 一旦被 path.Clean 折叠就看不出意图了），
// 再剥离顶层目录，最后再判一次穿越。
func safeArchivePath(name string) (string, error) {
	// 压缩包内统一用 / 分隔；Windows 下也要防 \ 穿越
	clean := strings.ReplaceAll(name, "\\", "/")
	clean = strings.TrimPrefix(clean, "./")
	if clean == "" {
		return "", nil
	}
	if strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("源码包包含绝对路径条目：%s", name)
	}
	if hasTraversalSegment(clean) {
		return "", fmt.Errorf("源码包包含路径穿越条目：%s", name)
	}

	clean = path.Clean(clean)
	if clean == "." || clean == "" {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || hasTraversalSegment(clean) {
		return "", fmt.Errorf("源码包包含路径穿越条目：%s", name)
	}

	// 剥离顶层目录（GitHub 源码包是 owner-repo-hash/…）；
	// 单段路径（顶层下的散文件）直接忽略。
	if i := strings.Index(clean, "/"); i >= 0 {
		clean = clean[i+1:]
	} else {
		clean = ""
	}
	if clean == "" || clean == "." {
		return "", nil
	}
	if hasTraversalSegment(clean) {
		return "", fmt.Errorf("源码包包含路径穿越条目：%s", name)
	}
	return clean, nil
}

// hasTraversalSegment 判断路径里是否存在 `..` 段（真正的目录穿越尝试）。
func hasTraversalSegment(clean string) bool {
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

// writeArchiveFileLimited 落盘一个文件，返回实际写入字节数。
//
// 权限固定 0644：源码不需要可执行位（可执行产物由构建阶段生成），
// 这样也避免上游包用 0777/设置位在宿主上留下意外权限。
//
// writtenSoFar 是本次解压已写入的累计字节：一旦越界立即中止，不再继续写盘，
// 这样即使单文件声明被伪造，也无法把磁盘写满。
func writeArchiveFileLimited(destRoot, rel string, reader io.Reader, declared, writtenSoFar int64) (int64, error) {
	// 先算配额再开文件：否则 remaining 耗尽时会在 OpenFile 之后提前返回，
	// 句柄既不关闭（Windows 上还会锁住刚创建的文件，让后续 RemoveAll 静默失败）。
	limit := declared
	if limit <= 0 || limit > maxEntryBytes {
		limit = maxEntryBytes
	}
	// 再叠加全局余量：单文件上限与总体积上限取小
	remaining := maxExtractedBytes - writtenSoFar
	if remaining <= 0 {
		// 已经写满配额：必须先失败，否则「恰好填满」之后的每个条目都会绕过限制
		return 0, fmt.Errorf("源码包解压后超过 %d MB 上限", maxExtractedBytes>>20)
	}
	if limit > remaining {
		limit = remaining
	}

	target := filepath.Join(destRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	// limit+1：多读 1 字节用于判断「超限」，不会与任何上限发生整数溢出
	// （limit 已被夹在 maxEntryBytes/maxExtractedBytes 之内）。
	written, err := io.Copy(file, io.LimitReader(reader, limit+1))
	closeErr := file.Close()
	if err != nil {
		return written, err
	}
	if closeErr != nil {
		return written, closeErr
	}
	if written > limit {
		return written, fmt.Errorf("条目 %s 超过解压体积上限", rel)
	}
	return written, nil
}

// validateStagedSource 确认解压结果确实是本站源码。
func validateStagedSource(stagedDir string) error {
	var missing []string
	for _, rel := range requiredArchiveFiles {
		if !isFile(filepath.Join(stagedDir, filepath.FromSlash(rel))) {
			missing = append(missing, rel)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("源码包内容不完整，缺少 %s", strings.Join(missing, "、"))
	}
	return nil
}

// sameFileContent 用 SHA-256 比较两个文件是否一致（目标不存在 = 不一致）。
func sameFileContent(a, b string) (bool, error) {
	infoA, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	infoB, err := os.Stat(b)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if infoA.IsDir() != infoB.IsDir() {
		return false, nil
	}
	if infoA.IsDir() {
		return true, nil
	}
	if infoA.Size() != infoB.Size() {
		return false, nil
	}
	sumA, err := sha256File(a)
	if err != nil {
		return false, err
	}
	sumB, err := sha256File(b)
	if err != nil {
		return false, err
	}
	return sumA == sumB, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// shortHashOf 给下载地址生成稳定的短标识，用于包文件名（同一提交可能试多个镜像）。
func shortHashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// redactURL 去掉 URL 里的查询串（可能含 token），只留主机与路径。
func redactURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}
