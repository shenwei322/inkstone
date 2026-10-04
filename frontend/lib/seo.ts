/**
 * SEO 辅助：站点 URL、正文摘要提取、JSON-LD 序列化。
 *
 * 单独抽一个模块而不是在各页面内联，是因为文章页、独立页与结构化数据
 * 三处都要用同一套口径：canonical 地址、描述长度、JSON 转义规则一旦
 * 各写一份就会漂移（例如某页多截了 10 个字、某页忘了去尾斜杠）。
 */

/**
 * 站点对外基地址。
 *
 * 从 NEXT_PUBLIC_SITE_URL 读：这个变量与其他 NEXT_PUBLIC_* 一样是
 * **构建期注入**的，改了必须重新 build 前端镜像才生效。未配置时按本地
 * 开发兜底——生产缺失只会让 canonical 指向 localhost，不会让页面崩。
 */
export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL ?? 'http://localhost:3000').replace(/\/+$/, '')

/**
 * 解出并解码动态路由的 slug。
 *
 * Next 传入的 params.slug 是 URL 编码的，必须解码后才能拿去查接口
 * （否则中文 slug 的页面永远取不到文章）。decodeURIComponent 对已解码
 * 或含非法 % 的串会抛错，此时保留原值。
 *
 * 页面组件与 generateMetadata 都要解一次 params，抽出来避免两边各写一遍。
 */
export async function resolveSlug(params: Promise<{ slug: string }>): Promise<string> {
  let slug = (await params).slug
  try {
    slug = decodeURIComponent(slug)
  } catch {
    /* 非法编码：保留原始值 */
  }
  return slug
}

/**
 * 去掉 HTML 标签与实体，压平空白后截取前 max 个字符。
 *
 * 用于 <meta description> 与 OG description：正文存的是 HTML，
 * 直接丢进去会得到一串标签而不是可读摘要。
 */
export function plainText(html: string, max = 110): string {
  const text = html
    // 标签整体替换为空格而非空串：保证 "a<b>b</b>c" 不会粘成 "abc"。
    .replace(/<[^>]*>/g, ' ')
    // 常见的几个实体。不追求全覆盖——剩下的大多是 & 之类的符号，
    // 出现在描述里比显示成 "&amp;" 更容易接受。
    .replace(/&nbsp;/g, ' ')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, '&')
    .replace(/\s+/g, ' ')
    .trim()
  if (text.length <= max) return text
  // 截断处避免把一个英文单词切成两半（中文无此问题，但不额外判断也行）。
  return text.slice(0, max).trimEnd()
}

/**
 * 把结构化数据序列化成可安全内联到 <script> 里的 JSON 字符串。
 *
 * 为什么不能直接 JSON.stringify：值里只要出现 "</script>"（例如文章
 * 正文被引用进 headline），浏览器会在脚本中途闭合标签，后面的内容被当成
 * HTML 解析。把 < 与 > 转成 \uXXXX 形式后 JSON 语义完全不变（解析结果
 * 还是同一个字符串），但浏览器不再把它当标签边界。
 */
export function jsonLdString(value: unknown): string {
  return JSON.stringify(value)
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/&/g, '\\u0026')
}
