interface SegmentedOption<T extends string> {
  key: T
  label: string
}

interface SegmentedControlProps<T extends string> {
  value: T
  options: SegmentedOption<T>[]
  onChange: (v: T) => void
}

/** 分段控件：圆角灰容器 + 选中项白底阴影滑块（替代旧二级文字下划线 tab） */
export default function SegmentedControl<T extends string>({ value, options, onChange }: SegmentedControlProps<T>) {
  return (
    <div className="inline-flex items-center gap-0.5 bg-gray-100 rounded-lg p-0.5">
      {options.map(opt => (
        <button key={opt.key} onClick={() => onChange(opt.key)}
          className={`px-3 py-1 text-xs rounded-md transition-all ${
            value === opt.key
              ? 'bg-white text-gray-700 shadow-sm font-medium'
              : 'text-gray-500 hover:text-gray-700'
          }`}>
          {opt.label}
        </button>
      ))}
    </div>
  )
}
