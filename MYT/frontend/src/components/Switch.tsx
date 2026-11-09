import { useId } from 'react'

interface Props {
  checked: boolean
  onChange: (v: boolean) => void
  disabled?: boolean
  /** 标签文字（可选，开关右侧）。 */
  label?: string
  /** 悬停提示。 */
  title?: string
  className?: string
}

/**
 * Switch 现代拨动开关：pill 形状，圆点滑动，CSS transition 过渡。
 * 开启蓝（blue-500），关闭灰（gray-300），支持 disabled 态。
 * 用 CSS transition 而非 gsap——拨动是连续手势，CSS 更轻量且不卡顿。
 */
export default function Switch({ checked, onChange, disabled, label, title, className = '' }: Props) {
  const id = useId()
  return (
    <label
      htmlFor={id}
      title={title}
      className={`inline-flex items-center gap-2 cursor-pointer select-none ${disabled ? 'opacity-40 cursor-not-allowed' : ''} ${className}`}
    >
      <span className="relative inline-flex shrink-0">
        <input
          id={id}
          type="checkbox"
          checked={checked}
          onChange={e => onChange(e.target.checked)}
          disabled={disabled}
          className="peer sr-only"
        />
        {/* 轨道：开启时蓝，关闭时灰。transition-colors 让换色平滑。 */}
        <span
          className={`h-5 w-9 rounded-full transition-colors duration-200 ${
            checked ? 'bg-blue-500' : 'bg-gray-300'
          } peer-focus-visible:ring-2 peer-focus-visible:ring-blue-400 peer-focus-visible:ring-offset-1`}
        />
        {/* 圆点：开启时右移+变白，关闭时左移+灰白。translate-x 过渡平滑。 */}
        <span
          className={`absolute left-0.5 top-1/2 -translate-y-1/2 size-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
            checked ? 'translate-x-4' : 'translate-x-0'
          }`}
        />
      </span>
      {label && <span className="text-[11px] text-gray-500 whitespace-nowrap">{label}</span>}
    </label>
  )
}
