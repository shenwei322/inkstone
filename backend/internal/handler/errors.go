package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// errorResponse maps domain errors to HTTP responses.
func errorResponse(c *gin.Context, err error) {
	var validationErr *service.ValidationError
	switch {
	case errors.As(err, &validationErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErr.Message})
	case errors.Is(err, service.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrTOTPInvalid):
		// 两步验证码错误：401 但带 need_totp，让前端停留在验证码输入步
		// 而不是退回密码步（用户会以为密码错了）。
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "need_totp": true})
	case errors.Is(err, service.ErrUserBanned):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrRegistrationClosed):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": "没有权限执行此操作"})
	case errors.Is(err, repository.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
	default:
		log.Printf("[error] %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

// parseUintParam reads a positive integer route parameter. It writes a 400
// response and returns false when the value is missing or malformed.
func parseUintParam(c *gin.Context, name, message string) (uint, bool) {
	raw := c.Param(name)
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": message})
		return 0, false
	}
	return uint(id), true
}

// parseIntQuery reads an integer query parameter with a fallback default.
func parseIntQuery(c *gin.Context, name string, fallback int) int {
	raw := c.Query(name)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 1 {
		return fallback
	}
	return v
}
