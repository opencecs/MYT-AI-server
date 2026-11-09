export interface StreamInfo {
  streamName: string
  appName: string
  publisherIP: string
  startTime: string
  videoBitrate: number
  audioBitrate: number
}

export interface PullerInfo {
  deviceIP: string
  type: string
  streamName: string
}

export interface Distribution {
  id: string
  streamName: string
  deviceIp: string
  cloudMachineId: string
  cloudMachineName?: string
  protocol: string
  mappedPort?: number
  resolution?: string
  status: 'active' | 'inactive' | 'pending' | 'unknown'
  statusReason?: string
  p2pListenPort?: number
}

export interface CameraStream {
  id: string
  deviceIp: string
  controlHost?: string
  devicePort: number
  cloudMachineId: string
  cloudMachineName?: string
  camName: string
  width: number
  height: number
  fps: number
  bitrate: number
  status: 'running' | 'stopped'
  pid?: number | null
  loading?: boolean
}

export type StreamTab = 'forward' | 'p2p' | 'camera' | 'guide'
