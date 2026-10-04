'use client'

import { useEffect, useRef, useState } from 'react'
import { Check, Link2, Mail, Send, Share2 } from 'lucide-react'
import { useNotify } from './toast'

/**
 * 文章分享：移动端优先调系统原生分享（navigator.share），
 * 桌面端弹出小菜单（复制链接 / 微博 / Twitter / 邮件）。
 */
export function ArticleShare({ title }: { title: string }) {
  const notify = useNotify()
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)
  // 复制成功后的「已复制」提示定时器句柄：卸载时必须清理，
  // 否则用户点完复制立刻切路由，1.5s 后会向已卸载组件 setState。
  const copiedTimerRef = useRef<number | null>(null)

  useEffect(
    () => () => {
      if (copiedTimerRef.current !== null) window.clearTimeout(copiedTimerRef.current)
    },
    [],
  )

  // 点击外部/ESC 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (boxRef.current && !boxRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])

  const shareURL = () => (typeof window !== 'undefined' ? window.location.href : '')

  const copyLink = async () => {
    try {
      await navigator.clipboard.writeText(shareURL())
      setCopied(true)
      if (copiedTimerRef.current !== null) window.clearTimeout(copiedTimerRef.current)
      copiedTimerRef.current = window.setTimeout(() => {
        setCopied(false)
        copiedTimerRef.current = null
      }, 1500)
      notify.success('链接已复制，去分享给朋友吧')
    } catch {
      notify.error('复制失败，请手动复制地址栏链接')
    }
  }

  const openExternal = (url: string) => {
    window.open(url, '_blank', 'noopener,noreferrer')
    setOpen(false)
  }

  const onClick = async () => {
    // 移动端：原生分享面板
    if (typeof navigator !== 'undefined' && typeof navigator.share === 'function') {
      try {
        await navigator.share({ title, url: shareURL() })
        return
      } catch {
        // 用户取消或不可用 → 退回菜单
      }
    }
    setOpen((v) => !v)
  }

  const enc = (s: string) => encodeURIComponent(s)
  const url = shareURL()

  return (
    <div className="relative" ref={boxRef}>
      <button
        type="button"
        onClick={() => void onClick()}
        className="flex items-center gap-2 rounded-full border border-border px-4 py-2 text-sm font-medium text-muted-foreground transition-all hover:border-accent/40 hover:text-accent"
      >
        <Share2 className="h-4 w-4" />
        分享
      </button>
      {open && (
        <div className="absolute bottom-full left-0 z-30 mb-2 w-44 overflow-hidden rounded-xl border border-border bg-card py-1 shadow-xl shadow-black/10">
          <button
            type="button"
            onClick={() => void copyLink()}
            className="flex w-full items-center gap-2.5 px-3.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            {copied ? <Check className="h-4 w-4 text-emerald-500" /> : <Link2 className="h-4 w-4" />}
            {copied ? '已复制链接' : '复制链接'}
          </button>
          <button
            type="button"
            onClick={() => openExternal(`https://service.weibo.com/share/share.php?url=${enc(url)}&title=${enc(title)}`)}
            className="flex w-full items-center gap-2.5 px-3.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <Send className="h-4 w-4" />
            分享到微博
          </button>
          <button
            type="button"
            onClick={() => openExternal(`https://twitter.com/intent/tweet?url=${enc(url)}&text=${enc(title)}`)}
            className="flex w-full items-center gap-2.5 px-3.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <Send className="h-4 w-4" />
            分享到 Twitter
          </button>
          <a
            href={`mailto:?subject=${enc(title)}&body=${enc(url)}`}
            className="flex w-full items-center gap-2.5 px-3.5 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <Mail className="h-4 w-4" />
            邮件分享
          </a>
        </div>
      )}
    </div>
  )
}
