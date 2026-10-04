package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/pkg/config"
	"github.com/shenwei/inkstone/backend/pkg/imageutil"
)

const maxUploadBytes = 10 << 20 // 10MB

var allowedImageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".gif": true, ".webp": true, ".avif": true,
}

type UploadsHandler struct {
	cfg *config.Config
}

func NewUploadsHandler(cfg *config.Config) *UploadsHandler {
	return &UploadsHandler{cfg: cfg}
}

// Create handles POST /api/v1/uploads — multipart image upload saved to the
// local uploads directory (WordPress-style uploads folder).
func (h *UploadsHandler) Create(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要上传的文件"})
		return
	}
	if file.Size > maxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "图片不能超过 10MB"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedImageExt[ext] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 jpg/png/gif/webp/avif 图片"})
		return
	}

	if err := os.MkdirAll(h.cfg.UploadDir, 0o755); err != nil {
		errorResponse(c, err)
		return
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		errorResponse(c, err)
		return
	}
	name := hex.EncodeToString(buf) + ext
	dst := filepath.Join(h.cfg.UploadDir, name)

	src, err := file.Open()
	if err != nil {
		errorResponse(c, err)
		return
	}
	defer src.Close()

	out, err := os.Create(dst)
	if err != nil {
		errorResponse(c, err)
		return
	}
	defer out.Close()

	// 客户端声明的 file.Size 可伪造，必须限制实际写入的字节数，防止磁盘被打满
	written, err := io.Copy(out, io.LimitReader(src, maxUploadBytes+1))
	if err != nil {
		_ = os.Remove(dst)
		errorResponse(c, err)
		return
	}
	if written > maxUploadBytes {
		_ = os.Remove(dst)
		c.JSON(http.StatusBadRequest, gin.H{"error": "图片不能超过 10MB"})
		return
	}

	// 图片后处理：超大原图压缩 + 生成缩略图。
	// 任何一步失败都只记日志、返回空缩略图地址——原图已经落地，
	// 不能因为处理失败让用户传不上图。
	thumbURL := h.processImage(dst, name)

	url := strings.TrimSuffix(h.cfg.AbsoluteUploadBase(), "/") + "/uploads/" + name
	c.JSON(http.StatusCreated, gin.H{"url": url, "filename": name, "thumb_url": thumbURL})
}

// processImage 对刚落地的图片做「压缩 + 缩略图」，返回缩略图可访问 URL。
//
// 返回空串表示这一步没做成（非图片、WebP、处理出错、或配置关闭）。
// 失败只记日志不返回 error：图片处理是增强项，不是上传的前置条件。
func (h *UploadsHandler) processImage(dst, name string) string {
	data, err := os.ReadFile(dst)
	if err != nil {
		log.Printf("[upload] 读取已上传图片失败 %s: %v", name, err)
		return ""
	}

	cfg := h.cfg
	// 原图压缩：任一边超过上限才处理，已够小的原图保持不动，
	// 避免无谓的二次有损压缩让画质变差。
	if cfg.ImageMaxEdge > 0 {
		resized, err := imageutil.Fit(data, cfg.ImageMaxEdge, cfg.ImageMaxEdge, 0)
		if err != nil {
			// 主要是 WebP / 尺寸超限 / 数据损坏。保留原图即可。
			if !errors.Is(err, imageutil.ErrWebPUnsupported) {
				log.Printf("[upload] 压缩原图失败 %s: %v", name, err)
			}
		} else if !bytes.Equal(resized, data) {
			// Fit 在「不需要缩放」时返回原数据副本，用 Equal 区分，
			// 避免把没变化的图又写一遍盘。
			mode := os.FileMode(0o644)
			if info, statErr := os.Stat(dst); statErr == nil {
				mode = info.Mode()
			}
			if err := os.WriteFile(dst, resized, mode); err != nil {
				log.Printf("[upload] 回写压缩图失败 %s: %v", name, err)
			}
		}
	}

	if cfg.ImageThumbEdge <= 0 {
		return ""
	}
	thumb, err := imageutil.Thumbnail(data, cfg.ImageThumbEdge)
	if err != nil {
		// WebP 是预期内的「不支持」，不算异常，不必刷错误日志
		if !errors.Is(err, imageutil.ErrWebPUnsupported) {
			log.Printf("[upload] 生成缩略图失败 %s: %v", name, err)
		}
		return ""
	}

	// 缩略图命名：photo.jpg → photo_thumb.jpg。
	// 与原图同目录，删除时按同样规则拼路径清理，不扫目录。
	ext := filepath.Ext(name)
	thumbName := strings.TrimSuffix(name, ext) + "_thumb" + ext
	thumbPath := filepath.Join(filepath.Dir(dst), thumbName)
	if err := os.WriteFile(thumbPath, thumb, 0o644); err != nil {
		log.Printf("[upload] 写入缩略图失败 %s: %v", thumbName, err)
		return ""
	}
	return strings.TrimSuffix(h.cfg.AbsoluteUploadBase(), "/") + "/uploads/" + thumbName
}
