//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// rebuildScriptName 本机重建脚本名（对应 deploy/scripts/rebuild.ps1）。
func rebuildScriptName() string {
	return "rebuild.ps1"
}

// startDetachedRebuild 以隐藏窗口启动重建脚本（Windows 开发/内网部署场景）。
//
// DETACHED_PROCESS + CREATE_NEW_PROCESS_GROUP：脚本不随后端进程退出而被回收，
// 它会等后端退出（Windows 上二进制被占用时无法覆盖）后重新编译并拉起 server.exe。
func startDetachedRebuild(script, repoRoot, target, updateDir string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
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
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动重建脚本失败：%w", err)
	}
	return nil
}
