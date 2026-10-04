package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// historyDetailRunes 限制历史记录里的说明长度（与操作日志的 500 字符上限对齐）。
const historyDetailRunes = 200

// stagePersistInterval 进度落盘的节流间隔：下载回调每 256KB 触发一次，
// 每次都写状态文件会造成大量 write+rename，这里限制到最多每秒一次。
const stagePersistInterval = time.Second

// Apply 执行一次「一键更新」：下载镜像包 / 源码包 → 校验 → 备份/就绪 → 替换/安装 → 触发重建。
//
// 同步部分只做校验与启动，真正的流程在后台 goroutine 里跑：
// 下载 + 构建动辄几分钟，HTTP 请求等不起；进度通过 Status() 轮询获取。
// handler 收到 nil 即可返回 202，随后让管理员看进度。
//
// commits 模式（默认）：下载源码包 → 替换源码 → 触发重建（runUpdate）；
// releases 模式（UPDATE_SOURCE=releases）：下载镜像包 → 写清单 → docker load（runUpdateRelease）。
func (s *UpdateService) Apply(_ context.Context, target, operator string) error {
	if !s.Enabled() {
		return NewValidationError("在线更新未开启（UPDATE_ENABLED=false）")
	}
	if s.isReleaseSource() {
		return s.applyRelease(target, operator)
	}
	if s.cfg.UpdateSourceDir == "" {
		return NewValidationError("未探测到源码目录，请设置 UPDATE_SOURCE_DIR 指向仓库根目录")
	}
	target = normalizeHash(target)
	if target == "" {
		return NewValidationError("缺少目标版本")
	}
	if len(target) < 7 || !looksLikeHash(target) {
		return NewValidationError("目标版本不是合法的提交哈希")
	}

	s.mu.Lock()
	if s.state.Last != nil && s.state.Last.Running {
		phase := s.state.Last.Phase
		s.mu.Unlock()
		return NewValidationError("已有更新任务在执行中（阶段：" + phase + "），请等待完成")
	}
	// 状态文件里已有待生效更新时：
	//   - 同一版本 → 已经替换完成、正等重建，重复提交没有意义（会写出一份空变更）；
	//     即便暂存目录被清理也一样拦，因为状态文件仍记录着它待生效；
	//   - 另一个版本 → 宿主代理可能正在重建，再改源码树会和构建过程打架。
	if pending := s.state.Pending; pending != nil {
		pendingShort := ShortCommit(pending.Hash)
		samePending := sameCommit(pending.Hash, target)
		s.mu.Unlock()
		if samePending {
			return NewValidationError("该版本（" + pendingShort +
				"）已替换完成，正在等待宿主更新代理重建与重启，无需重复更新")
		}
		return NewValidationError("已有待生效的更新（" + pendingShort +
			"），请等宿主更新代理完成重建并重启后再操作")
	}
	running, _, _ := s.recorder().runningCommit()
	if running != "" && sameCommit(running, target) {
		s.mu.Unlock()
		return NewValidationError("目标版本与当前运行版本相同，无需更新")
	}
	// 上次下载成功但替换阶段失败（没有写入 Pending）：暂存目录还在就直接复用，不重复下载
	reusable := dirExists(filepath.Join(s.cfg.UpdateDir, "staging", target, "backend"))

	stage := formatStage(PhaseStaging, target, 2, "准备更新…")
	stage.Operator = operator
	s.state.Last = stage
	s.saveStateLocked()
	s.mu.Unlock()

	go s.runUpdate(target, reusable)
	return nil
}

// downloadProgress 把「下载/解压进度回调」适配成阶段状态更新（走节流落盘）。
func (s *UpdateService) downloadProgress(progress int, message string) {
	s.reportStageThrottled(func(st *UpdateStage) {
		st.Phase = PhaseStaging
		st.Progress = progress
		st.Message = message
	})
}

// runUpdate 是后台更新流程本体。任何失败都会写入阶段错误，必要时自动回滚。
func (s *UpdateService) runUpdate(target string, reusable bool) {
	// 后台任务绝不能因为 panic 把整个后端带走：兜底记录为失败并释放 running 标记
	defer func() {
		if r := recover(); r != nil {
			s.finishFailed(target, fmt.Errorf("更新任务异常终止：%v", r), "")
			log.Printf("[update] 更新任务 panic：%v", r)
		}
	}()

	startedAt := time.Now()

	var (
		stagedDir   string
		archiveSize int64
		archiveSum  string
		archiveSrc  string
	)

	if reusable {
		pending := s.pendingSnapshot()
		stagedDir = pending.StagedDir
		archiveSize = pending.Bytes
		archiveSum = pending.SHA256
		archiveSrc = pending.Source
		s.reportStage(func(st *UpdateStage) {
			st.Phase = PhaseStaging
			st.Progress = 80
			st.Message = "复用已下载的源码包"
		})
	} else {
		s.reportStage(func(st *UpdateStage) {
			st.Phase = PhaseStaging
			st.Progress = 20
			st.Message = "开始下载源码包"
		})
		// 下载不挂请求上下文：管理员关掉页面不应中断更新
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		var err error
		stagedDir, archiveSize, archiveSum, archiveSrc, err = s.downloadAndExtract(ctx, target, s.downloadProgress)
		cancel()
		if err != nil {
			s.finishFailed(target, err, "")
			return
		}
	}

	// 备份 + 替换源码
	s.reportStage(func(st *UpdateStage) {
		st.Phase = PhaseSwapping
		st.Progress = 82
		st.Message = "备份并替换源码文件…"
	})
	backupID, changeList, err := s.applyStaged(stagedDir)
	if err != nil {
		rolledBack := false
		if backupID != "" {
			if rbErr := s.rollback(backupID); rbErr == nil {
				rolledBack = true
			} else {
				log.Printf("[update] 自动回滚失败（备份 %s）：%v", backupID, rbErr)
			}
		}
		s.finishFailed(target, fmt.Errorf("替换源码失败：%w", err), backupID)
		s.reportStage(func(st *UpdateStage) {
			st.RolledBack = rolledBack
		})
		return
	}

	writes, deletes, bytes := summarizeChanges(changeList)
	detail := describeChanges(writes, deletes, bytes)

	s.mu.Lock()
	s.state.Pending = &pendingState{
		Hash:      target,
		StagedDir: stagedDir,
		Files:     len(changeList),
		Bytes:     archiveSize,
		SHA256:    archiveSum,
		Source:    archiveSrc,
		StagedAt:  time.Now().Format(time.RFC3339),
		Mode:      s.Mode(),
	}
	if s.state.Last != nil {
		s.state.Last.FilesChanged = len(changeList)
		s.state.Last.BackupID = backupID
		s.state.Last.Progress = 90
		s.state.Last.Message = "源码已替换，准备重建"
	}
	s.saveStateLocked()
	s.mu.Unlock()

	// 通知宿主代理：容器部署必须靠它把新代码跑起来
	if err := writePendingManifest(s, target, stagedDir, backupID, changeList); err != nil {
		log.Printf("[update] 写入待更新清单失败：%v", err)
	}

	// 注意：闭包在持有 s.mu 时执行，里面只能碰 st，不能再调任何会加锁的方法
	rebuildHint := s.rebuildMessage()
	s.reportStage(func(st *UpdateStage) {
		st.Phase = PhaseRebuilding
		st.Progress = 92
		st.Message = rebuildHint
	})
	rebuildStarted := time.Now()
	rebuildErr := s.rebuild(target)
	rebuildMs := time.Since(rebuildStarted).Milliseconds()

	if rebuildErr != nil {
		s.mu.Lock()
		s.state.Last = stageError(s.state.Last, fmt.Errorf("重建失败：%w", rebuildErr))
		if s.state.Last != nil {
			s.state.Last.RebuildMS = rebuildMs
			s.state.Last.RebuildMode = s.Mode()
		}
		s.appendHistoryLocked(UpdateHistoryItem{
			At:       time.Now().Format(time.RFC3339),
			From:     ShortCommit(s.currentCommit()),
			To:       ShortCommit(target),
			Result:   "failed",
			Detail:   truncate(detail+"；重建失败："+rebuildErr.Error(), historyDetailRunes),
			BackupID: backupID,
			Operator: s.operatorLocked(), // 已持锁，不能再调会加锁的 stageOperator
		})
		s.saveStateLocked()
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	waitingAgent := s.Mode() == RebuildModeWaitingAgent
	s.state.Last.Running = false
	s.state.Last.Phase = PhaseSuccess
	s.state.Last.Success = true
	s.state.Last.Progress = 100
	s.state.Last.FinishedAt = time.Now().Format(time.RFC3339)
	s.state.Last.FilesChanged = len(changeList)
	s.state.Last.BackupID = backupID
	s.state.Last.RebuildMS = rebuildMs
	s.state.Last.RebuildMode = s.Mode()
	s.state.Last.WaitingAgent = waitingAgent
	if waitingAgent {
		s.state.Last.Message = "源码已更新，等待宿主更新代理完成重建与重启"
	} else {
		s.state.Last.Message = fmt.Sprintf("更新完成，用时 %.1f 秒", time.Since(startedAt).Seconds())
	}
	s.appendHistoryLocked(UpdateHistoryItem{
		At:       time.Now().Format(time.RFC3339),
		From:     ShortCommit(s.currentCommit()),
		To:       ShortCommit(target),
		Result:   "success",
		Detail:   truncate(detail, historyDetailRunes),
		BackupID: backupID,
		Operator: s.operatorLocked(), // 已持锁，不能再调会加锁的 stageOperator
	})
	s.saveStateLocked()
	s.mu.Unlock()

	s.cleanupOldStages(target)
}

// Rollback 回滚到某次更新的备份（commits 模式）或上一个镜像版本（releases 模式）。
func (s *UpdateService) Rollback(backupID, operator string) error {
	if !s.Enabled() {
		return NewValidationError("在线更新未开启")
	}
	s.mu.Lock()
	if s.state.Last != nil && s.state.Last.Running {
		s.mu.Unlock()
		return NewValidationError("有更新任务在执行中，暂不能回滚")
	}
	s.mu.Unlock()

	if s.isReleaseSource() {
		return s.rollbackRelease(backupID, operator)
	}

	if err := s.rollback(backupID); err != nil {
		return err
	}

	s.mu.Lock()
	s.state.Last = &UpdateStage{
		Running:    false,
		Phase:      PhaseRolledBack,
		Progress:   100,
		Message:    "已回滚到备份 " + backupID,
		FinishedAt: time.Now().Format(time.RFC3339),
		BackupID:   backupID,
		RolledBack: true,
		Operator:   operator,
	}
	s.state.Pending = nil
	s.appendHistoryLocked(UpdateHistoryItem{
		At:       time.Now().Format(time.RFC3339),
		Result:   "rolled_back",
		Detail:   "回滚到备份 " + backupID + "（需重新构建并重启才会生效）",
		BackupID: backupID,
		Operator: operator,
	})
	s.saveStateLocked()
	s.mu.Unlock()

	// 回滚同样需要宿主代理重建，重写清单让代理感知
	if err := writePendingManifest(s, "rollback:"+backupID, "", backupID, nil); err != nil {
		log.Printf("[update] 写入回滚清单失败：%v", err)
	}
	return nil
}

// BackupInfo 是一个可回滚的备份点。
type BackupInfo struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"path"`
}

// Backups 列出可用备份（最新在前），供前端选择回滚点。
//
// releases 模式没有源码备份：回滚点是「保留在本地的上一个镜像包」，
// 以安装历史（release-history.json）为准，ID 即版本号。
func (s *UpdateService) Backups() []BackupInfo {
	if s.isReleaseSource() {
		out := []BackupInfo{}
		for _, item := range s.loadReleaseHistory() {
			if !isFile(item.ImagePath) {
				continue
			}
			out = append(out, BackupInfo{
				ID:        item.Version,
				CreatedAt: item.UpdatedAt,
				Path:      item.ImagePath,
			})
		}
		return out
	}
	root := filepath.Join(s.cfg.UpdateDir, "backups")
	entries, err := os.ReadDir(root)
	if err != nil {
		return []BackupInfo{}
	}
	out := make([]BackupInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupInfo{
			ID:        entry.Name(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
			Path:      filepath.Join(root, entry.Name()),
		})
	}
	// 目录名形如 <hash>-YYYYMMDD-HHMMSS，按名字倒序即最新在前
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ID > out[i].ID {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 阶段状态读写（全部走锁，避免后台任务与前端轮询互相踩）
// ---------------------------------------------------------------------------

func (s *UpdateService) updateStage(mutate func(*UpdateStage)) *UpdateStage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Last == nil {
		s.state.Last = &UpdateStage{}
	}
	mutate(s.state.Last)
	s.saveStateLocked()
	return s.state.Last
}

// reportStage 更新进度并**立即落盘**（用于阶段切换等关键节点）。
// 注意：mutate 在持有 s.mu 时执行，里面只能改传入的 Stage 字段，
// **不能调用任何会加锁的 Service 方法**（Mutex 不可重入）。
func (s *UpdateService) reportStage(report func(*UpdateStage)) {
	s.updateStage(report)
}

// reportStageThrottled 给高频进度回调用的版本：内存状态立即更新，磁盘写入节流。
func (s *UpdateService) reportStageThrottled(report func(*UpdateStage)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Last == nil {
		s.state.Last = &UpdateStage{}
	}
	report(s.state.Last)
	s.saveStateThrottledLocked()
}

func (s *UpdateService) pendingSnapshot() pendingState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Pending == nil {
		return pendingState{}
	}
	return *s.state.Pending
}

// stageOperator 取当前任务的发起人（历史记录用）。
func (s *UpdateService) stageOperator() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.Last == nil {
		return ""
	}
	return s.state.Last.Operator
}

// currentCommit 返回当前运行代码的提交（每次现读，避免缓存过期）。
// 调用方在持锁路径上时直接用它即可：它只读文件与配置，不碰 s.mu。
func (s *UpdateService) currentCommit() string {
	commit, _, _ := s.recorder().runningCommit()
	return commit
}

// finishFailed 记录一次失败的更新（含历史），供前端与审计查看。
func (s *UpdateService) finishFailed(target string, err error, backupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Last = stageError(s.state.Last, err)
	if s.state.Last != nil {
		s.state.Last.BackupID = backupID
	}
	s.appendHistoryLocked(UpdateHistoryItem{
		At:       time.Now().Format(time.RFC3339),
		From:     ShortCommit(s.currentCommit()),
		To:       ShortCommit(target),
		Result:   "failed",
		Detail:   truncate(err.Error(), historyDetailRunes),
		BackupID: backupID,
		Operator: s.operatorLocked(),
	})
	s.saveStateLocked()
}

// operatorLocked 取当前任务发起人（调用方需已持有 s.mu）。
func (s *UpdateService) operatorLocked() string {
	if s.state.Last == nil {
		return ""
	}
	return s.state.Last.Operator
}

// cleanupOldStages 更新成功后清掉旧的暂存目录（回滚仍可用备份）。
func (s *UpdateService) cleanupOldStages(keep string) {
	root := filepath.Join(s.cfg.UpdateDir, "staging")
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == keep {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, entry.Name()))
	}
}

// rebuildMessage 描述「谁来重建」，前端把它显示在进度条上。
func (s *UpdateService) rebuildMessage() string {
	switch s.Mode() {
	case RebuildModeWaitingAgent:
		return "正在通知宿主更新代理重建…"
	case RebuildModeDocker:
		return "正在执行 docker compose 重建（可能耗时数分钟）…"
	default:
		return "正在本机重新编译并重启…"
	}
}

// describeChanges 生成变更摘要文本（历史记录用）。
func describeChanges(writes, deletes int, bytes int64) string {
	text := fmt.Sprintf("写入 %d 个文件（%.2f MB）", writes, float64(bytes)/1024/1024)
	if deletes > 0 {
		text += fmt.Sprintf("，清理上游已删除的 %d 个文件", deletes)
	}
	return text
}

// normalizeHash 规范化提交哈希：去空白、转小写。
func normalizeHash(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
