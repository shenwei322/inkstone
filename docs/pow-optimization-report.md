# POW 优化报告

对应测试报告：[pow-interception-report.md](./pow-interception-report.md)

测试报告里量出来的问题，逐条给出处理。**所有改动都保持协议兼容**：
`powDigest` 算法与挑战下发格式未变，`memory_mb` / `rounds` 仍在签发时快照下发，
因此老版本前端不会被这次升级打断（唯一新增的 `scene` 字段是可选的）。

验证方式：`cd backend && go test ./...`（含全量既有测试），连续多次运行零失败。

---

## 一、改了什么

### 1. 修复 X-Forwarded-For 限流绕过（高危 → 已修）

**问题**：`middleware.ClientIP` 无条件优先读客户端自带的 `X-Forwarded-For`，
而 `main.go` 从未调用 `router.SetTrustedProxies()`（gin 默认信任所有代理）。
任何能直连后端的客户端只要每次请求换一个 XFF 值，就能作废全部 IP 限流。

**修复**（`internal/middleware/clientip.go` 新建）：

1. 只有「直连对端」落在可信代理网段内，才采信转发头；
2. 采信时**从右往左**扫描 XFF，跳过可信代理，取第一个不可信地址
   （最左值是客户端可随意伪造的，不能直接采信）；
3. 其余情况一律回退 TCP 对端地址；
4. `main.go` 同时调用 `router.SetTrustedProxies()`，两处口径一致。

可信网段默认只含本机与私网（`127/8`、`::1`、`10/8`、`172.16/12`、`192.168/16`、`fc00::/7`），
覆盖「同机 / 同 compose 网络的 Nginx 反代」这一标准部署；
可用新增环境变量 `TRUSTED_PROXIES` 覆盖，写 `none` 表示完全不信任代理。
启动日志会打印实际生效的网段，配置错了好排查。

**实测效果**：

| 场景 | 修复前 | 修复后 |
|---|---|---|
| 公网直连 + 逐请求伪造 XFF | 放行 **200/200** | 放行 **0/200** |
| 公网直连 + 无 XFF | 放行 30/100 | 放行 30/100（不变） |
| 可信反代 + 100 个不同访客 | — | 100/100 全部放行（不误伤） |
| 可信反代 + 链式 XFF 左侧伪造 | 采信伪造值 | 取右侧真实值，正确限流 |

反向场景也做了回归测试：反代部署下 100 个不同访客各有独立配额，
不会被算成同一个人——避免"修了漏洞、却把限流变成全局误伤"。

### 2. 挑战绑定签发场景（跨场景挪用 0% → 100%）

**问题**：签发接口统一且不记场景，可以为最便宜的场景批量领挑战，
再拿去打登录等敏感场景，把 PoW 成本与实际攻击目标解耦。

**修复**：`powChallenge` 增加 `scene` 字段，新增 `IssueFor(scene)`；
`Verify` 在算力校验之前先比对场景。签发接口新增可选的 `scene` 字段
（请求体或 `X-Pow-Scene` 头），**不带则按"不绑定"处理**，老前端行为不变。

场景名做大小写与空白归一化，避免 `"Login"` 被误拒。
`takeChallenge` 仍是先删后校验，所以跨场景尝试同样要付一次完整求解成本，
且被拒的挑战无法二次使用（防重放语义未被削弱）。

**实测效果**：

| 档位 | 修复前 | 修复后 |
|---|---|---|
| A13 login 挑战拿去 comment 用 | 0% 拦截 | **100% 拦截** |

### 3. 服务端校验提速约 50 倍（3.77ms → 0.07ms）

测试报告发现服务端 4.5ms 里几乎全是「重建内存表」，而表大小对攻击者的
搜索成本几乎没有影响——因为表是**每挑战建一次**，不进入候选 nonce 的搜索循环。
反过来 `rounds` 乘在每一个候选上：客户端付 `16^difficulty` 次，服务端只付 1 次。

所以最优方向是「调小表、调大轮数」。用 `pow_calibrate_test.go` 标定后定默认值：

| 配置 | 服务端校验 | 客户端求解 | 每候选哈希数 |
|---|---|---|---|
| 旧默认 8MB / 4 轮 | 3.77 ms | ~40–80 ms | 5 |
| **新默认 1MB / 12 轮** | **0.07 ms** | ~65–78 ms | **13** |

结果：**服务端快约 50 倍，攻击者每个候选的成本反而提高 2.6 倍**，
客户端耗时量级不变。单核验证吞吐从 13 次/秒升到约 1591 次/秒。

配套改动：
- `powDigest` 拆出 `digestWithTable`，表由调用方提供；
- 新增表缓冲池 `acquirePowTable` / `releasePowTable`，避免每次校验新分配一整张表；
- `acquirePowTable` 只接受 MB（不接受 u32 个数）——两种单位差 262144 倍，
  写这个优化时确实踩过一次：把长度当 MB 传，内部又乘一次，申请了 8TB 内存把进程打死。
  现在越界直接 panic，并有 `TestPowTablePool` 钉住池化结果与直算逐位一致。

### 4. 修正对 signal 的失实描述（文档/注释）

实测「脚本解题 + 合成 signal」拦截率为 0%，signal 不构成对机器人的防护。
按"降级为 UX 门槛"处理：**保留功能，但不再把它宣传成防机器人手段**。

具体改动：
- `pow_service.go` 文件头把"防护能力"重写为「真正有效的两条」与「不要指望的两条」，
  明确写出 signal 拦截率 0%、内存表对搜索成本无影响；
- `SettingPowMinEvents` 注释补上"注意：signal 只是 UX 门槛，不构成对机器人的防护"；
- `SettingPowMemoryMB` / `SettingPowRounds` 注释写明取舍依据与实测数据。

同时把 `TestPowCostAndThroughput` 的"单核理论验证吞吐"改用校验耗时计算
（原先误用客户端求解耗时，数字差了两个数量级）。

---

## 二、没改什么（以及为什么）

- **`powDigest` 算法未动**。它是前后端共享契约，改了老前端直接验不过；
  且实测算法本身没有正确性问题（24 组向量 × 2 条前端实现路径逐位一致）。
- **`signal` 未做密码学加固**。要真正不可伪造需要重构为「与 challenge 绑定 +
  事件流最小时间跨度 + 严格递增」等约束，属于协议变更，超出本次"兼容优先"的范围。
  已在注释中如实标注其能力边界，避免后来者误信。
- **未引入挑战与 IP 的绑定**。IP 会在移动网络 / IPv6 隐私地址下变化，
  硬绑会造成正常用户验证失败；场景绑定已能覆盖"挪用"这一主要攻击路径。
- **未重做内存硬化**。现有构造下提高 `memoryMB` 主要是给服务端自己加成本。
  真正的内存硬化需要改协议（如 scrypt/Argon2 式的大内存多次访问），
  应在明确需要对抗 GPU 集群时再单独设计。

---

## 三、兼容性

| 项目 | 是否变更 | 影响 |
|---|---|---|
| `powDigest` 算法 | 否 | 前后端摘要逐位一致（已复测） |
| `/pow/challenge` 响应字段 | 否 | 老前端无需改动 |
| `/pow/challenge` 请求体 | 新增可选 `scene` | 不带则行为同旧版 |
| `memory_mb` / `rounds` 默认值 | 是（1 / 12） | 随挑战下发，前端按响应执行，无感 |
| `middleware.ClientIP` 语义 | 是 | 反代走公网 IP 时必须配 `TRUSTED_PROXIES` |
| 新增环境变量 | `TRUSTED_PROXIES` | 不配则用内置默认（本机+私网） |

**唯一需要运维注意的是 `TRUSTED_PROXIES`**：如果反代和后端不在同一台机器、
且反代通过公网 IP 连过来，必须显式指定反代地址，否则所有访客会被算成同一个人。
默认值已覆盖绝大多数部署（同机 / 同 compose 网络），启动日志会打印生效值。

---

## 四、复现

```bash
cd D:\inkstone\backend

# 全量（含既有 update/lap/sanitize 测试）
go test ./... -count=1 -v

# XFF 与限流
go test ./internal/handler/ -run 'TestClientIPResolution|TestPowChallenge' -v

# 场景绑定
go test ./internal/service/ -run 'TestPowScene' -v

# 参数标定与成本
go test ./internal/service/ -run 'TestPowCalibrate|TestPowCostAndThroughput|TestPowTablePool' -v

# 拦截率矩阵（A13 已变为 100% 拦截）
go test ./internal/service/ -run TestPowInterceptionMatrix -v

# 前后端算法对拍（需 Node + frontend/node_modules/typescript）
node D:\inkstone\pow-parity\run.mjs
```

前端类型检查：`npx tsc --noEmit -p frontend/tsconfig.json`（已通过）。

---

## 五、效果汇总

| 指标 | 优化前 | 优化后 |
|---|---|---|
| 伪造 XFF 绕过限流 | 200/200 全部放行 | 0/200（已阻断） |
| 跨场景挪用挑战 | 拦截率 0% | 拦截率 **100%** |
| 服务端单次校验 | 3.77 ms | **0.07 ms**（约 50×） |
| 单核验证吞吐 | 13 次/秒 | 约 **1591 次/秒** |
| 攻击者每候选成本 | 5 次 SHA-256 | **13 次**（2.6×） |
| 客户端求解耗时 | ~40–80 ms | ~65–78 ms（量级不变） |
| 真人误杀率 | 0% | 0%（仍有回归测试钉住） |
| signal 的能力描述 | 声称能防纯 HTTP 脚本 | 如实标注为 UX 门槛 |
