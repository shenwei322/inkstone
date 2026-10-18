package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// =====================================================================
// POW 前后端算法对齐（Go 侧：生成基准向量）
//
// 完整的分语言对齐流程分两步：
//  第一步（本文件，Go）  生成「后端真值」向量文件，供 Node 侧比对；
//  第二步（Node）        用 frontend/lib/pow.ts 的真实源码计算同输入，
//                        比对摘要、并实际求解一个挑战回传 nonce；
//  第三步（本文件，Go）  读回 Node 的求解结果，确认后端接受该 nonce。
//
// 两边用的都是各自仓库里的真实实现，不存在「为对齐而另写一份」的失真。
// 任一侧算法漂移都会在这里立刻暴露。
// =====================================================================

var (
	powVectorFile = filepath.Join("testdata", "pow_vectors.json")
	powResultFile = filepath.Join("testdata", "pow_js_result.json")
)

// powExportEnv 是「写回向量文件」的显式开关。
//
// 这个测试默认只做校验，导出属于独立的运维动作，原因有二：
//  1. solve_spec 的挑战来自 pow.Issue()：32 字节随机数并写入挑战表，
//     每次运行都不同。若无条件写回，任何一次 go test ./... 都会改动
//     testdata/pow_vectors.json，与业务改动混在 git status 里被误提交；
//  2. 导出还会真的签发一条挑战进库，默认跑测试没必要付这个代价。
//
// 需要刷新真值向量时显式打开：
//
//	POW_EXPORT=1 go test -run TestPowExportVectors ./internal/service/
const powExportEnv = "POW_EXPORT"

// powVector 是一条「后端真值」记录。
type powVector struct {
	Challenge string `json:"challenge"`
	Nonce     string `json:"nonce"`
	MemoryMB  int    `json:"memory_mb"`
	Rounds    int    `json:"rounds"`
	Digest    string `json:"digest"`
	SolvesFor int    `json:"solves_for"` // 该 vector 的 nonce 是否满足该难度（0=不适用）
	Solves    bool   `json:"solves"`
}

type powVectorFileShape struct {
	GeneratedAt string      `json:"generated_at"`
	Difficulty  int         `json:"difficulty"`
	SolveSpec   powSolveTag `json:"solve_spec"`
	Vectors     []powVector `json:"vectors"`
}

// powSolveTag 描述「请 Node 求解这个挑战」的请求。
type powSolveTag struct {
	Challenge  string `json:"challenge"`
	Difficulty int    `json:"difficulty"`
	MemoryMB   int    `json:"memory_mb"`
	Rounds     int    `json:"rounds"`
}

// TestPowExportVectors 校验两条摘要入口自洽；设置 POW_EXPORT=1 时另外写出向量文件。
//
// 两个职责刻意分开：
//   - 校验（无条件执行）：逐条比对 powDigest（线上入口）与 powDigestSharedTable
//     （压测入口）。任何一侧算法漂移都会在这里立刻暴露 —— 这是本测试的核心价值，
//     不该依赖「是否导出」。
//   - 导出（POW_EXPORT=1）：签发一条真实挑战供 Node 侧求解，并整体落盘供
//     TestPowAcceptJSolverResult 使用。
//
// 早先是无条件写回，导致每跑一次 go test 就把 testdata 弄脏一次（见 powExportEnv 注释）。
func TestPowExportVectors(t *testing.T) {
	pow, _ := powTestEnv(t, map[string]string{SettingPowDifficulty: "4"})

	// 覆盖多种参数组合，别只用默认值
	type combo struct{ mb, rounds int }
	combos := []combo{{1, 1}, {2, 3}, {8, 4}, {32, 16}, {4, 1}, {16, 8}}

	vectors := make([]powVector, 0, len(combos)*4)
	for _, c := range combos {
		for i := 0; i < 4; i++ {
			challenge := "vec-" + strconv.Itoa(c.mb) + "-" + strconv.Itoa(c.rounds) + "-" + strconv.Itoa(i)
			nonce := strconv.Itoa(i*977 + 13)
			digest := powDigest(challenge, nonce, c.mb, c.rounds)
			if got := powDigestSharedTable(powTable(c.mb, challenge), challenge, nonce, c.rounds); got != digest {
				t.Fatalf("压测入口与线上入口不自洽: %s vs %s", digest, got)
			}
			vectors = append(vectors, powVector{
				Challenge: challenge,
				Nonce:     nonce,
				MemoryMB:  c.mb,
				Rounds:    c.rounds,
				Digest:    digest,
			})
		}
	}

	// 校验到此完成。以下只在导出模式下执行 —— 提前返回也避免白白签发挑战进库。
	if os.Getenv(powExportEnv) != "1" {
		t.Logf("已校验 %d 条真值向量（压测入口与线上入口一致）；写回 testdata 需显式 %s=1",
			len(vectors), powExportEnv)
		return
	}

	// 取一条真实签发的挑战作为「求解题」：难度 3（4096 次期望尝试），
	// Node 侧求解后回传，Go 侧必须接受。
	solveChallenge, d, mb, rounds, _, _, err := pow.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	out := powVectorFileShape{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Difficulty:  4,
		SolveSpec: powSolveTag{
			Challenge:  solveChallenge,
			Difficulty: d,
			MemoryMB:   mb,
			Rounds:     rounds,
		},
		Vectors: vectors,
	}

	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	blob, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(powVectorFile, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已写出 %d 条真值向量 -> %s", len(vectors), powVectorFile)
}

// powJSResult 是 Node 侧回传的求解结果。
type powJSResult struct {
	GeneratedAt string `json:"generated_at"`
	Digests     []struct {
		Challenge string `json:"challenge"`
		Nonce     string `json:"nonce"`
		MemoryMB  int    `json:"memory_mb"`
		Rounds    int    `json:"rounds"`
		Digest    string `json:"digest"`
		Match     bool   `json:"match"`
	} `json:"digests"`
	Solve struct {
		Challenge  string `json:"challenge"`
		Difficulty int    `json:"difficulty"`
		MemoryMB   int    `json:"memory_mb"`
		Rounds     int    `json:"rounds"`
		Nonce      string `json:"nonce"`
		Attempts   int    `json:"attempts"`
	} `json:"solve"`
}

// TestPowAcceptJSolverResult 第三步：校验 Node 侧用真实前端源码算出的结果。
// 没有结果文件时跳过（说明还没跑 Node 那一步）。
func TestPowAcceptJSolverResult(t *testing.T) {
	raw, err := os.ReadFile(powResultFile)
	if err != nil {
		t.Skipf("Node 侧结果尚未生成（%v），先运行前端对齐脚本", err)
	}

	var res powJSResult
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("Node 结果解析失败: %v", err)
	}

	// 1) 摘要必须逐位一致
	bad := 0
	for _, d := range res.Digests {
		if !d.Match {
			bad++
			t.Errorf("前端/后端摘要不一致 challenge=%q nonce=%q mb=%d rounds=%d: 前端=%s",
				d.Challenge, d.Nonce, d.MemoryMB, d.Rounds, d.Digest)
		}
	}
	if len(res.Digests) == 0 {
		t.Fatal("Node 未回传任何摘要比对结果")
	}
	t.Logf("摘要对齐：%d/%d 一致", len(res.Digests)-bad, len(res.Digests))

	// 2) Node 求出的 nonce 必须被后端真实验收
	if res.Solve.Nonce == "" {
		t.Fatal("Node 未回传求解 nonce")
	}
	digest := powDigest(res.Solve.Challenge, res.Solve.Nonce, res.Solve.MemoryMB, res.Solve.Rounds)
	if !leadingZeros(digest, res.Solve.Difficulty) {
		t.Fatalf("Node 求得的 nonce=%s 后端重算不满足难度 %d（digest=%s）",
			res.Solve.Nonce, res.Solve.Difficulty, digest)
	}
	t.Logf("求解对齐：nonce=%s（%d 次尝试）后端重算满足难度 %d",
		res.Solve.Nonce, res.Solve.Attempts, res.Solve.Difficulty)
}
