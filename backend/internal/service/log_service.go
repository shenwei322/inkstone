package service

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// LogService records and queries operation logs. Recording is asynchronous so
// it never slows down the request path.
type LogService struct {
	repo  *repository.OperationLogRepository
	queue chan model.OperationLog
}

func NewLogService(repo *repository.OperationLogRepository) *LogService {
	s := &LogService{
		repo:  repo,
		queue: make(chan model.OperationLog, 1024),
	}
	go s.worker()
	go s.cleanupLoop()
	return s
}

// Entry describes one operation to be logged.
type Entry struct {
	UserID    uint
	Username  string
	Category  string
	Action    string
	Detail    string
	IP        string
	UserAgent string
	Success   bool
}

// maxDetailRunes caps the detail column (gorm size:500) so a long title or URL
// can never break the INSERT with a value-too-long error.
const maxDetailRunes = 500

// droppedLogs 统计被迫同步写入的次数，用于暴露队列压力。
// 审计日志不能丢：队列满时宁可让当次请求多等一次 INSERT，也不能静默丢弃
// ——否则关键操作（改设置、删用户、系统更新）可能无迹可查。
var droppedLogs atomic.Int64

// Record queues a log entry. The fast path is non-blocking; when the queue is
// full the entry is written synchronously so audit records are never lost.
func (s *LogService) Record(e Entry) {
	if s == nil {
		return
	}
	entry := model.OperationLog{
		UserID:    e.UserID,
		Username:  e.Username,
		Category:  e.Category,
		Action:    e.Action,
		Detail:    truncate(e.Detail, maxDetailRunes),
		IP:        e.IP,
		UserAgent: truncate(e.UserAgent, 250),
		Success:   e.Success,
		CreatedAt: time.Now(),
	}
	select {
	case s.queue <- entry:
	default:
		// 队列积压：同步落库 + 计数告警（绝不丢弃审计记录）
		if n := droppedLogs.Add(1); n == 1 || n%100 == 0 {
			log.Printf("[oplog] 队列已满，第 %d 条日志改为同步写入（检查数据库写入性能）", n)
		}
		if err := s.repo.Create(&entry); err != nil {
			log.Printf("[oplog] 同步写入失败（日志丢失）: %v", err)
		}
	}
}

// worker 逐个消费审计队列。
//
// ⚠️ 单次写入必须单独 recover（见 writeSafely）：这里是 `for range`，
// 若把 recover 放在 worker 外层，一次 panic 会让整个 goroutine 连带循环一起
// 退出——此后所有审计日志都不再落库，而审计恰恰是最不能丢的部分。
func (s *LogService) worker() {
	for entry := range s.queue {
		s.writeSafely(entry)
	}
}

// writeSafely 写入一条审计日志，panic 只影响这一条。
func (s *LogService) writeSafely(entry model.OperationLog) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[oplog] 写入审计日志时 panic 已恢复（该条丢失，队列继续消费）: %v", r)
		}
	}()
	if err := s.repo.Create(&entry); err != nil {
		log.Printf("[oplog] write failed: %v", err)
	}
}

// cleanupLoop 保留最近 90 天日志。
func (s *LogService) cleanupLoop() {
	for {
		time.Sleep(24 * time.Hour)
		_ = s.repo.Cleanup(time.Now().AddDate(0, 0, -90).Format("2006-01-02 15:04:05"))
	}
}

func (s *LogService) List(q repository.OperationLogQuery) ([]model.OperationLog, int64, error) {
	return s.repo.List(q)
}

// Export returns all logs matching the filter (capped), for CSV download.
func (s *LogService) Export(q repository.OperationLogQuery) ([]model.OperationLog, error) {
	return s.repo.Export(q)
}

// LogOverview aggregates headline counters for the admin dashboard.
type LogOverview struct {
	Total      int64            `json:"total"`
	Today      int64            `json:"today"`
	Failed     int64            `json:"failed"`
	ByCategory map[string]int64 `json:"by_category"`
}

func (s *LogService) Overview() (LogOverview, error) {
	total, err := s.repo.CountAll()
	if err != nil {
		return LogOverview{}, err
	}
	today, err := s.repo.CountToday()
	if err != nil {
		return LogOverview{}, err
	}
	failed, err := s.repo.CountFailed()
	if err != nil {
		return LogOverview{}, err
	}
	byCategory, err := s.repo.CountByCategory()
	if err != nil {
		return LogOverview{}, err
	}
	return LogOverview{
		Total:      total,
		Today:      today,
		Failed:     failed,
		ByCategory: byCategory,
	}, nil
}

// Stats returns log counts grouped by category.
func (s *LogService) Stats() (map[string]int64, error) {
	return s.repo.CountByCategory()
}

// truncate 按「字符」截断，避免切断 UTF-8 多字节序列产生非法 rune。
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
