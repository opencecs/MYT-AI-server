import { Check } from 'lucide-react'
import { SlotExpireInfo } from './types'
import { slotStyle } from './slotState'

/**
 * 单个实例格子：序号 + 到期日，状态色底。
 * 横向流式排列的一格（flex wrap，参照 pc-android-client 授权页）。
 */
interface Props {
  slotNum: number
  slots: SlotExpireInfo[] | undefined
  selected: boolean
  disabled: boolean
  onClick: () => void
}

export function SlotCell({ slotNum, slots, selected, disabled, onClick }: Props) {
  const slot = slots?.find(s => s.slot_index === slotNum)
  const style = slotStyle(slot ? slot.state : -1)

  return (
    <button onClick={onClick} disabled={disabled} title={slot?.expire_ts || undefined}
      className={`relative w-[76px] h-9 rounded-lg border text-xs font-medium transition-all duration-150
        ${selected ? 'border-blue-500 bg-blue-500 text-white shadow-md shadow-blue-200/60' : style.cell}
        ${disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer active:scale-95'}`}>
      <span className="relative z-10">实例-{slotNum}</span>
      {selected && (
        <span className="absolute -top-1.5 -right-1.5 z-20 w-4 h-4 bg-white rounded-full flex items-center justify-center shadow-sm">
          <Check size={10} className="text-blue-500" strokeWidth={3} />
        </span>
      )}
    </button>
  )
}
