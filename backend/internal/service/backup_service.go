package service

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// BackupService 把数据库内容导出为 gzip 压缩的 JSON 快照，落在
// data/backups/ 下，供后台「备份与恢复」页下载。
//
// 为什么不用 pg_dump：后端容器里没有 postgres 客户端，而它才是"完整可
// 恢复"的备份。宿主级完整备份请用 deploy/backup.sh（调 pg_dump -Fc）。
// 本服务解决的是另一半需求——管理员在浏览器里一键拿到本站的**内容数据**
// （文章/评论/设置/用户），用于迁移、留档或在误操作后比对，不依赖任何
// 外部二进制。
type BackupService struct {
	db  *gorm.DB
	dir string
}

func NewBackupService(db *gorm.DB, dir string) *BackupService {
	return &BackupService{db: db, dir: dir}
}

// backupVersion 是快照格式版本。往备份里加表/改字段结构时递增它，
// 恢复端据此判断能否处理旧文件。
const backupVersion = 1

// maxBackupFiles 是磁盘上保留的快照份数。超出后删除最旧的——
// 备份本身会占满磁盘，这是必须有的上限。
const maxBackupFiles = 20

// backupTable 描述一张参与备份的表：key 是快照里的字段名，table 是物理表名。
type backupTable struct {
	key   string
	table string
}

// backupTables 覆盖全部业务表。用物理表名（而非 GORM Model）是为了
// 一次查询拿到原始列值，且不受软删除过滤影响——回收站里的文章
// 也应该进备份，否则"删错了再去备份里找"这条最常用的恢复路径是断的。
var backupTables = []backupTable{
	{"users", "users"},
	{"articles", "articles"},
	{"pages", "pages"},
	{"categories", "categories"},
	{"tags", "tags"},
	{"article_tags", "article_tags"},
	{"comments", "comments"},
	{"reactions", "reactions"},
	{"settings", "settings"},
	{"friend_links", "friend_links"},
	{"friend_link_applications", "friend_link_applications"},
	{"file_assets", "file_assets"},
	{"operation_logs", "operation_logs"},
	{"daily_stats", "daily_stats"},
	{"visitor_days", "visitor_days"},
}

// BackupFile 描述一个快照文件的元信息。
type BackupFile struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason"`
}

// ErrBackupNotFound 表示请求的快照不存在。
var ErrBackupNotFound = errors.New("备份文件不存在")

// backupPrefix 是快照文件名前缀，List/解析靠它识别自家文件。
const backupPrefix = "inkstone-backup-"

// Create 生成一份快照并写入磁盘。reason 记入文件内，便于日后分辨
// "手动备份" 与 "更新前自动备份"。
func (s *BackupService) Create(reason string) (BackupFile, error) {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return BackupFile{}, err
	}
	now := time.Now()
	if strings.TrimSpace(reason) == "" {
		reason = "manual"
	}

	tables := make(map[string]any, len(backupTables))
	for _, t := range backupTables {
		var rows []map[string]any
		if err := s.db.Table(t.table).Find(&rows).Error; err != nil {
			// 某张表不存在不应让整次备份失败：老库可能少表（AutoMigrate
			// 之后的升级场景），跳过它比整个备份失败更有用。
			log.Printf("[backup] 跳过表 %s: %v", t.table, err)
			tables[t.key] = []map[string]any{}
			continue
		}
		tables[t.key] = rows
	}

	payload := map[string]any{
		"version":    backupVersion,
		"created_at": now,
		"reason":     reason,
		"tables":     tables,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return BackupFile{}, err
	}

	name := fmt.Sprintf("%s%s.json.gz", backupPrefix, now.Format("20060102-150405"))
	path := filepath.Join(s.dir, name)
	if err := writeGzip(path, data); err != nil {
		return BackupFile{}, err
	}
	info := BackupFile{Name: name, CreatedAt: now, Reason: reason}
	if fi, err := os.Stat(path); err == nil {
		info.Size = fi.Size()
	}
	// 保留策略：超出上限删最旧的。失败只记日志——备份已生成，
	// 不该因为清理失败就向用户报错。
	s.pruneOld()
	return info, nil
}

// writeGzip 以 0640 写入压缩文件：备份含 users 表（含密码哈希），
// 不该给"其他用户可读"的权限。
func writeGzip(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	if _, err := gz.Write(data); err != nil {
		return err
	}
	return gz.Close()
}

// List 返回全部快照，按创建时间倒序（最新在前）。
func (s *BackupService) List() ([]BackupFile, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupFile{}, nil
		}
		return nil, err
	}
	out := make([]BackupFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), backupPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		created := info.ModTime()
		// 文件名里带了时间戳，比 ModTime 更可靠（拷贝/解压会改 ModTime）。
		if t, ok := parseBackupTime(e.Name()); ok {
			created = t
		}
		out = append(out, BackupFile{
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: created,
			Reason:    "",
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path 返回某个快照的绝对路径，校验文件名合法（防目录穿越）。
func (s *BackupService) Path(name string) (string, error) {
	if !isSafeBackupName(name) {
		return "", ErrBackupNotFound
	}
	path := filepath.Join(s.dir, name)
	if _, err := os.Stat(path); err != nil {
		return "", ErrBackupNotFound
	}
	return path, nil
}

// Delete 删除一个快照。
func (s *BackupService) Delete(name string) error {
	path, err := s.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

// isSafeBackupName 只接受形如 inkstone-backup-YYYYMMDD-HHMMSS.json.gz 的
// 名字：去掉了路径分隔符与 ".." 的可能性，filepath.Join 也就无从穿越。
func isSafeBackupName(name string) bool {
	if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, ".json.gz") {
		return false
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return false
	}
	return len(name) == len(backupPrefix)+len("20060102-150405")+len(".json.gz")
}

// parseBackupTime 从文件名解析创建时间。
func parseBackupTime(name string) (time.Time, bool) {
	core := strings.TrimSuffix(strings.TrimPrefix(name, backupPrefix), ".json.gz")
	t, err := time.ParseInLocation("20060102-150405", core, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// pruneOld 删除超出保留份数的最旧快照。
func (s *BackupService) pruneOld() {
	files, err := s.List()
	if err != nil || len(files) <= maxBackupFiles {
		return
	}
	for _, f := range files[maxBackupFiles:] {
		if err := s.Delete(f.Name); err != nil {
			log.Printf("[backup] 清理旧快照 %s 失败: %v", f.Name, err)
		}
	}
}
