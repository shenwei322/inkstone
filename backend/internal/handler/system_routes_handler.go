package handler

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
)

// RouteInfo 是路由清单里的单条。
type RouteInfo struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler"`
}

// ListRoutes 返回当前 engine 的全部已注册路由。
//
// **为什么不手写 OpenAPI 注释**：手写的问题是改代码忘改注释，文档慢慢失真，
// 而失真的接口文档比没有文档更糟——调用方照着它实现，出错那天无从排查。
// 从 gin 路由树（Engine.Routes()）实时导出则不可能脱节：
// 注册即出现，删除即消失。
//
// 排序后再返回：gin 按注册顺序给出，改一次注册顺序清单就变。
// 按「路径 + 方法」排，输出稳定，便于 diff 两个版本的接口差异。
func ListRoutes(engine *gin.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		infos := make([]RouteInfo, 0, len(engine.Routes()))
		for _, r := range engine.Routes() {
			infos = append(infos, RouteInfo{
				Method:  r.Method,
				Path:    r.Path,
				Handler: r.Handler,
			})
		}
		sort.Slice(infos, func(i, j int) bool {
			if infos[i].Path != infos[j].Path {
				return infos[i].Path < infos[j].Path
			}
			return infos[i].Method < infos[j].Method
		})
		c.JSON(http.StatusOK, gin.H{"routes": infos, "total": len(infos)})
	}
}
