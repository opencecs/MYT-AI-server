// compareIP 按数值比较 IPv4（10.10.1.9 < 10.10.1.10），非法 IP 退回字符串比较
export function compareIP(a: string, b: string): number {
  const pa = a.split('.').map(Number)
  const pb = b.split('.').map(Number)
  if (pa.length === 4 && pb.length === 4 && pa.every(n => !isNaN(n)) && pb.every(n => !isNaN(n))) {
    for (let i = 0; i < 4; i++) {
      if (pa[i] !== pb[i]) return pa[i] - pb[i]
    }
    return 0
  }
  return a < b ? -1 : a > b ? 1 : 0
}
