package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv       string
	Port         string
	DBHost       string
	DBPort       string
	DBUser       string
	DBPassword   string
	DBName       string
	JWTSecret    string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	FrontendURL  string
	AdminURL     string
	PublicAPIURL string
	UploadDir    string
	FilesDir     string

	// TrustedProxies 是允许其转发头（X-Forwarded-For / X-Real-IP）被采信的
	// 代理网段（CIDR）。只有直连对端落在这些网段内，转发头才会被用来判定
	// 访客 IP——否则一律用 TCP 对端地址。默认覆盖本机与 RFC1918 私网，即
	// 「同机 / 同容器网络的 Nginx 反代」这一标准部署；公网直连部署时伪造的
	// XFF 不会被采信。可用 TRUSTED_PROXIES 覆盖（逗号分隔 CIDR）。
	TrustedProxies []string

	// 在线更新系统（后台「系统更新」页）
	UpdateEnabled      bool   // 总开关（UPDATE_ENABLED）
	UpdateRepoURL      string // 上游仓库（UPDATE_REPO_URL）
	UpdateBranch       string // 跟踪分支（UPDATE_BRANCH）
	UpdateSource       string // 更新源类型：commits（默认：提交+源码包）/ releases（GitHub Releases+镜像包）
	UpdateReleasesAPI  string // Releases 最新版本接口模板（默认 {UPDATE_GITHUB_API}/repos/{owner}/{name}/releases/latest）
	UpdateImageAsset   string // 镜像包资产名匹配（正则；默认 inkstone-images-.*\.tar$）
	UpdateImageMaxMB   int64  // 镜像包大小上限（MB，默认 2048）
	UpdateChecksum     string // 期望的下载内容 SHA-256（十六进制，可空）。非空则强制校验，不一致直接拒绝安装
	UpdateVersionFile  string // 部署版本记录文件（默认自动探测 data/deployed-version.json）
	UpdateComposeFile  string // 镜像更新使用的 compose 文件（默认自动探测）
	UpdateMirror       string // 源码包镜像地址模板，支持 {repo} {owner} {name} {ref} {commit} {short} 占位
	UpdateGitHubAPI    string // GitHub API 基址，内网镜像可指向自建代理
	UpdateCommitsAPI   string // 提交列表 JSON 接口模板（自建更新服务器）
	UpdateLatestAPI    string // 「仅返回最新 commit」的接口模板（自建更新服务器）
	UpdateCompareAPI   string // 提交对比 JSON 接口模板（自建更新服务器）
	UpdateToken        string // 私有仓库 / 提额用的 token
	UpdateSourceDir    string // 待替换的源码目录（默认自动探测仓库根）
	UpdateDir          string // 更新工作目录（源码包、备份、状态文件）
	UpdateDeployedFile string // 部署记录文件路径（默认自动探测 data/deployed-commit.json）
	UpdateRemote       string // git remote 名：写入待更新清单，供宿主代理脚本使用
	UpdateWaitingAgent bool   // true=替换源码后等待宿主代理重建；false=本机直接重建
	// AllowPrivateHosts 放开对更新/镜像地址的内网访问限制。
	//
	// 默认 false：下载链路会校验实际拨号地址，拒绝回环/私网/链路本地
	// （含云元数据 169.254.169.254）等。理由是下载地址可能来自上游 API
	// 响应（Release 资产的 browser_download_url）或第三方镜像，一旦上游被
	// 污染/中间人，就能借更新通道让后端请求内网服务。
	//
	// 只在「更新源就是内网自建服务器」时置为 true，并清楚这会关掉上述防护。
	AllowPrivateHosts bool
}

// defaultUpdateMirror 是默认源码包镜像：gh-proxy 的格式是「代理前缀 + 完整 GitHub 地址」，
// 不能写成 /{owner}/{name}/archive/...（实测 404，会导致每次都退到 codeload，
// 而 codeload 在国内通常不可达）。
const defaultUpdateMirror = "https://gh-proxy.com/https://github.com/{owner}/{name}/archive/{commit}.tar.gz"

// 更新源类型：commits 是历史行为（对比提交 + 下载源码包替换源码）；
// releases 走 GitHub Releases（版本号 + 发布说明 + 镜像包资产）。
const (
	UpdateSourceCommits  = "commits"
	UpdateSourceReleases = "releases"
)

// defaultImageAssetPattern 匹配发布资产里的镜像包文件名。
// 约定：inkstone-images-<tag>.tar（如 inkstone-images-v1.28.0.tar）。
const defaultImageAssetPattern = `inkstone-images-.*\.tar$`

// defaultImageMaxMB 镜像包大小上限：镜像包含前后端两个镜像，2G 足够且防呆。
const defaultImageMaxMB int64 = 2048

// normalizeUpdateSource 归一化更新源配置：空值按默认 commits，未知值也回落，
// 避免拼写错误把整套更新系统静默变成另一套语义。
func normalizeUpdateSource(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case UpdateSourceReleases:
		return UpdateSourceReleases
	default:
		return UpdateSourceCommits
	}
}

func getEnvInt(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// defaultTrustedProxies 是未显式配置 TRUSTED_PROXIES 时采信的代理网段。
//
// 只包含本机与私网：标准部署是同一台机器（或同一个 compose 网络）里的
// Nginx 反代，直连对端必然落在这些网段内。反向的取舍是——公网直连部署时
// 对端是公网地址，不在名单里，于是客户端自带的 X-Forwarded-For 被忽略，
// 限流无法被伪造头绕过。这正是我们要的默认行为。
var defaultTrustedProxies = []string{
	"127.0.0.0/8",    // IPv4 本机
	"::1/128",        // IPv6 本机
	"10.0.0.0/8",     // RFC1918
	"172.16.0.0/12",  // RFC1918（含 Docker 默认网段 172.17/16）
	"192.168.0.0/16", // RFC1918
	"fc00::/7",       // IPv6 ULA
}

// parseTrustedProxies 解析 TRUSTED_PROXIES（逗号分隔 CIDR）。空串用默认值；
// 显式写成 "none" 表示「不信任任何代理」，一律按 TCP 对端判定。
func parseTrustedProxies(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return append([]string(nil), defaultTrustedProxies...)
	}
	if strings.EqualFold(raw, "none") {
		return nil
	}
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), defaultTrustedProxies...)
	}
	return out
}

func Load() *Config {
	filesDir := getEnv("FILES_DIR", "./data/files")
	uploadDir := getEnv("UPLOAD_DIR", "./data/uploads")
	explicitSource := strings.TrimSpace(os.Getenv("UPDATE_SOURCE_DIR"))
	sourceDir := explicitSource
	if sourceDir == "" {
		sourceDir = DetectRepoRoot()
	}
	updateDir := strings.TrimSpace(os.Getenv("UPDATE_DIR"))
	if updateDir == "" {
		if explicitSource != "" {
			// 显式指定了源码目录时，更新目录跟着它走：否则相对路径会把镜像包、
			// 备份与待更新清单写到「进程工作目录」下，与宿主代理查找的位置对不上。
			updateDir = filepath.Join(explicitSource, "data", "update")
		} else {
			// 与上传目录同处一个数据卷：容器内是 /app/data/update；
			// 生产编排再把它绑定到宿主 ./data/update，宿主更新代理才读得到待更新清单。
			updateDir = filepath.Join(filepath.Dir(filepath.Clean(uploadDir)), "update")
		}
	}
	return &Config{
		AppEnv:       getEnv("APP_ENV", "development"),
		Port:         getEnv("PORT", "8080"),
		DBHost:       getEnv("DB_HOST", "localhost"),
		DBPort:       getEnv("DB_PORT", "5432"),
		DBUser:       getEnv("DB_USER", "blog"),
		DBPassword:   getEnv("DB_PASSWORD", "blog_dev_password"),
		DBName:       getEnv("DB_NAME", "blog_platform"),
		JWTSecret:    getEnv("JWT_SECRET", "dev-only-secret-change-in-production"),
		AccessTTL:    15 * time.Minute,
		RefreshTTL:   7 * 24 * time.Hour,
		FrontendURL:  getEnv("FRONTEND_URL", "http://localhost:3000"),
		AdminURL:     getEnv("ADMIN_URL", "http://localhost:3001"),
		PublicAPIURL: getEnv("PUBLIC_API_URL", ""),
		UploadDir:    uploadDir,
		FilesDir:     filesDir,

		TrustedProxies: parseTrustedProxies(os.Getenv("TRUSTED_PROXIES")),

		UpdateEnabled:      getEnv("UPDATE_ENABLED", "true") == "true",
		UpdateRepoURL:      getEnv("UPDATE_REPO_URL", "https://github.com/shenwei234/inkstone"),
		UpdateBranch:       getEnv("UPDATE_BRANCH", "main"),
		UpdateSource:       normalizeUpdateSource(getEnv("UPDATE_SOURCE", "")),
		UpdateReleasesAPI:  getEnv("UPDATE_RELEASES_API", ""),
		UpdateImageAsset:   getEnv("UPDATE_IMAGE_ASSET", defaultImageAssetPattern),
		UpdateImageMaxMB:   getEnvInt("UPDATE_IMAGE_MAX_MB", defaultImageMaxMB),
		UpdateChecksum:     strings.ToLower(strings.TrimSpace(strings.ReplaceAll(getEnv("UPDATE_CHECKSUM", ""), "sha256:", ""))),
		UpdateVersionFile:  getEnv("UPDATE_VERSION_FILE", ""),
		UpdateComposeFile:  getEnv("UPDATE_COMPOSE_FILE", ""),
		UpdateMirror:       getEnv("UPDATE_MIRROR", defaultUpdateMirror),
		UpdateGitHubAPI:    strings.TrimRight(getEnv("UPDATE_GITHUB_API", "https://api.github.com"), "/"),
		UpdateCommitsAPI:   getEnv("UPDATE_COMMITS_API", ""),
		UpdateLatestAPI:    getEnv("UPDATE_LATEST_API", ""),
		UpdateCompareAPI:   getEnv("UPDATE_COMPARE_API", ""),
		UpdateToken:        getEnv("UPDATE_TOKEN", ""),
		UpdateSourceDir:    sourceDir,
		UpdateDir:          updateDir,
		UpdateDeployedFile: getEnv("UPDATE_DEPLOYED_FILE", ""),
		UpdateRemote:       getEnv("UPDATE_REMOTE", "origin"),
		UpdateWaitingAgent: getEnv("UPDATE_WAITING_AGENT", "true") == "true",
		// 默认 false：下载链路校验实际拨号地址，拒绝内网/回环/元数据地址。
		// 仅当更新源本身就是内网自建服务器时才显式置 true。
		AllowPrivateHosts: getEnv("UPDATE_ALLOW_PRIVATE_HOSTS", "false") == "true",
	}
}

// DetectRepoRoot 从当前工作目录向上探测仓库根：
// 同时存在 backend/cmd/server/main.go 与 frontend/package.json 的目录才算命中。
// 找不到时返回空串——调用方据此判定「源码目录未知」，更新页会给出提示而不是猜路径。
func DetectRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		if looksLikeRepoRoot(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func looksLikeRepoRoot(dir string) bool {
	if dir == "" {
		return false
	}
	if !isFile(filepath.Join(dir, "backend", "cmd", "server", "main.go")) {
		return false
	}
	return isFile(filepath.Join(dir, "frontend", "package.json"))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// AbsoluteUploadBase returns the public base URL for serving uploaded files.
// Empty PublicAPIURL means the API is served same-origin (reverse proxy), so
// relative URLs are returned.
func (c *Config) AbsoluteUploadBase() string {
	if c.PublicAPIURL != "" {
		return c.PublicAPIURL
	}
	if c.IsProduction() {
		return ""
	}
	return "http://localhost:" + c.Port
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
