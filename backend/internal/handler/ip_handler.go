package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// =====================================================================
// IP 管理（后台 /admin/ip/*）
//
// handler 只做「参数绑定 + 调 service + 响应」三件事；
// 阈值判定、白名单校验、封禁时长换算全在 service.IPGuardService 里。
// =====================================================================

type IPHandler struct {
	guard *service.IPGuardService
	logs  *service.LogService
}

func NewIPHandler(guard *service.IPGuardService, logs *service.LogService) *IPHandler {
	return &IPHandler{guard: guard, logs: logs}
}

// parseSinceQuery 解析 "window" 查询参数（分钟），默认取配置的统计窗口。
//
// 之所以允许前端传：运维想排查「昨天那波」时，固定 5 分钟窗口看不出东西。
func (h *IPHandler) parseSinceQuery(c *gin.Context) time.Time {
	minutes := parseIntQuery(c, "window", 0)
	if minutes <= 0 {
		return time.Now().Add(-h.guard.Window())
	}
	if minutes > 60*24*30 {
		minutes = 60 * 24 * 30
	}
	return time.Now().Add(-time.Duration(minutes) * time.Minute)
}

// parseTimeQuery 解析 "from"/"to"（2006-01-02 或 RFC3339）。nil = 不设边界。
func parseTimeQuery(c *gin.Context, name string) *time.Time {
	raw := c.Query(name)
	if raw == "" {
		return nil
	}
	if t, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return &t
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return &t
	}
	return nil
}

// ---------- 访问明细 ----------

// ListAccess 处理 GET /admin/ip/access — 访问明细分页。
func (h *IPHandler) ListAccess(c *gin.Context) {
	q := repository.IPAccessQuery{
		IP:       c.Query("ip"),
		Path:     c.Query("path"),
		From:     parseTimeQuery(c, "from"),
		To:       parseTimeQuery(c, "to"),
		Page:     parseIntQuery(c, "page", 1),
		PageSize: parseIntQuery(c, "page_size", 30),
	}
	rows, total, err := h.guard.ListAccess(q)
	if err != nil {
		errorResponse(c, err)
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, gin.H{
			"id":         r.ID,
			"ip":         r.IP,
			"method":     r.Method,
			"path":       r.Path,
			"status":     r.Status,
			"hits":       r.Hits,
			"bucket_at":  r.BucketAt,
			"user_agent": r.UserAgent,
			"referer":    r.Referer,
			"user_id":    r.UserID,
			"username":   r.Username,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"access":    items,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
	})
}

// Overview 处理 GET /admin/ip/overview — 概览卡片所需的一揽子数字。
func (h *IPHandler) Overview(c *gin.Context) {
	since := h.parseSinceQuery(c)
	top, err := h.guard.TopIPs(since, 1)
	if err != nil {
		errorResponse(c, err)
		return
	}

	var busiest int64
	busiestIP := ""
	if len(top) > 0 {
		busiest = top[0].Requests
		busiestIP = top[0].IP
	}

	// 窗口总量单独聚合：TopIPs 只是榜单，
	// 拿它的结果累加会漏掉榜外 IP，让运维误以为统计不全。
	windowTotal, uniqueIPs := h.guard.TotalRequests(since)

	c.JSON(http.StatusOK, gin.H{
		"overview": gin.H{
			"window_minutes":  int(h.guard.Window().Minutes()),
			"retention_days":  h.guard.RetentionDays(),
			"active_bans":     h.guard.BanCount(),
			"dropped_records": h.guard.DroppedCount(),
			"busiest_ip":      busiestIP,
			"busiest_hits":    busiest,
			"window_requests": windowTotal,
			"window_ips":      uniqueIPs,
		},
	})
}

// Top 处理 GET /admin/ip/top — 按请求量排序的 IP 排行。
func (h *IPHandler) Top(c *gin.Context) {
	since := h.parseSinceQuery(c)
	limit := parseIntQuery(c, "limit", 20)
	rows, err := h.guard.TopIPs(since, limit)
	if err != nil {
		errorResponse(c, err)
		return
	}

	items := make([]gin.H, 0, len(rows))
	for _, r := range rows {
		items = append(items, gin.H{
			"ip":         r.IP,
			"requests":   r.Requests,
			"paths":      r.Paths,
			"first_seen": r.FirstSeen,
			"last_seen":  r.LastSeen,
			"user_agent": r.UserAgent,
			"banned":     r.Banned,
		})
	}
	c.JSON(http.StatusOK, gin.H{"top": items})
}

// Detail 处理 GET /admin/ip/:ip — 单个 IP 的路径分布。
//
// 路径参数里的 IPv6 含冒号，Gin 的路由段能原样接收（不需要转义），
// 但前端拼 URL 时仍会对冒号做编码，这里统一 Trim 一次。
func (h *IPHandler) Detail(c *gin.Context) {
	ip := strings.Trim(strings.TrimSpace(c.Param("ip")), "[]")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 IP 参数"})
		return
	}
	since := h.parseSinceQuery(c)
	paths, err := h.guard.Paths(ip, since, parseIntQuery(c, "limit", 50))
	if err != nil {
		errorResponse(c, err)
		return
	}
	ban, banned := h.guard.Banned(ip)

	items := make([]gin.H, 0, len(paths))
	for _, p := range paths {
		items = append(items, gin.H{
			"path":      p.Path,
			"requests":  p.Requests,
			"last_seen": p.LastSeen,
			"status":    p.Status,
		})
	}

	var banPayload any
	if banned {
		banPayload = gin.H{
			"reason":            ban.Reason,
			"source":            ban.Source,
			"hits":              ban.Hits,
			"expires_at":        ban.ExpiresAt,
			"remaining_seconds": ban.RemainingSeconds(time.Now()),
			"created_by":        ban.CreatedBy,
			"created_at":        ban.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ip":        ip,
		"paths":     items,
		"banned":    banned,
		"ban":       banPayload,
		"window":    int(h.guard.Window().Minutes()),
		"retention": h.guard.RetentionDays(),
	})
}

// ---------- 封禁 ----------

// ListBans 处理 GET /admin/ip/bans — 封禁列表分页。
func (h *IPHandler) ListBans(c *gin.Context) {
	q := repository.IPBanQuery{
		IP:       c.Query("ip"),
		Source:   c.Query("source"),
		Page:     parseIntQuery(c, "page", 1),
		PageSize: parseIntQuery(c, "page_size", 30),
	}
	if v := c.Query("active"); v == "true" || v == "false" {
		active := v == "true"
		q.Active = &active
	}

	rows, total, err := h.guard.ListBans(q)
	if err != nil {
		errorResponse(c, err)
		return
	}

	now := time.Now()
	items := make([]gin.H, 0, len(rows))
	for _, b := range rows {
		items = append(items, gin.H{
			"id":                b.ID,
			"ip":                b.IP,
			"reason":            b.Reason,
			"source":            b.Source,
			"hits":              b.Hits,
			"expires_at":        b.ExpiresAt,
			"remaining_seconds": b.RemainingSeconds(now),
			"active":            b.Active(now),
			"created_by":        b.CreatedBy,
			"note":              b.Note,
			"created_at":        b.CreatedAt,
			"updated_at":        b.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"bans":      items,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
	})
}

// CreateBan 处理 POST /admin/ip/bans — 手动封禁。
type createBanRequest struct {
	IP string `json:"ip" binding:"required"`
	// Minutes 用指针区分「没传」和「传了 0」：
	//   没传（nil） → 用后台配置的默认封禁时长（ip_guard_ban_min）
	//   0           → 永久封禁
	//   >0          → 指定分钟数
	// 用值类型会让「永久封禁」和「没填」变成同一个 0，前端无论怎么传都会
	// 被解释成永久——这是个很容易误封一整年的坑。
	Minutes *int   `json:"minutes"`
	Reason  string `json:"reason"`
	Note    string `json:"note"`
}

// CreateBan 见 createBanRequest 说明。
func (h *IPHandler) CreateBan(c *gin.Context) {
	var req createBanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写要封禁的 IP"})
		return
	}

	minutes := h.guard.DefaultBanMinutes()
	if req.Minutes != nil {
		minutes = *req.Minutes
	}

	by := ""
	if user, ok := middleware.GetCurrentUser(c); ok {
		by = user.Username
	}

	ban, err := h.guard.Ban(service.BanInput{
		IP:      req.IP,
		Minutes: minutes,
		Reason:  req.Reason,
		Note:    req.Note,
		By:      by,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySetting, "封禁 IP", req.IP, false)
		errorResponse(c, err)
		return
	}

	detail := "封禁 " + ban.IP + "（" + banDurationLabel(ban.ExpiresAt) + "）"
	recordOp(h.logs, c, model.LogCategorySetting, "封禁 IP", detail, true)

	c.JSON(http.StatusOK, gin.H{
		"ban": gin.H{
			"ip":                ban.IP,
			"reason":            ban.Reason,
			"source":            ban.Source,
			"expires_at":        ban.ExpiresAt,
			"remaining_seconds": ban.RemainingSeconds(time.Now()),
			"created_by":        ban.CreatedBy,
		},
		"message": "已封禁 " + ban.IP,
	})
}

// DeleteBan 处理 DELETE /admin/ip/bans/:ip — 解封。
func (h *IPHandler) DeleteBan(c *gin.Context) {
	ip := strings.Trim(strings.TrimSpace(c.Param("ip")), "[]")
	removed, err := h.guard.Unban(ip)
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySetting, "解除封禁", ip, false)
		errorResponse(c, err)
		return
	}
	if !removed {
		c.JSON(http.StatusNotFound, gin.H{"error": "该 IP 没有封禁记录"})
		return
	}
	recordOp(h.logs, c, model.LogCategorySetting, "解除封禁", ip, true)
	c.JSON(http.StatusOK, gin.H{"message": "已解除 " + ip + " 的封禁"})
}

// banDurationLabel 把封禁到期时刻转成中文说明（永久 / 指定时长）。
func banDurationLabel(expires *time.Time) string {
	if expires == nil {
		return "永久"
	}
	remaining := time.Until(*expires)
	if remaining <= 0 {
		return "已过期"
	}
	minutes := int(remaining.Minutes())
	if minutes >= 60 && minutes%60 == 0 {
		return strconv.Itoa(minutes/60) + " 小时"
	}
	if minutes < 1 {
		return "1 分钟"
	}
	return strconv.Itoa(minutes) + " 分钟"
}
