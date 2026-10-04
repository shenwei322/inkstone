package service

import (
	"log"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// ScheduledPublisher 定时发布扫描器。
//
// 到点的 scheduled 文章改成 published。用轮询而不是数据库定时任务：
// 项目只用 PostgreSQL，没装 pg_cron；引入外部调度器又会多一个部署依赖。
// 每分钟一次的 UPDATE 代价可以忽略，且发布延迟最多 1 分钟——博客场景完全可接受。
type ScheduledPublisher struct {
	articles ArticlePublisher
	interval time.Duration
	stop     chan struct{}
}

// ArticlePublisher 是扫描器需要的最小 repository 接口。
// 定义成接口而不是直接吃 *ArticleRepository：单测可以打桩，
// 不必为了测一个 cron 循环去连数据库。
type ArticlePublisher interface {
	// PublishDue 把 scheduled_at <= now 的 scheduled 文章改为 published，
	// 返回实际发布的文章数与新出现的文章（供日志记录）。
	PublishDue(now time.Time) ([]model.Article, error)
}

func NewScheduledPublisher(articles ArticlePublisher) *ScheduledPublisher {
	return &ScheduledPublisher{
		articles: articles,
		interval: time.Minute,
		stop:     make(chan struct{}),
	}
}

// Start 启动后台扫描。返回的 stop 函数由调用方在进程退出时调用。
//
// 故意用 channel 而非 context：这里只需要一个"停了"的信号，
// 不涉及超时/取消传播，context 反而要多包一层。
func (p *ScheduledPublisher) Start() {
	go func() {
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		log.Printf("[schedule] 定时发布器已启动，每 %v 扫描一次", p.interval)
		for {
			select {
			case <-ticker.C:
				p.runOnce()
			case <-p.stop:
				log.Printf("[schedule] 定时发布器已停止")
				return
			}
		}
	}()
}

// Stop 停止扫描。重复调用是安全的。
func (p *ScheduledPublisher) Stop() {
	select {
	case <-p.stop:
		// 已经关过：重复 close 会 panic，这里静默返回
	default:
		close(p.stop)
	}
}

func (p *ScheduledPublisher) runOnce() {
	published, err := p.articles.PublishDue(time.Now())
	if err != nil {
		// 失败不退出循环：下次 tick 会重试。定时发布失败的影响只是
		// 文章晚几分钟公开，不该让整个 goroutine 死掉。
		log.Printf("[schedule] 定时发布扫描失败: %v", err)
		return
	}
	for _, a := range published {
		log.Printf("[schedule] 文章《%s》（#%d）已按计划发布", a.Title, a.ID)
	}
}

// RunOnceForTest 暴露 runOnce 供测试用，避免测试里真的等一分钟。
func (p *ScheduledPublisher) RunOnceForTest() {
	p.runOnce()
}
