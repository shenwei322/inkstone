import Link from 'next/link'
import { FolderOpen } from 'lucide-react'
import { fetchCategories } from '@/lib/api'
import { PageTransition, Reveal } from '@/components/motion'

// 分类汇总页。此前分类只能通过 /?category=slug 复用首页筛选，没有独立
// 落地页，标签云点进去共用一个首页 title —— 对 SEO 和内链都不友好。
export default async function CategoriesPage() {
  let categories: { id: number; name: string; slug: string; article_count: number }[] = []
  let failed = false
  try {
    categories = (await fetchCategories()).categories
  } catch {
    failed = true
  }

  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-10">
        <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          <FolderOpen className="h-6 w-6 text-accent" /> 全部分类
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">按主题浏览文章</p>

        {failed ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <p className="text-muted-foreground">分类加载失败，请稍后重试</p>
          </Reveal>
        ) : categories.length === 0 ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <FolderOpen className="mx-auto h-10 w-10 text-muted-foreground/50" />
            <p className="mt-4 text-muted-foreground">还没有创建分类</p>
          </Reveal>
        ) : (
          <ul className="mt-8 grid gap-3 sm:grid-cols-2">
            {categories.map((c) => (
              <li key={c.id}>
                <Link href={`/category/${c.slug}`} className="block">
                  <div className="group rounded-xl border border-border bg-card p-5 shadow-sm transition-all duration-300 hover:border-accent/30 hover:shadow-lg hover:shadow-accent/5">
                    <div className="flex items-center justify-between gap-3">
                      <h2 className="text-base font-semibold transition-colors group-hover:text-accent">
                        {c.name}
                      </h2>
                      <span className="shrink-0 rounded-full bg-accent/10 px-2.5 py-1 text-xs font-medium text-accent">
                        {c.article_count} 篇
                      </span>
                    </div>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </div>
    </PageTransition>
  )
}
