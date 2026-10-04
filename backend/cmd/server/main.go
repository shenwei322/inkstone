package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/handler"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
	"github.com/shenwei/inkstone/backend/pkg/config"
	"github.com/shenwei/inkstone/backend/pkg/mailer"
)

func main() {
	cfg := config.Load()

	db := repository.NewDB()

	// JWT 密钥强度提示：生产环境弱密钥可被离线爆破，所有令牌会同时失效
	if len(cfg.JWTSecret) < 32 {
		log.Printf("[warn] JWT_SECRET 长度 %d < 32，生产环境请使用更长的随机密钥（openssl rand -hex 32）", len(cfg.JWTSecret))
	}

	tokens := service.NewTokenManager(cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)

	userRepo := repository.NewUserRepository(db)
	articleRepo := repository.NewArticleRepository(db)
	taxonomyRepo := repository.NewTaxonomyRepository(db)
	commentRepo := repository.NewCommentRepository(db)
	reactionRepo := repository.NewReactionRepository(db)

	settingsSvc := service.NewSettingsService(db)
	mailer := mailer.New(settingsSvc)

	authSvc := service.NewAuthService(userRepo, tokens, settingsSvc)
	articleSvc := service.NewArticleService(articleRepo, taxonomyRepo)
	adminSvc := service.NewAdminService(userRepo, articleRepo)
	commentSvc := service.NewCommentService(commentRepo, articleRepo)
	reactionSvc := service.NewReactionService(reactionRepo, articleRepo)

	pageRepo := repository.NewPageRepository(db)
	pageSvc := service.NewPageService(pageRepo)

	linkRepo := repository.NewLinkRepository(db)
	linkSvc := service.NewLinkService(linkRepo, cfg.FrontendURL)
	linkSvc.StartAutoCheck()

	linkAppRepo := repository.NewLinkApplicationRepository(db)
	linkAppSvc := service.NewLinkApplicationService(linkAppRepo, linkRepo, settingsSvc)

	fileRepo := repository.NewFileRepository(db)
	fileSvc := service.NewFileService(fileRepo, settingsSvc, cfg.FilesDir)

	statRepo := repository.NewStatRepository(db)
	statSvc := service.NewStatService(statRepo, settingsSvc)
	logRepo := repository.NewOperationLogRepository(db)
	logSvc := service.NewLogService(logRepo)

	emailCodeSvc := service.NewEmailCodeService(settingsSvc, mailer)
	geetestSvc := service.NewGeetestService(settingsSvc)
	lapSvc := service.NewLapService(settingsSvc)
	powSvc := service.NewPowService(settingsSvc)
	captchaSvc := service.NewCaptchaService(settingsSvc, geetestSvc, lapSvc, powSvc)
	apiLimiter := middleware.NewSlidingLimiter()

	taxonomySvc := service.NewTaxonomyService(taxonomyRepo)
	authHandler := handler.NewAuthHandler(authSvc, emailCodeSvc, captchaSvc, apiLimiter, logSvc)
	articleHandler := handler.NewArticleHandler(articleSvc, logSvc)
	adminHandler := handler.NewAdminHandler(adminSvc, userRepo, articleSvc, articleRepo, commentSvc, logSvc)
	taxonomyHandler := handler.NewTaxonomyHandler(taxonomyRepo)
	commentHandler := handler.NewCommentHandler(commentSvc, tokens, captchaSvc, apiLimiter, logSvc)
	reactionHandler := handler.NewReactionHandler(reactionSvc)
	rssHandler := handler.NewRSSHandler(articleSvc, cfg.FrontendURL)
	sitemapHandler := handler.NewSitemapHandler(articleSvc, pageSvc, taxonomyRepo, cfg.FrontendURL)
	settingsHandler := handler.NewSettingsHandler(settingsSvc, mailer, emailCodeSvc, captchaSvc, logSvc)
	lapProxyHandler := handler.NewLapProxyHandler(settingsSvc)
	powHandler := handler.NewPowHandler(powSvc)
	pageHandler := handler.NewPageHandler(pageSvc, logSvc)
	systemHandler := handler.NewSystemHandler(settingsSvc, cfg.UpdateSourceDir)
	linkHandler := handler.NewLinkHandler(linkSvc, logSvc)
	linkAppHandler := handler.NewLinkApplicationHandler(linkAppSvc, captchaSvc, logSvc)
	fileHandler := handler.NewFileHandler(fileSvc, cfg.PublicAPIURL, logSvc)
	statHandler := handler.NewStatHandler(statSvc)
	logHandler := handler.NewLogHandler(logSvc)
	emailCodeHandler := handler.NewEmailCodeHandler(emailCodeSvc)
	adminTagHandler := handler.NewAdminTagHandler(taxonomySvc, logSvc)

	// 在线更新：检查上游提交、下载镜像包、替换源码、触发重建
	updateSvc := service.NewUpdateService(cfg, logSvc)
	updateHandler := handler.NewUpdateHandler(updateSvc, logSvc)
	updateSource := "commits（提交 + 源码包）"
	if cfg.UpdateSource == config.UpdateSourceReleases {
		updateSource = "releases（GitHub Releases + 镜像包）"
	}
	if commit, _, _ := service.ResolveRunningCommit(cfg.UpdateSourceDir); commit != "" {
		log.Printf("[update] 当前运行版本 %s（源码目录 %s，更新源 %s，模式 %s）",
			service.ShortCommit(commit), cfg.UpdateSourceDir, updateSource, updateSvc.Mode())
	} else {
		log.Printf("[update] 未取得运行版本记录（源码目录 %s，更新源 %s，模式 %s）",
			cfg.UpdateSourceDir, updateSource, updateSvc.Mode())
	}

	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	// 可信代理网段：决定 X-Forwarded-For 是否被采信。
	//
	// 不配置时 gin 默认信任「所有」代理，任何客户端都能靠伪造 XFF 绕过全部
	// IP 限流（实测逐请求换 XFF 可 200/200 全放行）。这里显式收窄到配置的
	// 网段，默认只含本机与私网，覆盖同机/同容器网络的 Nginx 反代。
	middleware.SetTrustedProxies(cfg.TrustedProxies)
	if err := router.SetTrustedProxies(middleware.TrustedProxies()); err != nil {
		log.Printf("[warn] 设置可信代理失败（回退为不信任任何代理）: %v", err)
	}
	{
		proxies := middleware.TrustedProxies()
		if len(proxies) == 0 {
			// 不信任任何代理：一律按 TCP 对端判定访客 IP。
			// 直连部署下这是最安全的配置；若前面确实有反代，限流会把所有
			// 访客算成同一个人，此时应通过 TRUSTED_PROXIES 指定反代网段。
			log.Printf("[security] 未配置可信代理，访客 IP 一律取 TCP 对端地址。" +
				"若部署在反向代理之后，请设置 TRUSTED_PROXIES=<反代 CIDR>，否则 IP 限流会误伤全部访客")
		} else {
			log.Printf("[security] 可信代理网段: %s（仅这些来源的 X-Forwarded-For 会被采信）",
				strings.Join(proxies, ", "))
		}
	}
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(middleware.SecurityHeaders())
	router.Use(middleware.TrafficStats(statSvc))
	// 允许的源：访客站 + 独立管理后台（后台跨源部署时携带 Authorization 请求）
	allowedOrigins := []string{cfg.FrontendURL}
	if cfg.AdminURL != "" && !strings.EqualFold(cfg.AdminURL, cfg.FrontendURL) {
		allowedOrigins = append(allowedOrigins, cfg.AdminURL)
	}
	// dev 便利：本机 127.0.0.1 与 localhost 等价。用户换 IP 写法访问（127.0.0.1:3001）
	// 时 Origin 不在白名单，浏览器 CORS 会拦截登录/带鉴权请求（表现为前端「登录失败」兜底）。
	// 仅对默认 localhost 源追加变体，生产域名不受影响。
	for _, u := range []string{cfg.FrontendURL, cfg.AdminURL} {
		v := strings.Replace(u, "localhost", "127.0.0.1", 1)
		if v == "" || v == u {
			continue
		}
		dup := false
		for _, o := range allowedOrigins {
			if strings.EqualFold(o, v) {
				dup = true
				break
			}
		}
		if !dup {
			allowedOrigins = append(allowedOrigins, v)
		}
	}
	router.Use(middleware.CORS(allowedOrigins))
	router.MaxMultipartMemory = 12 << 20

	securityEnabled := func() bool {
		return settingsSvc.BoolValue(service.SettingSecurityEnabled, true)
	}
	rateLimitMiddle := middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: apiLimiter,
		LimitFn: func() int {
			if !securityEnabled() {
				return 0
			}
			return settingsSvc.IntValue(service.SettingSecurityAPIMax, 300)
		},
		Window:  time.Minute,
		Message: "api",
	})
	loginLimit := middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: apiLimiter,
		LimitFn: func() int {
			if !securityEnabled() {
				return 0
			}
			return settingsSvc.IntValue(service.SettingSecurityLoginMax, 10)
		},
		Window:  15 * time.Minute,
		Message: "login",
	})
	registerLimit := middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: apiLimiter,
		LimitFn: func() int {
			if !securityEnabled() {
				return 0
			}
			return settingsSvc.IntValue(service.SettingSecurityRegisterMax, 5)
		},
		Window:  time.Hour,
		Message: "register",
	})
	// POW 挑战签发限流：防刷 challenge 池（正常登录一次只领 1 个）
	powChallengeLimit := middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: apiLimiter,
		LimitFn: func() int {
			if !securityEnabled() {
				return 0
			}
			return 30
		},
		Window:  time.Minute,
		Message: "pow-challenge",
	})
	articleLimit := middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: apiLimiter,
		LimitFn: func() int {
			if !securityEnabled() {
				return 0
			}
			return settingsSvc.IntValue(service.SettingSecurityCommentMax, 10)
		},
		Window:  10 * time.Minute,
		Message: "article",
	})
	commentLimit := middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: apiLimiter,
		LimitFn: func() int {
			if !securityEnabled() {
				return 0
			}
			return settingsSvc.IntValue(service.SettingSecurityCommentMax, 10)
		},
		Window:  10 * time.Minute,
		Message: "comment",
	})

	// 每次带鉴权请求都回查数据库：封禁/删除的账号立即失效，且**角色以库为准**。
	// 若角色沿用 JWT 里的旧 claim，管理员被降级后在 refresh TTL 内仍持有
	// admin 权限（RequireRole 比对的正是该 claim）。
	userStatusOK := func(id uint) (middleware.UserStatus, bool) {
		u, err := userRepo.FindByID(id)
		if err != nil || u.IsBanned() {
			return middleware.UserStatus{}, false
		}
		return middleware.UserStatus{Username: u.Username, Role: u.Role}, true
	}

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	router.GET("/feed.xml", rssHandler.Feed)
	router.GET("/sitemap.xml", sitemapHandler.Sitemap)
	router.GET("/robots.txt", sitemapHandler.Robots)
	router.Static("/uploads", cfg.UploadDir)
	router.Static("/files", cfg.FilesDir)

	uploadsHandler := handler.NewUploadsHandler(cfg)

	api := router.Group("/api/v1", rateLimitMiddle)
	{
		uploads := api.Group("/uploads", middleware.Auth(tokens, userStatusOK))
		{
			uploads.POST("", uploadsHandler.Create)
		}

		auth := api.Group("/auth")
		{
			auth.POST("/register", registerLimit, authHandler.Register)
			auth.POST("/email-code", registerLimit, emailCodeHandler.Send)
			auth.POST("/login", loginLimit, authHandler.Login)
			auth.POST("/refresh", authHandler.Refresh)
			auth.GET("/me", middleware.Auth(tokens, userStatusOK), authHandler.Me)
			authAuthed := api.Group("/auth", middleware.Auth(tokens, userStatusOK))
			{
				authAuthed.PUT("/password", authHandler.ChangePassword)
				authAuthed.PUT("/profile", authHandler.UpdateProfile)
				authAuthed.GET("/my-comments", commentHandler.MyComments)
			}
		}

		api.GET("/system/info", systemHandler.Info)

		api.GET("/categories", taxonomyHandler.ListCategories)
		api.GET("/tags", taxonomyHandler.ListTags)
		api.GET("/site-config", settingsHandler.SiteConfig)
		// Lap（工作量证明验证码）同源代理：访客浏览器不直连 workers.dev，
		// 规避 DNS 污染 / 超时（白名单：widget.js、wasm、challenge、redeem）
		api.Any("/lap/*path", lapProxyHandler.Proxy)
		// POW（自研工作量证明）挑战签发：零外部依赖，服务器/本机通用。
		// 挑战一次性消费，签发走 IP 限流防刷池。
		api.POST("/pow/challenge", powChallengeLimit, powHandler.Challenge)
		api.GET("/pages", pageHandler.ListPublic)
		api.GET("/pages/:slug", pageHandler.GetBySlug)
		api.GET("/links", linkHandler.ListPublic)

		// 友链自助申请（公开，验证码场景=comment，独立 IP 限流 5 次/小时）
		friendApplyLimit := middleware.IPRateLimit(middleware.RateLimitConfig{
			Limiter: apiLimiter,
			LimitFn: func() int {
				if !securityEnabled() {
					return 0
				}
				return 5
			},
			Window:  time.Hour,
			Message: "friend-apply",
		})
		api.POST("/link-applications", friendApplyLimit, linkAppHandler.Submit)

		articles := api.Group("/articles", middleware.OptionalAuth(tokens, userStatusOK))
		{
			articles.GET("", articleHandler.List)
			articles.GET("/:id", articleHandler.Get)
			articles.GET("/slug/:slug", articleHandler.GetBySlug)
			articles.GET("/:id/comments", commentHandler.List)
			articles.GET("/:id/reactions", reactionHandler.Stats)

			authed := articles.Group("", middleware.Auth(tokens, userStatusOK))
			{
				authed.POST("", articleLimit, articleHandler.Create)
				// 更新同样受限流约束：否则可以「建草稿 → 反复更新为已发布」
				// 绕过 Create 的限流（此前 Update 既无限流也无验证码）。
				authed.PUT("/:id", articleLimit, articleHandler.Update)
				authed.DELETE("/:id", articleHandler.Delete)
				authed.POST("/:id/comments", commentLimit, commentHandler.Create)
				authed.POST("/:id/reactions", reactionHandler.Toggle)
			}
		}

		comments := api.Group("/comments", middleware.Auth(tokens, userStatusOK))
		{
			comments.DELETE("/:id", commentHandler.Delete)
		}

		admin := api.Group("/admin", middleware.Auth(tokens, userStatusOK), middleware.RequireRole(model.RoleAdmin))
		{
			admin.GET("/stats", adminHandler.Stats)
			admin.GET("/stats/traffic", statHandler.Traffic)
			admin.GET("/stats/resources", statHandler.Resources)
			admin.GET("/logs", logHandler.List)
			admin.GET("/logs/overview", logHandler.Overview)
			admin.GET("/logs/stats", logHandler.Stats)
			admin.GET("/logs/export", logHandler.Export)
			admin.GET("/users", adminHandler.ListUsers)
			admin.POST("/users", adminHandler.CreateUser)
			admin.PUT("/users/:id/role", adminHandler.UpdateUserRole)
			admin.PUT("/users/:id", adminHandler.UpdateUser)
			admin.PUT("/users/:id/status", adminHandler.UpdateUserStatus)
			admin.DELETE("/users/:id", adminHandler.DeleteUser)
			admin.GET("/articles", adminHandler.ListArticles)
			admin.PUT("/articles/:id/status", adminHandler.SetArticleStatus)
			admin.DELETE("/articles/:id", adminHandler.DeleteArticle)
			admin.POST("/tags", adminTagHandler.Create)
			admin.PUT("/tags/:id", adminTagHandler.Update)
			admin.DELETE("/tags/:id", adminTagHandler.Delete)
			admin.GET("/comments", adminHandler.ListComments)
			admin.DELETE("/comments/:id", adminHandler.DeleteComment)
			admin.GET("/settings", settingsHandler.Get)
			admin.PUT("/settings", settingsHandler.Update)
			admin.POST("/settings/test-mail", settingsHandler.TestMail)
			admin.GET("/links", linkHandler.ListAdmin)
			admin.POST("/links", linkHandler.Create)
			admin.PUT("/links/:id", linkHandler.Update)
			admin.DELETE("/links/:id", linkHandler.Delete)
			admin.POST("/links/check", linkHandler.CheckAll)
			admin.POST("/links/validate", linkHandler.Validate)
			admin.POST("/links/:id/check", linkHandler.CheckOne)
			admin.GET("/link-applications", linkAppHandler.ListAdmin)
			admin.POST("/link-applications/:id/approve", linkAppHandler.Approve)
			admin.POST("/link-applications/:id/reject", linkAppHandler.Reject)
			admin.DELETE("/link-applications/:id", linkAppHandler.Delete)
			admin.GET("/files", fileHandler.List)
			admin.POST("/files", fileHandler.Upload)
			admin.GET("/files/:id/download", fileHandler.Download)
			admin.DELETE("/files/:id", fileHandler.Delete)
			admin.GET("/sitemap", sitemapHandler.SiteMapData)
			admin.GET("/pages", pageHandler.ListAll)
			admin.POST("/pages", pageHandler.Create)
			admin.GET("/pages/:id", pageHandler.Get)
			admin.PUT("/pages/:id", pageHandler.Update)
			admin.DELETE("/pages/:id", pageHandler.Delete)

			// 系统更新（高危：替换源码 + 重建重启，全程记入系统类操作日志）
			admin.GET("/system/update", updateHandler.Status)
			admin.POST("/system/update/check", updateHandler.Check)
			admin.POST("/system/update/apply", updateHandler.Apply)
			admin.POST("/system/update/rollback", updateHandler.Rollback)
		}
	}

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router}
	go func() {
		log.Printf("server listening on :%s (env=%s)", cfg.Port, cfg.AppEnv)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("failed to start server: %v", err)
		}
	}()

	// 优雅停机：容器重启（SIGTERM）时先停止接收新请求并等待在途请求完成。
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}
