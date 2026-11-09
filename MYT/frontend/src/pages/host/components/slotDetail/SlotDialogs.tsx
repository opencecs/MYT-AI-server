import { AndroidInfo, DeviceState, MirrorItem, SlotExpiry } from '../../../../types/index'
import { getDeviceId } from '../../../../utils/deviceId'
import { useDeployMode } from '../../../../hooks/useDeployMode'
import { AskFn } from '../../../../hooks/useConfirm'
import { containerDisplayName } from '../../../../utils/containerName'
import ScreenCastModal from '../../screencast/ScreenCastModal'
import SetStreamModal from '../../screencast/SetStreamModal'
import MoveModal from '../../operations/data/move/MoveModal'
import ExportModal from '../../operations/data/ExportModal'
import CopyModal from '../../operations/data/CopyModal'
import UpdateImageModal from '../../create/updateImage/UpdateImageModal'
import TerminalModal from '../../terminal/TerminalModal'
import VpcModal from '../../vpc/VpcModal'
import FileManagerModal from '../../terminal/FileManagerModal'
import BatchSwitchModelModal from '../../operations/model/BatchSwitchModelModal'
import RenameModal from '../../operations/system/RenameModal'
import SwitchBackupModal from '../../operations/model/SwitchBackupModal'
import ResetModal from '../../operations/system/ResetModal'
import S5ProxyModal from '../../operations/network/S5ProxyModal'
import GpsModal from '../../operations/network/GpsModal'
import GoogleCertModal from '../../operations/system/GoogleCertModal'
import ApiDetailModal from '../../diagnose/ApiDetailModal'
import DeviceProfileModal from '../../operations/profile/DeviceProfileModal'

/** SlotView 的弹窗集群：所有操作弹窗（投屏/移动/复制/重命名/重置等）集中渲染，透传 target + onClose。 */
export interface SlotDialogsProps {
  device: DeviceState
  androids: AndroidInfo[]
  devices: DeviceState[]
  slotExpiry: SlotExpiry[]
  mirrors: MirrorItem[]
  notes: Record<string, string>
  ask: AskFn
  castTarget: { ip: string; slot: number } | null
  moveTarget: AndroidInfo | null
  exportTarget: AndroidInfo | null
  copyTarget: AndroidInfo | null
  upgradeTarget: AndroidInfo | null
  terminalTarget: AndroidInfo | null
  switchModelTarget: AndroidInfo | null
  vpcTarget: AndroidInfo | null
  fileManagerTarget: AndroidInfo | null
  apiDetailTarget: AndroidInfo | null
  streamTarget: AndroidInfo | null
  renameTarget: AndroidInfo | null
  switchBackupTarget: AndroidInfo | null
  resetTarget: AndroidInfo | null
  s5Target: AndroidInfo | null
  gpsTarget: AndroidInfo | null
  googleCertTarget: AndroidInfo | null
  profileTarget: AndroidInfo | null
  onClose: {
    cast: () => void
    move: () => void
    exportA: () => void
    copy: () => void
    upgrade: () => void
    terminal: () => void
    switchModel: () => void
    vpc: () => void
    fileManager: () => void
    apiDetail: () => void
    stream: () => void
    rename: () => void
    switchBackup: () => void
    reset: () => void
    s5: () => void
    gps: () => void
    googleCert: () => void
    profile: () => void
  }
}

export function SlotDialogs({ device, androids, devices, slotExpiry, mirrors, notes, ask, castTarget, moveTarget, exportTarget, copyTarget, upgradeTarget, terminalTarget, switchModelTarget, vpcTarget, fileManagerTarget, apiDetailTarget, streamTarget, renameTarget, switchBackupTarget, resetTarget, s5Target, gpsTarget, googleCertTarget, profileTarget, onClose }: SlotDialogsProps) {
  const { mode } = useDeployMode()
  // V4 投屏身份 = (host, backend, instanceID)：castTarget 只带 (ip, slot)，按
  // (ip, indexNum) 回查该行实例取 android.id 的 "backend/id" 联合主键（V4 的 slot 是
  // 客户端派生序号，服务端不认识）。查不到时留空——Go 侧会报「实例标识缺失」。
  const castAndroid = castTarget
    ? androids.find(a => a.ip === castTarget.ip && a.indexNum === castTarget.slot)
    : undefined
  const v4Slash = mode === 'v4' && castAndroid ? castAndroid.id.indexOf('/') : -1
  return (
    <>
      {castTarget && (
        <ScreenCastModal
          deviceIP={castTarget.ip} slot={castTarget.slot} orientation="portrait"
          v4Backend={v4Slash > 0 ? castAndroid!.id.slice(0, v4Slash) : ''}
          v4InstanceID={castAndroid ? (v4Slash > 0 ? castAndroid.id.slice(v4Slash + 1) : castAndroid.id) : ''}
          v4Title={castAndroid ? (notes[castAndroid.id] || containerDisplayName(castAndroid.name, castAndroid.deviceId, castAndroid.indexNum)) : undefined}
          onClose={onClose.cast}
        />
      )}
      {moveTarget && <MoveModal deviceIP={getDeviceId(moveTarget, mode)} deviceID={moveTarget.deviceId} android={moveTarget} slotExpiry={slotExpiry} androids={androids} devices={devices} displayName={notes[moveTarget.id] || moveTarget.name} onClose={onClose.move} />}
      {exportTarget && <ExportModal deviceIP={getDeviceId(exportTarget, mode)} androidName={exportTarget.name} onClose={onClose.exportA} />}
      {copyTarget && <CopyModal deviceIP={getDeviceId(copyTarget, mode)} deviceID={copyTarget.deviceId} android={copyTarget} slotExpiry={slotExpiry} androids={androids} onClose={onClose.copy} />}
      {upgradeTarget && <UpdateImageModal device={device} android={upgradeTarget} onClose={onClose.upgrade} onUpdated={onClose.upgrade} />}
      {terminalTarget && (
        <TerminalModal deviceIP={getDeviceId(terminalTarget, mode)} containerName={terminalTarget.name}
          displayName={notes[terminalTarget.id] || terminalTarget.name} isV2={mode === 'v2'} isV4={mode === 'v4'} onClose={onClose.terminal} />
      )}
      {switchModelTarget && <BatchSwitchModelModal selectedAndroids={[switchModelTarget]} mirrors={mirrors} onClose={onClose.switchModel} />}
      {vpcTarget && <VpcModal deviceIP={getDeviceId(vpcTarget, mode)} androidName={vpcTarget.name} displayName={notes[vpcTarget.id] || vpcTarget.name} androids={androids} onClose={onClose.vpc} />}
      {fileManagerTarget && <FileManagerModal deviceIP={getDeviceId(fileManagerTarget, mode)} androidName={fileManagerTarget.name} displayName={notes[fileManagerTarget.id] || fileManagerTarget.name} onClose={onClose.fileManager} />}
      {apiDetailTarget && <ApiDetailModal android={apiDetailTarget} deviceIP={getDeviceId(apiDetailTarget, mode)} onClose={onClose.apiDetail} />}
      {streamTarget && <SetStreamModal deviceIP={getDeviceId(streamTarget, mode)} android={streamTarget} onClose={onClose.stream} />}
      {renameTarget && <RenameModal android={renameTarget} onClose={onClose.rename} />}
      {switchBackupTarget && <SwitchBackupModal android={switchBackupTarget} onClose={onClose.switchBackup} />}
      {resetTarget && <ResetModal android={resetTarget} ask={ask} onClose={onClose.reset} />}
      {s5Target && <S5ProxyModal android={s5Target} onClose={onClose.s5} />}
      {gpsTarget && <GpsModal android={gpsTarget} onClose={onClose.gps} />}
      {googleCertTarget && <GoogleCertModal android={googleCertTarget} onClose={onClose.googleCert} />}
      {profileTarget && <DeviceProfileModal android={profileTarget} onClose={onClose.profile} />}
    </>
  )
}
