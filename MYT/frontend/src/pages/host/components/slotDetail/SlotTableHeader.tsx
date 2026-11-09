import { useTranslation } from 'react-i18next'

interface Props {
  /** 行列网格（COL / COL_WIDE / COL_FLAT），必须与行渲染同值，否则逐列错位 */
  grid: string
  /** 行内边距（SLOT_CHILD_PAD / FLAT_PAD），同上必须与行同值 */
  pad: string
  /** 摊平模式：多一列「坑位」 */
  flat?: boolean
}

/**
 * 坑位表共享列头。分组渲染与摊平渲染、单设备（主机 tab）与多设备（云机 tab 列表模式）
 * 共用这一份 —— 云机 tab 列表模式由 MultiDeviceSlotView 在最上层渲染一次
 * （SlotView 传 hideHeader），其余场景由 SlotView 自行渲染。
 *
 * 列头吸顶固定在滚动区顶部（宿主滚动容器为 overflow-auto，sticky 直接生效）。
 */
export default function SlotTableHeader({ grid, pad, flat = false }: Props) {
  const { t } = useTranslation()
  return (
    <div className={`${grid} ${pad} py-1.5 text-xs text-gray-400 bg-gray-50 border-b border-gray-100 sticky top-0 z-10`}>
      <span></span>
      {flat && <span className="text-center">{t('host:slotView.colSlot')}</span>}
      <span className="text-center">{t('host:slotView.colNote')}</span>
      <span className="text-center">{t('host:slotView.colImageName')}</span>
      <span className="text-center">{t('host:slotView.colType')}</span>
      <span className="text-center">{t('host:slotView.colAndroidVersion')}</span>
      <span className="text-center">{t('host:slotView.colNetwork')}</span>
      <span className="text-center">{t('host:slotView.colContainerIP')}</span>
      <span className="text-center">{t('host:slotView.colStatus')}</span>
      <span className="text-center">{t('host:slotView.colTags')}</span>
      <span className="text-center">{t('host:slotView.colModel')}</span>
    </div>
  )
}
