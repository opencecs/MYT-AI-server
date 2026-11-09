import { DeviceState, AndroidInfo, SlotExpiry } from '../types/index'

const USE_MOCK = false

const MOCK_DEVICES: DeviceState[] = [
  { ip: '192.168.1.101', device_type: 'p1', device_id: 'P1-mock-001', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 1, disk_used: 45 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.102', device_type: 'p1', device_id: 'P1-mock-002', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 2, disk_used: 98 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.103', device_type: 'q1', device_id: 'CQR-mock-001', device_name: 'q1_v3', version: 'v3', port: 30105, state: 0, order: 3, disk_used: 20 * 1024 * 1024 * 1024, disk_total: 64 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.104', device_type: 'r1s', device_id: 'CQR-mock-002', device_name: 'r1s_v3', version: 'v3', port: 30105, state: 1, order: 0 },
  { ip: '192.168.1.105', device_type: 'p1', device_id: 'P1-mock-003', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 4, disk_used: 110 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.106', device_type: 'q1', device_id: 'CQR-mock-003', device_name: 'q1_v3', version: 'v3', port: 30105, state: 0, order: 5, disk_used: 10 * 1024 * 1024 * 1024, disk_total: 64 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.107', device_type: 'c1', device_id: 'CQR-mock-004', device_name: 'c1_v3', version: 'v3', port: 30105, state: 1, order: 0 },
  { ip: '192.168.1.108', device_type: 'p1', device_id: 'P1-mock-004', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 0, disk_used: 60 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.109', device_type: 'p1', device_id: 'P1-mock-005', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 6, disk_used: 70 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.110', device_type: 'q1', device_id: 'CQR-mock-005', device_name: 'q1_v3', version: 'v3', port: 30105, state: 0, order: 7, disk_used: 30 * 1024 * 1024 * 1024, disk_total: 64 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.111', device_type: 'p1', device_id: 'P1-mock-006', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 8, disk_used: 85 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.112', device_type: 'r1s', device_id: 'CQR-mock-006', device_name: 'r1s_v3', version: 'v3', port: 30105, state: 1, order: 0 },
  { ip: '192.168.1.113', device_type: 'p1', device_id: 'P1-mock-007', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 9, disk_used: 50 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.114', device_type: 'q1', device_id: 'CQR-mock-007', device_name: 'q1_v3', version: 'v3', port: 30105, state: 0, order: 10, disk_used: 25 * 1024 * 1024 * 1024, disk_total: 64 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.115', device_type: 'p1', device_id: 'P1-mock-008', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 11, disk_used: 95 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.116', device_type: 'c1', device_id: 'CQR-mock-008', device_name: 'c1_v3', version: 'v3', port: 30105, state: 0, order: 0, disk_used: 15 * 1024 * 1024 * 1024, disk_total: 64 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.117', device_type: 'p1', device_id: 'P1-mock-009', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 12, disk_used: 40 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.118', device_type: 'q1', device_id: 'CQR-mock-009', device_name: 'q1_v3', version: 'v3', port: 30105, state: 1, order: 0 },
  { ip: '192.168.1.119', device_type: 'p1', device_id: 'P1-mock-010', device_name: 'p1_v3', version: 'v3', port: 30105, state: 0, order: 13, disk_used: 55 * 1024 * 1024 * 1024, disk_total: 128 * 1024 * 1024 * 1024 },
  { ip: '192.168.1.120', device_type: 'q1', device_id: 'CQR-mock-010', device_name: 'q1_v3', version: 'v3', port: 30105, state: 0, order: 14, disk_used: 35 * 1024 * 1024 * 1024, disk_total: 64 * 1024 * 1024 * 1024 },
]

const MOCK_ANDROIDS: AndroidInfo[] = [
  // P1-mock-001: 坑位1(1运行+4关机), 坑位2(1运行+2关机), 坑位3(1运行+1关机)
  { id: 'android-p1-001-1-1', name: '微信-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-1-2', name: '微信-02', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-1-3', name: '抖音-01', status: 'stopped', image: 'P116-ALL-v0.4.8:Android 12', indexNum: 1, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-1-4', name: '抖音-02', status: 'created', image: 'P116-ALL-v0.4.8:Android 12', indexNum: 1, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-1-5', name: '抖音-03', status: 'created', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-2-1', name: 'QQ-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-2-2', name: 'QQ-02', status: 'stopped', image: 'P116-ALL-v0.4.8:Android 12', indexNum: 2, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-2-3', name: '微博-01', status: 'stopped', image: 'P116-ALL-v0.4.8:Android 12', indexNum: 2, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-3-1', name: '淘宝-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 3, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  { id: 'android-p1-001-3-2', name: '京东-01', status: 'stopped', image: 'P116-ALL-v0.4.8:Android 12', indexNum: 3, deviceId: 'P1-mock-001', ip: '192.168.1.101' },
  // P1-mock-002: 坑位1(1运行+2关机), 坑位2(1运行)
  { id: 'android-p1-002-1-1', name: '游戏-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-002', ip: '192.168.1.102' },
  { id: 'android-p1-002-1-2', name: '游戏-02', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-002', ip: '192.168.1.102' },
  { id: 'android-p1-002-1-3', name: '游戏-03', status: 'created', image: 'CQR16-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-002', ip: '192.168.1.102' },
  { id: 'android-p1-002-2-1', name: '社交-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-002', ip: '192.168.1.102' },
  // CQR-mock-001: 坑位1(1运行+1关机), 坑位2(1运行)
  { id: 'android-cqr-001-1-1', name: '办公-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'CQR-mock-001', ip: '192.168.1.103' },
  { id: 'android-cqr-001-1-2', name: '办公-02', status: 'stopped', image: 'CQR16-ALL-v0.4.8:Android 12', indexNum: 1, deviceId: 'CQR-mock-001', ip: '192.168.1.103' },
  { id: 'android-cqr-001-2-1', name: '私用-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'CQR-mock-001', ip: '192.168.1.103' },
  // P1-mock-003: 坑位1(1运行+2关机), 坑位2(1运行+1关机), 坑位3(1运行), 坑位4(1关机)
  { id: 'android-p1-003-1-1', name: '开发-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 1, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  { id: 'android-p1-003-1-2', name: '开发-02', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  { id: 'android-p1-003-1-3', name: '开发-03', status: 'created', image: 'CQR16-ALL-v0.4.8:Android 12', indexNum: 1, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  { id: 'android-p1-003-2-1', name: '测试-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  { id: 'android-p1-003-2-2', name: '测试-02', status: 'stopped', image: 'CQR16-ALL-v0.4.8:Android 12', indexNum: 2, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  { id: 'android-p1-003-3-1', name: '生产-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 3, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  { id: 'android-p1-003-4-1', name: '预发-01', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 4, deviceId: 'P1-mock-003', ip: '192.168.1.105' },
  // CQR-mock-003
  { id: 'android-cqr-003-1-1', name: '客服-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'CQR-mock-003', ip: '192.168.1.106' },
  // P1-mock-004
  { id: 'android-p1-004-1-1', name: '运营-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-004', ip: '192.168.1.108' },
  { id: 'android-p1-004-2-1', name: '运营-02', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 2, deviceId: 'P1-mock-004', ip: '192.168.1.108' },
  // P1-mock-005: 坑位1(1运行+1关机), 坑位2(1运行), 坑位3(1运行)
  { id: 'android-p1-005-1-1', name: '管理-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-005', ip: '192.168.1.109' },
  { id: 'android-p1-005-1-2', name: '管理-02', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-005', ip: '192.168.1.109' },
  { id: 'android-p1-005-2-1', name: '运维-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-005', ip: '192.168.1.109' },
  { id: 'android-p1-005-3-1', name: '质检-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 3, deviceId: 'P1-mock-005', ip: '192.168.1.109' },
  // CQR-mock-005
  { id: 'android-cqr-005-1-1', name: '销售-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'CQR-mock-005', ip: '192.168.1.110' },
  { id: 'android-cqr-005-2-1', name: '销售-02', status: 'stopped', image: 'CQR16-ALL-v0.4.8:Android 12', indexNum: 2, deviceId: 'CQR-mock-005', ip: '192.168.1.110' },
  // P1-mock-006
  { id: 'android-p1-006-1-1', name: '直播-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-006', ip: '192.168.1.111' },
  { id: 'android-p1-006-2-1', name: '直播-02', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-006', ip: '192.168.1.111' },
  // P1-mock-007
  { id: 'android-p1-007-1-1', name: '电商-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 1, deviceId: 'P1-mock-007', ip: '192.168.1.113' },
  { id: 'android-p1-007-2-1', name: '电商-02', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 2, deviceId: 'P1-mock-007', ip: '192.168.1.113' },
  { id: 'android-p1-007-3-1', name: '电商-03', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 3, deviceId: 'P1-mock-007', ip: '192.168.1.113' },
  // CQR-mock-007
  { id: 'android-cqr-007-1-1', name: '客服-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'CQR-mock-007', ip: '192.168.1.114' },
  // P1-mock-008: 坑位1(1运行+1关机), 坑位2(1运行), 坑位3(1运行), 坑位4(1运行)
  { id: 'android-p1-008-1-1', name: '财务-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-008', ip: '192.168.1.115' },
  { id: 'android-p1-008-1-2', name: '财务-02', status: 'stopped', image: 'P116-ALL-v0.5.0:Android 12', indexNum: 1, deviceId: 'P1-mock-008', ip: '192.168.1.115' },
  { id: 'android-p1-008-2-1', name: '人事-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-008', ip: '192.168.1.115' },
  { id: 'android-p1-008-3-1', name: '法务-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 3, deviceId: 'P1-mock-008', ip: '192.168.1.115' },
  { id: 'android-p1-008-4-1', name: '行政-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 4, deviceId: 'P1-mock-008', ip: '192.168.1.115' },
  // CQR-mock-008
  { id: 'android-cqr-008-1-1', name: '售后-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'CQR-mock-008', ip: '192.168.1.116' },
  // P1-mock-009
  { id: 'android-p1-009-1-1', name: '推广-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'P1-mock-009', ip: '192.168.1.117' },
  { id: 'android-p1-009-2-1', name: '推广-02', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 2, deviceId: 'P1-mock-009', ip: '192.168.1.117' },
  // P1-mock-010
  { id: 'android-p1-010-1-1', name: '营销-01', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 1, deviceId: 'P1-mock-010', ip: '192.168.1.119' },
  { id: 'android-p1-010-2-1', name: '营销-02', status: 'running', image: 'P116-ALL-v0.5.0:Android 15', indexNum: 2, deviceId: 'P1-mock-010', ip: '192.168.1.119' },
  { id: 'android-p1-010-3-1', name: '营销-03', status: 'running', image: 'P116-ALL-v0.5.0:Android 14', indexNum: 3, deviceId: 'P1-mock-010', ip: '192.168.1.119' },
  // CQR-mock-010
  { id: 'android-cqr-010-1-1', name: '接待-01', status: 'running', image: 'CQR16-ALL-v0.5.0:Android 14', indexNum: 1, deviceId: 'CQR-mock-010', ip: '192.168.1.120' },
  { id: 'android-cqr-010-2-1', name: '接待-02', status: 'stopped', image: 'CQR16-ALL-v0.4.8:Android 12', indexNum: 2, deviceId: 'CQR-mock-010', ip: '192.168.1.120' },
]

const MOCK_SLOT_EXPIRY: Record<string, SlotExpiry[]> = {
  'P1-mock-001': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i < 3 ? ['1790000000', '1718000000', String(Math.floor(Date.now() / 1000) + 2 * 86400)][i] : '1790000000', state: [0, 2, 1][i] ?? 0 })),
  'P1-mock-002': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i === 0 ? '0' : '1790000000', state: i === 0 ? 2 : 0 })),
  'CQR-mock-001': Array.from({ length: 12 }, (_, i) => ({ index: i, expire_time: i === 1 ? String(Math.floor(Date.now() / 1000) + 1 * 86400) : '1790000000', state: i === 1 ? 1 : 0 })),
  'P1-mock-003': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: [0, 1, 2, 3].includes(i) ? ['1790000000', '1718000000', String(Math.floor(Date.now() / 1000) + 2 * 86400), '1790000000'][i] : '1790000000', state: [0, 2, 1, 0][i] ?? 0 })),
  'CQR-mock-003': Array.from({ length: 12 }, (_, i) => ({ index: i, expire_time: '1790000000', state: 0 })),
  'P1-mock-004': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i === 1 ? '0' : '1790000000', state: i === 1 ? 2 : 0 })),
  'P1-mock-005': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i === 1 ? String(Math.floor(Date.now() / 1000) + 3 * 86400) : '1790000000', state: i === 1 ? 1 : 0 })),
  'CQR-mock-005': Array.from({ length: 12 }, (_, i) => ({ index: i, expire_time: i === 1 ? '1718000000' : '1790000000', state: i === 1 ? 2 : 0 })),
  'P1-mock-006': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: '1790000000', state: 0 })),
  'P1-mock-007': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i === 1 ? String(Math.floor(Date.now() / 1000) + 1 * 86400) : i === 2 ? '0' : '1790000000', state: i === 1 ? 1 : i === 2 ? 2 : 0 })),
  'CQR-mock-007': Array.from({ length: 12 }, (_, i) => ({ index: i, expire_time: '1790000000', state: 0 })),
  'P1-mock-008': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i === 2 ? String(Math.floor(Date.now() / 1000) + 2 * 86400) : i === 3 ? '1718000000' : '1790000000', state: i === 2 ? 1 : i === 3 ? 2 : 0 })),
  'CQR-mock-008': Array.from({ length: 12 }, (_, i) => ({ index: i, expire_time: '1790000000', state: 0 })),
  'P1-mock-009': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: '1790000000', state: 0 })),
  'P1-mock-010': Array.from({ length: 24 }, (_, i) => ({ index: i, expire_time: i === 1 ? String(Math.floor(Date.now() / 1000) + 1 * 86400) : '1790000000', state: i === 1 ? 1 : 0 })),
  'CQR-mock-010': Array.from({ length: 12 }, (_, i) => ({ index: i, expire_time: i === 1 ? '0' : '1790000000', state: i === 1 ? 2 : 0 })),
}

export { USE_MOCK, MOCK_DEVICES, MOCK_ANDROIDS, MOCK_SLOT_EXPIRY }
