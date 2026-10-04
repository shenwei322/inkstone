/**
 * 带外部 settle 句柄的 Promise。
 *
 * 人机验证的三个 provider（pow / geetest / lap）都需要「run() 被调用时先
 * 拿到 Promise，晚一些再由组件内部 resolve/reject」。以前的写法是在
 * `new Promise((resolve, reject) => { pendingRef.current = { resolve, reject } })`
 * 里直接把句柄存进 ref，没法把 promise 本身也存进去——于是 run() 被并发
 * 调用时会覆盖 pendingRef，被覆盖的那个 Promise 永远不 settle，它的 await
 * 永久挂起（提交按钮卡在 loading，只能刷新页面）。
 *
 * 有了这个工具就能把 promise 一起存进 pending，重复调用直接复用同一个。
 */
export interface Deferred<T> {
  promise: Promise<T>
  resolve: (value: T) => void
  reject: (reason?: unknown) => void
}

export function createDeferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}
