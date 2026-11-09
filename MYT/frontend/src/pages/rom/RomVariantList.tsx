import { useTranslation } from 'react-i18next'
import { Download, Loader2, Trash2, Upload } from 'lucide-react'
import { formatBytes, type RomVariantRow } from './romShared'
import { btnDanger } from '../../utils/buttonStyles'

interface Props {
  /** 当前选中主机（删除操作按它执行） */
  host: string
  rows: RomVariantRow[]
  busyVariant: string
  onPull: (v: RomVariantRow) => void
  onUpload: (v: RomVariantRow) => void
  onDelete: (v: RomVariantRow) => void
}

/**
 * ROM 变体行列表（从 RomPage 抽出的展示组件，保持页面文件 ≤300 行）。
 *
 * 一个变体 = system.img + vendor.img + initramfs.img 三件套；行内操作：
 * 下载（补缺失文件）/ 上传 / 删除。删除走宿主的 ask 确认链路，此处只回调。
 */
export default function RomVariantList({ rows, busyVariant, onPull, onUpload, onDelete }: Props) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-col gap-1.5">
      {rows.map(v => {
        const busy = busyVariant === v.variant
        const ready = v.ready && v.missing.length === 0
        return (
          <div
            key={v.variant}
            className="flex items-center gap-3 px-3 py-2 rounded-lg border border-gray-100 bg-white hover:border-gray-200 hover:shadow-sm transition-all"
          >
            {/* 就绪状态点 */}
            <span
              className={`w-2 h-2 rounded-full shrink-0 ${ready ? 'bg-green-500' : 'bg-amber-400'}`}
              aria-hidden
            />
            {/* 变体名 + 文件明细 */}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2 min-w-0">
                <span className="text-sm text-gray-800 truncate font-mono" title={v.variant}>{v.variant}</span>
                <span className={`text-[10px] px-1.5 py-px rounded whitespace-nowrap ${
                  ready ? 'bg-green-50 text-green-600' : 'bg-amber-50 text-amber-600'
                }`}>
                  {ready ? t('rom:status.ready') : t('rom:status.incomplete')}
                </span>
              </div>
              <div className="flex items-center gap-2 mt-0.5 text-[11px] text-gray-400 min-w-0 flex-wrap">
                <span className="shrink-0">{formatBytes(v.totalSize)}</span>
                {v.files.map(f => (
                  <span
                    key={f.name}
                    className={`font-mono ${f.ready ? 'text-gray-400' : 'text-amber-500'}`}
                    title={f.ready ? formatBytes(f.size) : t('rom:fileMissing')}
                  >
                    {f.name}{f.ready ? '' : ' ✗'}
                  </span>
                ))}
              </div>
            </div>
            {/* 操作 */}
            <div className="flex items-center gap-1 shrink-0">
              <button
                onClick={() => onPull(v)}
                disabled={busy}
                className="flex items-center gap-1 text-[11px] text-blue-600 hover:text-blue-700 hover:bg-blue-50 disabled:opacity-40 disabled:cursor-not-allowed rounded px-2 py-1 transition-colors cursor-pointer"
                title={t('rom:download')}
              >
                <Download size={11} />
                {t('rom:download')}
              </button>
              <button
                onClick={() => onUpload(v)}
                disabled={busy}
                className="flex items-center gap-1 text-[11px] text-gray-600 hover:text-blue-600 hover:bg-blue-50 disabled:opacity-40 disabled:cursor-not-allowed rounded px-2 py-1 transition-colors cursor-pointer"
                title={t('rom:upload.title')}
              >
                <Upload size={11} />
                {t('rom:upload.entry')}
              </button>
              <button
                onClick={() => onDelete(v)}
                disabled={busy}
                className={`${btnDanger} px-2 py-1 text-[11px]`}
                title={t('rom:delete')}
              >
                {busy ? <Loader2 size={11} className="animate-spin" /> : <Trash2 size={11} />}
                <span className="ml-0.5">{t('rom:delete')}</span>
              </button>
            </div>
          </div>
        )
      })}
    </div>
  )
}
