/** 全站共享的 UI 常量与工具，确保样式统一。 */

// ---------- 表单控件 ----------

/** 表单输入框统一样式（边框、聚焦光环、占位符） */
export const inputClass =
  'w-full rounded-lg border border-border bg-background px-3.5 py-2.5 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20'

// ---------- 徽章 ----------

const badgeBase = 'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium'

export const badgeSuccess = `${badgeBase} bg-emerald-500/10 text-emerald-600 dark:text-emerald-400`
export const badgeWarning = `${badgeBase} bg-amber-500/10 text-amber-600 dark:text-amber-400`
export const badgeDanger = `${badgeBase} bg-red-500/10 text-red-500`

// ---------- 正文排版 ----------

/**
 * 文章/独立页正文（Markdown 渲染的 .prose 容器）统一样式。
 * 设计取舍：
 * - prose-lg + leading-1.8 + text-pretty：中文长文阅读舒适区
 * - headings 加 scroll-mt-24：目录锚点跳转时不被 sticky 导航遮挡
 * - h2 左侧 accent 竖线 + h2/h3 逐级递减字号：结构层次一眼可辨
 * - 引用浅底色圆角、图片边框阴影、代码块描边、表格斑马纹：与全站卡片视觉统一
 * - 段间距用 em 而非固定 rem，标题/字号缩放时段落节奏同步缩放
 */
export const proseBody =
  'prose prose-reading prose-neutral dark:prose-invert prose-lg max-w-none overflow-x-auto ' +
  // 段落：中文长文 1.8 倍行高 + 均衡断行，段间距略收紧避免「空得发虚」
  'prose-p:leading-[1.8] prose-p:text-pretty prose-p:my-[1.1em] ' +
  // 标题：锚点避让 sticky 导航；h1/h2/h3 层递 + 字重区分
  'prose-headings:scroll-mt-24 prose-headings:font-semibold prose-headings:tracking-tight prose-headings:text-balance ' +
  'prose-h1:mt-12 prose-h1:mb-4 prose-h1:text-3xl ' +
  'prose-h2:mt-12 prose-h2:mb-4 prose-h2:border-b prose-h2:border-border prose-h2:pb-2.5 prose-h2:text-2xl ' +
  'prose-h3:mt-9 prose-h3:mb-3 prose-h3:text-xl ' +
  'prose-h4:mt-7 prose-h4:mb-2 prose-h4:text-base ' +
  // 链接：默认无下划线，悬停出现 accent 下划线；外链样式由 globals.css 补箭头
  'prose-a:font-medium prose-a:text-accent prose-a:no-underline prose-a:decoration-accent/40 prose-a:underline-offset-4 ' +
  'hover:prose-a:underline ' +
  // 行内代码：圆角浅底 + 等宽字体微缩，与代码块区分开
  'prose-code:rounded-md prose-code:bg-muted prose-code:px-1.5 prose-code:py-0.5 prose-code:text-[0.85em] prose-code:font-normal ' +
  'prose-code:before:content-none prose-code:after:content-none ' +
  // 代码块：外框由 .md-codeblock 提供，这里只保留基础留白
  'prose-pre:rounded-xl prose-pre:border prose-pre:border-border prose-pre:bg-muted prose-pre:shadow-sm ' +
  // 图片：圆角描边 + 柔和投影，长图不撑破卡片
  'prose-img:rounded-xl prose-img:border prose-img:border-border prose-img:shadow-md ' +
  // 引用：左侧 accent 竖线 + 浅底 + 圆角，去掉斜体（中文斜体不可读）
  'prose-blockquote:not-italic prose-blockquote:border-l-accent prose-blockquote:bg-muted/40 prose-blockquote:py-1 prose-blockquote:px-4 prose-blockquote:rounded-r-lg ' +
  // 列表：marker 弱化 + 与段落一致的间距，嵌套列表收紧
  'prose-li:marker:text-muted-foreground/60 prose-ul:my-[1.1em] prose-ol:my-[1.1em] prose-li:my-1.5 ' +
  'prose-hr:my-12 prose-hr:border-border'

// ---------- 工具函数 ----------

/** 字节数格式化（B / KB / MB / GB） */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

/**
 * 截断文本并追加省略号。
 *
 * 按「码点」而不是 UTF-16 code unit 截断：直接用 slice 可能落在代理对中间，
 * 产出孤立的半截字符（显示为 ）。用 Array.from 展开后再截则不会切断 emoji。
 */
export function truncate(text: string, max: number): string {
  if (max <= 0) return ''
  const chars = Array.from(text)
  if (chars.length <= max) return text
  // 留一个位置给省略号，避免结果比 max 还长
  return chars.slice(0, max - 1).join('') + '…'
}
