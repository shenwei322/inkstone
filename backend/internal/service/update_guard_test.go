package service

import (
	"strings"
	"testing"
)

// TestIsGuardedPathWindowsCase NTFS 大小写不敏感，守卫比较也必须不敏感。
// 此前 HasPrefix("data/") 是大小写敏感比较，上游包里放 Data/x.json、.ENV、
// .Git/config 就能骗过守卫，把站点数据/密钥/部署记录覆盖掉。
func TestIsGuardedPathWindowsCase(t *testing.T) {
	mustGuard := []string{
		// 大小写变体（Windows NTFS 落到磁盘是同一目录）
		"data/x.json",
		"Data/x.json",
		"DATA/x.json",
		"DaTa/sqlite/blog.db",
		"backend/data/x.json",
		"Backend/Data/x.json",
		"NODE_MODULES/pkg/index.js",
		"Node_Modules/pkg/index.js",
		".GIT/config",
		".Git/config",
		".NEXT/build-manifest.json",
		".UPDATE/state.json",
		".TOOLS/go/bin/go.exe",
		// .env 变体
		".env",
		".ENV",
		".Env.local",
		".ENV.production",
		"backend/.ENV",
		// Windows 特有形态
		"data/x:stream",    // NTFS 备用数据流
		"~short/data.json", // 8.3 短名
		// 反斜杠形态（archive 内可能出现）
		`Data\x.json`,
		`BACKEND\DATA\x.json`,
		// 带 ./ 前缀
		"./data/x.json",
		"./DATA/x.json",
	}
	for _, rel := range mustGuard {
		if !isGuardedPath(rel) {
			t.Errorf("isGuardedPath(%q) = false，应当受保护（Windows 大小写/形态绕过）", rel)
		}
	}

	// 正常源码路径必须放行——否则更新会被静默吞掉
	mustAllow := []string{
		"backend/internal/service/update_swap.go",
		"frontend/app/admin/files/page.tsx", // files/ 只做顶层匹配
		"frontend/app/admin/uploads/page.tsx",
		"README.md",
		"a/data-readme.md", // data-readme 不是 data 目录
		"myuploads/x.txt",  // uploads 只做顶层匹配
		"backend/cmd/server/main.go",
	}
	for _, rel := range mustAllow {
		if isGuardedPath(rel) {
			t.Errorf("isGuardedPath(%q) = true，误伤了正常源码路径", rel)
		}
	}
}

// TestSkipSourceDirCase 遍历跳过同样需要大小写不敏感。
func TestSkipSourceDirCase(t *testing.T) {
	for _, name := range []string{"data", "Data", "DATA", ".git", ".GIT", "node_modules", "NODE_MODULES", "uploads", "UPLOADS"} {
		if !skipSourceDir(name) {
			t.Errorf("skipSourceDir(%q) = false，应当跳过", name)
		}
	}
	for _, name := range []string{"internal", "Frontend", "database.go"} {
		if skipSourceDir(name) {
			t.Errorf("skipSourceDir(%q) = true，不应当跳过", name)
		}
	}
}

// TestSafeSourceRelativeRejectsGuardVariant 回滚清单的第二道校验也必须挡住
// 大小写变体（它与 isGuardedPath 是双保险）。
func TestSafeSourceRelativeRejectsGuardVariant(t *testing.T) {
	for _, rel := range []string{"Data/x.json", ".ENV", `BACKEND\DATA\x`, "../etc/passwd", "/abs/path"} {
		clean, ok := safeSourceRelative(rel)
		if ok && !isGuardedPath(clean) {
			t.Errorf("safeSourceRelative(%q) 通过了守卫：%q", rel, clean)
		}
		if !ok && !strings.HasPrefix(rel, "/") && !strings.HasPrefix(rel, "../") {
			t.Errorf("safeSourceRelative(%q) 被拒，但原因应来自 isGuardedPath 而非路径形态", rel)
		}
	}
}
