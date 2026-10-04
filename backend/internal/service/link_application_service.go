package service

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// friendApply 相关限制。
const (
	friendApplyNameMax = 50  // 站点名称上限
	friendApplyDescMax = 120 // 简介上限
	friendApplyURLMax  = 500 // URL 长度上限
)

type LinkApplicationService struct {
	apps     *repository.LinkApplicationRepository
	links    *repository.LinkRepository
	settings *SettingsService
}

func NewLinkApplicationService(
	apps *repository.LinkApplicationRepository,
	links *repository.LinkRepository,
	settings *SettingsService,
) *LinkApplicationService {
	return &LinkApplicationService{apps: apps, links: links, settings: settings}
}

// ApplyEnabled 是否开放友链自助申请（默认开放）。
func (s *LinkApplicationService) ApplyEnabled() bool {
	if s.settings == nil {
		return true
	}
	return s.settings.BoolValue(SettingFriendApplyEnabled, true)
}

type LinkApplicationInput struct {
	SiteName    string
	URL         string
	Description string
	IconURL     string
	Email       string
}

// normalizeApplyURL 规范化站点地址：小写 scheme/host、去默认端口与尾斜杠。
func normalizeApplyURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Hostname())
	if u.Path == "/" || u.Path == "" {
		u.Path = ""
	} else {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

// canSubmitURL 是否允许提交：无 pending 申请、且不是已有友链。
func (s *LinkApplicationService) canSubmitURL(normalized string) error {
	if existing, err := s.apps.FindActiveByURL(normalized); err != nil {
		return err
	} else if existing != nil {
		return NewValidationError("该站点已有审核中的申请，请耐心等待")
	}
	if links, err := s.links.List(); err == nil {
		for i := range links {
			if normalizeApplyURL(links[i].URL) == normalized {
				return NewValidationError("该站点已是本站友情链接")
			}
		}
	}
	return nil
}

// Submit 访客提交友链申请（公开接口，由 handler 层做人机验证与限流）。
func (s *LinkApplicationService) Submit(input LinkApplicationInput, clientIP string) (*model.FriendLinkApplication, error) {
	if !s.ApplyEnabled() {
		return nil, NewValidationError("站长暂未开放友链申请")
	}
	siteName := strings.TrimSpace(input.SiteName)
	rawURL := strings.TrimSpace(input.URL)
	description := strings.TrimSpace(input.Description)
	iconURL := strings.TrimSpace(input.IconURL)
	email := strings.TrimSpace(input.Email)

	if siteName == "" {
		return nil, NewValidationError("请填写站点名称")
	}
	if l := len([]rune(siteName)); l > friendApplyNameMax {
		return nil, NewValidationError("站点名称不能超过 50 个字符")
	}
	if l := len([]rune(description)); l > friendApplyDescMax {
		return nil, NewValidationError("简介不能超过 120 个字符")
	}
	if rawURL == "" {
		return nil, NewValidationError("请填写站点地址")
	}
	if len([]rune(rawURL)) > friendApplyURLMax {
		return nil, NewValidationError("站点地址过长")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, NewValidationError("站点地址需以 http:// 或 https:// 开头")
	}
	normalized := normalizeApplyURL(rawURL)
	if _, err := url.ParseRequestURI(normalized); err != nil {
		return nil, NewValidationError("站点地址格式不正确")
	}
	// 友链检测会让服务端主动请求该地址，因此这里先挡掉内网/保留地址
	// （第二道防线在探测传输层的 DialContext，可防 DNS rebinding）。
	if err := RejectPrivateHost(normalized); err != nil {
		return nil, err
	}
	if email != "" && !emailRegex.MatchString(email) {
		return nil, NewValidationError("联系邮箱格式不正确")
	}

	// 重复提交防护：规范化 URL 比对（pending / 已是友链）
	if err := s.canSubmitURL(normalized); err != nil {
		return nil, err
	}

	app := &model.FriendLinkApplication{
		SiteName:    siteName,
		URL:         rawURL,
		Description: description,
		IconURL:     iconURL,
		Email:       email,
		Status:      model.LinkAppPending,
		IPHash:      hashIP(clientIP),
	}
	if err := s.apps.Create(app); err != nil {
		return nil, err
	}
	return app, nil
}

// hashIP 对提交 IP 做 SHA256（仅防滥用追溯，不存原始地址）。
func hashIP(ip string) string {
	if ip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])
}

// ListAdmin 审核列表（status 为空返回全部，新的在前）。
func (s *LinkApplicationService) ListAdmin(status string) ([]model.FriendLinkApplication, error) {
	return s.apps.List(status)
}

// PendingCount 待审核数量。
func (s *LinkApplicationService) PendingCount() (int64, error) {
	return s.apps.PendingCount()
}

// Approve 通过申请：转为正式友链 + 标记已审核。
func (s *LinkApplicationService) Approve(id uint, operatorID uint) (*model.FriendLinkApplication, error) {
	app, err := s.apps.FindByID(id)
	if err != nil {
		return nil, err
	}
	// 这一步只用于尽早给出友好提示；真正的并发闸门在后面 ClaimForReview。
	if app.Status != model.LinkAppPending {
		return nil, NewValidationError("该申请已处理过")
	}

	link := &model.FriendLink{
		Name:        app.SiteName,
		URL:         app.URL,
		CheckURL:    app.URL,
		IconURL:     app.IconURL,
		Description: app.Description,
		Available:   true,
	}
	if err := s.links.Create(link); err != nil {
		return nil, err
	}

	// 抢占式改状态：WHERE 带 status = pending，由数据库完成「检查+修改」。
	// 双击审核时两个请求都会走到这里，只有一个能抢占成功；失败的把刚建的
	// 友链撤掉，避免同一条申请产生重复友链。
	claimed, err := s.apps.ClaimForReview(id, model.LinkAppApproved, operatorID, "")
	if err != nil {
		_ = s.links.Delete(link.ID)
		return nil, err
	}
	if !claimed {
		_ = s.links.Delete(link.ID)
		return nil, NewValidationError("该申请已处理过")
	}

	now := time.Now()
	app.Status = model.LinkAppApproved
	app.Reason = ""
	app.ReviewedBy = operatorID
	app.ReviewedAt = &now

	// 建链后后台探测可达性（异步，带 panic 保护）
	go func(linkID uint) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[link-apply] 审核后探测 panic recovered: %v", r)
			}
		}()
		svc := NewLinkService(s.links, "")
		_, _ = svc.CheckOne(linkID)
	}(link.ID)

	return app, nil
}

// Reject 拒绝申请（reason 为拒绝原因，展示给申请人）。
func (s *LinkApplicationService) Reject(id uint, operatorID uint, reason string) (*model.FriendLinkApplication, error) {
	app, err := s.apps.FindByID(id)
	if err != nil {
		return nil, err
	}
	if app.Status != model.LinkAppPending {
		return nil, NewValidationError("该申请已处理过")
	}
	reason = strings.TrimSpace(reason)
	// 与 Approve 同一套抢占式闸门：双击拒绝时两个请求都能读到 pending，
	// 后写的那个会把前一个的拒绝原因覆盖掉。
	claimed, err := s.apps.ClaimForReview(id, model.LinkAppRejected, operatorID, reason)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, NewValidationError("该申请已处理过")
	}
	now := time.Now()
	app.Status = model.LinkAppRejected
	app.Reason = reason
	app.ReviewedBy = operatorID
	app.ReviewedAt = &now
	return app, nil
}

// Delete 删除申请记录。
func (s *LinkApplicationService) Delete(id uint) error {
	return s.apps.Delete(id)
}
