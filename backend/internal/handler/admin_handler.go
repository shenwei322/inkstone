package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type AdminHandler struct {
	admin       *service.AdminService
	users       *repository.UserRepository
	articles    *service.ArticleService
	articleRepo *repository.ArticleRepository
	comments    *service.CommentService
	logs        *service.LogService
}

func NewAdminHandler(admin *service.AdminService, users *repository.UserRepository, articles *service.ArticleService, articleRepo *repository.ArticleRepository, comments *service.CommentService, logs *service.LogService) *AdminHandler {
	return &AdminHandler{admin: admin, users: users, articles: articles, articleRepo: articleRepo, comments: comments, logs: logs}
}

// Stats handles GET /admin/stats.
func (h *AdminHandler) Stats(c *gin.Context) {
	st, err := h.admin.Stats()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, st)
}

// ListUsers handles GET /admin/users?page=&page_size=&q=.
func (h *AdminHandler) ListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	users, total, err := h.users.List(page, pageSize, c.Query("q"))
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(users))
	for _, u := range users {
		items = append(items, gin.H{
			"id":         u.ID,
			"email":      u.Email,
			"username":   u.Username,
			"role":       u.Role,
			"status":     u.Status,
			"created_at": u.CreatedAt,
			// 登录失败锁定时刻。不带这个字段的话，管理员在用户列表里看不出
			// 「用户不是被封禁却登不进来」，也就不知道要去点「解锁」。
			// *time.Time 序列化为 ISO 字符串或 null，前端直接可用。
			"locked_until":       u.LockedUntil,
			"failed_login_count": u.FailedLoginCount,
		})
	}
	c.JSON(http.StatusOK, gin.H{"users": items, "total": total, "page": page, "page_size": pageSize})
}

type createUserRequest struct {
	Email    string `json:"email" binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Role     string `json:"role"`
}

// CreateUser handles POST /admin/users.
func (h *AdminHandler) CreateUser(c *gin.Context) {
	var req createUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写完整的用户信息"})
		return
	}

	user, err := h.admin.CreateUser(service.CreateUserInput{
		Email:    req.Email,
		Username: req.Username,
		Password: req.Password,
		Role:     req.Role,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryUser, "创建用户", fmt.Sprintf("用户名 %s（%s）", req.Username, req.Email), false)
		errorResponse(c, err)
		return
	}

	recordOp(h.logs, c, model.LogCategoryUser, "创建用户",
		fmt.Sprintf("%s（#%d，角色：%s）", user.Username, user.ID, user.Role), true)
	c.JSON(http.StatusCreated, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"email":    user.Email,
			"username": user.Username,
			"role":     user.Role,
			"status":   user.Status,
		},
	})
}

type updateStatusRequest struct {
	Status string `json:"status" binding:"required"`
} // UpdateUser handles PUT /admin/users/:id — 修改邮箱/用户名/密码。
func (h *AdminHandler) UpdateUser(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的用户 ID")
	if !ok {
		return
	}
	var req struct {
		Email    *string `json:"email"`
		Username *string `json:"username"`
		Password *string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	user, err := h.admin.UpdateUser(id, service.UpdateUserInput{
		Email:    req.Email,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryUser, "修改用户资料", fmt.Sprintf("用户 #%d", id), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryUser, "修改用户资料",
		fmt.Sprintf("%s（#%d）", user.Username, user.ID), true)
	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":       user.ID,
			"email":    user.Email,
			"username": user.Username,
			"role":     user.Role,
			"status":   user.Status,
		},
	})
}

// UpdateUserStatus handles PUT /admin/users/:id/status (ban/unban).
func (h *AdminHandler) UpdateUserStatus(c *gin.Context) {
	current, _ := middleware.GetCurrentUser(c)

	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}

	var req updateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 status 字段"})
		return
	}
	if current.ID == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能封禁自己的账号"})
		return
	}

	if err := h.admin.SetUserStatus(uint(id), req.Status); err != nil {
		recordOp(h.logs, c, model.LogCategoryUser, "修改用户状态", fmt.Sprintf("用户 #%d → %s", id, req.Status), false)
		errorResponse(c, err)
		return
	}
	action := "解禁用户"
	if req.Status == model.StatusBanned {
		action = "封禁用户"
	}
	recordOp(h.logs, c, model.LogCategoryUser, action, fmt.Sprintf("用户 #%d", id), true)
	c.JSON(http.StatusOK, gin.H{"message": "状态已更新"})
}

// UnlockUser handles POST /admin/users/:id/unlock — 解除登录失败锁定。
//
// 存在理由：连续 5 次密码错误会把账号锁 15 分钟。用户忘记密码又不去走
// 「忘记密码」流程时，唯一的出路就是找管理员解锁。不清零 TokenVersion——
// 解锁只是解除登录限制，不该顺手把所有设备踢下线。
func (h *AdminHandler) UnlockUser(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if err := h.admin.UnlockUser(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryUser, "解锁用户", fmt.Sprintf("用户 #%d", id), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryUser, "解锁用户", fmt.Sprintf("用户 #%d 已解除登录锁定", id), true)
	c.JSON(http.StatusOK, gin.H{"message": "已解除该账号的登录锁定"})
}

type updateRoleRequest struct {
	Role string `json:"role" binding:"required"`
}

// UpdateUserRole handles PUT /admin/users/:id/role.
func (h *AdminHandler) UpdateUserRole(c *gin.Context) {
	current, _ := middleware.GetCurrentUser(c)

	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}

	var req updateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 role 字段"})
		return
	}
	if req.Role != model.RoleAdmin && req.Role != model.RoleUser {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的角色值"})
		return
	}
	if current.ID == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能修改自己的角色"})
		return
	}

	// 走 service 层（分层约定：handler 不直调 repository）
	if err := h.admin.UpdateUserRole(uint(id), req.Role); err != nil {
		recordOp(h.logs, c, model.LogCategoryUser, "修改用户角色", fmt.Sprintf("用户 #%d → %s", id, req.Role), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryUser, "修改用户角色",
		fmt.Sprintf("用户 #%d → %s", id, req.Role), true)
	c.JSON(http.StatusOK, gin.H{"message": "角色已更新"})
}

// DeleteUser handles DELETE /admin/users/:id. Also removes the user's articles.
func (h *AdminHandler) DeleteUser(c *gin.Context) {
	current, _ := middleware.GetCurrentUser(c)

	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if current.ID == uint(id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能删除自己的账号"})
		return
	}

	// 删文章 + 删用户在 service 层的同一事务内完成：此前是两个独立事务，
	// 第二步失败会留下「文章已全删但用户还在」的不一致状态。
	if err := h.admin.DeleteUserWithArticles(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryUser, "删除用户", fmt.Sprintf("用户 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryUser, "删除用户",
		fmt.Sprintf("用户 #%d（连同其文章一并删除）", id), true)
	c.Status(http.StatusNoContent)
}

// ListArticles handles GET /admin/articles?status=&page=&page_size= (all statuses).
func (h *AdminHandler) ListArticles(c *gin.Context) {
	q := repository.ArticleQuery{
		Page:     parseIntOr(c.Query("page"), 1),
		PageSize: parseIntOr(c.Query("page_size"), 20),
		Status:   c.Query("status"),
		All:      c.Query("status") == "",
	}
	articles, total, err := h.articles.List(q)
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]articleResponse, 0, len(articles))
	for i := range articles {
		items = append(items, toArticleResponse(&articles[i]))
	}
	c.JSON(http.StatusOK, gin.H{"articles": items, "total": total, "page": q.Page, "page_size": q.PageSize})
}

// SetArticleStatus handles PUT /admin/articles/:id/status.
func (h *AdminHandler) SetArticleStatus(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}

	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 status 字段"})
		return
	}

	article, err := h.admin.SetArticleStatus(uint(id), req.Status)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "修改文章状态", fmt.Sprintf("文章 #%d → %s", id, req.Status), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "修改文章状态",
		fmt.Sprintf("《%s》（#%d → %s）", article.Title, id, statusLabel(article.Status)), true)
	c.JSON(http.StatusOK, gin.H{"article": toArticleResponse(article)})
}

// BulkDeleteArticles handles POST /admin/articles/bulk-delete.
// Body: {"ids": [1,2,3]}
func (h *AdminHandler) BulkDeleteArticles(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供要删除的文章 ID 列表"})
		return
	}
	count, err := h.admin.BulkDeleteArticles(req.IDs)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "批量删除文章",
			fmt.Sprintf("%d 篇", len(req.IDs)), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "批量删除文章",
		fmt.Sprintf("共 %d 篇（进入回收站）", count), true)
	// 返回 affected 而非回显全部文章：批量操作后前端只需要一个数字
	// 来刷新计数，回显 N 篇文章的完整对象是白传几 KB 数据。
	c.JSON(http.StatusOK, gin.H{"affected": count})
}

// BulkSetArticleStatus handles POST /admin/articles/bulk-status.
// Body: {"ids": [1,2,3], "status": "published"}
func (h *AdminHandler) BulkSetArticleStatus(c *gin.Context) {
	var req struct {
		IDs    []uint `json:"ids" binding:"required"`
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供要操作的文章 ID 列表与目标状态"})
		return
	}
	count, err := h.admin.BulkSetArticleStatus(req.IDs, req.Status)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "批量修改文章状态",
			fmt.Sprintf("%d 篇 → %s", len(req.IDs), req.Status), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "批量修改文章状态",
		fmt.Sprintf("共 %d 篇 → %s", count, statusLabel(req.Status)), true)
	c.JSON(http.StatusOK, gin.H{"affected": count})
}

// ListComments handles GET /admin/comments.
func (h *AdminHandler) ListComments(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	comments, total, err := h.comments.ListAll(page, pageSize)
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(comments))
	for i := range comments {
		items = append(items, toCommentResponse(&comments[i]))
	}
	c.JSON(http.StatusOK, gin.H{"comments": items, "total": total, "page": page, "page_size": pageSize})
}

// SetCommentStatus handles PUT /admin/comments/:id/status.
// body: {"status": "approved" | "rejected"}
//
// 只有 pending 状态的评论出现在待审队列里，但接口不限制源状态：
// 管理员把已通过的评论改回驳回、或反过来，都是合法操作。
func (h *AdminHandler) SetCommentStatus(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请指定审核状态（approved 或 rejected）"})
		return
	}
	if err := h.comments.SetStatus(uint(id), req.Status); err != nil {
		recordOp(h.logs, c, model.LogCategoryComment, "审核评论", fmt.Sprintf("评论 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "评论不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	verb := "通过评论"
	if req.Status == model.CommentRejected {
		verb = "驳回评论"
	}
	recordOp(h.logs, c, model.LogCategoryComment, verb, fmt.Sprintf("评论 #%d", id), true)
	c.JSON(http.StatusOK, gin.H{"ok": true, "status": req.Status})
}

// PendingCommentCount handles GET /admin/comments/pending-count.
// 后台侧边栏「评论」菜单上的待审角标用。
func (h *AdminHandler) PendingCommentCount(c *gin.Context) {
	count, err := h.comments.CountPending()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"pending": count})
}

// DeleteComment handles DELETE /admin/comments/:id.
func (h *AdminHandler) DeleteComment(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if err := h.comments.DeleteAny(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryComment, "删除评论", fmt.Sprintf("评论 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "评论不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryComment, "删除评论", fmt.Sprintf("评论 #%d", id), true)
	c.Status(http.StatusNoContent)
}

// ListTrashArticles handles GET /admin/articles/trash — 回收站列表。
func (h *AdminHandler) ListTrashArticles(c *gin.Context) {
	page := parseIntQuery(c, "page", 1)
	pageSize := parseIntQuery(c, "page_size", 20)
	articles, total, err := h.articles.Trash(page, pageSize)
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(articles))
	for i := range articles {
		a := &articles[i]
		item := gin.H{
			"id":         a.ID,
			"title":      a.Title,
			"slug":       a.Slug,
			"status":     a.Status,
			"deleted_at": a.DeletedAt.Time,
			"author":     gin.H{"id": a.Author.ID, "username": a.Author.Username},
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"articles": items, "total": total, "page": page, "page_size": pageSize})
}

// RestoreArticle handles POST /admin/articles/:id/restore — 从回收站还原。
func (h *AdminHandler) RestoreArticle(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if err := h.articles.Restore(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "还原文章", fmt.Sprintf("文章 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "还原文章", fmt.Sprintf("文章 #%d 已从回收站还原", id), true)
	c.JSON(http.StatusOK, gin.H{"message": "文章已还原"})
}

// PurgeArticle handles DELETE /admin/articles/:id/purge — 彻底删除。
func (h *AdminHandler) PurgeArticle(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if err := h.articles.Purge(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "彻底删除文章", fmt.Sprintf("文章 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "彻底删除文章",
		fmt.Sprintf("文章 #%d 已彻底删除（评论与点赞一并清除）", id), true)
	c.Status(http.StatusNoContent)
}

// DeleteArticle handles DELETE /admin/articles/:id (any article).
//
// 现在是软删除：文章进入回收站，可在 30 天内还原。彻底删除走
// DELETE /admin/articles/:id/purge。
func (h *AdminHandler) DeleteArticle(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	// 删除前先取标题，让审计日志记录删的是哪篇文章
	title := ""
	if article, err := h.articles.GetByID(uint(id)); err == nil {
		title = article.Title
	}
	if err := h.articleRepo.DeleteAny(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "删除文章", fmt.Sprintf("文章 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "删除文章",
		fmt.Sprintf("《%s》（#%d）", title, id), true)
	c.Status(http.StatusNoContent)
}
