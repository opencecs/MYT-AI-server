/**
 * 扫描雷达可视化：三环 + 中心点 + conic 扫掠。
 * idle 时静默（灰）；scanning 时激活（描边变蓝 + 扫掠旋转），并为每台已发现设备
 * 点亮一个 blip（位置按序固定取自 BLIP_SPOTS）。
 * 动画在 prefers-reduced-motion 下自动停用（样式层统一降级）。
 */

const BLIP_SPOTS: Array<[number, number]> = [
  [32, 22], [68, 34], [22, 58], [58, 68], [78, 18], [42, 40], [66, 76], [14, 36],
]

export default function ScanRadar({ active, blipCount }: { active: boolean; blipCount: number }) {
  const blips = Math.max(0, Math.min(blipCount, BLIP_SPOTS.length))
  return (
    <div className={`scan-radar ${active ? 'scan-radar--active' : ''}`}>
      <i className="scan-radar__ring" />
      <i className="scan-radar__ring scan-radar__ring--2" />
      <i className="scan-radar__ring scan-radar__ring--3" />
      <div className="scan-radar__sweep" />
      <i className="scan-radar__center" />
      {Array.from({ length: blips }, (_, i) => (
        <i key={i} className="scan-radar__blip" style={{ left: `${BLIP_SPOTS[i][0]}%`, top: `${BLIP_SPOTS[i][1]}%` }} />
      ))}
    </div>
  )
}
