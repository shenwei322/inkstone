package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// changelogResult 是一次「提交差异」查询的结果。
//
// behind 语义：>=0 表示确定的落后提交数；-1 表示无法确定（日志可能有缺页）。
type changelogResult struct {
	commits []CommitBrief
	behind  int
	note    string
}

// changelog 取 from → to 之间的提交列表（新 → 旧）。
//
// 取数策略（按可靠性降级）：
//  1. 提交对比接口（GitHub compare / 自建 UPDATE_COMPARE_API）：一次拿到完整列表；
//  2. 提交列表接口分页：从最新往回翻，直到翻到本机版本那一页；
//  3. 全都失败：给出空列表与说明，绝不假装「已是最新」。
//
// from 为空（本机没有版本记录）时退化为「最近 N 条提交」。
func (s *UpdateService) changelog(ctx context.Context, from, to string) ([]CommitBrief, int, string) {
	res, err := s.queryChangelog(ctx, from, to)
	if err != nil {
		return nil, -1, "拉取提交列表失败：" + err.Error()
	}
	return res.commits, res.behind, res.note
}

// queryChangelog 是 changelog 的取数实现，返回结构化结果便于复用与测试。
func (s *UpdateService) queryChangelog(ctx context.Context, from, to string) (changelogResult, error) {
	// 1) 对比接口（一次拿全）
	if url := s.compareURL(from, to); url != "" {
		raw, _, err := s.client.getJSON(ctx, url)
		if err == nil {
			if res, ok := parseCompareResponse(s, raw, from, to); ok {
				return res, nil
			}
		}
	}
	// 2) 提交列表分页
	return s.changelogByPaging(ctx, from, to)
}

// compareURL 组装对比接口地址；无 from（没有基准版本）或仓库地址无法解析时返回空串。
func (s *UpdateService) compareURL(from, to string) string {
	if from == "" {
		return ""
	}
	if s.cfg.UpdateCompareAPI != "" {
		return s.expandAPI(s.cfg.UpdateCompareAPI, to)
	}
	owner, name := splitRepo(s.cfg.UpdateRepoURL)
	if owner == "" || name == "" {
		return ""
	}
	tmpl := s.cfg.UpdateGitHubAPI + "/repos/{owner}/{name}/compare/{from}...{to}"
	repl := strings.NewReplacer(
		"{from}", escapeURLPath(from),
		"{to}", escapeURLPath(to),
		"{owner}", owner,
		"{name}", name,
		"{commit}", escapeURLPath(to),
		"{short}", ShortCommit(to),
		"{repo}", s.cfg.UpdateRepoURL,
		"{branch}", escapeQueryValue(s.cfg.UpdateBranch),
	)
	return repl.Replace(tmpl)
}

// parseCompareResponse 解析 GitHub compare 形状与自建极简形状。
func parseCompareResponse(s *UpdateService, raw []byte, from, to string) (changelogResult, bool) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		// 也可能直接就是提交数组
		if commits, ok := parseCommitsResponse(raw); ok {
			return finishChangelog(s, commits, from, to), true
		}
		return changelogResult{}, false
	}

	var commits []remoteCommit
	if inner, ok := obj["commits"]; ok {
		if encoded, err := json.Marshal(inner); err == nil {
			commits, _ = parseCommitsResponse(encoded)
		}
	}
	if commits == nil {
		// 只给出计数、不含明细的实现也算解析成功（前端会提示日志不可用）
		if _, hasFrom := obj["from"]; !hasFrom {
			if _, hasTo := obj["to"]; !hasTo {
				if _, hasTotal := obj["total_commits"]; !hasTotal {
					return changelogResult{}, false
				}
			}
		}
	}

	res := finishChangelog(s, commits, from, to)
	if total := jsonInt(obj, "total_commits", "ahead_by", "count"); total > 0 {
		res.behind = total
		if len(res.commits) < total {
			res.note = joinNotes(res.note, fmt.Sprintf("上游共 %d 个新提交，此处展示最新 %d 个。", total, len(res.commits)))
		}
	}
	if files := jsonInt(obj, "files", "changed_files"); files > 0 {
		res.note = joinNotes(res.note, fmt.Sprintf("涉及 %d 个文件变更。", files))
	}
	return res, true
}

// changelogByPaging 通过提交列表接口分页回溯，直到遇到本机版本。
func (s *UpdateService) changelogByPaging(ctx context.Context, from, to string) (changelogResult, error) {
	const (
		perPage  = 100
		maxPages = 15
	)

	base := s.commitsEndpoint()
	if base == "" {
		return changelogResult{}, fmt.Errorf("仓库地址 %q 无法解析出 owner/name", s.cfg.UpdateRepoURL)
	}
	// 展开除分页外的占位符，分页参数由本函数接管。
	//
	// 分支名必须在 expandAPI **之前**转义：expandAPI 内部会用未转义的分支名
	// 消费 {branch}，原先放在它之后的 ReplaceAll 是无效的（占位符已不存在），
	// 分支名含 #、&、空格时会原样进 URL 并截断查询串。
	base = strings.ReplaceAll(base, "{branch}", escapeQueryValue(s.cfg.UpdateBranch))
	base = s.expandAPI(base, to)
	base = strings.ReplaceAll(base, "{limit}", strconv.Itoa(perPage))

	var collected []remoteCommit
	for page := 1; page <= maxPages; page++ {
		url := base
		if strings.Contains(url, "{page}") {
			url = strings.ReplaceAll(url, "{page}", strconv.Itoa(page))
		} else {
			sep := "?"
			if strings.Contains(url, "?") {
				sep = "&"
			}
			url = fmt.Sprintf("%s%sper_page=%d&page=%d", url, sep, perPage, page)
		}

		raw, _, err := s.client.getJSON(ctx, url)
		if err != nil {
			if len(collected) > 0 {
				// 已有数据先用着，缺页只降低精度
				return changelogResult{
					commits: s.briefsFrom(collected),
					behind:  -1,
					note:    fmt.Sprintf("上游分页请求在第 %d 页失败，更新日志可能不完整。", page),
				}, nil
			}
			return changelogResult{}, err
		}
		commits, ok := parseCommitsResponse(raw)
		if !ok || len(commits) == 0 {
			break
		}

		// 命中本机版本：该页里它之前（更新）的提交 + 前面所有页，就是完整差异
		if from != "" {
			if idx := indexOfCommit(commits, from); idx >= 0 {
				collected = append(collected, commits[:idx]...)
				return changelogResult{commits: s.briefsFrom(collected), behind: len(s.briefsFrom(collected))}, nil
			}
		}
		collected = append(collected, commits...)

		if len(commits) < perPage {
			// 已到仓库起点仍未找到基准版本
			return changelogResult{
				commits: s.briefsFrom(collected),
				behind:  -1,
				note:    "已回溯到仓库最早的提交，仍未找到本机版本，更新日志按最近提交展示。",
			}, nil
		}
	}

	if len(collected) == 0 {
		return changelogResult{}, fmt.Errorf("上游没有返回任何提交")
	}
	return changelogResult{
		commits: s.briefsFrom(collected),
		behind:  -1,
		note: fmt.Sprintf("在新提交中未找到本机版本 %s，更新日志仅展示最近 %d 条。",
			ShortCommit(from), len(collected)),
	}, nil
}

// commitsEndpoint 返回提交列表接口模板。
func (s *UpdateService) commitsEndpoint() string {
	if s.cfg.UpdateCommitsAPI != "" {
		return s.cfg.UpdateCommitsAPI
	}
	owner, name := splitRepo(s.cfg.UpdateRepoURL)
	if owner == "" || name == "" {
		return ""
	}
	return fmt.Sprintf("%s/repos/%s/%s/commits?sha={branch}&per_page={limit}",
		s.cfg.UpdateGitHubAPI, owner, name)
}

// finishChangelog 把 remoteCommit 列表规范成展示列表并推断落后数量。
func finishChangelog(s *UpdateService, commits []remoteCommit, from, to string) changelogResult {
	if from == "" {
		return changelogResult{commits: s.briefsFrom(commits), behind: -1}
	}
	if idx := indexOfCommit(commits, from); idx >= 0 {
		// 列表是「from 之后的新提交」，落后数量即其长度
		return changelogResult{commits: s.briefsFrom(commits[:idx]), behind: len(s.briefsFrom(commits[:idx]))}
	}
	// 列表里没有本机版本：可能已是最新，也可能历史被改写
	onlyNew := make([]remoteCommit, 0, len(commits))
	for _, c := range commits {
		if sameCommit(c.Hash, to) || len(onlyNew) > 0 {
			onlyNew = append(onlyNew, c)
		}
	}
	if len(onlyNew) == 0 {
		onlyNew = commits
	}
	return changelogResult{
		commits: s.briefsFrom(onlyNew),
		behind:  -1,
		note:    "提交列表中未出现本机版本，可能上游历史被改写；更新日志仅供参考。",
	}
}

// briefsFrom 把内部提交结构转成展示结构（去重 + 补齐短哈希）。
// decorate 会纠正上游返回的仓库链接（历史仓库名）。
func (s *UpdateService) briefsFrom(commits []remoteCommit) []CommitBrief {
	out := make([]CommitBrief, 0, len(commits))
	seen := make(map[string]bool, len(commits))
	for _, c := range commits {
		if c.Hash == "" || seen[c.Hash] {
			continue
		}
		seen[c.Hash] = true
		author := strings.TrimSpace(c.Author)
		if author == "" {
			// 部分镜像不返回作者信息，前端原样展示这个占位
			author = "上游提交"
		}
		decorated := s.decorate(c)
		out = append(out, CommitBrief{
			Hash:    decorated.Hash,
			Short:   ShortCommit(decorated.Hash),
			Message: firstLine(decorated.Message),
			Author:  author,
			Date:    decorated.Date,
			URL:     decorated.URL,
		})
	}
	return out
}

// indexOfCommit 在新 → 旧列表中定位某个提交，返回其下标（-1 = 未找到）。
func indexOfCommit(commits []remoteCommit, target string) int {
	if target == "" {
		return -1
	}
	for i, c := range commits {
		if sameCommit(c.Hash, target) {
			return i
		}
	}
	return -1
}

// changelogSource 返回更新日志的来源标识，便于排查「为什么日志不对」。
func (s *UpdateService) changelogSource() string {
	if s.cfg.UpdateCompareAPI != "" {
		return "compare-api"
	}
	if s.cfg.UpdateCommitsAPI != "" {
		return "commits-api"
	}
	return "github-api"
}

// jsonInt 从 JSON 对象里读一个整数字段，兼容 float64 / json.Number / 字符串。
func jsonInt(obj map[string]any, keys ...string) int {
	for _, key := range keys {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case float64:
			return int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				return int(n)
			}
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
	}
	return 0
}

// joinNotes 拼接两条说明，空串不产生多余分隔。
func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}
