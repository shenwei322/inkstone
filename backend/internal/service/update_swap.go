package service

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 永不覆盖的路径：站点数据、配置、依赖与构建产物。
// 更新只替换「源码」，这些内容一旦被上游包覆盖就是灾难：
// 数据丢失（data/）、密钥被重置（.env）、依赖被清空（node_modules/）。
var guardedPrefixes = []string{
	"data/",
	"node_modules/",
	"uploads/",
	"files/",
	".git/",
	".update/",
	".next/",
	".tools/",
	".vscode/",
	".idea/",
}

// skipSourceDir 遍历时跳过重目录。
// 目录名统一转小写后比较：NTFS 大小写不敏感，`Data/`、`.Git/` 与
// `data/`、`.git/` 落到磁盘上是同一个目录。
func skipSourceDir(name string) bool {
	switch strings.ToLower(name) {
	case "node_modules", ".git", ".next", ".update", ".tools", "data", "uploads":
		return true
	}
	return false
}

// isGuardedPath 判断相对路径（斜杠分隔）是否受保护。
//
// 两层规则（与遍历时的 skipSourceDir 配合使用）：
//  1. 顶层前缀匹配（guardedPrefixes）：保护仓库根的 data/、uploads/、files/ 等目录；
//  2. 路径段匹配（protectedSegment）：保护出现在**任意层级**的危险目录
//     （backend/data/x.json、frontend/.next/y.json）。
//
// 两层的名单故意不完全相同：files/uploads 只做顶层匹配，因为站点源码里
// 确实存在 frontend/app/admin/files/page.tsx 这类同名目录。
//
// ⚠️ Windows 大小写不敏感：所有比较必须先转小写。此前这里直接 HasPrefix("data/")
// 等大小写敏感比较，于是上游包里放一个 `Data/x.json`、`.ENV`、`.Git/config`
// 就能骗过守卫——NTFS 会把它写到真正的 data/、.env、.git/ 上，
// 造成站点数据被覆盖、密钥被替换、deployed-commit.json 被投毒。
// Linux 的 ext4 大小写敏感，不受影响；这是仅 Windows 部署存在的缺口。
func isGuardedPath(rel string) bool {
	clean := strings.Trim(strings.TrimPrefix(filepath.ToSlash(rel), "./"), "/")
	if clean == "" {
		return true
	}
	// Windows 特有形态，源码路径里不可能合法出现，宁可拒绝：
	//   - ':' NTFS 备用数据流（evil.ps1:hidden），可让写入落到文件流上；
	//   - '~' 8.3 短名（PROGRA~1），用于在路径形态上躲避目录名匹配。
	if strings.ContainsRune(clean, ':') || strings.ContainsRune(clean, '~') {
		return true
	}
	lower := strings.ToLower(clean)
	// 任何层级的 .env* 都不动（密钥属于站点，不属于上游）
	base := lower[strings.LastIndexByte(lower, '/')+1:]
	if strings.HasPrefix(base, ".env") {
		return true
	}
	for _, prefix := range guardedPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, segment := range strings.Split(lower, "/") {
		if protectedSegment(segment) {
			return true
		}
	}
	return false
}

// protectedSegment 判断单个路径段是否属于受保护目录（嵌套层级用）。
//
// 只列入「不会出现在正常源码路径里」的目录名：data 是本仓库真实存在的运行
// 数据目录（backend/data/…），而 uploads/files 只能靠顶层前缀匹配保护——
// 仓库里确实有 frontend/app/admin/files/page.tsx 这样的源码目录，
// 一旦按段匹配就会把「文件管理」页面的更新静默吞掉。
func protectedSegment(segment string) bool {
	switch strings.ToLower(segment) {
	case "data", "node_modules", ".git", ".update", ".next", ".tools":
		return true
	}
	return false
}

// changeEntry 是一次更新中单个文件的变更记录（用于审计、回滚清单与统计）。
// Existed 记录「写入前目标文件是否已存在」：回滚时要据此把本次新增的文件删掉，
// 否则回滚后新旧代码会混在一起。
type changeEntry struct {
	Path    string `json:"path"`
	Action  string `json:"action"` // write / delete
	Hash    string `json:"hash"`   // 新内容哈希（写入前）
	Bytes   int64  `json:"bytes"`
	Existed bool   `json:"existed,omitempty"`
}

// stagedChanges 比较暂存源码与当前源码，列出需要写入/删除的文件。
func stagedChanges(stagedRoot, sourceDir string) ([]changeEntry, error) {
	var changes []changeEntry
	err := filepath.WalkDir(stagedRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipSourceDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(stagedRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isGuardedPath(rel) {
			return nil
		}
		same, err := sameFileContent(path, filepath.Join(sourceDir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if same {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sum, err := sha256File(path)
		if err != nil {
			return err
		}
		changes = append(changes, changeEntry{Path: rel, Action: "write", Hash: sum, Bytes: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 上游删除的文件：本机仍存在则一并清理（含空目录），否则残留代码会和新代码混在一起
	err = filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != sourceDir && (skipSourceDir(name) || isGuardedPath(relSlash(sourceDir, path))) {
				return filepath.SkipDir
			}
			return nil
		}
		rel := relSlash(sourceDir, path)
		if isGuardedPath(rel) {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(stagedRoot, filepath.FromSlash(rel))); statErr == nil {
			return nil
		}
		changes = append(changes, changeEntry{Path: rel, Action: "delete"})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changes, nil
}

// countChanges 只统计变更数量（不做哈希明细），用于「没有版本记录」时
// 判断目标提交的源码是否与本地一致——避免把「无记录」误判成「有新版本」。
func countChanges(stagedRoot, sourceDir string) (int, error) {
	changes, err := stagedChanges(stagedRoot, sourceDir)
	if err != nil {
		return 0, err
	}
	return len(changes), nil
}

func relSlash(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// applyStaged 备份后把暂存源码写入源码目录，返回本次更新的备份 ID 与变更清单。
//
// 写入顺序：先备份 → 再写入 → 最后删除。任一步失败即返回错误，调用方负责回滚。
// 遍历暂存目录时顺带补上预览缓存：Apply 之后的 Status 轮询就不必再哈希一遍全部文件。
func (s *UpdateService) applyStaged(stagedRoot string) (string, []changeEntry, error) {
	sourceDir := s.cfg.UpdateSourceDir
	if sourceDir == "" {
		return "", nil, NewValidationError("未探测到源码目录，无法替换源码")
	}
	if !dirExists(stagedRoot) {
		return "", nil, NewValidationError("暂存源码不存在，请重新下载更新包")
	}
	changes, err := stagedChanges(stagedRoot, sourceDir)
	if err != nil {
		return "", nil, err
	}

	writes, deletes, bytes := summarizeChanges(changes)
	s.mu.Lock()
	s.previewKey = stagedRoot
	s.previewValue = &PendingPreview{Writes: writes, Deletes: deletes, Bytes: bytes}
	s.mu.Unlock()

	backupID := fmt.Sprintf("%s-%s", ShortCommit(filepath.Base(stagedRoot)), time.Now().Format("20060102-150405"))
	backupRoot := filepath.Join(s.cfg.UpdateDir, "backups", backupID)

	// 1) 备份将被覆盖 / 被删除的文件
	// 目录条目跳过：上游若把「目录」换成「同名文件」，备份一个目录会 io.Copy 失败
	// （EISDIR），整次更新失败；写入阶段由 copyFileAtomic 负责移除旧目录。
	for i := range changes {
		src := filepath.Join(sourceDir, filepath.FromSlash(changes[i].Path))
		info, statErr := os.Stat(src)
		if statErr != nil || info.IsDir() {
			continue // 新增文件 / 目录占位：无需备份，回滚时直接删除
		}
		changes[i].Existed = true
		if err := copyFilePreserve(src, filepath.Join(backupRoot, filepath.FromSlash(changes[i].Path))); err != nil {
			return backupID, changes, fmt.Errorf("备份 %s 失败：%w", changes[i].Path, err)
		}
	}
	manifest := map[string]any{
		"backup_id":  backupID,
		"source_dir": sourceDir,
		"staged_dir": stagedRoot,
		"created_at": time.Now().Format(time.RFC3339),
		"changes":    changes,
	}
	if err := writeJSONFile(filepath.Join(backupRoot, "manifest.json"), manifest); err != nil {
		return backupID, changes, fmt.Errorf("写入回滚清单失败：%w", err)
	}

	// 2) 写入新内容
	for _, change := range changes {
		if change.Action != "write" {
			continue
		}
		from := filepath.Join(stagedRoot, filepath.FromSlash(change.Path))
		to := filepath.Join(sourceDir, filepath.FromSlash(change.Path))
		if err := copyFileAtomic(from, to); err != nil {
			return backupID, changes, fmt.Errorf("写入 %s 失败：%w", change.Path, err)
		}
	}

	// 3) 清理上游已删除的文件，并回收空目录
	for _, change := range changes {
		if change.Action != "delete" {
			continue
		}
		target := filepath.Join(sourceDir, filepath.FromSlash(change.Path))
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return backupID, changes, fmt.Errorf("删除 %s 失败：%w", change.Path, err)
		}
		removeEmptyParents(filepath.Dir(target), sourceDir)
	}
	return backupID, changes, nil
}

// rollback 用备份还原源码。
//
// 优先按备份里的 manifest.json 清单执行（能识别「本次新增的文件」并删除，
// 避免回滚后新旧代码混在一起）；读不到清单时退化为「把备份目录里的文件全部还原」，
// 这样历史备份也能回滚。
func (s *UpdateService) rollback(backupID string) error {
	if strings.TrimSpace(backupID) == "" {
		return NewValidationError("缺少备份 ID")
	}
	// 备份 ID 必须是 createBackupID() 生成的白名单字符（时间戳 + 短随机串）。
	// 此前只拒 / 和 \，于是 `..`、`.`、`C:` 这类值能通过：
	// backupRoot 会变成 UpdateDir 本身或别处目录，rollbackFromDirectory
	// 会把 update-state.json、deployed-commit.json、backups/** 全量复制进
	// 源码树，污染部署记录并塞入垃圾文件。
	if !isSafeBackupID(backupID) {
		return NewValidationError("备份 ID 非法")
	}
	sourceDir := s.cfg.UpdateSourceDir
	if sourceDir == "" {
		return NewValidationError("未探测到源码目录，无法回滚")
	}
	backupRoot := filepath.Join(s.cfg.UpdateDir, "backups", backupID)
	if !dirExists(backupRoot) {
		return NewValidationError("备份不存在：" + backupID)
	}

	if changes := readBackupManifest(filepath.Join(backupRoot, "manifest.json")); len(changes) > 0 {
		return s.rollbackByManifest(changes, backupRoot, sourceDir)
	}
	return rollbackFromDirectory(backupRoot, sourceDir)
}

// isSafeBackupID 校验备份 ID 只含 createBackupID() 会生成的字符。
// 白名单 ^[A-Za-z0-9._-]{1,64}$，且拒绝以 "." 开头——后者让 `..`、`.` 这类
// 相对路径值无法通过（否则 filepath.Join(UpdateDir,"backups","..") 会指向
// UpdateDir 本身）。
func isSafeBackupID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	if id[0] == '.' {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// rollbackByManifest 按变更清单回滚：还原被覆盖/删除的文件，并删除本次新增的文件。
// 清单里没有可用条目（例如只有受保护路径）时视为已经处于目标状态，幂等成功。
func (s *UpdateService) rollbackByManifest(changes []changeEntry, backupRoot, sourceDir string) error {
	for _, change := range changes {
		rel, ok := safeSourceRelative(change.Path)
		if !ok || isGuardedPath(rel) {
			continue
		}
		target := filepath.Join(sourceDir, filepath.FromSlash(rel))
		switch {
		case change.Action == "write" && change.Existed:
			// 更新前已存在 → 必须从备份还原；备份文件缺失说明备份被破坏，
			// 不能静默跳过（那会变成「部分回滚」却报告成功）
			backupPath := filepath.Join(backupRoot, filepath.FromSlash(rel))
			if !isFile(backupPath) {
				return fmt.Errorf("备份中缺少 %s，无法完整回滚该文件", rel)
			}
			if err := copyFilePreserve(backupPath, target); err != nil {
				return fmt.Errorf("还原 %s 失败：%w", rel, err)
			}
		case change.Action == "write" && !change.Existed:
			// 更新前不存在 → 本次新增，回滚要删掉
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("删除新增文件 %s 失败：%w", rel, err)
			}
			removeEmptyParents(filepath.Dir(target), sourceDir)
		case change.Action == "delete":
			// 更新时被上游删掉的文件：备份里有就还原，没有（例如它本来就不存在）就跳过
			backupPath := filepath.Join(backupRoot, filepath.FromSlash(rel))
			if isFile(backupPath) {
				if err := copyFilePreserve(backupPath, target); err != nil {
					return fmt.Errorf("还原 %s 失败：%w", rel, err)
				}
			}
		}
	}
	return nil
}

// safeSourceRelative 校验清单里的路径并归一化：必须是相对路径、不含 `..`。
// 清单文件本身是本地可改的，不能无条件信任（`..` 会让还原写到源码目录之外）。
func safeSourceRelative(rel string) (string, bool) {
	clean := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(clean, "/") {
		return "", false // 绝对路径
	}
	clean = strings.Trim(clean, "/")
	if clean == "" || hasTraversalSegment(clean) {
		return "", false
	}
	return clean, true
}

// rollbackFromDirectory 没有清单时的兜底：把备份目录里所有文件还原回去。
// 它无法识别「本次新增」的文件，属降级行为（历史备份才会有这种情况）。
func rollbackFromDirectory(backupRoot, sourceDir string) error {
	return filepath.WalkDir(backupRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := relSlash(backupRoot, path)
		if rel == "manifest.json" || isGuardedPath(rel) {
			return nil
		}
		clean, ok := safeSourceRelative(rel)
		if !ok {
			return nil
		}
		return copyFilePreserve(path, filepath.Join(sourceDir, filepath.FromSlash(clean)))
	})
}

// readBackupManifest 读取备份清单里的变更列表（缺失或损坏时返回 nil，走目录兜底）。
func readBackupManifest(path string) []changeEntry {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var manifest struct {
		Changes []changeEntry `json:"changes"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil
	}
	return manifest.Changes
}

// ---------------------------------------------------------------------------
// 文件操作（全部先写临时文件再 rename，避免进程被杀时留下半截文件）
// ---------------------------------------------------------------------------

// copyFileAtomic 把 src 内容原子写到 dst；目标存在且是目录时先移除。
// 可执行位（0755）会被保留：仓库里的 .sh 脚本需要它。
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if info.Mode()&0o111 != 0 {
		perm = 0o755
	}
	if err := ensureParentDir(dst); err != nil {
		return err
	}
	if target, err := os.Lstat(dst); err == nil && target.IsDir() {
		// 目录变文件（上游把某个目录换成了同名文件）
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}
	tmp := dst + ".inkstone-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// copyFilePreserve 备份用：保留权限，同样先临时文件再 rename。
func copyFilePreserve(src, dst string) error {
	if err := ensureParentDir(dst); err != nil {
		return err
	}
	if target, err := os.Lstat(dst); err == nil && target.IsDir() {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".inkstone-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, info.Mode().Perm()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// ensureParentDir 创建父目录；父路径被同名文件占位时先移除（文件变目录）。
func ensureParentDir(path string) error {
	parent := filepath.Dir(path)
	if info, err := os.Lstat(parent); err == nil && !info.IsDir() {
		if err := os.Remove(parent); err != nil {
			return err
		}
	}
	return os.MkdirAll(parent, 0o755)
}

// removeEmptyParents 自底向上删除空目录，最多到 stopAt（不含）。
func removeEmptyParents(dir, stopAt string) {
	for {
		if dir == "" || dir == stopAt || dir == filepath.Dir(dir) || len(dir) < len(stopAt) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func writeJSONFile(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// summarizeChanges 生成变更摘要（供历史记录展示，避免把上千条路径塞进日志）。
func summarizeChanges(changes []changeEntry) (writes, deletes int, bytes int64) {
	for _, c := range changes {
		if c.Action == "delete" {
			deletes++
			continue
		}
		writes++
		bytes += c.Bytes
	}
	return writes, deletes, bytes
}
