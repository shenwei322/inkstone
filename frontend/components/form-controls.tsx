'use client'

/**
 * 全站统一的表单选择类控件。
 *
 * 为什么不用原生控件：
 * - 原生 `<select>` 的 option 弹层由**浏览器自绘**，CSS 完全管不到。Windows 下
 *   选项列表是白底黑字，深色主题里像一块突兀的补丁；各平台行高/内边距也各不相同。
 * - 原生 `<input type="checkbox">` / `radio` 同理，Windows 偏方、macOS 偏圆，
 *   且无法跟随全站 accent 配色（login 页此前已为此手写过一遍 sr-only + 视觉层的
 *   替代实现，本文件把那套做法提取为公共组件）。
 *
 * 设计原则：
 * 1. **保留原生语义**：checkbox / radio 仍然是真实 `<input>`，只是 `sr-only`
 *    隐藏视觉层，键盘操作、表单提交、辅助技术全部走原生行为，JS 只负责画。
 *    这样不会出现「自绘控件 Tab 跳不过去」这类无障碍退化。
 * 2. **Select 用 listbox 语义**：触发器 `role="combobox"`，弹层 `role="listbox"`，
 *    配合 aria-activedescendant 让屏幕阅读器能念出当前高亮项。
 * 3. **弹层走 portal + fixed**：后台大量控件位于 `overflow-hidden` 的卡片/表格里，
 *    绝对定位会被裁掉。portal 到 body 并按视口空间自动上下翻转。
 * 4. **颜色全部取 CSS 变量**（--accent / --border / --card / --muted），
 *    所以深浅主题、后台可调的 accent 色相都能自动跟随，无需再来改组件。
 */

import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import { Check, ChevronDown, Minus } from 'lucide-react'

// SSR 下 useLayoutEffect 会告警：服务端没有布局阶段。组件是纯客户端交互，
// 但仍可能在 RSC 首屏被渲染一次，所以退化为 useEffect。
const useIsoLayoutEffect = typeof window !== 'undefined' ? useLayoutEffect : useEffect

/** 三档尺寸，对应项目里真实出现的三种密度：表格内 / 工具条 / 表单 */
export type ControlSize = 'xs' | 'sm' | 'md'

const TRIGGER_SIZE: Record<ControlSize, string> = {
  // 用户管理表格「角色」下拉：text-xs px-2 py-1
  xs: 'h-7 pl-2 pr-1.5 text-xs gap-1',
  // 分页每页条数、页面编辑器工具条：text-sm px-2~3 py-1.5
  sm: 'h-8 pl-2.5 pr-2 text-sm gap-1.5',
  // 后台筛选条、设置页表单：text-sm px-3.5 py-2.5
  md: 'h-10 pl-3.5 pr-3 text-sm gap-2',
}

const CHEVRON_SIZE: Record<ControlSize, string> = {
  xs: 'h-3 w-3',
  sm: 'h-3.5 w-3.5',
  md: 'h-4 w-4',
}

// ---------- Select ----------

export interface SelectOption<T extends string = string> {
  value: T
  label: ReactNode
  /** 选项右侧的次要说明（如「默认」「需审核」）；仅作视觉提示，不参与选中值 */
  hint?: ReactNode
  disabled?: boolean
}

export interface SelectProps<T extends string = string> {
  value: T
  onChange: (value: T) => void
  options: readonly SelectOption<T>[]
  /** 无匹配项时触发器显示的占位文案 */
  placeholder?: string
  disabled?: boolean
  size?: ControlSize
  /** 触发器额外样式，用于控制宽度等布局（如 `w-full` / `min-w-[8rem]`） */
  className?: string
  /** 弹层宽度；默认与触发器同宽，传数字可加宽（避免长选项被截断） */
  menuWidth?: number
  ariaLabel?: string
  id?: string
  /** 有值时额外渲染一个隐藏 input，让自绘控件也能参与原生表单提交 */
  name?: string
  /** 校验失败态：边框转红，与输入框的 invalid 观感一致 */
  invalid?: boolean
}

interface MenuPlacement {
  left: number
  width: number
  maxHeight: number
  /** 下方空间不足时向上弹 */
  dropUp: boolean
  top?: number
  bottom?: number
}

const MENU_GAP = 6
const MENU_MIN_NEEDED = 180 // 下方少于这个高度就考虑翻转
const MENU_MAX_HEIGHT = 288

export function Select<T extends string = string>({
  value,
  onChange,
  options,
  placeholder = '请选择',
  disabled = false,
  size = 'md',
  className = '',
  menuWidth,
  ariaLabel,
  id,
  name,
  invalid = false,
}: SelectProps<T>) {
  const autoId = useId()
  const listId = `${id ?? autoId}-listbox`
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const [open, setOpen] = useState(false)
  const [placement, setPlacement] = useState<MenuPlacement | null>(null)
  // 键盘高亮项（与「已选中项」是两件事：可以只移动高亮而不改变值）
  const [activeIndex, setActiveIndex] = useState(0)
  // 首字母跳转：记录上次按键时间与已累积的前缀
  const typeaheadRef = useRef({ text: '', at: 0 })

  const selectedIndex = useMemo(
    () => options.findIndex((o) => o.value === value),
    [options, value],
  )
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined

  /** 按触发器在视口中的位置计算弹层几何；滚动与缩放时重算。 */
  const measure = useCallback(() => {
    const el = triggerRef.current
    if (!el) return
    const r = el.getBoundingClientRect()
    const vh = window.innerHeight
    const vw = window.innerWidth
    const spaceBelow = vh - r.bottom - MENU_GAP - 8
    const spaceAbove = r.top - MENU_GAP - 8
    const dropUp = spaceBelow < MENU_MIN_NEEDED && spaceAbove > spaceBelow
    const height = Math.max(96, Math.min(MENU_MAX_HEIGHT, dropUp ? spaceAbove : spaceBelow))
    // 弹层至少和触发器一样宽；但固定上限并夹在视口内，避免窄触发器旁弹出超宽面板
    const wanted = menuWidth ?? r.width
    const width = Math.min(Math.max(wanted, 132), vw - 16)
    const left = Math.min(Math.max(8, r.left), vw - width - 8)
    setPlacement(
      dropUp
        ? { left, width, maxHeight: height, dropUp: true, bottom: vh - r.top + MENU_GAP }
        : { left, width, maxHeight: height, dropUp: false, top: r.bottom + MENU_GAP },
    )
  }, [menuWidth])

  // 打开时定位；用 iso layout effect 让弹层在第一帧就落在正确位置，不闪
  useIsoLayoutEffect(() => {
    if (open) measure()
  }, [open, measure])

  useEffect(() => {
    if (!open) return
    // capture 阶段监听：卡片/表格自身的滚动也要触发重定位，否则弹层会脱钩
    const onMove = () => measure()
    window.addEventListener('scroll', onMove, true)
    window.addEventListener('resize', onMove)
    return () => {
      window.removeEventListener('scroll', onMove, true)
      window.removeEventListener('resize', onMove)
    }
  }, [open, measure])

  // 点击外部关闭。用 pointerdown 而非 click：避免与触发器自身的 click 抢先后
  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: PointerEvent) => {
      const t = e.target as Node
      if (triggerRef.current?.contains(t) || panelRef.current?.contains(t)) return
      setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown, true)
    return () => document.removeEventListener('pointerdown', onPointerDown, true)
  }, [open])

  // 高亮项滚动入视野（键盘移动时不让选中项跑出弹层）
  useIsoLayoutEffect(() => {
    if (!open) return
    const el = listRef.current?.querySelector<HTMLElement>(`[data-index="${activeIndex}"]`)
    el?.scrollIntoView({ block: 'nearest' })
  }, [open, activeIndex])

  const openMenu = useCallback(
    (index?: number) => {
      if (disabled) return
      setActiveIndex(index ?? (selectedIndex >= 0 ? selectedIndex : 0))
      setOpen(true)
    },
    [disabled, selectedIndex],
  )

  const closeMenu = useCallback((focusTrigger = true) => {
    setOpen(false)
    if (focusTrigger) triggerRef.current?.focus()
  }, [])

  const commit = useCallback(
    (index: number) => {
      const opt = options[index]
      if (!opt || opt.disabled) return
      onChange(opt.value)
      closeMenu()
    },
    [options, onChange, closeMenu],
  )

  /** 上下移动高亮，自动跳过 disabled 项 */
  const moveActive = useCallback(
    (delta: 1 | -1) => {
      if (!options.length) return
      let i = activeIndex
      for (let step = 0; step < options.length; step++) {
        i = (i + delta + options.length) % options.length
        if (!options[i]?.disabled) {
          setActiveIndex(i)
          return
        }
      }
    },
    [activeIndex, options],
  )

  const handleKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        if (!open) openMenu()
        else moveActive(1)
        break
      case 'ArrowUp':
        e.preventDefault()
        if (!open) openMenu()
        else moveActive(-1)
        break
      case 'Home':
        if (!open) return
        e.preventDefault()
        setActiveIndex(0)
        break
      case 'End':
        if (!open) return
        e.preventDefault()
        setActiveIndex(options.length - 1)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (open) commit(activeIndex)
        else openMenu()
        break
      case 'Escape':
        if (!open) return
        e.preventDefault()
        closeMenu()
        break
      case 'Tab':
        if (open) setOpen(false)
        break
      default: {
        // 首字母跳转（原生 select 的既有习惯）。500ms 内的连续按键累积成前缀。
        if (e.key.length !== 1 || e.ctrlKey || e.metaKey || e.altKey) return
        const now = Date.now()
        const prev = typeaheadRef.current
        const text = (now - prev.at < 500 ? prev.text : '') + e.key.toLowerCase()
        typeaheadRef.current = { text, at: now }
        const hit = options.findIndex(
          (o) => !o.disabled && String(o.label).toLowerCase().startsWith(text),
        )
        if (hit >= 0) {
          if (open) setActiveIndex(hit)
          else onChange(options[hit].value)
        }
      }
    }
  }

  const showPlaceholder = !selected

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        id={id}
        role="combobox"
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={open ? listId : undefined}
        aria-label={ariaLabel}
        aria-activedescendant={open ? `${listId}-opt-${activeIndex}` : undefined}
        aria-invalid={invalid || undefined}
        disabled={disabled}
        onClick={() => (open ? closeMenu() : openMenu())}
        onKeyDown={handleKeyDown}
        className={`inline-flex max-w-full select-none items-center justify-between rounded-md border font-medium transition-colors duration-150 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-50 ${
          TRIGGER_SIZE[size]
        } ${
          invalid
            ? 'border-red-400 focus-visible:ring-2 focus-visible:ring-red-500/20 dark:border-red-500/60'
            : open
              ? 'border-accent bg-background ring-2 ring-accent/20'
              : 'border-border bg-background hover:border-accent/50 focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-accent/20'
        } ${className}`}
      >
        <span
          className={`min-w-0 truncate text-left ${showPlaceholder ? 'text-muted-foreground/70' : 'text-foreground'}`}
        >
          {selected ? selected.label : placeholder}
        </span>
        <ChevronDown
          aria-hidden="true"
          className={`${CHEVRON_SIZE[size]} shrink-0 text-muted-foreground transition-transform duration-200 ${
            open ? 'rotate-180 text-accent' : ''
          }`}
        />
      </button>

      {name && <input type="hidden" name={name} value={value} />}

      {open &&
        placement &&
        createPortal(
          <div
            ref={panelRef}
            className="fixed z-[80] overflow-hidden rounded-md border border-border bg-card p-1 shadow-xl shadow-black/10 dark:shadow-black/50"
            style={{
              left: placement.left,
              width: placement.width,
              maxHeight: placement.maxHeight,
              ...(placement.dropUp
                ? { bottom: placement.bottom }
                : { top: placement.top }),
            }}
          >
            <div
              ref={listRef}
              id={listId}
              role="listbox"
              aria-label={ariaLabel}
              tabIndex={-1}
              className="max-h-full overflow-y-auto overscroll-contain"
              style={{ maxHeight: placement.maxHeight - 8 }}
            >
              {options.length === 0 && (
                <div className="px-2.5 py-2 text-sm text-muted-foreground">无可选项</div>
              )}
              {options.map((opt, i) => {
                const isSelected = opt.value === value
                const isActive = i === activeIndex
                return (
                  <div
                    key={opt.value}
                    id={`${listId}-opt-${i}`}
                    data-index={i}
                    role="option"
                    aria-selected={isSelected}
                    aria-disabled={opt.disabled || undefined}
                    onPointerEnter={() => !opt.disabled && setActiveIndex(i)}
                    onClick={() => commit(i)}
                    className={`flex cursor-pointer items-center gap-2 rounded-sm px-2.5 py-2 text-sm transition-colors duration-100 ${
                      opt.disabled
                        ? 'cursor-not-allowed opacity-40'
                        : isActive
                          ? 'bg-muted text-foreground'
                          : 'text-foreground/80'
                    }`}
                  >
                    <span className="min-w-0 flex-1 truncate">
                      {opt.label}
                      {opt.hint && (
                        <span className="ml-2 text-xs text-muted-foreground">{opt.hint}</span>
                      )}
                    </span>
                    {isSelected && (
                      <Check aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-accent" />
                    )}
                  </div>
                )
              })}
            </div>
          </div>,
          document.body,
        )}
    </>
  )
}

// ---------- Checkbox ----------

export interface CheckboxProps {
  checked: boolean
  onChange: (checked: boolean) => void
  label?: ReactNode
  /** 标签下方的补充说明 */
  description?: ReactNode
  disabled?: boolean
  /** 半选态（如「部分子项已勾选」） */
  indeterminate?: boolean
  className?: string
  id?: string
  name?: string
  /** label 内文案的强调（默认 true，登录页「记住我」那种加粗） */
  emphasis?: boolean
}

export function Checkbox({
  checked,
  onChange,
  label,
  description,
  disabled = false,
  indeterminate = false,
  className = '',
  id,
  name,
  emphasis = true,
}: CheckboxProps) {
  const autoId = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const active = checked || indeterminate

  // indeterminate 只能用 DOM 属性设置，React 没有对应 prop
  useEffect(() => {
    if (inputRef.current) inputRef.current.indeterminate = indeterminate
  }, [indeterminate])

  return (
    <label
      htmlFor={id ?? autoId}
      className={`group inline-flex items-start gap-2.5 text-sm ${
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'
      } ${className}`}
    >
      <input
        ref={inputRef}
        id={id ?? autoId}
        name={name}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="peer sr-only"
      />
      <span
        aria-hidden="true"
        className={`mt-px flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[5px] border transition-all duration-200 ease-out peer-focus-visible:ring-2 peer-focus-visible:ring-accent/40 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-card ${
          active
            ? 'border-accent bg-accent shadow-[0_2px_8px_-2px_var(--accent)]'
            : 'border-border bg-background group-hover:border-accent/60 group-hover:bg-muted/50'
        } ${disabled ? '' : ''}`}
      >
        {indeterminate ? (
          <Minus className="h-3 w-3 text-white" strokeWidth={3} />
        ) : (
          <Check
            className={`h-3 w-3 text-white transition-all duration-200 ease-out ${
              checked ? 'scale-100 opacity-100' : 'scale-50 opacity-0'
            }`}
            strokeWidth={3}
          />
        )}
      </span>
      {(label || description) && (
        <span className="min-w-0 leading-relaxed">
          {label && (
            <span
              className={
                emphasis
                  ? 'font-medium transition-colors group-hover:text-accent'
                  : 'text-muted-foreground transition-colors group-hover:text-accent'
              }
            >
              {label}
            </span>
          )}
          {description && (
            <span className="mt-0.5 block text-xs text-muted-foreground">{description}</span>
          )}
        </span>
      )}
    </label>
  )
}

// ---------- Radio ----------

export interface RadioOption<T extends string = string> {
  value: T
  label: ReactNode
  description?: ReactNode
  disabled?: boolean
}

export interface RadioGroupProps<T extends string = string> {
  value: T
  onChange: (value: T) => void
  options: readonly RadioOption<T>[]
  /** 同组必须同名，否则浏览器不把它们当成互斥的一组 */
  name: string
  disabled?: boolean
  className?: string
  orientation?: 'vertical' | 'horizontal'
}

export function RadioGroup<T extends string = string>({
  value,
  onChange,
  options,
  name,
  disabled = false,
  className = '',
  orientation = 'vertical',
}: RadioGroupProps<T>) {
  return (
    <div
      role="radiogroup"
      className={`flex ${
        orientation === 'horizontal' ? 'flex-row flex-wrap gap-x-5 gap-y-2' : 'flex-col gap-2.5'
      } ${className}`}
    >
      {options.map((opt) => {
        const checked = opt.value === value
        const itemDisabled = disabled || opt.disabled
        return (
          <label
            key={opt.value}
            className={`group inline-flex cursor-pointer items-start gap-2.5 text-sm ${
              itemDisabled ? 'cursor-not-allowed opacity-50' : ''
            }`}
          >
            <input
              type="radio"
              name={name}
              value={opt.value}
              checked={checked}
              disabled={itemDisabled}
              onChange={() => onChange(opt.value)}
              className="peer sr-only"
            />
            <span
              aria-hidden="true"
              className={`mt-px flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full border transition-all duration-200 ease-out peer-focus-visible:ring-2 peer-focus-visible:ring-accent/40 peer-focus-visible:ring-offset-2 peer-focus-visible:ring-offset-card ${
                checked
                  ? 'border-accent bg-accent shadow-[0_2px_8px_-2px_var(--accent)]'
                  : 'border-border bg-background group-hover:border-accent/60 group-hover:bg-muted/50'
              }`}
            >
              <span
                className={`h-2 w-2 rounded-full bg-white transition-transform duration-200 ease-out ${
                  checked ? 'scale-100' : 'scale-0'
                }`}
              />
            </span>
            <span className="min-w-0 leading-relaxed">
              <span className="font-medium transition-colors group-hover:text-accent">
                {opt.label}
              </span>
              {opt.description && (
                <span className="mt-0.5 block text-xs text-muted-foreground">
                  {opt.description}
                </span>
              )}
            </span>
          </label>
        )
      })}
    </div>
  )
}

// ---------- Switch ----------

export interface SwitchProps {
  checked: boolean
  onChange: (checked: boolean) => void
  disabled?: boolean
  /** 无障碍名称（视觉上不显示） */
  label: string
  size?: 'sm' | 'md'
  className?: string
}

/**
 * 纯 CSS 过渡的开关。与 components/gsap.tsx 的 GsapSwitch 的区别：
 * 那个用 GSAP 驱动滑块（后台维护模式开关在用），这个走 CSS transition，
 * 更适合筛选条、设置列表这类一屏内出现多个的场景——避免为一堆开关各建一个 tween。
 */
export function Switch({
  checked,
  onChange,
  disabled = false,
  label,
  size = 'md',
  className = '',
}: SwitchProps) {
  const track = size === 'sm' ? 'h-5 w-9' : 'h-6 w-11'
  const knob = size === 'sm' ? 'h-4 w-4' : 'h-5 w-5'
  const travel = size === 'sm' ? 'translate-x-4' : 'translate-x-5'

  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative shrink-0 rounded-full transition-colors duration-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card ${track} ${
        checked ? 'bg-accent' : 'bg-muted'
      } ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer'} ${className}`}
    >
      <span
        aria-hidden="true"
        className={`absolute left-0.5 top-0.5 rounded-full bg-white shadow-md transition-transform duration-300 ease-out ${knob} ${
          checked ? travel : 'translate-x-0'
        }`}
      />
    </button>
  )
}

// ---------- 组合：带标题的选项组 ----------

export interface FieldProps {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  htmlFor?: string
  children: ReactNode
  className?: string
}

/** 表单字段容器：统一「标签 / 说明 / 错误」三段的间距与字号。 */
export function Field({ label, hint, error, htmlFor, children, className = '' }: FieldProps) {
  return (
    <div className={`flex flex-col gap-2 ${className}`}>
      <label htmlFor={htmlFor} className="text-sm font-medium text-foreground">
        {label}
      </label>
      {children}
      {hint && !error && <p className="text-xs text-muted-foreground">{hint}</p>}
      {error && <p className="text-xs text-red-500">{error}</p>}
    </div>
  )
}
