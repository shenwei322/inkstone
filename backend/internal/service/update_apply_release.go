package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Releases 模式的更新执行（UPDATE_SOURCE=releases）
//
// 与源码更新（update_apply.go）的本质区别：
//   - 不下载源码包、不替换源码、不做逐文件备份；
//   - 下载 Release 里的镜像包资产（inkstone-images-<tag>.tar）；
//   - 生效动作是 docker load + docker compose up -d：
//       · 容器部署（默认 waiting_agent）：后端只负责下载与写清单，
//         宿主代理 deploy/update-agent.ps1 / .sh 完成安装；
//       · 后端有 docker 权限（UPDATE_WAITING_AGENT=false）：后端自己装。
// ---------------------------------------------------------------------------

// releaseComposeFallback 后端自己执行 compose 时的兜底编排文件名
// （自动探测结果为空时不用猜，直接报错让管理员配 UPDATE_COMPOSE_FILE）。
const releaseComposeFallback = "docker-compose.offline.yml"

// applyRelease 启动一次镜像包更新。同步部分只做校验，流程在后台 goroutine。
func (s *UpdateService) applyRelease(target, operator string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		// 允许「不先点检查直接更新」：补一次检查确定上游最新版本
		status, err := s.Check(context.Background())
		if err != nil {
			return err
		}
		target = strings.TrimSpace(status.Version.LatestVersion)
		if target == "" {
			return NewValidationError("未能确定上游最新版本，请先执行一次检查更新")
		}
	}
	if _, ok := parseVersionValue(target); !ok {
		return NewValidationError("目标版本不是合法的版本号（形如 v1.28.0）")
	}
	mode := s.Mode()
	if mode != RebuildModeWaitingAgent && mode != RebuildModeDocker {
		return NewValidationError("镜像包更新需要 docker compose：请设置 UPDATE_COMPOSE_FILE，或保持 UPDATE_WAITING_AGENT=true 由宿主代理安装")
	}

	s.mu.Lock()
	if s.state.Last != nil && s.state.Last.Running {
		phase := s.state.Last.Phase
		s.mu.Unlock()
		return NewValidationError("已有更新任务在执行中（阶段：" + phase + "），请等待完成")
	}
	if pending := s.state.Pending; pending != nil {
		label := pending.Version
		if label == "" {
			label = ShortCommit(pending.Hash)
		}
		s.mu.Unlock()
		return NewValidationError("已有待生效的更新（" + label +
			"），请等宿主更新代理完成安装后再操作")
	}
	local, _ := s.localVersion()
	if sameVersion(local, target) {
		s.mu.Unlock()
		return NewValidationError("目标版本与当前运行版本相同，无需更新")
	}
	stage := formatStage(PhaseStaging, target, 2, "准备下载镜像包…")
	stage.Operator = operator
	s.state.Last = stage
	s.saveStateLocked()
	s.mu.Unlock()

	go s.runUpdateRelease(target, local)
	return nil
}

// runUpdateRelease 是镜像包更新的后台流程本体：下载 → 写待安装清单 → 安装。
func (s *UpdateService) runUpdateRelease(target, previous string) {
	// 后台任务绝不能因为 panic 把整个后端带走：兜底记录为失败并释放 running 标记
	defer func() {
		if r := recover(); r != nil {
			s.finishFailedRelease(target, fmt.Errorf("更新任务异常终止：%v", r), previous)
			log.Printf("[update] 镜像包更新任务 panic：%v", r)
		}
	}()

	// 下载不挂请求上下文：管理员关掉页面不应中断更新
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()

	s.reportStage(func(st *UpdateStage) {
		st.Phase = PhaseStaging
		st.Progress = 10
		st.Message = "获取 Release 信息…"
	})
	rel, err := s.fetchRelease(ctx, target)
	if err != nil {
		s.finishFailedRelease(target, err, previous)
		return
	}
	asset, err := s.pickImageAsset(rel)
	if err != nil {
		s.finishFailedRelease(target, err, previous)
		return
	}
	if maxBytes := s.maxImageBytes(); maxBytes > 0 && asset.Size > maxBytes {
		s.finishFailedRelease(target, fmt.Errorf(
			"镜像包 %.1f MB 超过上限 %.0f MB（UPDATE_IMAGE_MAX_MB）",
			float64(asset.Size)/1024/1024, float64(maxBytes)/1024/1024), previous)
		return
	}

	imagePath, size, sum, err := s.downloadImageAsset(ctx, rel.Tag, asset, s.downloadProgress)
	if err != nil {
		s.finishFailedRelease(target, err, previous)
		return
	}

	s.reportStage(func(st *UpdateStage) {
		st.Phase = PhaseSwapping
		st.Progress = 80
		st.Message = "镜像包已就绪，准备安装"
	})

	s.mu.Lock()
	s.state.Pending = &pendingState{
		Hash:        rel.Tag,
		Kind:        pendingKindReleaseImage,
		Version:     rel.Tag,
		Bytes:       size,
		SHA256:      sum,
		Source:      asset.URL,
		StagedAt:    time.Now().Format(time.RFC3339),
		Mode:        s.Mode(),
		ImagePath:   imagePath,
		ImageName:   asset.Name,
		ComposeFile: s.releaseComposeFile(),
		Previous:    previous,
	}
	if s.state.Last != nil {
		s.state.Last.Progress = 85
		s.state.Last.Message = "镜像包已就绪，准备安装"
	}
	s.saveStateLocked()
	s.mu.Unlock()

	// 通知宿主代理：容器部署必须靠它把新镜像跑起来
	if err := writePendingImageManifest(s, rel, asset, imagePath, sum, previous); err != nil {
		log.Printf("[update] 写入镜像包待安装清单失败：%v", err)
	}

	rebuildHint := s.rebuildMessage()
	s.reportStage(func(st *UpdateStage) {
		st.Phase = PhaseRebuilding
		st.Progress = 90
		st.Message = rebuildHint
	})
	rebuildStarted := time.Now()
	rebuildErr := s.rebuild(rel.Tag)
	rebuildMs := time.Since(rebuildStarted).Milliseconds()
	if rebuildErr != nil {
		s.mu.Lock()
		s.state.Last = stageError(s.state.Last, fmt.Errorf("镜像安装失败：%w", rebuildErr))
		if s.state.Last != nil {
			s.state.Last.RebuildMS = rebuildMs
			s.state.Last.RebuildMode = s.Mode()
		}
		s.appendHistoryLocked(UpdateHistoryItem{
			At:       time.Now().Format(time.RFC3339),
			From:     previous,
			To:       rel.Tag,
			Result:   "failed",
			Detail:   truncate("镜像安装失败："+rebuildErr.Error(), historyDetailRunes),
			Operator: s.operatorLocked(),
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
	s.state.Last.RebuildMS = rebuildMs
	s.state.Last.RebuildMode = s.Mode()
	s.state.Last.WaitingAgent = waitingAgent
	if waitingAgent {
		s.state.Last.Message = "镜像包已就绪，等待宿主更新代理 docker load 并重启"
	} else {
		s.state.Last.Message = fmt.Sprintf("镜像包已安装，用时 %.1f 秒", time.Since(rebuildStarted).Seconds())
	}
	s.appendHistoryLocked(UpdateHistoryItem{
		At:       time.Now().Format(time.RFC3339),
		From:     previous,
		To:       rel.Tag,
		Result:   "success",
		Detail:   truncate(fmt.Sprintf("安装镜像包 %s（%.1f MB）", asset.Name, float64(size)/1024/1024), historyDetailRunes),
		Operator: s.operatorLocked(),
	})
	if !waitingAgent {
		// 后端自己装的：立刻写部署版本记录；等待代理的时候由代理写回
		if err := s.writeDeployedVersion(rel.Tag); err != nil {
			log.Printf("[update] 写入部署版本记录失败：%v", err)
		}
		_ = s.appendReleaseHistory(releaseHistoryItem{
			Version:   rel.Tag,
			ImagePath: imagePath,
			ImageName: asset.Name,
			SHA256:    sum,
		})
		s.state.Pending = nil
	}
	s.saveStateLocked()
}

// rebuild 按部署形态触发安装（releases 模式重载了源码模式的语义）。
func (s *UpdateService) rebuildRelease() error {
	switch s.Mode() {
	case RebuildModeWaitingAgent:
		// 清单已写入；这里只确认更新目录可用（代理轮询的位置）
		if !dirExists(s.cfg.UpdateDir) {
			return fmt.Errorf("更新目录不可写：%s", s.cfg.UpdateDir)
		}
		return nil
	case RebuildModeDocker:
		return s.rebuildReleaseDocker()
	default:
		return fmt.Errorf("镜像包更新需要 docker compose 环境（UPDATE_WAITING_AGENT=false 时后端本机安装）")
	}
}

// rebuildReleaseDocker 在后端有 docker 权限时安装镜像包：docker load + compose up -d。
func (s *UpdateService) rebuildReleaseDocker() error {
	pending := s.pendingSnapshot()
	if pending.ImagePath == "" {
		return fmt.Errorf("缺少镜像包路径")
	}
	if !isFile(pending.ImagePath) {
		return fmt.Errorf("镜像包不存在：%s", pending.ImagePath)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("本机没有 docker 命令，请改用宿主更新代理（UPDATE_WAITING_AGENT=true）")
	}
	composeFile := pending.ComposeFile
	if composeFile == "" {
		composeFile = releaseComposeFallback
	}
	composePath := filepath.Join(s.cfg.UpdateSourceDir, composeFile)
	if s.cfg.UpdateSourceDir == "" || !isFile(composePath) {
		return fmt.Errorf("未找到 compose 编排文件 %s（可设置 UPDATE_COMPOSE_FILE）", composeFile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	loadArgs := []string{"load", "-i", pending.ImagePath}
	if out, err := runCommand(ctx, s.cfg.UpdateSourceDir, "docker", loadArgs...); err != nil {
		return fmt.Errorf("docker load 失败：%w：%s", err, tailLines(out, 12))
	}

	upArgs := []string{"compose", "-f", composePath}
	if envFile := filepath.Join(s.cfg.UpdateSourceDir, ".env"); isFile(envFile) {
		upArgs = append(upArgs, "--env-file", envFile)
	}
	upArgs = append(upArgs, "up", "-d")
	if out, err := runCommand(ctx, s.cfg.UpdateSourceDir, "docker", upArgs...); err != nil {
		return fmt.Errorf("docker compose up 失败：%w：%s", err, tailLines(out, 12))
	}
	return nil
}

// finishFailedRelease 记录一次失败的镜像包更新（版本维度，history 用版本号）。
func (s *UpdateService) finishFailedRelease(target string, err error, previous string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Last = stageError(s.state.Last, err)
	s.appendHistoryLocked(UpdateHistoryItem{
		At:       time.Now().Format(time.RFC3339),
		From:     previous,
		To:       target,
		Result:   "failed",
		Detail:   truncate(err.Error(), historyDetailRunes),
		Operator: s.operatorLocked(),
	})
	s.saveStateLocked()
}

// rollbackRelease 回滚到上一个已安装的镜像版本：把「回滚清单」写给宿主代理，
// 由它 docker load 旧镜像包 + compose up -d（后端自己装的环境则直接执行）。
func (s *UpdateService) rollbackRelease(backupID, operator string) error {
	backupID = strings.TrimSpace(backupID)
	if backupID == "" {
		return NewValidationError("缺少要回滚的版本号")
	}
	local, _ := s.localVersion()
	if sameVersion(local, backupID) {
		return NewValidationError("该版本（" + backupID + "）与当前运行版本相同，无需回滚")
	}
	item, ok := s.releaseHistoryItemByVersion(backupID)
	if !ok {
		return NewValidationError("找不到版本 " + backupID + " 的本地镜像包（更新历史仅保留最近 " +
			itoa(recentReleaseHistory) + " 个版本），无法回滚")
	}
	if !isFile(item.ImagePath) {
		return NewValidationError("版本 " + backupID + " 的镜像包已被清理（" + item.ImagePath + "），无法回滚")
	}

	s.mu.Lock()
	s.state.Last = &UpdateStage{
		Running:     false,
		Phase:       PhaseRebuilding,
		Progress:    50,
		Message:     "正在回滚到版本 " + backupID + "…",
		StartedAt:   time.Now().Format(time.RFC3339),
		Target:      backupID,
		RebuildMode: s.Mode(),
		Operator:    operator,
	}
	s.state.Pending = nil
	s.saveStateLocked()
	s.mu.Unlock()

	manifest := pendingManifest{
		Version:       1,
		Action:        "rollback",
		Kind:          pendingKindReleaseImage,
		Commit:        backupID,
		TargetVersion: backupID,
		SourceDir:     s.cfg.UpdateSourceDir,
		HostSource:    os.Getenv("UPDATE_HOST_SOURCE_DIR"),
		ImagePath:     item.ImagePath,
		ImageName:     item.ImageName,
		ImageSHA256:   item.SHA256,
		ComposeFile:   s.releaseComposeFile(),
		Previous:      local,
		CreatedAt:     time.Now().Format(time.RFC3339),
		State:         "pending",
		ResultFile:    filepath.Join(s.cfg.UpdateDir, "update-result.json"),
		Changes:       []changeEntry{},
		RequestedBy:   operator,
		Mode:          s.Mode(),
		Hint:          "宿主代理读取本文件后 docker load 旧版镜像包并重建，完成后把 state 置为 done",
	}
	if err := writeJSONFile(s.manifestPath(), manifest); err != nil {
		return err
	}

	if s.Mode() == RebuildModeWaitingAgent {
		// 等代理执行；成功阶段与版本记录由代理写回后收敛
		s.mu.Lock()
		s.state.Last.Message = "回滚清单已提交，等待宿主更新代理恢复版本 " + backupID
		s.state.Last.WaitingAgent = true
		s.appendHistoryLocked(UpdateHistoryItem{
			At:       time.Now().Format(time.RFC3339),
			From:     local,
			To:       backupID,
			Result:   "rolled_back",
			Detail:   "回滚到镜像版本 " + backupID + "（等待宿主代理安装）",
			Operator: operator,
		})
		s.saveStateLocked()
		s.mu.Unlock()
		return nil
	}

	// 后端本机安装
	if err := s.rebuildReleaseDocker(); err != nil {
		s.mu.Lock()
		s.state.Last = stageError(s.state.Last, fmt.Errorf("回滚安装失败：%w", err))
		s.appendHistoryLocked(UpdateHistoryItem{
			At:       time.Now().Format(time.RFC3339),
			From:     local,
			To:       backupID,
			Result:   "failed",
			Detail:   truncate("回滚安装失败："+err.Error(), historyDetailRunes),
			Operator: operator,
		})
		s.saveStateLocked()
		s.mu.Unlock()
		return err
	}
	if err := s.writeDeployedVersion(backupID); err != nil {
		log.Printf("[update] 写入部署版本记录失败：%v", err)
	}
	s.mu.Lock()
	s.state.Last = &UpdateStage{
		Running:     false,
		Phase:       PhaseRolledBack,
		Progress:    100,
		Message:     "已回滚到版本 " + backupID,
		FinishedAt:  time.Now().Format(time.RFC3339),
		Target:      backupID,
		RebuildMode: s.Mode(),
		Success:     true,
		Operator:    operator,
	}
	s.appendHistoryLocked(UpdateHistoryItem{
		At:       time.Now().Format(time.RFC3339),
		From:     local,
		To:       backupID,
		Result:   "rolled_back",
		Detail:   "回滚到镜像版本 " + backupID,
		Operator: operator,
	})
	s.saveStateLocked()
	s.mu.Unlock()
	return nil
}

// releaseHistoryItemByVersion 按版本号查安装历史。
func (s *UpdateService) releaseHistoryItemByVersion(version string) (releaseHistoryItem, bool) {
	for _, item := range s.loadReleaseHistory() {
		if sameVersion(item.Version, version) {
			return item, true
		}
	}
	return releaseHistoryItem{}, false
}
