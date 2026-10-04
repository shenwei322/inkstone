package service

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

type StatService struct {
	stats    *repository.StatRepository
	settings *SettingsService
	queue    chan recordTask
}

type recordTask struct {
	date        string
	bytesIn     int64
	bytesOut    int64
	visitorHash string
}

func NewStatService(stats *repository.StatRepository, settings *SettingsService) *StatService {
	s := &StatService{
		stats:    stats,
		settings: settings,
		queue:    make(chan recordTask, 4096),
	}
	go s.worker()
	go s.cleanupLoop()
	return s
}

// droppedSamples 统计因队列积压而丢弃的流量样本数。
// 流量统计可以丢（不应拖慢请求），但必须可观测——否则指标偏低时无从判断
// 是「真没流量」还是「队列一直满」。
var droppedSamples atomic.Int64

// DroppedSamples 返回进程启动以来被丢弃的样本数。
func DroppedSamples() int64 { return droppedSamples.Load() }

// Record queues a traffic sample; it never blocks the request path.
func (s *StatService) Record(bytesIn, bytesOut int64, clientIP string) {
	task := recordTask{
		date:     time.Now().Format("2006-01-02"),
		bytesIn:  bytesIn,
		bytesOut: bytesOut,
	}
	// 保护隐私：仅存 IP 的哈希，不落原始地址
	sum := sha256.Sum256([]byte(clientIP + "|inkstone"))
	task.visitorHash = hex.EncodeToString(sum[:16])

	select {
	case s.queue <- task:
	default:
		// 队列满时丢弃，避免拖慢请求；每 500 条打一次日志便于发现异常
		if n := droppedSamples.Add(1); n == 1 || n%500 == 0 {
			log.Printf("[stats] 队列已满，累计丢弃 %d 个流量样本（数据库写入可能过慢）", n)
		}
	}
}

// worker 逐个消费流量样本队列。
//
// ⚠️ 单次写入必须单独 recover（见 recordSafely）：这里是 `for range`，
// 若 panic 逃出循环，整个统计 worker 会静默死亡——流量曲线从此停止更新，
// 且没有任何报错提示。
func (s *StatService) worker() {
	for task := range s.queue {
		s.recordSafely(task)
	}
}

// recordSafely 写入一条流量样本，panic 只影响这一条。
func (s *StatService) recordSafely(task recordTask) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[stats] 记录流量样本时 panic 已恢复（该条丢失，队列继续消费）: %v", r)
		}
	}()
	if err := s.stats.Record(task.date, task.bytesIn, task.bytesOut, task.visitorHash); err != nil {
		log.Printf("[stats] record failed: %v", err)
	}
}

func (s *StatService) cleanupLoop() {
	for {
		time.Sleep(12 * time.Hour)
		_ = s.stats.Cleanup(time.Now().AddDate(0, 0, -90).Format("2006-01-02"))
	}
}

type TrendPoint struct {
	Date      string `json:"date"`
	PageViews int64  `json:"page_views"`
	Visitors  int64  `json:"visitors"`
	BytesIn   int64  `json:"bytes_in"`
	BytesOut  int64  `json:"bytes_out"`
}

// Trend returns the last N days of traffic, filling gaps with zeros so the
// chart always renders a continuous timeline.
func (s *StatService) Trend(days int) ([]TrendPoint, error) {
	if days < 1 || days > 365 {
		days = 30
	}
	now := time.Now()
	from := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	to := now.Format("2006-01-02")

	rows, err := s.stats.Range(from, to)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]model.DailyStat, len(rows))
	for _, r := range rows {
		byDate[r.Date] = r
	}

	out := make([]TrendPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		row := byDate[d]
		out = append(out, TrendPoint{
			Date:      d,
			PageViews: row.PageViews,
			Visitors:  row.Visitors,
			BytesIn:   row.BytesIn,
			BytesOut:  row.BytesOut,
		})
	}
	return out, nil
}

// ---------- 系统资源 ----------

type SystemResource struct {
	CPUPercent   float64 `json:"cpu_percent"`
	MemUsedMB    float64 `json:"mem_used_mb"`
	MemTotalMB   float64 `json:"mem_total_mb"`
	MemPercent   float64 `json:"mem_percent"`
	Goroutines   int     `json:"goroutines"`
	UptimeSecond int64   `json:"uptime_seconds"`
	AppMemMB     float64 `json:"app_mem_mb"`
}

// SystemResources reports host CPU/memory usage plus app-level metrics.
// Host metrics come from stat_linux.go / stat_windows.go (per platform).
// falls back to the Go runtime's own load estimate.
func (s *StatService) SystemResources() SystemResource {
	res := SystemResource{
		Goroutines:   runtime.NumGoroutine(),
		UptimeSecond: int64(time.Since(appStartTime).Seconds()),
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	res.AppMemMB = float64(m.Alloc) / 1024 / 1024

	if used, total, ok := readMemInfo(); ok {
		res.MemUsedMB = used
		res.MemTotalMB = total
		if total > 0 {
			res.MemPercent = used / total * 100
		}
	}
	res.CPUPercent = readCPUPercent()
	return res
}
