package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// PowHandler 暴露 POW 挑战签发接口（免登录：验证发生在登录/注册/评论之前，
// 浏览器必须先拿到 challenge 才能计算答案）。
type PowHandler struct {
	pow *service.PowService
}

func NewPowHandler(pow *service.PowService) *PowHandler {
	return &PowHandler{pow: pow}
}

// powChallengeResponse 是前端 use-pow-captcha 期望的响应结构。
// memoryMB / rounds / minEvents 为签发时刻快照的本地资源参数——求解与
// signal 采集都按这份参数执行，后端校验时以挑战记录里的同一快照为准。
type powChallengeResponse struct {
	Challenge  string `json:"challenge"`   // 随机挑战（十六进制）
	Difficulty int    `json:"difficulty"`  // 难度：答案哈希前导零个数
	MemoryMB   int    `json:"memory_mb"`   // 内存表大小（MB）
	Rounds     int    `json:"rounds"`      // 表查找-混合轮数
	MinEvents  int    `json:"min_events"`  // 需采集的本地交互事件数（0=不校验）
	TTLSeconds int    `json:"ttl_seconds"` // 挑战有效期（秒）
}

// Challenge handles POST /api/v1/pow/challenge — 签发一个 POW 挑战。
// 限流由路由层 middleware.IPRateLimit 负责（见 main.go 装配）。
func (h *PowHandler) Challenge(c *gin.Context) {
	challenge, difficulty, memoryMB, rounds, minEvents, ttlSeconds, err := h.pow.Issue()
	if err != nil {
		// 挑战池满等异常：提示繁忙，让用户稍后重试（非配置错误）
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "人机验证服务繁忙，请稍后重试"})
		return
	}
	c.JSON(http.StatusOK, powChallengeResponse{
		Challenge:  challenge,
		Difficulty: difficulty,
		MemoryMB:   memoryMB,
		Rounds:     rounds,
		MinEvents:  minEvents,
		TTLSeconds: ttlSeconds,
	})
}
