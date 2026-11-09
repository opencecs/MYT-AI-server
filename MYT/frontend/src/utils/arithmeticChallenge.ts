import type { ConfirmChallenge } from '../components/ConfirmDialog'

/** 随机整数 [min, max]（含两端） */
const randInt = (min: number, max: number) => min + Math.floor(Math.random() * (max - min + 1))

/**
 * 生成随机两位数加/减法算术题（结果非负），用于危险操作确认。
 * 减法保证被减数 ≥ 减数；答案统一转字符串，比较时由调用方做 trim 后精确匹配。
 */
export function makeArithmeticChallenge(): ConfirmChallenge {
  const a = randInt(10, 99)
  const b = randInt(10, 99)
  const plus = Math.random() < 0.5
  const [x, y, op, result] = plus
    ? [a, b, '+', a + b]
    : a >= b ? [a, b, '−', a - b] : [b, a, '−', b - a]
  return { question: `${x} ${op} ${y} = ?`, expectedAnswer: String(result) }
}
