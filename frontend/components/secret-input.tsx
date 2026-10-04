'use client'

import { useState } from 'react'
import { Eye, EyeOff } from 'lucide-react'
import { inputClass } from '@/lib/ui'

interface Props {
  value: string
  onChange: (v: string) => void
  /** 服务端是否已保存过该密钥 */
  isSet?: boolean
  placeholder?: string
  className?: string
  autoComplete?: string
  /** 供外部 <label htmlFor> 关联 */
  id?: string
}

/**
 * 密钥/密码输入框
 *
 * 两种状态：
 * 1. 已保存且未重新输入：显示 `********`，**不显示小眼睛**（真实值不可查看）。
 *    聚焦/开始输入即清空占位，进入「编辑」状态。
 * 2. 编辑状态（未保存过，或已开始输入）：正常输入，右侧小眼睛可切换明文/密文。
 */
export function SecretInput({
  value,
  onChange,
  isSet = false,
  placeholder,
  // 默认套用全站统一样式：漏传时会渲染成裸 input，无 w-full 会把圆角卡片撑破
  className = inputClass,
  autoComplete = 'new-password',
  id,
}: Props) {
  const [visible, setVisible] = useState(false)
  // 用户是否已开始编辑（聚焦或输入过）
  const [editing, setEditing] = useState(false)

  // 已保存 + 未开始编辑 = 只读占位态
  const locked = isSet && !editing
  // 编辑状态下才显示小眼睛
  const showEye = !locked

  return (
    <div className="relative">
      <input
        type={visible && showEye ? 'text' : 'password'}
        id={id}
        value={locked ? '••••••••••••' : value}
        onChange={(e) => {
          if (!editing) setEditing(true)
          onChange(e.target.value)
        }}
        onFocus={() => {
          // 聚焦即进入编辑态并清空占位，允许直接输入新密钥
          if (locked) {
            setEditing(true)
            onChange('')
          }
        }}
        readOnly={locked}
        placeholder={placeholder ?? (locked ? '已保存' : '请输入新密钥')}
        autoComplete={autoComplete}
        className={`${className}${showEye ? ' pr-10' : ''}`}
      />
      {showEye && (
        <button
          type="button"
          onClick={() => setVisible((v) => !v)}
          title={visible ? '隐藏' : '显示'}
          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground transition-colors hover:text-foreground"
        >
          {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
        </button>
      )}
    </div>
  )
}
