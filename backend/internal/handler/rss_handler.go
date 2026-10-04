package handler

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type RSSHandler struct {
	articles    *service.ArticleService
	frontendURL string
	siteName    string
	// settings 只用来取站点名与站点描述。允许为 nil（测试/老调用方），
	// 此时回落到 siteName 的默认值。
	settings *service.SettingsService
}

func NewRSSHandler(articles *service.ArticleService, frontendURL string, settings *service.SettingsService) *RSSHandler {
	return &RSSHandler{articles: articles, frontendURL: frontendURL, siteName: "InkStone", settings: settings}
}

type rssItem struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        string   `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Description string   `xml:"description"`
	// Content 是 content:encoded（RSS 1.0 Content 模块的命名空间属性）。
	// 此前只输出 Description（剥标签后截断 300 字的纯文本摘要），全文阅读器
	// 用户必须跳回站点才能看正文——订阅就失去了意义。补上全文后，
	// Reeder / Inoreader / FreshRSS 等都能离线读完。
	//
	// 正文存的是已消毒的 HTML（保存时过 bluemonday），此处原样输出即可；
	// Go 的 xml.Marshal 会自行转义 & < > 等字符。
	Content string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

type rssChannel struct {
	XMLName     xml.Name  `xml:"channel"`
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language"`
	LastBuild   string    `xml:"lastBuildDate"`
	Items       []rssItem `xml:"item"`
}

type rssFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

// Feed handles GET /feed.xml — RSS 2.0 feed of the latest published articles.
func (h *RSSHandler) Feed(c *gin.Context) {
	articles, _, err := h.articles.List(repository.ArticleQuery{
		Status:   "published",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		errorResponse(c, err)
		return
	}

	items := make([]rssItem, 0, len(articles))
	for _, a := range articles {
		pub := a.PublishedAt
		if pub == nil {
			pub = &a.CreatedAt
		}
		plain := stripHTMLTags(a.Content)
		if len([]rune(plain)) > 300 {
			plain = string([]rune(plain)[:300]) + "..."
		}
		items = append(items, rssItem{
			Title:       a.Title,
			Link:        h.frontendURL + "/posts/" + a.Slug,
			GUID:        h.frontendURL + "/posts/" + a.Slug,
			PubDate:     pub.Format(time.RFC1123Z),
			Description: plain,
			Content:     a.Content,
		})
	}

	// 频道标题用后台设置的站点名：此前硬编码 "InkStone"，改了站点名的站点
	// 在阅读器里仍然显示 InkStone，用户会以为是订阅错了源。
	description := "最新文章订阅"
	if h.settings != nil {
		if name, err := h.settings.Get(service.SettingSiteName); err == nil && strings.TrimSpace(name) != "" {
			h.siteName = strings.TrimSpace(name)
		}
		if desc, err := h.settings.Get(service.SettingSiteDescription); err == nil && strings.TrimSpace(desc) != "" {
			description = strings.TrimSpace(desc)
		}
	}

	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:       h.siteName,
			Link:        h.frontendURL,
			Description: description,
			Language:    "zh-CN",
			LastBuild:   time.Now().Format(time.RFC1123Z),
			Items:       items,
		},
	}

	output, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.Data(http.StatusOK, "application/rss+xml; charset=utf-8", append([]byte(xml.Header), output...))
}

func stripHTMLTags(s string) string {
	out := make([]byte, 0, len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out = append(out, ch)
		}
	}
	return string(out)
}
