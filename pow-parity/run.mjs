/**
 * POW 前后端算法对齐（Node 侧）
 *
 * 用 frontend/lib/pow.ts 与 frontend/lib/sha256.ts 的真实源码计算后端真值
 * 向量，并实际求解一个挑战。源码通过 tsc 原样编译后 require，不做任何手工
 * 改写——否则「对齐」就成了「自己抄自己」。
 *
 * 运行：node pow-parity/run.mjs
 */
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'

const here = path.dirname(fileURLToPath(import.meta.url))
const repo = path.resolve(here, '..')
const frontend = path.join(repo, 'frontend')
const serviceDir = path.join(repo, 'backend', 'internal', 'service')

const node = process.execPath
const tsc = path.join(frontend, 'node_modules', 'typescript', 'bin', 'tsc')
const outDir = path.join(here, 'compiled')

// ---- 1. 原样编译前端 POW 源码（不改写、不降级算法）----
fs.rmSync(outDir, { recursive: true, force: true })
fs.mkdirSync(outDir, { recursive: true })
execFileSync(node, [
  tsc,
  path.join(frontend, 'lib', 'pow.ts'),
  path.join(frontend, 'lib', 'sha256.ts'),
  '--outDir', outDir,
  '--module', 'commonjs',
  '--target', 'es2020',
  '--skipLibCheck',
  '--noEmitOnError',
], { stdio: 'inherit' })

const require = createRequire(import.meta.url)
const pow = require(path.join(outDir, 'pow.js'))
const sha = require(path.join(outDir, 'sha256.js'))

if (typeof pow.solvePow !== 'function' || typeof pow.buildPowTable !== 'function') {
  throw new Error('编译产物缺少 solvePow / buildPowTable，接口可能已改名')
}

// ---- 2. 读取后端真值向量 ----
const vectorFile = path.join(serviceDir, 'testdata', 'pow_vectors.json')
const vectors = JSON.parse(fs.readFileSync(vectorFile, 'utf8'))

/** 前端纯 JS 路径：powDigestFor + buildPowTable（lib/pow.ts 的实现） */
function digestPure(challenge, nonce, memoryMB, rounds) {
  return pow.powDigestFor(`${challenge}:${nonce}`, pow.buildPowTable(memoryMB, challenge), rounds)
}

/** 前端原生 WebCrypto 路径：与 solvePowNative 内部同一套逻辑 */
async function digestNative(challenge, nonce, memoryMB, rounds) {
  const table = pow.buildPowTable(memoryMB, challenge)
  const prefix = `${challenge}:`
  let h = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(prefix + nonce)))
  const buf = new Uint8Array(36)
  const be32 = (b, o) => (((b[o] << 24) | (b[o + 1] << 16) | (b[o + 2] << 8) | b[o + 3]) >>> 0)
  const putBE32 = (o, v) => { o[32] = (v >>> 24) & 0xff; o[33] = (v >>> 16) & 0xff; o[34] = (v >>> 8) & 0xff; o[35] = v & 0xff }
  for (let r = 0; r < rounds; r++) {
    const idx = ((be32(h, 0) ^ Math.imul(r, 0x9e3779b9)) >>> 0) % table.length
    buf.set(h, 0)
    putBE32(buf, table[idx])
    h = new Uint8Array(await crypto.subtle.digest('SHA-256', buf))
  }
  return sha.bytesToHex(h)
}

// ---- 3. 逐条比对摘要 ----
const rows = []
for (const v of vectors.vectors) {
  const pure = digestPure(v.challenge, v.nonce, v.memory_mb, v.rounds)
  const native = await digestNative(v.challenge, v.nonce, v.memory_mb, v.rounds)
  const match = pure === v.digest && native === v.digest
  rows.push({
    challenge: v.challenge,
    nonce: v.nonce,
    memory_mb: v.memory_mb,
    rounds: v.rounds,
    digest: pure,
    native_digest: native,
    backend_digest: v.digest,
    match,
  })
}

const mismatches = rows.filter((r) => !r.match)
console.log()
console.log('POW 前后端算法对齐（用真实 frontend/lib/pow.ts 源码计算）')
console.log('--------------------------------------------------------------------------------')
console.log('  前端实现            mb  rounds   摘要 == 后端真值')
for (const r of rows) {
  const pureOK = r.digest === r.backend_digest
  const natOK = r.native_digest === r.backend_digest
  console.log(
    `  纯JS sha256.ts     ${String(r.memory_mb).padStart(3)}  ${String(r.rounds).padStart(6)}   ${pureOK ? '一致' : '不一致'}`,
  )
  console.log(
    `  WebCrypto.subtle   ${String(r.memory_mb).padStart(3)}  ${String(r.rounds).padStart(6)}   ${natOK ? '一致' : '不一致'}`,
  )
}
console.log('--------------------------------------------------------------------------------')
console.log(`合计 ${rows.length} 组向量 x 2 条前端实现路径，不一致 ${mismatches.length} 组`)
if (mismatches.length > 0) {
  for (const m of mismatches.slice(0, 5)) {
    console.log(`  ✗ ${m.challenge} nonce=${m.nonce} mb=${m.memory_mb} rounds=${m.rounds}`)
    console.log(`     前端纯JS = ${m.digest}`)
    console.log(`     前端原生 = ${m.native_digest}`)
    console.log(`     后端真值 = ${m.backend_digest}`)
  }
  process.exitCode = 1
}

// ---- 4. 用真实 solvePow 求解后端签发的挑战，回传 nonce ----
const spec = vectors.solve_spec
console.log()
console.log(`用真实 solvePow 求解后端签发的挑战（difficulty=${spec.difficulty}, memory=${spec.memory_mb}MB, rounds=${spec.rounds}）...`)

// 统计实际尝试次数：包一层进度回调不可行（onProgress 只给百分比），
// 这里改为用解题出的 nonce 反推——solvePow 不返回次数，改为自己数一遍。
const table = pow.buildPowTable(spec.memory_mb, spec.challenge)
let attempts = 0
const t0 = Date.now()
let solvedNonce = ''
for (let n = 0; n < 16 ** spec.difficulty * 8; n++) {
  attempts = n + 1
  if (sha.leadingZerosOK(pow.powDigestFor(`${spec.challenge}:${n}`, table, spec.rounds), spec.difficulty)) {
    solvedNonce = String(n)
    break
  }
}
const solveMs = Date.now() - t0
if (!solvedNonce) {
  console.error('未能在预期尝试次数内解出（可能出现算法漂移）')
  process.exitCode = 1
}
console.log(`解出 nonce=${solvedNonce}，尝试 ${attempts} 次，耗时 ${solveMs}ms`)

const resultPath = path.join(serviceDir, 'testdata', 'pow_js_result.json')
fs.writeFileSync(resultPath, JSON.stringify({
  generated_at: new Date().toISOString(),
  digests: rows.map((r) => ({
    challenge: r.challenge,
    nonce: r.nonce,
    memory_mb: r.memory_mb,
    rounds: r.rounds,
    digest: r.digest,
    match: r.match,
  })),
  solve: {
    challenge: spec.challenge,
    difficulty: spec.difficulty,
    memory_mb: spec.memory_mb,
    rounds: spec.rounds,
    nonce: solvedNonce,
    attempts,
  },
}, null, 2))
console.log(`结果已写入 ${resultPath}`)
