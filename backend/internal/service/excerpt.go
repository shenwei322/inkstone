package service

import "strings"

// defaultExcerptRunes 是摘要默认字符上限。中文卡片一行放得下的宽度
// 约在 120 个全角字符上下，超出的部分被折叠成「…」。
const defaultExcerptRunes = 120

// ExcerptFor 返回用于列表 / SEO 描述的摘要文本。
//
// 取值优先级（从高到低）：
//  1. 文章的显式摘要字段（excerpt / rss description）——当前 Article
//     模型没有这一列，该级暂不可用；后续管理后台若补上摘要输入框，
//     调用方应优先取显式字段、为空时再回落到本函数。这里只能接收
//     正文，无法分辨「显式摘要为空」。
//  2. 正文 <p>…</p> 段落按顺序拼接（跳过空段落）；
//  3. 正文整体剥标签后的纯文本，按字符数截断。
//
// 为什么第 3 级仍然必要：正文可能是纯文本（用户直接粘贴，没有 <p>
// 包裹），或是图片 + 说明文字的格式，此时第 2 级一无所获，不能直接
// 给前端返回空摘要。
//
// maxRunes 是返回的字符上限，按 rune 计——中文摘要截断若按字节会把
// 一个汉字劈成两半，输出乱码（系统规范：面向用户的文案一律中文，
// 摘要里同样要按字符裁）。maxRunes <= 0 时取默认 120。
//
// 参数 content 是已消毒的 HTML（保存时过 bluemonday），可以按标签结构
// 扫描；即便如此也不做完整解析：摘要只需要「读得顺」，不追求
// HTML 语义完备，避免为此引入新依赖。
func ExcerptFor(content string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = defaultExcerptRunes
	}
	text := strings.TrimSpace(extractParagraphText(content))
	if text == "" {
		// 正文没有可用的 <p> 段落（纯文本粘贴、整篇都是图片）时，
		// 退化为全文剥标签后的纯文本。
		text = strings.TrimSpace(stripTagsLocally(content))
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}

// extractParagraphText 用一次线性扫描拼接正文里各 <p> 段的纯文本，
// 空段跳过。输入为已消毒 HTML，但写法对大小写与带属性的开标签
// （<p class="x">）都免疫，不依赖 bluemonday 的输出格式。
func extractParagraphText(html string) string {
	var b strings.Builder
	rest := html
	for {
		open := openParagraphAt(rest)
		if open < 0 {
			break
		}
		rest = rest[open:]
		var seg string
		if end := indexFoldASCII(rest, "</p"); end >= 0 {
			seg = rest[:end]
			rest = rest[end+len("</p"):]
		} else {
			// 未闭合的尾段：截到文末，剩下的内容没有下一段了。
			seg = rest
			rest = ""
		}
		if t := strings.TrimSpace(stripTagsLocally(seg)); t != "" {
			if b.Len() > 0 {
				// 段落之间补空格，避免英文单词在拼接处粘连；中文片段
				// 多一个空格不影响阅读。
				b.WriteByte(' ')
			}
			b.WriteString(t)
		}
	}
	return b.String()
}

// openParagraphAt 返回 s 中第一个 <p ...> 开标签结束（'>' 之后）的下标；
// 没有这样的开标签时返回 -1。跳过 <pre / <picture 等以 p 开头但不是
// 段落的标签：要求 '<' 之后紧跟 'p'（不分大小写），且 'p' 之后是 '>'、
// 空白或 '/'（防御 <p> 的怪异写法 <p/>）。
func openParagraphAt(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		next := lowerASCII(s[i+1])
		if next != 'p' {
			continue
		}
		j := i + 2
		if j < len(s) && !isHTMLTagBoundary(s[j]) {
			continue
		}
		for ; j < len(s); j++ {
			if s[j] == '>' {
				return j + 1
			}
		}
		return -1 // '<p' 之后再也找不到 '>'：没有完整开标签
	}
	return -1
}

// isHTMLTagBoundary 判断标签名之后的字符是否是合法边界（属性开始或
// 标签结束），用于区分 <p ...> 与 <pre>/<picture> 这类同前缀标签。
func isHTMLTagBoundary(ch byte) bool {
	switch ch {
	case '>', '/', ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

// indexFoldASCII 返回 sub（仅含 ASCII）在 s 中第一次出现的小写不敏感下标。
// 正文标签清扫不需要 UTF-8 感知：被查找的 </p> 是纯 ASCII，直接按字节
// 扫描不会切开多字节字符。
func indexFoldASCII(s, sub string) int {
	n := len(sub)
	if n == 0 || n > len(s) {
		return -1
	}
	for i := 0; i+n <= len(s); i++ {
		match := true
		for k := 0; k < n; k++ {
			if lowerASCII(s[i+k]) != lowerASCII(sub[k]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func lowerASCII(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}

// stripTagsLocally 剥掉 HTML 标签，只留文本节点。
//
// 与 handler 包的 stripHTMLTags 逐字节等价，但**有意不在两包之间共用**：
// 分层方向只能是 handler → service → repository，service 反向 import
// handler 会形成环（handler 本来就依赖 service）。公共纯函数若要共享，
// 正确做法是下沉到独立的基础包；当前只有 RSS 与摘要两处小需求，
// 各留一份最小实现、注释互相指路即可。改动这里请同步检查
// handler.stripHTMLTags。
func stripTagsLocally(s string) string {
	out := make([]byte, 0, len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch == '<':
			inTag = true
		case ch == '>':
			inTag = false
		case !inTag:
			out = append(out, ch)
		}
	}
	return string(out)
}
