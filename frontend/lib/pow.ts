/**
 * POW v2（工作量证明 + 本地资源）求解器——与后端 pow_service.go 的
 * powDigest 严格一致，任一侧改动必须同步另一侧。
 *
 * 算法（两端逐位一致）：
 *   1. 内存表：TABLE_LEN = memoryMB * 262144 个 u32；种子为
 *      SHA-256(challenge) 前 16 字节（大端 4 个 u32）驱动 xorshift128 填充；
 *   2. 迭代：h = SHA-256(challenge + ":" + nonce)
 *      rounds 轮：
 *        idx = ( BE32(h[0:4]) ^ (r * 0x9E3779B9) ) % TABLE_LEN
 *        h   = SHA-256( h || BE32(table[idx]) )
 *   3. 答案：hex(h) 前 difficulty 个十六进制位为 '0'。
 *
 * 为什么这样设计（攻击者的成本在用户本地资源上）：
 *   - 每次验证都要构建并随机访问 memoryMB 大小的表——内存带宽瓶颈让
 *     GPU/ASIC 集群的并行优势大幅缩小（相对纯 SHA-256 循环）；
 *   - 服务端零成本只校验一次，全部计算负担落在访客浏览器本地。
 *
 * 计算路径：优先原生 WebCrypto（crypto.subtle，https/localhost 快数十倍），
 * 回退 lib/sha256.ts 纯 JS 实现（http 部署也可用）。
 */

import { sha256Bytes, sha256Raw, bytesToHex, leadingZerosOK } from './sha256'

export interface PowSolveOptions {
  challenge: string
  /** 难度：答案哈希前导零个数（1-6） */
  difficulty: number
  /** 内存表大小（MB，1-32） */
  memoryMB: number
  /** 表查找-混合轮数（1-16） */
  rounds: number
  /** 进度回调（0-100 估算，命中时直接到 100） */
  onProgress?: (percent: number) => void
  /** 取消判定（弹窗关闭/组件卸载时返回 true，结束计算并 reject） */
  cancelled?: () => boolean
}

/** BE32：大端读取 4 字节为无符号整数 */
function be32(bytes: Uint8Array, off: number): number {
  return ((bytes[off] << 24) | (bytes[off + 1] << 16) | (bytes[off + 2] << 8) | bytes[off + 3]) >>> 0
}

/** BE32 写入（4 字节大端） */
function putBE32(out: Uint8Array, off: number, v: number): void {
  out[off] = (v >>> 24) & 0xff
  out[off + 1] = (v >>> 16) & 0xff
  out[off + 2] = (v >>> 8) & 0xff
  out[off + 3] = v & 0xff
}

/**
 * xorshift128 伪随机发生器（与 Go 端 powXorshift128 一致）。
 * 种子 = SHA-256(challenge) 前 16 字节（大端 4 个 u32）。
 */
function makeXorshift128(challenge: string): { next: () => number } {
  const seed = sha256Bytes(challenge)
  let s0 = be32(seed, 0)
  let s1 = be32(seed, 4)
  let s2 = be32(seed, 8)
  let s3 = be32(seed, 12)
  return {
    next() {
      // t = s0 ^ (s0 << 11)，s3 = s3 ^ (s3 >> 19) ^ t ^ (t >> 8)（32 位回绕）
      const t = (s0 ^ Math.imul(s0, 2048)) >>> 0
      s0 = s1
      s1 = s2
      s2 = s3
      s3 = (s3 ^ (s3 >>> 19) ^ t ^ (t >>> 8)) >>> 0
      return s3
    },
  }
}

/** 构建内存表（Uint32Array，长度 = memoryMB * 262144） */
export function buildPowTable(memoryMB: number, challenge: string): Uint32Array {
  const table = new Uint32Array(memoryMB * 262144)
  const rng = makeXorshift128(challenge)
  for (let i = 0; i < table.length; i++) table[i] = rng.next()
  return table
}

const yieldToUI = () => new Promise<void>((resolve) => setTimeout(resolve, 0))

/** 估算进度：以期望尝试次数为单位，未命中前封顶 99% */
function progressOf(tried: number, expected: number): number {
  return Math.min(99, Math.floor((tried / expected) * 100))
}

/**
 * 求解满足难度的 nonce（十进制字符串）。
 * cancelled() 返回 true 时 reject（调用方按「已取消」处理）。
 */
export async function solvePow(opts: PowSolveOptions): Promise<string> {
  const { difficulty, memoryMB, rounds } = opts
  if (difficulty < 1 || difficulty > 6) throw new Error('人机验证难度配置无效')
  if (memoryMB < 1 || memoryMB > 32) throw new Error('人机验证内存参数无效')
  if (rounds < 1 || rounds > 16) throw new Error('人机验证参数无效')

  const expected = 16 ** difficulty
  const table = buildPowTable(memoryMB, opts.challenge)
  const hasSubtle =
    typeof crypto !== 'undefined' &&
    typeof crypto.subtle !== 'undefined' &&
    typeof crypto.subtle.digest === 'function'
  if (hasSubtle) return solvePowNative(opts, table, expected)
  return solvePowPure(opts, table, expected)
}

/** 原生 WebCrypto 路径：批量并发 digest，https/localhost 下速度最快 */
async function solvePowNative(
  opts: PowSolveOptions,
  table: Uint32Array,
  expected: number,
): Promise<string> {
  const { challenge, difficulty, rounds, onProgress, cancelled } = opts
  const prefix = `${challenge}:`
  const BATCH = 2048
  let base = 0

  for (;;) {
    if (cancelled?.()) throw new Error('__cancelled__')
    const jobs: Array<Promise<ArrayBuffer>> = []
    const metas: string[] = []
    for (let i = 0; i < BATCH; i++) {
      const nonce = String(base + i)
      metas.push(nonce)
      jobs.push(digestNative(prefix + nonce, table, rounds))
    }
    const results = await Promise.all(jobs)
    for (let i = 0; i < results.length; i++) {
      const bytes = new Uint8Array(results[i])
      if (leadingZerosOK(bytesToHex(bytes), difficulty)) {
        onProgress?.(100)
        return metas[i]
      }
    }
    base += BATCH
    onProgress?.(progressOf(base, expected))
    // 每批让出一次 UI 线程，保持进度条与取消按钮响应
    await yieldToUI()
  }
}

/** 原生：外层 h + rounds 轮表混合，返回最终摘要的 Promise */
async function digestNative(
  text: string,
  table: Uint32Array,
  rounds: number,
): Promise<ArrayBuffer> {
  // 注意：并发调用共享模块级缓冲区会互相覆盖，这里必须每次新建
  const buf = new Uint8Array(36)
  let h = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text)))
  for (let r = 0; r < rounds; r++) {
    // 注意：^ 会把操作数转 int32，结果必须 >>> 0 回无符号（与 Go uint32 一致）
    const idx = ((be32(h, 0) ^ Math.imul(r, 0x9e3779b9)) >>> 0) % table.length
    buf.set(h, 0)
    putBE32(buf, 32, table[idx])
    h = new Uint8Array(await crypto.subtle.digest('SHA-256', buf))
  }
  return h.buffer as ArrayBuffer
}

/** 纯 JS：给定输入文本，跑外层 h + rounds 轮表混合，输出最终摘要 hex（对拍/调试用） */
export function powDigestFor(text: string, table: Uint32Array, rounds: number): string {
  let h = sha256Bytes(text)
  const mix = new Uint8Array(36)
  for (let r = 0; r < rounds; r++) {
    // 注意：^ 会把操作数转 int32，结果必须 >>> 0 回无符号（与 Go uint32 一致）
    const idx = ((be32(h, 0) ^ Math.imul(r, 0x9e3779b9)) >>> 0) % table.length
    mix.set(h, 0)
    putBE32(mix, 32, table[idx])
    h = sha256Raw(mix)
  }
  return bytesToHex(h)
}

/** 纯 JS 回退路径：任何上下文可用（含 http 部署），逐批 yield */
async function solvePowPure(
  opts: PowSolveOptions,
  table: Uint32Array,
  expected: number,
): Promise<string> {
  const { challenge, difficulty, rounds, onProgress, cancelled } = opts
  const BATCH = 256
  let nonce = 0
  for (;;) {
    if (cancelled?.()) throw new Error('__cancelled__')
    for (let i = 0; i < BATCH; i++, nonce++) {
      const digest = powDigestFor(`${challenge}:${nonce}`, table, rounds)
      if (leadingZerosOK(digest, difficulty)) {
        onProgress?.(100)
        return String(nonce)
      }
    }
    onProgress?.(progressOf(nonce, expected))
    await yieldToUI()
  }
}
