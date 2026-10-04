package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shenwei/inkstone/backend/pkg/config"
)

// maxChangelogEntries 一次对比最多展示多少条提交（避免几万条提交把页面压垮）。
const maxChangelogEntries = 120

// changelogNoteFallback 在没有基准版本时说明「更新日志只是最近提交」，
// 避免管理员把「最近 N 条提交」误读成「本次会应用这 N 条」。
const changelogNoteFallback = "本机没有版本记录，以下仅为上游最近提交，不是本次更新的精确差异。"

// 更新流程阶段（前端按阶段渲染进度条与文案）。
// 注意：检查更新是同步接口（前端用 mutation 的 pending 状态即可），
// 不需要 "checking" 阶段，因此这里只列出真正会落到状态文件里的阶段。
const (
	PhaseIdle       = "idle"        // 空闲
	PhaseStaging    = "staging"     // 下载 + 校验 + 解压
	PhaseSwapping   = "swapping"    // 备份 + 替换源码
	PhaseRebuilding = "rebuilding"  // 触发重建
	PhaseSuccess    = "success"     // 已完成（等待宿主重启或已重启）
	PhaseFailed     = "failed"      // 失败
	PhaseRolledBack = "rolled_back" // 已回滚
)

// 重建方式：决定「替换源码之后」谁来把新代码跑起来。
const (
	RebuildModeWaitingAgent = "waiting_agent" // 写入待更新清单，由宿主更新代理重建（容器部署默认）
	RebuildModeInPlace      = "in_place"      // 本机直接编译并重启（二进制 / systemd 部署）
	RebuildModeDocker       = "docker"        // 本机 docker compose 重建
	RebuildModeUnavailable  = "unavailable"   // 更新未开启或源码目录未知
)

// 默认镜像：GitHub 官方源码包地址；国内可置空 UPDATE_MIRROR 后走 UPDATE_GITHUB_API。
const defaultFallbackArchive = "https://codeload.github.com/{owner}/{name}/tar.gz/{commit}"

// pendingState.Kind 取值：源码更新（commits 模式）与镜像包更新（releases 模式）。
const (
	pendingKindSource       = "source"
	pendingKindReleaseImage = "release_image"
)

// UpdateStatus 是「系统更新」页的唯一数据源（GET /admin/system/update/status）。
type UpdateStatus struct {
	Enabled   bool                `json:"enabled"`
	RepoURL   string              `json:"repo_url"`
	RepoName  string              `json:"repo_name"`
	Branch    string              `json:"branch"`
	Source    string              `json:"source"`     // commits（提交+源码包）/ releases（GitHub Releases+镜像包）
	Mode      string              `json:"mode"`       // waiting_agent / in_place / docker / unavailable
	SourceDir string              `json:"source_dir"` // 待替换的源码目录（空 = 未探测到）
	UpdateDir string              `json:"update_dir"` // 更新工作目录（镜像包 / 备份 / 状态）
	Message   string              `json:"message"`    // 面向管理员的说明或降级原因
	Version   VersionInfo         `json:"version"`
	Stage     *UpdateStage        `json:"stage"`
	History   []UpdateHistoryItem `json:"history"`
}

// VersionInfo 汇总三处版本：正在运行、已部署、上游最新、已暂存待生效。
//
// 两套更新源共用同一份结构，靠 source 字段区分：
//   - commits：哈希维度，Current/Latest/Changelog 都是提交；
//   - releases：版本号维度，LatestVersion/Release/CurrentVersion 承载内容，
//     Latest.Hash 仍填 tag（前端兼容展示），Changelog 为空数组。
type VersionInfo struct {
	Current            CommitBrief   `json:"current"`              // 运行中代码
	CurrentFrom        string        `json:"current_from"`         // ldflags / deployed / env / ""
	CurrentVersion     string        `json:"current_version"`      // 本地版本号（releases 模式）
	CurrentVersionFrom string        `json:"current_version_from"` // version_file / app_version
	Deployed           DeployedBrief `json:"deployed"`             // 上一次部署记录
	Latest             CommitBrief   `json:"latest"`               // 上游最新提交
	LatestVersion      string        `json:"latest_version"`       // 上游最新版本号（releases 模式）
	Release            *ReleaseBrief `json:"release"`              // 上游最新 Release 详情（releases 模式）
	// Kind 记录这次检查快照的更新源（source / release_image）：进程重启后
	// 区分「待生效的是源码更新还是镜像包更新」。
	Kind               string        `json:"kind"`
	UpdateAvail        bool          `json:"update_available"`
	Behind             int           `json:"behind"`           // 落后提交数（未知 = -1）
	CompareNote        string        `json:"compare_note"`     // 差异无法精确对比时的说明
	Changelog          []CommitBrief `json:"changelog"`        // 更新日志（新 → 旧）
	ChangelogOffset    int           `json:"changelog_offset"` // 日志被截断时，首条在差异中的序号
	ChangelogTruncated bool          `json:"changelog_truncated"`
	ChangelogFrom      string        `json:"changelog_from"` // 日志来源接口
	LastChecked        string        `json:"last_checked"`   // 上次检查时间（RFC3339）
	Pending            *PendingBrief `json:"pending"`        // 已暂存、等待重建生效的更新
}

// PendingPreview 是「如果现在更新，会动哪些文件」的预览统计。
type PendingPreview struct {
	Writes  int   `json:"writes"`
	Deletes int   `json:"deletes"`
	Bytes   int64 `json:"bytes"`
}

// CommitBrief 是一次提交的可展示信息（新 → 旧排列时作为数组元素）。
type CommitBrief struct {
	Hash    string `json:"hash"`    // 完整哈希
	Short   string `json:"short"`   // 7 位短哈希
	Message string `json:"message"` // 首行提交说明
	Author  string `json:"author"`
	Date    string `json:"date"` // RFC3339
	URL     string `json:"url"`
}

type DeployedBrief struct {
	Hash      string `json:"hash"`
	Short     string `json:"short"`
	UpdatedAt string `json:"updated_at"`
	Path      string `json:"path"`
}

// PendingBrief 描述「已暂存、等待重建生效」的更新。
//
// commits 模式：已下载并替换完的源码（Hash/StagedDir/Preview 有效）；
// releases 模式：已下载的镜像包（Kind=release_image，Version/ImagePath 有效）。
type PendingBrief struct {
	Hash        string          `json:"hash"` // 目标提交哈希或版本 tag
	Short       string          `json:"short"`
	Kind        string          `json:"kind"`    // source（默认）/ release_image
	Version     string          `json:"version"` // releases 模式的目标版本号
	Downloaded  bool            `json:"downloaded"`
	ApplyReady  bool            `json:"apply_ready"`
	Files       int             `json:"files"` // 与当前源码不同的文件数
	Bytes       int64           `json:"bytes"` // 源码包 / 镜像包大小
	SHA256      string          `json:"sha256"`
	Source      string          `json:"source"` // 实际下载地址（已去查询串）
	StagedAt    string          `json:"staged_at"`
	StagedDir   string          `json:"staged_dir"`   // commits 模式的暂存目录
	ImagePath   string          `json:"image_path"`   // releases 模式的镜像包路径
	ImageName   string          `json:"image_name"`   // releases 模式的镜像包文件名
	ComposeFile string          `json:"compose_file"` // releases 模式使用的 compose 文件
	Preview     *PendingPreview `json:"preview"`
}

// UpdateStage 是异步更新任务的实时进度。
type UpdateStage struct {
	Running    bool   `json:"running"`
	Phase      string `json:"phase"`
	Progress   int    `json:"progress"` // 0-100
	Message    string `json:"message"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Target     string `json:"target"`
	Error      string `json:"error"`
	Success    bool   `json:"success"`

	FilesChanged int    `json:"files_changed"`
	BackupID     string `json:"backup_id"`
	RolledBack   bool   `json:"rolled_back"`
	RebuildMode  string `json:"rebuild_mode"`
	RebuildMS    int64  `json:"rebuild_ms"`
	WaitingAgent bool   `json:"waiting_agent"`
	Operator     string `json:"operator"` // 发起人（不展示给普通用户，仅审计对照）
}

// UpdateHistoryItem 是历史更新记录（来自状态文件，重启后仍可查）。
type UpdateHistoryItem struct {
	At       string `json:"at"`
	From     string `json:"from"`
	To       string `json:"to"`
	Result   string `json:"result"`
	Detail   string `json:"detail"`
	BackupID string `json:"backup_id"`
	Operator string `json:"operator"`
}

// updateStateFile 持久化在 UPDATE_DIR 下，用于跨重启保留「待生效更新」与历史。
type updateStateFile struct {
	Pending   *pendingState       `json:"pending"`
	Last      *UpdateStage        `json:"last"`
	History   []UpdateHistoryItem `json:"history"`
	CheckedAt string              `json:"checked_at"`
	// Check 是上一次「检查更新」的快照：落盘后刷新页面/重启进程仍能看到
	// 上游最新版本与更新日志（否则每次打开页面都要重新联网检查一遍）。
	Check *VersionInfo `json:"check_snapshot"`
}

type pendingState struct {
	Hash      string `json:"hash"`    // 目标提交哈希或版本 tag
	Kind      string `json:"kind"`    // source（源码更新）/ release_image（镜像包更新）
	Version   string `json:"version"` // releases 模式的目标版本号
	StagedDir string `json:"staged_dir"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
	Source    string `json:"source"`
	StagedAt  string `json:"staged_at"`
	Mode      string `json:"mode"`
	// releases 模式专用
	ImagePath   string `json:"image_path"`
	ImageName   string `json:"image_name"`
	ComposeFile string `json:"compose_file"`
	Previous    string `json:"previous_version"` // 安装前的本地版本（回滚用）
}

// UpdateService 实现在线更新：检查 → 下载镜像包 → 校验 → 解压 → 备份 → 替换源码 → 触发重建。
//
// 安全边界（设计取舍）：
//   - 只从配置的上游 / 镜像地址取包，不接受请求体指定任意 URL；
//   - 解压拒绝绝对路径、.. 穿越、符号链接，并限制文件数与解压体积（防 zip 炸弹）；
//   - 只覆盖仓库内文件，绝不触碰 data/、uploads/ 与 .env（站点数据与配置永远保留）；
//   - 覆盖前按文件备份，失败自动回滚。
type UpdateService struct {
	cfg    *config.Config
	logs   *LogService
	client *httpClient

	// deployedFile 是 UPDATE_DEPLOYED_FILE 的显式路径（可空，按候选路径自动探测）
	deployedFile string

	mu    sync.Mutex
	state updateStateFile

	// 待更新预览缓存：Status 会被前端高频轮询，而预览要对几千个文件做 SHA-256，
	// 必须缓存；key 用暂存目录（同一提交的目录名固定）。
	previewKey   string
	previewValue *PendingPreview

	// 进度落盘节流：内存里的 stage 每次都更新，但状态文件最多每秒写一次
	lastPersist time.Time
}

func NewUpdateService(cfg *config.Config, logs *LogService) *UpdateService {
	s := &UpdateService{
		cfg:          cfg,
		logs:         logs,
		client:       newHTTPClient(20*time.Second, cfg.UpdateToken),
		deployedFile: strings.TrimSpace(cfg.UpdateDeployedFile),
	}
	s.reloadState()
	return s
}

// recorder 返回部署记录读写器（路径按配置与源码目录解析）。
func (s *UpdateService) recorder() deployedRecorder {
	return deployedRecorder{
		file:      s.deployedFile,
		repoRoot:  s.cfg.UpdateSourceDir,
		updateDir: s.cfg.UpdateDir,
	}
}

// Enabled 报告更新功能是否可用（总开关 + 已知源码目录）。
func (s *UpdateService) Enabled() bool {
	return s != nil && s.cfg != nil && s.cfg.UpdateEnabled
}

// Mode 返回重建方式。
//
// releases 模式下不替换源码、也不需要源码目录：能否重建只看 docker
// （镜像包由 docker load 生效）。等待宿主代理仍是默认（容器内没有 docker 权限）。
func (s *UpdateService) Mode() string {
	if !s.Enabled() {
		return RebuildModeUnavailable
	}
	if s.cfg.UpdateWaitingAgent {
		return RebuildModeWaitingAgent
	}
	// releases 模式只认 compose 编排文件（离线镜像部署的根目录同样有）；
	// commits 模式沿用旧的 prod 编排探测，保持历史行为不变。
	if s.isReleaseSource() {
		if s.releaseComposeFile() != "" {
			return RebuildModeDocker
		}
		return RebuildModeInPlace
	}
	if _, err := os.Stat(filepath.Join(s.cfg.UpdateSourceDir, "docker-compose.prod.yml")); err == nil {
		return RebuildModeDocker
	}
	return RebuildModeInPlace
}

// releaseComposeFile 返回镜像更新使用的 compose 文件（自动探测，可被配置覆盖）：
// 优先 UPDATE_COMPOSE_FILE，其次离线编排（镜像部署的标准形态），最后 prod 编排。
func (s *UpdateService) releaseComposeFile() string {
	if name := strings.TrimSpace(s.cfg.UpdateComposeFile); name != "" {
		return name
	}
	base := s.cfg.UpdateSourceDir
	if base == "" {
		return ""
	}
	for _, name := range []string{"docker-compose.offline.yml", "docker-compose.prod.yml", "docker-compose.yml"} {
		if isFile(filepath.Join(base, name)) {
			return name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// 状态文件
// ---------------------------------------------------------------------------

func (s *UpdateService) stateFilePath() string {
	return filepath.Join(s.cfg.UpdateDir, "update-state.json")
}

func (s *UpdateService) reloadState() {
	raw, err := os.ReadFile(s.stateFilePath())
	if err != nil {
		return
	}
	var st updateStateFile
	if err := json.Unmarshal(raw, &st); err != nil {
		// 状态文件损坏不影响更新能力：丢掉历史继续跑
		return
	}
	if st.Last != nil {
		// 进程重启后不可能还有任务在跑，避免 UI 卡在「进行中」
		st.Last.Running = false
		if st.Last.Phase == PhaseStaging || st.Last.Phase == PhaseSwapping || st.Last.Phase == PhaseRebuilding {
			st.Last.Phase = PhaseFailed
			if st.Last.Error == "" {
				st.Last.Error = "后端进程在更新过程中重启，任务已中断"
			}
		}
	}
	s.state = st
}

func (s *UpdateService) saveStateLocked() {
	if s.cfg.UpdateDir == "" {
		return
	}
	// 落盘失败必须留痕：状态文件承载「待生效更新」与「上次检查结果」，
	// 静默失败会让更新页在重启后失忆，而管理员看不到任何线索。
	if err := os.MkdirAll(s.cfg.UpdateDir, 0o755); err != nil {
		log.Printf("[update] 创建更新目录失败 %s：%v", s.cfg.UpdateDir, err)
		return
	}
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		log.Printf("[update] 序列化更新状态失败：%v", err)
		return
	}
	tmp := s.stateFilePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		log.Printf("[update] 写入更新状态失败 %s：%v", tmp, err)
		return
	}
	if err := os.Rename(tmp, s.stateFilePath()); err != nil {
		log.Printf("[update] 提交更新状态失败 %s：%v", s.stateFilePath(), err)
	}
}

// saveStateThrottledLocked 给高频进度回调用的落盘：内存状态照常更新，
// 磁盘写入限制到最多每秒一次（下载进度回调每 256KB 就会调一次）。
// 阶段切换等关键节点仍应直接用 saveStateLocked，确保状态及时落盘。
func (s *UpdateService) saveStateThrottledLocked() {
	if time.Since(s.lastPersist) < stagePersistInterval {
		return
	}
	s.saveStateLocked()
	s.lastPersist = time.Now()
}

// snapshot 返回状态文件副本（避免调用方在锁外读到半更新结构）。
func (s *UpdateService) snapshot() updateStateFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	if st.Pending != nil {
		p := *st.Pending
		st.Pending = &p
	}
	if st.Last != nil {
		l := *st.Last
		st.Last = &l
	}
	st.History = append([]UpdateHistoryItem(nil), st.History...)
	return st
}

// Status 组装「系统更新」页所需的全部信息（不发任何网络请求）。
func (s *UpdateService) Status() UpdateStatus {
	st := s.snapshot()
	out := UpdateStatus{
		Enabled:   s.Enabled(),
		RepoURL:   s.cfg.UpdateRepoURL,
		RepoName:  repoFullName(s.cfg.UpdateRepoURL),
		Branch:    s.cfg.UpdateBranch,
		Source:    s.sourceName(),
		Mode:      s.Mode(),
		SourceDir: s.cfg.UpdateSourceDir,
		UpdateDir: s.cfg.UpdateDir,
		History:   st.History,
	}
	if out.History == nil {
		out.History = []UpdateHistoryItem{}
	}
	// 终态（成功/失败/已回滚）也要下发：前端靠它渲染结果面板与错误文案，
	// 只在 running 时下发会导致更新结束后页面停在旧快照。
	out.Stage = st.Last

	info := s.buildVersionInfo()
	out.Version = info
	out.Message = s.modeHint()
	return out
}

func (s *UpdateService) buildVersionInfo() VersionInfo {
	st := s.snapshot()
	recorder := s.recorder()
	running, runningAt, runningFrom := recorder.runningCommit()
	deployed := recorder.resolve()
	localVersion, localVersionFrom := s.localVersion()

	// 宿主代理重建成功后会写回部署记录：此时旧代码已经不在运行，pending 收敛掉，
	// 否则页面会永远停在「等待宿主代理重启」。
	// releases 模式的部署记录是版本号文件（镜像部署没有 commit），收敛逻辑同理。
	if st.Pending != nil {
		if st.Pending.Kind == pendingKindReleaseImage {
			if localVersion != "" && sameVersion(localVersion, st.Pending.Version) {
				s.clearPendingLocked()
				st.Pending = nil
			}
		} else if deployed.Commit != "" && sameCommit(deployed.Commit, st.Pending.Hash) {
			s.clearPendingLocked()
			st.Pending = nil
		}
	}

	info := VersionInfo{
		Current: CommitBrief{
			Hash:  running,
			Short: ShortCommit(running),
			Date:  runningAt,
			URL:   s.commitURL(running),
		},
		CurrentFrom:        runningFrom,
		CurrentVersion:     localVersion,
		CurrentVersionFrom: localVersionFrom,
		Deployed: DeployedBrief{
			Hash:      deployed.Commit,
			Short:     ShortCommit(deployed.Commit),
			UpdatedAt: deployed.UpdatedAt,
			Path:      deployed.Path,
		},
		LastChecked: st.CheckedAt,
		Behind:      -1,
	}
	// 用上一次检查的快照填充「上游最新版本 / 更新日志」：这样刷新页面或重启进程后
	// 不必重新联网检查就能看到结论；Current/Deployed 始终以磁盘现状为准。
	//
	// 快照必须与当前更新源同代：commits 的快照没有版本号、releases 的快照没有提交，
	// 换过 UPDATE_SOURCE 之后旧快照无法解释（页面会显示「有更新」却给不出目标版本），
	// 这种情况整份忽略，等下一次「检查更新」覆写。
	if st.Check != nil && st.Check.Kind == s.checkSnapshotKind() {
		info.Latest = st.Check.Latest
		info.UpdateAvail = st.Check.UpdateAvail
		info.Behind = st.Check.Behind
		info.CompareNote = st.Check.CompareNote
		info.ChangelogOffset = st.Check.ChangelogOffset
		info.ChangelogTruncated = st.Check.ChangelogTruncated
		info.ChangelogFrom = st.Check.ChangelogFrom
		if st.Check.Changelog != nil {
			info.Changelog = st.Check.Changelog
		}
		info.LatestVersion = st.Check.LatestVersion
		info.Release = st.Check.Release
		// 快照里的「当前版本」若已过期（例如刚完成更新），以实时读到的为准
		if st.Check.Kind == pendingKindReleaseImage {
			info.UpdateAvail = st.Check.UpdateAvail && !sameVersion(localVersion, info.LatestVersion)
		} else if !sameCommit(st.Check.Current.Hash, running) {
			info.UpdateAvail = updatedSinceCheck(info, running)
		}
	}
	if st.Pending != nil {
		kind := st.Pending.Kind
		if kind == "" {
			kind = pendingKindSource
		}
		pending := &PendingBrief{
			Hash:        st.Pending.Hash,
			Short:       ShortCommit(st.Pending.Hash),
			Kind:        kind,
			Version:     st.Pending.Version,
			Files:       st.Pending.Files,
			Bytes:       st.Pending.Bytes,
			SHA256:      st.Pending.SHA256,
			Source:      redactURL(st.Pending.Source),
			StagedAt:    st.Pending.StagedAt,
			StagedDir:   st.Pending.StagedDir,
			ImagePath:   st.Pending.ImagePath,
			ImageName:   st.Pending.ImageName,
			ComposeFile: st.Pending.ComposeFile,
		}
		if kind == pendingKindReleaseImage {
			// 镜像更新没有「替换源码」这一步：镜像包下载完成即可安装
			pending.Downloaded = isFile(st.Pending.ImagePath)
			pending.ApplyReady = pending.Downloaded
		} else {
			pending.Downloaded = dirExists(st.Pending.StagedDir)
			pending.ApplyReady = dirExists(filepath.Join(st.Pending.StagedDir, "backend"))
			pending.Preview = s.cachedPreview(st.Pending.StagedDir)
		}
		info.Pending = pending
	}
	return info
}

// clearPendingLocked 清理「待生效更新」（调用方需已持有 s.mu）。
func (s *UpdateService) clearPendingLocked() {
	s.state.Pending = nil
	s.previewKey, s.previewValue = "", nil
	s.saveStateLocked()
}

// sourceName 返回当前更新源类型（commits / releases）。
func (s *UpdateService) sourceName() string {
	if s.cfg.UpdateSource == config.UpdateSourceReleases {
		return config.UpdateSourceReleases
	}
	return config.UpdateSourceCommits
}

// isReleaseSource 是否走 GitHub Releases + 镜像包更新。
func (s *UpdateService) isReleaseSource() bool {
	return s.sourceName() == config.UpdateSourceReleases
}

// checkSnapshotKind 返回当前更新源对应的检查快照类型：
// commits 模式是 source（提交维度），releases 模式是 release_image（版本维度）。
func (s *UpdateService) checkSnapshotKind() string {
	if s.isReleaseSource() {
		return pendingKindReleaseImage
	}
	return pendingKindSource
}

// updatedSinceCheck 判断「检查之后本机版本变了」时是否还有可更新内容：
// 本机版本已经等于快照里的上游最新版本 → 已是最新；否则仍视为有更新待处理。
func updatedSinceCheck(info VersionInfo, running string) bool {
	if info.Latest.Hash == "" {
		return false
	}
	return !sameCommit(running, info.Latest.Hash)
}

// cachedPreview 返回（并缓存）暂存内容的差异预览；同名暂存目录只算一次。
func (s *UpdateService) cachedPreview(stagedDir string) *PendingPreview {
	s.mu.Lock()
	if s.previewKey == stagedDir {
		cached := s.previewValue
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	preview := computePendingPreview(stagedDir, s.cfg.UpdateSourceDir)

	s.mu.Lock()
	s.previewKey, s.previewValue = stagedDir, preview
	s.mu.Unlock()
	return preview
}

// computePendingPreview 统计暂存内容与当前源码的差异（下载完成后即可预览更新规模）。
func computePendingPreview(stagedDir, sourceDir string) *PendingPreview {
	if !dirExists(stagedDir) || !dirExists(filepath.Join(stagedDir, "backend")) {
		return nil
	}
	changes, err := stagedChanges(stagedDir, sourceDir)
	if err != nil {
		return nil
	}
	writes, deletes, bytes := summarizeChanges(changes)
	return &PendingPreview{Writes: writes, Deletes: deletes, Bytes: bytes}
}

// modeHint 给出「更新后由谁重启」的说明，容器部署必须讲清楚这一步。
func (s *UpdateService) modeHint() string {
	if !s.Enabled() {
		return "在线更新已关闭（UPDATE_ENABLED=false）。"
	}
	if s.isReleaseSource() {
		switch s.Mode() {
		case RebuildModeWaitingAgent:
			return "镜像包更新：下载 GitHub Release 镜像包后，由宿主更新代理（deploy/update-agent.sh / .ps1）执行 docker load 与重启。"
		case RebuildModeDocker:
			return "镜像包更新：下载 GitHub Release 镜像包后，由后端执行 docker load 并重建容器。"
		default:
			return "镜像包更新已就绪，但未找到 docker compose 编排文件：请设置 UPDATE_COMPOSE_FILE，或保持 UPDATE_WAITING_AGENT=true 由宿主代理安装。"
		}
	}
	if s.cfg.UpdateSourceDir == "" {
		return "未能探测到源码目录，请设置 UPDATE_SOURCE_DIR 指向仓库根目录。"
	}
	switch s.Mode() {
	case RebuildModeWaitingAgent:
		return "容器部署：替换源码后需由宿主更新代理（deploy/update-agent.sh）完成重建与重启。"
	case RebuildModeDocker:
		return "本机 docker compose 部署：更新后由后端调用 docker compose 重建并重启。"
	default:
		return "本机部署：更新后由后端编译并重启进程。"
	}
}

// ---------------------------------------------------------------------------
// 检查更新
// ---------------------------------------------------------------------------

// Check 拉取上游最新版本并与当前版本对比，结果写入状态文件。
//
// commits 模式（默认）：最新提交 + 源码包更新日志；
// releases 模式（UPDATE_SOURCE=releases）：最新 Release tag + 发布说明 + 镜像包资产。
func (s *UpdateService) Check(ctx context.Context) (UpdateStatus, error) {
	if !s.Enabled() {
		return s.Status(), NewValidationError("在线更新未开启（UPDATE_ENABLED=false）")
	}

	if s.isReleaseSource() {
		return s.checkRelease(ctx)
	}

	pending, err := s.remoteLatest(ctx)
	if err != nil {
		return s.Status(), err
	}

	info := s.buildVersionInfo()
	info.Kind = pendingKindSource // 快照同代标记：换更新源后旧快照不再被采信
	info.Latest = CommitBrief{
		Hash:    pending.Hash,
		Short:   ShortCommit(pending.Hash),
		Message: firstLine(pending.Message),
		Author:  pending.Author,
		Date:    pending.Date,
		URL:     firstNonEmpty(pending.URL, s.commitURL(pending.Hash)),
	}

	running := currentComparableCommit(info)
	// 契约：changelog 始终是数组（无更新时为空数组），前端不必处理 null
	info.Changelog = []CommitBrief{}
	if pending.Hash == "" {
		info.CompareNote = "上游未返回提交标识，无法判断是否有更新。"
	} else if running == "" {
		// 本机没有版本记录（老部署 / 未写部署记录）：不能直接说「有更新」，
		// 目标版本的源码与本地逐文件比对一致时，结论应是「已是最新」。
		changed, verifyErr := s.verifyStagedAgainstLocal(ctx, pending.Hash)
		switch {
		case verifyErr != nil:
			// 没有版本记录 + 比对失败：无法二选一，只能如实说明并保留「可更新」，
			// 让管理员自己核对（错误原因写在说明里，不静默吞掉）。
			info.UpdateAvail = true
			info.CompareNote = "本机没有版本记录，按文件比对也无法确认（" + verifyErr.Error() +
				"）；无法判断是否真的落后，请核对源码状态后再更新。"
		case changed == 0:
			info.UpdateAvail = false
			info.CompareNote = "本机没有版本记录，但上游最新提交的源码与本地逐文件比对完全一致，判定已是最新。"
		default:
			info.UpdateAvail = true
			info.CompareNote = fmt.Sprintf(
				"本机没有版本记录，按文件比对发现 %d 个文件与上游最新提交不同，可更新。", changed)
		}
		if info.UpdateAvail {
			// 没有基准版本时只能展示「最近提交」而非精确差异；
			// 注意：这里的说明要与上面 switch 写的判定原因合并，不能覆盖它。
			commits, _, changelogNote := s.changelog(ctx, "", pending.Hash)
			info.Changelog = commits
			info.CompareNote = joinNotes(info.CompareNote, joinNotes(changelogNoteFallback, changelogNote))
			if len(info.Changelog) > maxChangelogEntries {
				info.ChangelogTruncated = true
				info.ChangelogOffset = len(info.Changelog) - maxChangelogEntries
				info.Changelog = info.Changelog[:maxChangelogEntries]
			}
			info.ChangelogFrom = s.changelogSource()
		}
	} else if sameCommit(running, pending.Hash) {
		info.UpdateAvail = false
	} else {
		info.UpdateAvail = true
		info.Changelog, info.Behind, info.CompareNote = s.changelog(ctx, running, pending.Hash)
		if len(info.Changelog) > maxChangelogEntries {
			info.ChangelogTruncated = true
			info.ChangelogOffset = len(info.Changelog) - maxChangelogEntries
			info.Changelog = info.Changelog[:maxChangelogEntries]
		}
		info.ChangelogFrom = s.changelogSource()
	}

	// 保存本次检查结果：刷新页面或重启进程后仍能看到上游版本与更新日志，
	// 不必每次打开页面都重新联网检查一遍。
	info.LastChecked = time.Now().Format(time.RFC3339)
	snapshot := info

	s.mu.Lock()
	s.state.CheckedAt = snapshot.LastChecked
	s.state.Check = &snapshot
	s.saveStateLocked()
	s.mu.Unlock()

	return UpdateStatus{
		Enabled:   true,
		RepoURL:   s.cfg.UpdateRepoURL,
		RepoName:  repoFullName(s.cfg.UpdateRepoURL),
		Branch:    s.cfg.UpdateBranch,
		Source:    s.sourceName(),
		Mode:      s.Mode(),
		SourceDir: s.cfg.UpdateSourceDir,
		UpdateDir: s.cfg.UpdateDir,
		Message:   s.modeHint(),
		Version:   info,
		Stage:     s.snapshot().Last,
		History:   nonNilHistory(s.snapshot().History),
	}, nil
}

// checkRelease 是 releases 模式的「检查更新」：拉取最新 Release，按版本号对比。
//
// 与 commits 模式的区别：基准是版本号（Beta1.27 / v1.28.0）而非提交哈希；
// 发布说明（body）直接作为更新日志展示，镜像包资产在勾选「更新」时才下载。
func (s *UpdateService) checkRelease(ctx context.Context) (UpdateStatus, error) {
	rel, err := s.fetchLatestRelease(ctx)
	if err != nil {
		return s.Status(), err
	}

	info := s.buildVersionInfo()
	info.Kind = pendingKindReleaseImage
	// 契约：changelog 始终是数组；releases 模式的发布说明走 Release.Body
	info.Changelog = []CommitBrief{}
	info.LatestVersion = rel.Tag
	info.Latest = CommitBrief{
		Hash:    rel.Tag,
		Short:   rel.Tag,
		Message: firstLine(firstNonEmpty(rel.Name, rel.Body)),
		Date:    rel.PublishedAt,
		URL:     rel.URL,
	}
	info.Release = &rel
	info.Behind = -1

	local := info.CurrentVersion
	localVer, localOK := parseVersionValue(local)
	relVer, relOK := parseVersionValue(rel.Tag)
	switch {
	case !relOK:
		info.UpdateAvail = true
		info.CompareNote = fmt.Sprintf("无法解析上游 Release 版本号（%s），请人工核对后再更新。", rel.Tag)
	case !localOK:
		info.UpdateAvail = true
		info.CompareNote = fmt.Sprintf("本地版本号（%s）无法解析，请人工核对后再更新。", local)
	default:
		switch cmp := compareVersions(localVer, relVer); {
		case cmp == 0:
			info.UpdateAvail = false
			info.CompareNote = fmt.Sprintf("当前版本 %s 已是最新 Release。", rel.Tag)
		case cmp < 0:
			info.UpdateAvail = true
			info.CompareNote = fmt.Sprintf("发现新版本 %s（当前 %s），发布说明见下方。", rel.Tag, local)
		default:
			info.UpdateAvail = false
			info.CompareNote = fmt.Sprintf("当前版本 %s 高于上游最新 Release %s，已是最新。", local, rel.Tag)
		}
	}

	// 资产里没有镜像包时也要如实告知（否则管理员点了更新才失败）
	if _, err := s.pickImageAsset(rel); err != nil {
		info.CompareNote = joinNotes(info.CompareNote, "注意："+err.Error())
	}

	// 保存本次检查结果：刷新页面或重启进程后仍能看到上游版本与发布说明，
	// 不必每次打开页面都重新联网检查一遍。
	info.LastChecked = time.Now().Format(time.RFC3339)
	snapshot := info

	s.mu.Lock()
	s.state.CheckedAt = snapshot.LastChecked
	s.state.Check = &snapshot
	s.saveStateLocked()
	s.mu.Unlock()

	return UpdateStatus{
		Enabled:   true,
		RepoURL:   s.cfg.UpdateRepoURL,
		RepoName:  repoFullName(s.cfg.UpdateRepoURL),
		Branch:    s.cfg.UpdateBranch,
		Source:    s.sourceName(),
		Mode:      s.Mode(),
		SourceDir: s.cfg.UpdateSourceDir,
		UpdateDir: s.cfg.UpdateDir,
		Message:   s.modeHint(),
		Version:   info,
		Stage:     s.snapshot().Last,
		History:   nonNilHistory(s.snapshot().History),
	}, nil
}

// verifyStagedAgainstLocal 在本机没有版本记录时，下载目标提交的源码树并与本地逐文件比对，
// 返回「与本地不同的文件数」。已暂存同一提交时直接复用，不重复下载。
//
// 这是「没有版本记录」场景下唯一可靠的判断依据：文件级比对不会撒谎。
func (s *UpdateService) verifyStagedAgainstLocal(ctx context.Context, target string) (int, error) {
	stagedDir := ""
	if pending := s.pendingSnapshot(); sameCommit(pending.Hash, target) && dirExists(pending.StagedDir) {
		stagedDir = pending.StagedDir
	} else {
		dir, _, _, _, err := s.downloadAndExtract(ctx, target, func(int, string) {})
		if err != nil {
			return 0, err
		}
		stagedDir = dir
	}
	return countChanges(stagedDir, s.cfg.UpdateSourceDir)
}

// currentComparableCommit 选取「与上游比较」的基准：优先运行中代码，其次部署记录。
// 已经暂存但尚未重启时，运行中的仍是旧代码，因此不能拿本次更新的目标当基准。
func currentComparableCommit(info VersionInfo) string {
	if info.Current.Hash != "" {
		return info.Current.Hash
	}
	return info.Deployed.Hash
}

// remoteLatest 取上游默认分支最新提交。返回空哈希 + nil 表示「接口可用但没有提交」。
func (s *UpdateService) remoteLatest(ctx context.Context) (remoteCommit, error) {
	cfg := s.cfg

	// 1) 专用「最新提交」接口（自建更新服务器 / 极简镜像）
	if cfg.UpdateLatestAPI != "" {
		url := s.expandAPI(cfg.UpdateLatestAPI, "")
		raw, _, err := s.client.getJSON(ctx, url)
		if err != nil {
			return remoteCommit{}, NewValidationError("更新源不可用：" + err.Error())
		}
		if c, ok := parseLatestResponse(raw, cfg.UpdateBranch); ok {
			return s.decorate(c), nil
		}
		// 返回体不像「最新提交」，继续尝试提交列表接口
	}

	// 2) 提交列表接口（GitHub API 或兼容实现）
	commitsURL := cfg.UpdateCommitsAPI
	if commitsURL == "" {
		commitsURL = cfg.UpdateGitHubAPI + "/repos/{owner}/{name}/commits?sha={branch}&per_page={limit}"
	}
	url := s.expandAPI(commitsURL, "")
	url = strings.ReplaceAll(url, "{branch}", escapeQueryValue(cfg.UpdateBranch))
	url = strings.ReplaceAll(url, "{limit}", "20")
	raw, _, err := s.client.getJSON(ctx, url)
	if err != nil {
		return remoteCommit{}, NewValidationError("更新源不可用：" + err.Error() + "（可配置 UPDATE_MIRROR / UPDATE_LATEST_API 走国内镜像）")
	}
	commits, ok := parseCommitsResponse(raw)
	if !ok || len(commits) == 0 {
		// 接口通了但没有 commits？某些代理会这样返回；视为「无法判断」
		return remoteCommit{}, NewValidationError("更新源返回的数据无法解析为提交列表")
	}
	return s.decorate(commits[0]), nil
}

// decorate 补全 / 纠正提交的展示信息。
//
// 上游返回的 html_url 可能指向「历史仓库名」（改名或转移前），那样页面会把
// 「查看提交」链到别人的仓库；只要它和本地配置不是同一个 owner/name 就改用
// 本地配置拼接，保证链接永远指向我们实际跟踪的仓库。
func (s *UpdateService) decorate(c remoteCommit) remoteCommit {
	if c.Hash == "" {
		return c
	}
	local := repoFullName(s.cfg.UpdateRepoURL)
	if c.URL == "" || (local != "" && !sameRepo(commitURLRepo(c.URL), local)) {
		c.URL = s.commitURL(c.Hash)
	}
	return c
}

// commitURLRepo 从提交网页地址里取出「owner/name」部分（取不到时返回空串）。
func commitURLRepo(commitURL string) string {
	raw := strings.TrimSpace(commitURL)
	if raw == "" {
		return ""
	}
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	parts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	// host / owner / name / ...
	return parts[1] + "/" + parts[2]
}

// expandAPI 展开自建更新服务器接口模板里的占位符。
func (s *UpdateService) expandAPI(tmpl, commit string) string {
	owner, name := splitRepo(s.cfg.UpdateRepoURL)
	repl := strings.NewReplacer(
		"{repo}", s.cfg.UpdateRepoURL,
		"{owner}", owner,
		"{name}", name,
		"{branch}", s.cfg.UpdateBranch,
		"{commit}", commit,
		"{short}", ShortCommit(commit),
	)
	return repl.Replace(tmpl)
}

// commitURL 返回上游网页上的提交地址（仓库不是 GitHub 时可能不适用，仅作展示）。
func (s *UpdateService) commitURL(hash string) string {
	if hash == "" {
		return ""
	}
	owner, name := splitRepo(s.cfg.UpdateRepoURL)
	if owner == "" || name == "" {
		return ""
	}
	host := strings.TrimRight(s.cfg.UpdateGitHubAPI, "/")
	if strings.Contains(host, "api.github.com") {
		host = "https://github.com"
	} else if i := strings.Index(host, "/api/"); i > 0 {
		host = host[:i]
	}
	return host + "/" + owner + "/" + name + "/commit/" + escapeURLPath(hash)
}

// ---------------------------------------------------------------------------
// 历史记录
// ---------------------------------------------------------------------------

// appendHistoryLocked 追加历史并做长度截断（只保留最近 20 条）。
func (s *UpdateService) appendHistoryLocked(item UpdateHistoryItem) {
	s.state.History = append([]UpdateHistoryItem{item}, s.state.History...)
	if len(s.state.History) > 20 {
		s.state.History = s.state.History[:20]
	}
}

func nonNilHistory(items []UpdateHistoryItem) []UpdateHistoryItem {
	if items == nil {
		return []UpdateHistoryItem{}
	}
	return items
}

// ---------------------------------------------------------------------------
// 小工具
// ---------------------------------------------------------------------------

func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isFile 判断路径是否存在且是普通文件（目录返回 false）。
func isFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sameCommit 比较两个提交是否指向同一次提交（容忍短哈希）。
func sameCommit(a, b string) bool {
	x := strings.ToLower(strings.TrimSpace(a))
	y := strings.ToLower(strings.TrimSpace(b))
	if x == "" || y == "" {
		return false
	}
	if x == y {
		return true
	}
	if len(x) >= 7 && len(y) >= 7 {
		return strings.HasPrefix(x, y) || strings.HasPrefix(y, x)
	}
	return false
}

func formatStage(name, target string, progress int, message string) *UpdateStage {
	return &UpdateStage{
		Running:   true,
		Phase:     name,
		Progress:  progress,
		Message:   message,
		StartedAt: time.Now().Format(time.RFC3339),
		Target:    target,
	}
}

// stageError 把错误写进阶段状态，供前端直接展示。
func stageError(stage *UpdateStage, err error) *UpdateStage {
	if stage == nil {
		stage = &UpdateStage{}
	}
	stage.Running = false
	stage.Phase = PhaseFailed
	stage.FinishedAt = time.Now().Format(time.RFC3339)
	stage.Error = err.Error()
	return stage
}
