package service

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// sanitizePolicy 是面向富文本（TipTap 编辑器输出）的净化策略：
// 允许常见的排版标签，但移除 <script>、事件属性（on*）、javascript: 链接等危险内容，
// 防止通过文章/页面正文注入存储型 XSS。
var sanitizePolicy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// UGC 策略已允许大部分常用标签，这里补充完整排版支持
	p.AllowAttrs("class").Globally()
	p.AllowAttrs("id").Globally()
	// 允许相对/站内图片与外部图片
	p.AllowAttrs("src").OnElements("img")
	p.AllowAttrs("alt", "width", "height").OnElements("img")
	// 表格支持
	p.AllowElements("table", "thead", "tbody", "tr", "td", "th")
	// 任务列表复选框（仅允许 type=checkbox，配合 Markdown 的 - [x] 语法）
	p.AllowElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	// 音视频（仅允许站内/常见源，但默认 UGC 已限制协议为 http/https）
	p.AllowAttrs("controls", "src").OnElements("video", "audio")
	return p
}()

// SanitizeHTML 移除富文本中的危险内容，返回安全的 HTML。
func SanitizeHTML(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	return sanitizePolicy.Sanitize(raw)
}

// widgetPolicy 是「后台自定义 HTML 小工具」的净化策略：比正文更严格。
//
// 背景（这是一个真实可利用的存储型 XSS）：sidebar_widgets 存在设置表里，
// 由前端 HtmlWidget 直接 dangerouslySetInnerHTML 注入到**每一个访客页面**。
// 设置更新路径此前完全没有消毒（SanitizeHTML 只被 article/page service 调用），
// 所以后台一旦写入脚本，全站访客都会执行。
//
// 不给 input/table 等正文排版元素：小工具只需要标题、段落、链接、图片与列表。
// 同样不放行 class/id——它们不是小工具展示所必需，却能被用来套用站点既有
// 样式（例如 fixed inset-0）做界面伪装。
var widgetPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	// 基础排版与链接
	p.AllowStandardAttributes()
	p.AllowStandardURLs()
	p.AllowElements(
		"p", "br", "hr", "span", "div", "strong", "b", "em", "i", "u", "s",
		"h1", "h2", "h3", "h4", "h5", "h6", "blockquote", "pre", "code",
		"ul", "ol", "li", "dl", "dt", "dd", "small", "sub", "sup",
	)
	p.AllowAttrs("href", "title").OnElements("a")
	p.RequireNoFollowOnLinks(true)
	p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	p.RequireNoFollowOnLinks(true)
	return p
}()

// SanitizeWidgetHTML 净化后台「自定义 HTML」小工具的内容。
// 任何写入路径（后台保存 / API 直调）都必须先过这里。
func SanitizeWidgetHTML(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	return widgetPolicy.Sanitize(raw)
}
