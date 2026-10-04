package service

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shenwei/inkstone/backend/pkg/config"
)

func newTestUpdateService(t *testing.T, sourceDir string) *UpdateService {
	t.Helper()
	svc := NewUpdateService(&config.Config{
		AppEnv:             "test",
		UpdateEnabled:      true,
		UpdateRepoURL:      "https://github.com/shenwei234/inkstone",
		UpdateBranch:       "main",
		UpdateGitHubAPI:    "https://api.github.com",
		UpdateSourceDir:    sourceDir,
		UpdateDir:          filepath.Join(t.TempDir(), "update"),
		UpdateDeployedFile: filepath.Join(t.TempDir(), "deployed-commit.json"),
		UpdateWaitingAgent: true,
	}, nil)
	return svc
}

// ---------------------------------------------------------------------------
// 仓库地址解析
// ---------------------------------------------------------------------------

func TestSplitRepo(t *testing.T) {
	cases := []struct {
		url       string
		wantOwner string
		wantName  string
	}{
		{"https://github.com/shenwei234/inkstone", "shenwei234", "inkstone"},
		{"https://github.com/shenwei234/inkstone.git", "shenwei234", "inkstone"},
		{"git@github.com:shenwei234/inkstone.git", "shenwei234", "inkstone"},
		{"https://gitee.com/owner/name", "owner", "name"},
		{"https://git.example.com/group/owner/name.git", "owner", "name"},
		{"", "", ""},
		{"inkstone", "", ""},
	}
	for _, c := range cases {
		owner, name := splitRepo(c.url)
		if owner != c.wantOwner || name != c.wantName {
			t.Errorf("splitRepo(%q) = (%q, %q), want (%q, %q)", c.url, owner, name, c.wantOwner, c.wantName)
		}
	}
}

func TestRepoFullName(t *testing.T) {
	if got := repoFullName("https://github.com/shenwei234/inkstone.git"); got != "shenwei234/inkstone" {
		t.Errorf("repoFullName = %q", got)
	}
	if got := repoFullName(""); got != "" {
		t.Errorf("空仓库地址应返回空串，得到 %q", got)
	}
}

// ---------------------------------------------------------------------------
// 提交哈希比较与展示
// ---------------------------------------------------------------------------

func TestSameCommit(t *testing.T) {
	full := "ba0677b8c7defa22c0a296630f5e63199174bd79"
	cases := []struct {
		a, b string
		want bool
	}{
		{full, full, true},
		{full, "ba0677b", true},
		{"BA0677B", "ba0677b8c7defa22c0a296630f5e63199174bd79", true},
		{full, "ba0677c", false},
		{full, "", false},
		{"", "", false},
		{"abc", "abc", true}, // 短于 7 位时只做全等比较
		{"abc", "abcd", false},
	}
	for _, c := range cases {
		if got := sameCommit(c.a, c.b); got != c.want {
			t.Errorf("sameCommit(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestShortCommitAndNormalizeHash(t *testing.T) {
	if got := ShortCommit("ba0677b8c7defa22"); got != "ba0677b" {
		t.Errorf("ShortCommit = %q", got)
	}
	if got := ShortCommit("abc"); got != "abc" {
		t.Errorf("短哈希应原样返回，得到 %q", got)
	}
	if got := normalizeHash("  BA0677B8  "); got != "ba0677b8" {
		t.Errorf("normalizeHash = %q", got)
	}
}

func TestLooksLikeHash(t *testing.T) {
	cases := map[string]bool{
		"ba0677b8c7defa22c0a296630f5e63199174bd79": true,
		"BA0677B":      true,
		"1234567":      true,
		"123456":       false, // 太短
		"zzzzzzz":      false, // 非十六进制
		"refs/heads/m": false,
		"":             false,
	}
	for input, want := range cases {
		if got := looksLikeHash(input); got != want {
			t.Errorf("looksLikeHash(%q) = %v, want %v", input, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 更新源响应解析
// ---------------------------------------------------------------------------

func TestParseCommitsResponseGitHub(t *testing.T) {
	body := []byte(`[
		{"sha":"BA0677B8C7DEFA22C0A296630F5E63199174BD79",
		 "commit":{"message":"feat: 增加更新系统\n\n详细说明","author":{"name":"shenwei","date":"2026-09-26T20:49:28Z"}},
		 "html_url":"https://github.com/shenwei234/inkstone/commit/ba0677b"},
		{"sha":"a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
		 "commit":{"message":"fix: 修复分页","author":{"name":"other","date":"2026-09-25T10:00:00Z"}}}
	]`)
	commits, ok := parseCommitsResponse(body)
	if !ok || len(commits) != 2 {
		t.Fatalf("解析失败：ok=%v len=%d", ok, len(commits))
	}
	if commits[0].Hash != "ba0677b8c7defa22c0a296630f5e63199174bd79" {
		t.Errorf("哈希未规范化为小写：%q", commits[0].Hash)
	}
	if commits[0].Author != "shenwei" || commits[0].Date != "2026-09-26T20:49:28Z" {
		t.Errorf("作者/时间解析错误：%+v", commits[0])
	}
	if !strings.Contains(commits[0].Message, "增加更新系统") {
		t.Errorf("提交说明解析错误：%q", commits[0].Message)
	}
}

func TestParseCommitsResponseWrapped(t *testing.T) {
	body := []byte(`{"commits":[{"hash":"ba0677b8c7de","message":"中文提交","author":"阿伟","timestamp":1790000000}]}`)
	commits, ok := parseCommitsResponse(body)
	if !ok || len(commits) != 1 {
		t.Fatalf("包装结构解析失败：ok=%v", ok)
	}
	if commits[0].Author != "阿伟" {
		t.Errorf("作者解析错误：%q", commits[0].Author)
	}
	if commits[0].Date == "" {
		t.Error("秒级时间戳应被规范化为 RFC3339")
	}
}

func TestParseCommitsResponseRejectsNoise(t *testing.T) {
	if _, ok := parseCommitsResponse([]byte(`{"error":"rate limited"}`)); ok {
		t.Error("错误响应不应被当成提交列表")
	}
	if _, ok := parseCommitsResponse([]byte(`[{"id":123}]`)); ok {
		t.Error("纯数字 id 不应被当成提交哈希")
	}
}

func TestParseLatestResponse(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		branch string
		want   string
	}{
		{"纯对象", `{"sha":"ba0677b8c7defa22"}`, "main", "ba0677b8c7defa22"},
		{"包装 commit 字符串", `{"commit":"BA0677B8C7DEFA22"}`, "main", "ba0677b8c7defa22"},
		{"包装 latest 对象", `{"latest":{"sha":"a1b2c3d4e5f6a7b8","message":"hi"}}`, "main", "a1b2c3d4e5f6a7b8"},
		{"分支映射", `{"branches":{"main":{"sha":"c1d2e3f4a5b6c7d8"}}}`, "main", "c1d2e3f4a5b6c7d8"},
		{"数组取首条", `[{"sha":"d1e2f3a4b5c6d7e8"}]`, "main", "d1e2f3a4b5c6d7e8"},
	}
	for _, c := range cases {
		got, ok := parseLatestResponse([]byte(c.body), c.branch)
		if !ok || got.Hash != c.want {
			t.Errorf("%s: parseLatestResponse = (%+v, %v), want %q", c.name, got, ok, c.want)
		}
	}
	if _, ok := parseLatestResponse([]byte(`{"branches":{"dev":{"sha":"c1d2e3f4a5b6c7d8"}}}`), "main"); ok {
		t.Error("分支不匹配时不应返回提交")
	}
}

func TestParseCompareResponse(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	from := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	body, _ := json.Marshal(map[string]any{
		"total_commits": 2,
		"files":         5,
		"commits": []map[string]any{
			{"sha": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "commit": map[string]any{"message": "新提交一"}},
			{"sha": from, "commit": map[string]any{"message": "本机版本"}},
		},
	})
	res, ok := parseCompareResponse(svc, body, from, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if !ok {
		t.Fatal("compare 响应解析失败")
	}
	if len(res.commits) != 1 || res.commits[0].Message != "新提交一" {
		t.Fatalf("应只保留本机版本之后的新提交：%+v", res.commits)
	}
	// total_commits 覆盖推断值，并带上文件数说明
	if res.behind != 2 {
		t.Errorf("behind = %d, want 2", res.behind)
	}
	if !strings.Contains(res.note, "5 个文件") {
		t.Errorf("缺少文件变更说明：%q", res.note)
	}
}

func TestParseCompareResponseWithoutFrom(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	// 对比接口返回的列表里没有本机版本：应保留全部并给出说明，而不是谎报落后 0
	body := []byte(`{"commits":[{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","commit":{"message":"x"}}]}`)
	res, ok := parseCompareResponse(svc, body, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if !ok {
		t.Fatal("解析失败")
	}
	if res.behind != -1 {
		t.Errorf("behind = %d, want -1（未知）", res.behind)
	}
	if len(res.commits) != 1 {
		t.Errorf("应保留提交明细，实际 %d 条", len(res.commits))
	}
}

// ---------------------------------------------------------------------------
// 解压安全
// ---------------------------------------------------------------------------

func TestSafeArchivePath(t *testing.T) {
	cases := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"inkstone-main/backend/go.mod", "backend/go.mod", false},
		{"inkstone-main/", "", false},
		{"./inkstone-main/frontend/package.json", "frontend/package.json", false},
		// 穿越条目必须在 path.Clean 折叠之前就被拒绝（否则会退化成「无操作」而静默放行）
		{"inkstone-main/../evil.txt", "", true},
		{"inkstone-main/backend/../../evil", "", true},
		{"../evil.txt", "", true},
		{"/etc/passwd", "", true},
		{"inkstone-main\\backend\\go.mod", "backend/go.mod", false},
	}
	for _, c := range cases {
		got, err := safeArchivePath(c.name)
		if c.wantErr {
			if err == nil {
				t.Errorf("safeArchivePath(%q) 应报错，得到 %q", c.name, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("safeArchivePath(%q) 意外报错：%v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("safeArchivePath(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

// makeTarGz 构造一个 tar.gz 包用于解压测试。
func makeTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf strings.Builder
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// map 顺序不稳定，但对本测试无影响（用固定顺序避免随机性）
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		content := entries[name]
		header := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("写 tar 头失败：%v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("写 tar 内容失败：%v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("关闭 tar 失败：%v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("关闭 gzip 失败：%v", err)
	}

	path := filepath.Join(t.TempDir(), "pkg.tar.gz")
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("写测试包失败：%v", err)
	}
	return path
}

func TestExtractArchive(t *testing.T) {
	archive := makeTarGz(t, map[string]string{
		"inkstone-main/backend/cmd/server/main.go": "package main",
		"inkstone-main/backend/go.mod":             "module x",
		"inkstone-main/frontend/package.json":      "{}",
		"inkstone-main/README.md":                  "docs",
	})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	for _, rel := range requiredArchiveFiles {
		if !isFile(filepath.Join(dest, filepath.FromSlash(rel))) {
			t.Errorf("缺少文件：%s", rel)
		}
	}
	// 顶层目录必须被剥离
	if dirExists(filepath.Join(dest, "inkstone-main")) {
		t.Error("顶层目录未被剥离")
	}
	if err := validateStagedSource(dest); err != nil {
		t.Errorf("合法源码包校验失败：%v", err)
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	archive := makeTarGz(t, map[string]string{
		"inkstone-main/../../evil.txt": "pwned",
		"inkstone-main/ok.txt":         "fine",
	})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err == nil {
		t.Fatal("含路径穿越的包应被拒绝")
	}
	// 穿越目标绝不能落在解压目录之外
	if isFile(filepath.Join(filepath.Dir(dest), "evil.txt")) {
		t.Fatal("路径穿越文件被写出了解压目录")
	}
}

func TestExtractArchiveSkipsSymlink(t *testing.T) {
	var buf strings.Builder
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "inkstone-main", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: "inkstone-main/link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777,
	}); err != nil {
		t.Fatal(err)
	}
	content := "package main"
	if err := tw.WriteHeader(&tar.Header{
		Name: "inkstone-main/backend/cmd/server/main.go", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(content)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()

	archive := filepath.Join(t.TempDir(), "symlink.tar.gz")
	if err := os.WriteFile(archive, []byte(buf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatalf("解压失败：%v", err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); err == nil {
		t.Error("符号链接不应被解压出来")
	}
}

func TestValidateStagedSourceIncomplete(t *testing.T) {
	dest := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dest, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "backend", "go.mod"), []byte("module x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateStagedSource(dest); err == nil {
		t.Error("缺少入口文件的包应校验失败（避免把错误页当源码替换进去）")
	}
}

// ---------------------------------------------------------------------------
// 受保护路径与变更计算
// ---------------------------------------------------------------------------

func TestIsGuardedPath(t *testing.T) {
	guarded := []string{
		"data/deployed-commit.json",
		"backend/data/deployed-commit.json", // 嵌套层级也要保护
		".env",
		"backend/.env",
		"docker-compose.prod.yml/.env",
		"frontend/node_modules/react/index.js",
		"uploads/a.png",
		"files/b.zip",
		".git/config",
		".update/state.json",
		"frontend/.next/build-manifest.json",
	}
	for _, rel := range guarded {
		if !isGuardedPath(rel) {
			t.Errorf("%s 应受保护", rel)
		}
	}
	allowed := []string{
		"backend/go.mod",
		"frontend/package.json",
		"docker-compose.prod.yml",
		"deploy/nginx/inkstone.conf",
		// 站点源码里真的有这两个目录名：只能靠「顶层前缀」保护，
		// 一旦把它们放进路径段匹配，上游对它们的改动就会被静默跳过。
		"frontend/app/admin/files/page.tsx",
		"frontend/components/uploads-view.tsx",
	}
	for _, rel := range allowed {
		if isGuardedPath(rel) {
			t.Errorf("%s 不应受保护（会静默漏掉上游更新）", rel)
		}
	}
}

// writeFiles 在 root 下批量写文件（路径用 / 分隔）。
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("建目录失败：%v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("写文件失败：%v", err)
		}
	}
}

func TestStagedChangesDetectsWriteAndDelete(t *testing.T) {
	staged := t.TempDir()
	source := t.TempDir()
	writeFiles(t, staged, map[string]string{
		"backend/go.mod":             "module x",         // 与源码一致
		"backend/internal/new.go":    "package internal", // 新增
		"backend/internal/keep.go":   "package internal", // 双方一致
		"frontend/package.json":      `{"v":2}`,          // 修改
		"data/deployed-commit.json":  `{"commit":"x"}`,   // 受保护，必须忽略
		"frontend/.env":              "SECRET=1",         // 受保护
		"frontend/node_modules/a.js": "dep",              // 受保护
	})
	writeFiles(t, source, map[string]string{
		"backend/go.mod":           "module x",
		"backend/internal/keep.go": "package internal",
		"frontend/package.json":    `{"v":1}`,
		"docs/old.md":              "upstream 已删除",
	})

	changes, err := stagedChanges(staged, source)
	if err != nil {
		t.Fatalf("stagedChanges 失败：%v", err)
	}
	got := map[string]string{}
	for _, c := range changes {
		got[c.Path] = c.Action
	}
	if got["backend/internal/new.go"] != "write" {
		t.Errorf("新增文件未识别：%+v", got)
	}
	if got["frontend/package.json"] != "write" {
		t.Errorf("修改文件未识别：%+v", got)
	}
	if got["docs/old.md"] != "delete" {
		t.Errorf("上游删除的文件未识别：%+v", got)
	}
	if _, exists := got["backend/go.mod"]; exists {
		t.Error("内容一致的文件不应出现在变更里")
	}
	if _, exists := got["backend/internal/keep.go"]; exists {
		t.Error("双方都有的未修改文件不应出现在变更里")
	}
	for path := range got {
		if isGuardedPath(path) {
			t.Errorf("受保护路径出现在变更里：%s", path)
		}
	}

	writes, deletes, _ := summarizeChanges(changes)
	if writes != 2 || deletes != 1 {
		t.Errorf("变更统计错误：writes=%d deletes=%d", writes, deletes)
	}
}

// ---------------------------------------------------------------------------
// 一键更新的核心：备份 + 替换 + 回滚
// ---------------------------------------------------------------------------

func TestApplyStagedAndRollback(t *testing.T) {
	staged := t.TempDir()
	source := t.TempDir()
	writeFiles(t, staged, map[string]string{
		"backend/cmd/server/main.go": "package main // v2",
		"backend/internal/new.go":    "package internal // 新增",
		"frontend/package.json":      `{"version":"2"}`,
	})
	writeFiles(t, source, map[string]string{
		"backend/cmd/server/main.go": "package main // v1",
		"frontend/package.json":      `{"version":"1"}`,
		"backend/data/keep.json":     `{"keep":true}`,
		".env":                       "DB_PASSWORD=secret",
	})

	svc := newTestUpdateService(t, source)
	backupID, changes, err := svc.applyStaged(staged)
	if err != nil {
		t.Fatalf("applyStaged 失败：%v", err)
	}
	if backupID == "" {
		t.Fatal("备份 ID 为空")
	}
	if len(changes) != 3 {
		t.Fatalf("变更数量 = %d, want 3（两个修改 + 一个新增）", len(changes))
	}

	readFile := func(rel string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("读 %s 失败：%v", rel, err)
		}
		return string(raw)
	}
	if got := readFile("backend/cmd/server/main.go"); !strings.Contains(got, "v2") {
		t.Errorf("源码未替换：%q", got)
	}
	if got := readFile("backend/internal/new.go"); !strings.Contains(got, "新增") {
		t.Errorf("新增文件未写入：%q", got)
	}
	// 受保护文件必须原样保留
	if got := readFile("backend/data/keep.json"); !strings.Contains(got, "keep") {
		t.Errorf("data 目录被覆盖：%q", got)
	}
	if got := readFile(".env"); !strings.Contains(got, "secret") {
		t.Errorf(".env 被覆盖：%q", got)
	}

	// 回滚后恢复原内容，且保留受保护文件
	if err := svc.rollback(backupID); err != nil {
		t.Fatalf("rollback 失败：%v", err)
	}
	if got := readFile("backend/cmd/server/main.go"); !strings.Contains(got, "v1") {
		t.Errorf("回滚后内容不对：%q", got)
	}
	if got := readFile("frontend/package.json"); !strings.Contains(got, `"1"`) {
		t.Errorf("回滚后 package.json 不对：%q", got)
	}
	// 本次新增的文件必须被删掉，否则回滚后新旧代码混在一起
	if isFile(filepath.Join(source, "backend", "internal", "new.go")) {
		t.Error("回滚应删除本次新增的文件")
	}
	if got := readFile(".env"); !strings.Contains(got, "secret") {
		t.Errorf("回滚不应破坏 .env：%q", got)
	}
}

// 纯新增更新（备份里没有可还原内容）的回滚必须是幂等成功，而不是报「备份为空」。
func TestRollbackPureAdditions(t *testing.T) {
	staged := t.TempDir()
	source := t.TempDir()
	writeFiles(t, staged, map[string]string{
		"backend/only_new.go": "package backend",
		"backend/keep.go":     "package backend", // 与源码一致：不应算作变更
	})
	writeFiles(t, source, map[string]string{"backend/keep.go": "package backend"})

	svc := newTestUpdateService(t, source)
	backupID, changes, err := svc.applyStaged(staged)
	if err != nil {
		t.Fatalf("applyStaged 失败：%v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("变更数量 = %d, want 1", len(changes))
	}
	if !isFile(filepath.Join(source, "backend", "only_new.go")) {
		t.Fatal("新增文件未写入")
	}
	if err := svc.rollback(backupID); err != nil {
		t.Fatalf("纯新增更新的回滚不应报错：%v", err)
	}
	if isFile(filepath.Join(source, "backend", "only_new.go")) {
		t.Error("回滚后新增文件应被删除")
	}
	if !isFile(filepath.Join(source, "backend", "keep.go")) {
		t.Error("回滚不应删除与本次更新无关的文件")
	}
}

// countChanges 是「本机没有版本记录」时判定是否有更新的依据，必须准确。
func TestCountChanges(t *testing.T) {
	staged := t.TempDir()
	source := t.TempDir()
	writeFiles(t, staged, map[string]string{
		"backend/go.mod":        "module x",
		"backend/new.go":        "package backend",
		"frontend/package.json": `{"v":2}`,
	})
	writeFiles(t, source, map[string]string{
		"backend/go.mod":        "module x", // 一致
		"frontend/package.json": `{"v":1}`,  // 修改
		"docs/gone.md":          "上游已删除",
	})

	count, err := countChanges(staged, source)
	if err != nil {
		t.Fatalf("countChanges 失败：%v", err)
	}
	if count != 3 {
		t.Errorf("变更数量 = %d, want 3（新增 1 + 修改 1 + 删除 1）", count)
	}

	// 完全一致的源码树：应判定 0 变更（= 已是最新）
	same := t.TempDir()
	writeFiles(t, same, map[string]string{
		"backend/go.mod":        "module x",
		"backend/new.go":        "package backend",
		"frontend/package.json": `{"v":2}`,
	})
	sameCount, err := countChanges(staged, same)
	if err != nil {
		t.Fatalf("countChanges 失败：%v", err)
	}
	if sameCount != 0 {
		t.Errorf("一致源码树的变更数量 = %d, want 0", sameCount)
	}
}

func TestRollbackRejectsUnknownBackup(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	if err := svc.rollback("nonexistent-20260101-000000"); err == nil {
		t.Error("不存在的备份应报错")
	}
	if err := svc.rollback("../../etc"); err == nil {
		t.Error("带路径分隔符的备份 ID 应被拒绝")
	}
	if err := svc.rollback(""); err == nil {
		t.Error("空备份 ID 应报错")
	}
}

func TestApplyStagedReplacesFileWithDirectory(t *testing.T) {
	staged := t.TempDir()
	source := t.TempDir()
	// 上游把 config 从文件改成了目录
	writeFiles(t, staged, map[string]string{"frontend/config/index.js": "module.exports = {}"})
	writeFiles(t, source, map[string]string{"frontend/config": "old file"})

	svc := newTestUpdateService(t, source)
	if _, _, err := svc.applyStaged(staged); err != nil {
		t.Fatalf("文件→目录替换失败：%v", err)
	}
	if !isFile(filepath.Join(source, "frontend", "config", "index.js")) {
		t.Error("目录结构未正确建立")
	}
}

func TestApplyRejectsBadInput(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	ctx := context.Background()
	if err := svc.Apply(ctx, "", "tester"); err == nil {
		t.Error("空目标应被拒绝")
	}
	if err := svc.Apply(ctx, "not-a-hash", "tester"); err == nil {
		t.Error("非法哈希应被拒绝")
	}
	// 目标与当前运行版本相同（部署记录指向它）时不应启动任务
	source := t.TempDir()
	svc2 := newTestUpdateService(t, source)
	if err := WriteDeployedCommit(svc2.recorder().commitPath(), "ba0677b8c7defa22c0a296630f5e63199174bd79"); err != nil {
		t.Fatal(err)
	}
	if err := svc2.Apply(ctx, "ba0677b8c7defa22c0a296630f5e63199174bd79", "tester"); err == nil {
		t.Error("与当前版本相同的目标应被拒绝")
	}
}

// ---------------------------------------------------------------------------
// 部署记录
// ---------------------------------------------------------------------------

func TestDeployedRecorderResolveAndWrite(t *testing.T) {
	root := t.TempDir()
	recorder := deployedRecorder{file: filepath.Join(t.TempDir(), "deployed-commit.json"), repoRoot: root}

	if rec := recorder.resolve(); rec.Commit != "" {
		t.Fatalf("空目录不应解析出部署记录：%+v", rec)
	}

	// 写入路径回退到显式配置，同时把文件建出来
	path := recorder.commitPath()
	if path != recorder.file {
		t.Fatalf("commitPath 应回退到显式配置 %q，得到 %q", recorder.file, path)
	}
	if err := WriteDeployedCommit(path, "ba0677b8c7defa22c0a296630f5e63199174bd79"); err != nil {
		t.Fatalf("写入部署记录失败：%v", err)
	}
	rec := recorder.resolve()
	if rec.Commit != "ba0677b8c7defa22c0a296630f5e63199174bd79" {
		t.Errorf("读回的提交不对：%q", rec.Commit)
	}
	if rec.Path != path {
		t.Errorf("路径记录不对：%q", rec.Path)
	}
	if rec.UpdatedAt == "" {
		t.Error("应写入更新时间")
	}
	// 自动探测布局：<repoRoot>/data/deployed-commit.json 也要能读到
	fallbackRoot := t.TempDir()
	fallback := deployedRecorder{repoRoot: fallbackRoot}
	if err := WriteDeployedCommit(filepath.Join(fallbackRoot, "data", "deployed-commit.json"), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if got := fallback.resolve().Commit; got != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("自动探测失败：%q", got)
	}
}

func TestDeployedRecorderRunningCommit(t *testing.T) {
	root := t.TempDir()
	deployed := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recorder := deployedRecorder{file: filepath.Join(root, "deployed-commit.json"), repoRoot: root}
	if err := WriteDeployedCommit(recorder.file, deployed); err != nil {
		t.Fatal(err)
	}

	original := BuildCommit
	defer func() { BuildCommit = original }()

	BuildCommit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	commit, _, from := recorder.runningCommit()
	if commit != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || from != CommitSourceLDFlags {
		t.Errorf("应优先使用编译期注入的提交：%q from=%q", commit, from)
	}

	BuildCommit = ""
	commit, _, from = recorder.runningCommit()
	if commit != deployed || from != CommitSourceDeployed {
		t.Errorf("无 ldflags 时应回退到部署记录：%q from=%q", commit, from)
	}

	// 既无 ldflags 也无部署记录时回退环境变量
	empty := deployedRecorder{file: filepath.Join(t.TempDir(), "none.json"), repoRoot: t.TempDir()}
	t.Setenv("INKSTONE_COMMIT", "cccccccccccccccccccccccccccccccccccccccc")
	commit, _, from = empty.runningCommit()
	if commit != "cccccccccccccccccccccccccccccccccccccccc" || from != CommitSourceEnv {
		t.Errorf("应回退到环境变量：%q from=%q", commit, from)
	}
}

// ---------------------------------------------------------------------------
// 下载
// ---------------------------------------------------------------------------

func TestHTTPDownloadChecksumAndLength(t *testing.T) {
	payload := strings.Repeat("inkstone-source-", 512)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "inkstone-updater/") {
			t.Errorf("缺少 User-Agent：%q", got)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	client := newHTTPClient(5*time.Second, "")
	dest := filepath.Join(t.TempDir(), "sub", "pkg.tar.gz")
	res, err := client.download(context.Background(), server.URL+"/archive", dest, nil)
	if err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	if res.Bytes != int64(len(payload)) {
		t.Errorf("字节数 = %d, want %d", res.Bytes, len(payload))
	}
	sum := sha256.Sum256([]byte(payload))
	if res.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 校验值不对：%s", res.SHA256)
	}
	if !isFile(dest) {
		t.Error("目标文件未生成")
	}
}

func TestHTTPDownloadFailureCleansUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar.gz")
	client := newHTTPClient(5*time.Second, "")
	if _, err := client.download(context.Background(), server.URL, dest, nil); err == nil {
		t.Fatal("404 应返回错误")
	}
	if isFile(dest) {
		t.Error("失败后不应留下半截文件")
	}
}

func TestHTTPDownloadDetectsTruncatedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 声明 100 字节却只发 10 字节
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()

	client := newHTTPClient(5*time.Second, "")
	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")
	if _, err := client.download(context.Background(), server.URL, dest, nil); err == nil {
		t.Error("响应体被截断时应报错")
	}
}

// ---------------------------------------------------------------------------
// 待更新清单（宿主代理契约）
// ---------------------------------------------------------------------------

func TestWritePendingManifest(t *testing.T) {
	source := t.TempDir()
	svc := newTestUpdateService(t, source)
	svc.mu.Lock()
	svc.state.Last = &UpdateStage{Operator: "tester"}
	svc.mu.Unlock()
	changes := []changeEntry{{Path: "backend/go.mod", Action: "write", Bytes: 12}}

	if err := writePendingManifest(svc, "ba0677b8c7defa22", "/staged/ba0677b8", "ba0677b-20260101-000000", changes); err != nil {
		t.Fatalf("写清单失败：%v", err)
	}
	raw, err := os.ReadFile(svc.manifestPath())
	if err != nil {
		t.Fatalf("读清单失败：%v", err)
	}
	var manifest pendingManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("清单不是合法 JSON：%v", err)
	}
	if manifest.Action != "rebuild" || manifest.State != "pending" {
		t.Errorf("清单内容不对：%+v", manifest)
	}
	if manifest.StagedDir != "/staged/ba0677b8" || manifest.SourceDir != source {
		t.Errorf("路径字段不对：%+v", manifest)
	}
	if len(manifest.Changes) != 1 {
		t.Errorf("变更清单未写入：%+v", manifest.Changes)
	}
	if manifest.RequestedBy != "tester" {
		t.Errorf("发起人未写入：%q", manifest.RequestedBy)
	}
	if manifest.Mode != RebuildModeWaitingAgent {
		t.Errorf("重建方式未写入：%q", manifest.Mode)
	}
	if manifest.ResultFile != filepath.Join(svc.cfg.UpdateDir, "update-result.json") {
		t.Errorf("结果文件路径不对：%q", manifest.ResultFile)
	}

	// 回滚清单：action 必须是 rollback
	if err := writePendingManifest(svc, "rollback:ba0677b-20260101-000000", "", "ba0677b-20260101-000000", nil); err != nil {
		t.Fatalf("写回滚清单失败：%v", err)
	}
	raw, _ = os.ReadFile(svc.manifestPath())
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Action != "rollback" || manifest.BackupID != "ba0677b-20260101-000000" {
		t.Errorf("回滚清单内容不对：%+v", manifest)
	}
}

func TestAgentResultMissing(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	if res := svc.AgentResult(); res != nil {
		t.Errorf("没有结果文件时应返回 nil，得到 %+v", res)
	}
}

// ---------------------------------------------------------------------------
// 阶段常量与配置默认值
// ---------------------------------------------------------------------------

func TestRebuildModeAndHints(t *testing.T) {
	source := t.TempDir()
	svc := newTestUpdateService(t, source)
	if got := svc.Mode(); got != RebuildModeWaitingAgent {
		t.Errorf("默认模式 = %q, want %q", got, RebuildModeWaitingAgent)
	}
	if hint := svc.modeHint(); !strings.Contains(hint, "宿主更新代理") {
		t.Errorf("容器部署提示未说明需要宿主代理：%q", hint)
	}

	svc.cfg.UpdateWaitingAgent = false
	if got := svc.Mode(); got != RebuildModeInPlace {
		t.Errorf("无 compose 文件时应为本机重建，得到 %q", got)
	}
	if err := os.WriteFile(filepath.Join(source, "docker-compose.prod.yml"), []byte("services: {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := svc.Mode(); got != RebuildModeDocker {
		t.Errorf("有 compose 文件时应为 docker 模式，得到 %q", got)
	}

	svc.cfg.UpdateEnabled = false
	if got := svc.Mode(); got != RebuildModeUnavailable {
		t.Errorf("关闭更新后应为 unavailable，得到 %q", got)
	}
}

func TestArchiveURLsOrderAndPlaceholders(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	svc.cfg.UpdateMirror = "https://gh-proxy.com/{owner}/{name}/archive/{commit}.tar.gz"
	urls := svc.archiveURLs("ba0677b8c7defa22")
	if len(urls) != 2 {
		t.Fatalf("候选地址数量 = %d, want 2", len(urls))
	}
	if urls[0] != "https://gh-proxy.com/shenwei234/inkstone/archive/ba0677b8c7defa22.tar.gz" {
		t.Errorf("镜像地址展开错误：%s", urls[0])
	}
	if !strings.Contains(urls[1], "codeload.github.com/shenwei234/inkstone") {
		t.Errorf("兜底地址错误：%s", urls[1])
	}

	svc.cfg.UpdateMirror = ""
	urls = svc.archiveURLs("ba0677b8c7defa22")
	if len(urls) != 1 || !strings.Contains(urls[0], "codeload.github.com") {
		t.Errorf("未配置镜像时应只剩兜底地址：%+v", urls)
	}
}

func TestCompareURL(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	url := svc.compareURL("aaaaaaaaaaaa", "bbbbbbbbbbbb")
	want := "https://api.github.com/repos/shenwei234/inkstone/compare/aaaaaaaaaaaa...bbbbbbbbbbbb"
	if url != want {
		t.Errorf("compareURL = %q, want %q", url, want)
	}
	if got := svc.compareURL("", "bbbbbbbbbbbb"); got != "" {
		t.Errorf("没有基准版本时不应生成对比地址：%q", got)
	}
}
