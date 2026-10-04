package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pendingManifest 是给宿主更新代理看的「待办清单」。
//
// 容器部署下后端没有 docker 权限，也没有宿主源码目录的写权限之外的任何能力；
// 真正把新代码 / 新镜像跑起来的动作必须由宿主侧完成。代理脚本轮询这个文件：
//
//	deploy/update-agent.sh          （Linux / macOS）
//	deploy/update-agent.ps1         （Windows）
//
// 代理做完重建与重启后，把结果写进 result_file 并把 state 置为 done。
//
// kind=source（默认）：源码更新，代理重建并重启；
// kind=release_image：镜像包更新，代理 docker load + compose up -d。
type pendingManifest struct {
	Version       int           `json:"version"`        // 清单格式版本，便于以后扩展
	Action        string        `json:"action"`         // rebuild / rollback
	Kind          string        `json:"kind"`           // source（默认）/ release_image
	Commit        string        `json:"commit"`         // 目标提交（回滚时为 rollback:<备份ID>）
	TargetVersion string        `json:"target_version"` // releases 模式的版本 tag
	BackupID      string        `json:"backup_id"`
	StagedDir     string        `json:"staged_dir"` // 解压后的新源码（后端写入，代理可读）
	SourceDir     string        `json:"source_dir"` // 宿主源码目录（容器内路径，代理按需映射）
	HostSource    string        `json:"host_source_dir"`
	ImagePath     string        `json:"image_path"`       // releases 模式的镜像包路径
	ImageName     string        `json:"image_name"`       // releases 模式的镜像包文件名
	ImageSHA256   string        `json:"image_sha256"`     // 镜像包 SHA-256（代理对账）
	ImageBytes    int64         `json:"image_bytes"`      // 镜像包大小（代理对账）
	ComposeFile   string        `json:"compose_file"`     // 安装用的 compose 文件（releases 模式）
	Previous      string        `json:"previous_version"` // 安装前版本（回滚与记录用）
	CreatedAt     string        `json:"created_at"`
	State         string        `json:"state"` // pending / done
	ResultFile    string        `json:"result_file"`
	Changes       []changeEntry `json:"changes"`
	RequestedBy   string        `json:"requested_by"`
	Mode          string        `json:"mode"`
	Hint          string        `json:"hint"`
}

// manifestPath 是待办清单的文件路径。
func (s *UpdateService) manifestPath() string {
	return filepath.Join(s.cfg.UpdateDir, "pending-update.json")
}

// writePendingManifest 原子写入待办清单，供宿主代理轮询。
func writePendingManifest(s *UpdateService, target, stagedDir, backupID string, changes []changeEntry) error {
	if s == nil || s.cfg == nil || s.cfg.UpdateDir == "" {
		return fmt.Errorf("更新目录未配置")
	}
	action := "rebuild"
	if strings.HasPrefix(target, "rollback:") {
		action = "rollback"
	}
	manifest := pendingManifest{
		Version:     1,
		Action:      action,
		Commit:      target,
		BackupID:    backupID,
		StagedDir:   stagedDir,
		SourceDir:   s.cfg.UpdateSourceDir,
		HostSource:  os.Getenv("UPDATE_HOST_SOURCE_DIR"),
		CreatedAt:   time.Now().Format(time.RFC3339),
		State:       "pending",
		ResultFile:  filepath.Join(s.cfg.UpdateDir, "update-result.json"),
		Changes:     changes,
		RequestedBy: s.stageOperator(),
		Mode:        s.Mode(),
		Hint:        "宿主代理读取本文件后执行重建与重启，完成后把 state 置为 done",
	}
	if manifest.Changes == nil {
		manifest.Changes = []changeEntry{} // 契约：始终是数组，前端不必处理 null
	}
	return writeJSONFile(s.manifestPath(), manifest)
}

// writePendingImageManifest 原子写入「镜像包待安装」清单（releases 模式）。
func writePendingImageManifest(s *UpdateService, rel ReleaseBrief, asset ReleaseAssetBrief, imagePath, sum, previous string) error {
	if s == nil || s.cfg == nil || s.cfg.UpdateDir == "" {
		return fmt.Errorf("更新目录未配置")
	}
	manifest := pendingManifest{
		Version:       1,
		Action:        "rebuild",
		Kind:          pendingKindReleaseImage,
		Commit:        rel.Tag,
		TargetVersion: rel.Tag,
		StagedDir:     "",
		SourceDir:     s.cfg.UpdateSourceDir,
		HostSource:    os.Getenv("UPDATE_HOST_SOURCE_DIR"),
		ImagePath:     imagePath,
		ImageName:     asset.Name,
		ImageSHA256:   sum,
		ImageBytes:    asset.Size,
		ComposeFile:   s.releaseComposeFile(),
		Previous:      previous,
		CreatedAt:     time.Now().Format(time.RFC3339),
		State:         "pending",
		ResultFile:    filepath.Join(s.cfg.UpdateDir, "update-result.json"),
		Changes:       []changeEntry{}, // 契约：始终是数组
		RequestedBy:   s.stageOperator(),
		Mode:          s.Mode(),
		Hint:          "宿主代理读取本文件后执行 docker load 与 compose 重建，完成后把 state 置为 done",
	}
	return writeJSONFile(s.manifestPath(), manifest)
}

// RebuildResult 是宿主代理写回的执行结果（后端只读，用于 UI 提示）。
type RebuildResult struct {
	State      string `json:"state"`
	Commit     string `json:"commit"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Agent      string `json:"agent"`
	FinishedAt string `json:"finished_at"`
	DurationMS int64  `json:"duration_ms"`
}

// AgentResult 读取宿主代理写回的执行结果（不存在时返回 nil）。
func (s *UpdateService) AgentResult() *RebuildResult {
	raw, err := os.ReadFile(filepath.Join(s.cfg.UpdateDir, "update-result.json"))
	if err != nil {
		return nil
	}
	var res RebuildResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil
	}
	return &res
}

// rebuild 按部署形态触发重建。
func (s *UpdateService) rebuild(target string) error {
	if s.isReleaseSource() {
		return s.rebuildRelease()
	}
	switch s.Mode() {
	case RebuildModeWaitingAgent:
		// 清单已在替换源码后写入；这里只确认宿主代理是否存在可用的落点
		if !dirExists(s.cfg.UpdateDir) {
			return fmt.Errorf("更新目录不可写：%s", s.cfg.UpdateDir)
		}
		return nil
	case RebuildModeDocker:
		return s.rebuildDocker(target)
	default:
		return s.rebuildInPlace(target)
	}
}
