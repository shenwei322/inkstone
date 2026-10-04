package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// rebuildDocker 在本机 docker compose 部署下重建并重启（需要容器内可访问 docker）。
//
// 大多数容器部署并没有 docker socket，走不到这里——那种情况请把
// UPDATE_WAITING_AGENT 保持为 true（默认），由宿主代理完成重建。
func (s *UpdateService) rebuildDocker(target string) error {
	composeFile := ""
	for _, name := range []string{"docker-compose.prod.yml", "docker-compose.offline.yml", "docker-compose.yml"} {
		candidate := filepath.Join(s.cfg.UpdateSourceDir, name)
		if isFile(candidate) {
			composeFile = candidate
			break
		}
	}
	if composeFile == "" {
		return fmt.Errorf("未找到 docker compose 编排文件")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("本机没有 docker 命令，请改用宿主更新代理（UPDATE_WAITING_AGENT=true）")
	}

	base := []string{"compose", "-f", composeFile}
	envFile := filepath.Join(s.cfg.UpdateSourceDir, ".env")
	if isFile(envFile) {
		base = append(base, "--env-file", envFile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	build := append(append([]string{}, base...), "build")
	if out, err := runCommand(ctx, s.cfg.UpdateSourceDir, "docker", build...); err != nil {
		return fmt.Errorf("docker compose build 失败：%w：%s", err, tailLines(out, 12))
	}
	up := append(append([]string{}, base...), "up", "-d")
	if out, err := runCommand(ctx, s.cfg.UpdateSourceDir, "docker", up...); err != nil {
		return fmt.Errorf("docker compose up 失败：%w：%s", err, tailLines(out, 12))
	}

	// 记录已部署版本；容器重启后由新代码读到
	if err := WriteDeployedCommit(s.recorder().commitPath(), target); err != nil {
		return fmt.Errorf("写入部署记录失败：%w", err)
	}
	return nil
}

// rebuildInPlace 在本机（非容器）部署下重新编译并重启进程。
//
// 编译由平台脚本负责（scripts/rebuild.sh / rebuild.ps1），脚本在后台等待本进程退出后
// 再构建与启动，避免「自己把自己编译掉」。
func (s *UpdateService) rebuildInPlace(target string) error {
	script := filepath.Join(s.cfg.UpdateSourceDir, "deploy", "scripts", rebuildScriptName())
	if !isFile(script) {
		return fmt.Errorf("缺少本机重建脚本 %s（容器部署请启用宿主更新代理）", script)
	}
	if err := startDetachedRebuild(script, s.cfg.UpdateSourceDir, target, s.cfg.UpdateDir); err != nil {
		return err
	}
	return nil
}

// runCommand 执行命令并合并输出（失败时用于报错）。
func runCommand(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// tailLines 取输出末尾若干行，避免把整段构建日志塞进 API 错误信息。
func tailLines(text string, limit int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r")
	}
	return strings.Join(lines, " | ")
}

// executablePath 返回当前可执行文件路径（用于重建脚本重启同一个二进制）。
func executablePath() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	return path
}

// itoa 小工具：避免为一次整数转换引入 strconv 依赖分散在各文件。
func itoa(n int) string {
	return strconv.Itoa(n)
}
