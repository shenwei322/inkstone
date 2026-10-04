package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AppVersion is the current backend release version.
const AppVersion = "Beta1.27"

var appStartTime = time.Now()

// BuildCommit 让二进制自带构建来源，后台「关于系统」与更新页据此显示运行版本：
//
//	go build -ldflags "-X github.com/shenwei/inkstone/backend/internal/service.BuildCommit=$(git rev-parse HEAD)"
var BuildCommit = ""

// 提交信息来源：不同渠道的可信度不同，前端需要区分「确定」与「推断」。
const (
	CommitSourceLDFlags  = "ldflags"  // 编译期注入，最准
	CommitSourceDeployed = "deployed" // 宿主部署脚本写回的部署记录
	CommitSourceEnv      = "env"      // 容器编排注入的环境变量
)

type SystemInfo struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	GoVersion  string `json:"go_version"`
	Uptime     string `json:"uptime"`
	Author     string `json:"author"`
	Commit     string `json:"commit"`        // 运行中代码的提交（短哈希，未知为空）
	CommitAt   string `json:"commit_at"`     // 该提交的部署时间（RFC3339，未知为空）
	CommitFrom string `json:"commit_source"` // ldflags / deployed / env / ""
}

func BuildSystemInfo(siteName, repoRoot string) SystemInfo {
	uptime := time.Since(appStartTime)
	days := int(uptime.Hours() / 24)
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60
	commit, at, from := ResolveRunningCommit(repoRoot)
	return SystemInfo{
		Name:       siteName,
		Version:    AppVersion,
		GoVersion:  runtime.Version(),
		Uptime:     fmt.Sprintf("%d 天 %d 小时 %d 分钟", days, hours, minutes),
		Author:     "shenwei",
		Commit:     ShortCommit(commit),
		CommitAt:   at,
		CommitFrom: from,
	}
}

// ---------------------------------------------------------------------------
// 提交信息来源
//
// 更新系统要回答两个不同的问题：
//  1. 「现在跑的代码是哪一版」——判断有没有可更新内容；
//  2. 「上一次更新部署到哪一版」——宿主重建完成后写回，用于确认更新已生效。
// 前者按 ldflags > 部署记录 > 环境变量 取用，后者只认部署记录。
// ---------------------------------------------------------------------------

// deployedCommitFile 是部署产物：宿主代理 / 部署脚本在重建成功后写入。
// 字段与历史 docker 部署脚本保持一致（commit / updated_at），保证老站点平滑接入。
type deployedCommitFile struct {
	Commit    string `json:"commit"`
	UpdatedAt string `json:"updated_at"`
}

// DeployedCommit 描述一次已完成的部署。
type DeployedCommit struct {
	Commit    string `json:"commit"`
	UpdatedAt string `json:"updated_at"`
	Path      string `json:"-"` // 实际读到的文件路径，写回时沿用
}

// deployedRecorder 把「部署记录文件在哪」与「源码目录在哪」绑在一个对象上，
// 避免用包级全局变量（同进程多个实例会互相覆盖）。
type deployedRecorder struct {
	file      string // 部署记录文件路径（UPDATE_DEPLOYED_FILE，可空）
	repoRoot  string // 源码目录（仓库根）
	updateDir string // 更新工作目录：宿主更新代理默认把部署记录写在这里
}

// deployedCommitCandidates 覆盖四种运行布局（前面的优先）：
//   - 更新目录：宿主更新代理 / 本机重建脚本写回的 deployed-commit.json
//   - 容器内：WORKDIR=/app，数据卷挂在 /app/data → data/deployed-commit.json
//   - 仓库根直接跑 server：./data/deployed-commit.json
//   - 老式单目录部署：backend/data/deployed-commit.json
func (r deployedRecorder) candidates() []string {
	var candidates []string
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			candidates = append(candidates, path)
		}
	}
	add(r.file)
	if strings.TrimSpace(r.updateDir) != "" {
		add(filepath.Join(r.updateDir, "deployed-commit.json"))
	}
	wd, _ := os.Getwd()
	for _, base := range []string{wd, r.repoRoot} {
		if base == "" {
			continue
		}
		add(filepath.Join(base, "data", "deployed-commit.json"))
		add(filepath.Join(base, "backend", "data", "deployed-commit.json"))
	}
	return candidates
}

// resolve 按候选路径查找部署记录，全部缺失时返回空结构（不报错）。
func (r deployedRecorder) resolve() DeployedCommit {
	for _, path := range r.candidates() {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec deployedCommitFile
		if err := json.Unmarshal(raw, &rec); err != nil {
			continue
		}
		if strings.TrimSpace(rec.Commit) == "" {
			continue
		}
		return DeployedCommit{
			Commit:    strings.TrimSpace(rec.Commit),
			UpdatedAt: strings.TrimSpace(rec.UpdatedAt),
			Path:      path,
		}
	}
	return DeployedCommit{}
}

// commitPath 返回部署记录的写入路径：优先沿用已读到的文件，
// 否则用显式配置，最后回退到「当前工作目录/data/deployed-commit.json」。
func (r deployedRecorder) commitPath() string {
	if rec := r.resolve(); rec.Path != "" {
		return rec.Path
	}
	if r.file != "" {
		return r.file
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "data", "deployed-commit.json")
	}
	return ""
}

// runningCommit 返回当前运行代码的提交、时间与来源。
func (r deployedRecorder) runningCommit() (commit, at, source string) {
	if c := strings.TrimSpace(BuildCommit); c != "" {
		rec := r.resolve()
		return c, rec.UpdatedAt, CommitSourceLDFlags
	}
	rec := r.resolve()
	if rec.Commit != "" {
		return rec.Commit, rec.UpdatedAt, CommitSourceDeployed
	}
	if c := strings.TrimSpace(os.Getenv("INKSTONE_COMMIT")); c != "" {
		return c, "", CommitSourceEnv
	}
	return "", "", ""
}

// WriteDeployedCommit 原子写入部署记录（临时文件 + rename，避免半截 JSON）。
func WriteDeployedCommit(path, commit string) error {
	if strings.TrimSpace(path) == "" {
		return NewValidationError("部署记录路径未知")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(deployedCommitFile{
		Commit:    commit,
		UpdatedAt: time.Now().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ResolveRunningCommit 返回当前运行代码的提交、时间与来源（使用 UPDATE_SOURCE_DIR 作为仓库根）。
func ResolveRunningCommit(repoRoot string) (commit, at, source string) {
	return deployedRecorder{repoRoot: repoRoot}.runningCommit()
}

// ShortCommit 截断为 7 位短哈希（Git 默认展示长度），空值原样返回。
func ShortCommit(commit string) string {
	c := strings.TrimSpace(commit)
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
