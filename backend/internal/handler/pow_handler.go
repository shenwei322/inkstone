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

// powChallengeRequest 是签发请求体，字段全部可选——老版本前端不带 body
// 也照常签发（此时挑战不绑定场景，语义与加固前一致）。
type powChallengeRequest struct {
	// Scene 声明这次挑战将用于哪个业务场景（login/register/comment）。
	// 带上它以后，该挑战只能在同名场景使用，防止「用便宜场景领挑战、
	// 拿去打敏感场景」。不带则不绑定。
	Scene string `json:"scene"`
}

// Challenge handles POST /api/v1/pow/challenge — 签发一个 POW 挑战。
// 限流由路由层 middleware.IPRateLimit 负责（见 main.go 装配）。
func (h *PowHandler) Challenge(c *gin.Context) {
	// 场景来源优先级：请求体 scene > 请求头 X-Pow-Scene。
	// 两者都没有时按「不绑定」处理，保证旧前端行为不变。
	// body 解析失败不报错：签发接口对格式宽容，宁可少一层绑定也不能让
	// 用户卡在拿不到挑战上。
	var req powChallengeRequest
	_ = c.ShouldBindJSON(&req)
	scene := req.Scene
	if scene == "" {
		scene = c.GetHeader("X-Pow-Scene")
	}

	challenge, difficulty, memoryMB, rounds, minEvents, ttlSeconds, err := h.pow.IssueFor(scene)
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
