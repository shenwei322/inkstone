//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// rebuildScriptName 本机重建脚本名（对应 deploy/scripts/rebuild.sh）。
func rebuildScriptName() string {
	return "rebuild.sh"
}

// startDetachedRebuild 用独立会话启动重建脚本：脚本自己会等本进程退出
// （避免覆盖正在运行的二进制），再构建并拉起服务。
func startDetachedRebuild(script, repoRoot, target, updateDir string) error {
	cmd := exec.Command("sh", script)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"INKSTONE_TARGET="+target,
		"INKSTONE_REPO_ROOT="+repoRoot,
		"INKSTONE_UPDATE_DIR="+updateDir,
		"INKSTONE_SELF="+executablePath(),
		"INKSTONE_PID="+itoa(os.Getpid()),
		"INKSTONE_SERVICE="+os.Getenv("INKSTONE_SERVICE"),
		"INKSTONE_PORT="+os.Getenv("PORT"),
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Setsid：脱离当前进程会话，父进程退出后脚本继续跑
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动重建脚本失败：%w", err)
	}
	// 不 Wait：脚本要在本进程退出后才继续，这里必须放手
	return nil
}
