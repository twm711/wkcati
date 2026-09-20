package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
)

// auditMiddleware 统一记录写操作及导出/录音等敏感访问。
// 请求体不落库，避免把客户电话、答案和备注复制到审计表。
func auditMiddleware(db *store.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		method := c.Request.Method
		sensitive := strings.HasPrefix(c.Request.URL.Path, "/api/export/") || strings.HasPrefix(c.Request.URL.Path, "/api/recording/")
		mutating := method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
		if !mutating && !sensitive {
			return
		}
		v, exists := c.Get("user")
		if !exists {
			return
		}
		u, ok := v.(*auth.User)
		if !ok || u == nil {
			return
		}
		id := fmt.Sprintf("%d", time.Now().UnixNano())
		created := time.Now().UTC().Format(time.RFC3339)
		if db.Driver == "mysql" {
			created = time.Now().UTC().Format("2006-01-02 15:04:05")
		}
		// 审计失败不能覆盖原业务响应；失败由数据库/运行日志另行暴露。
		_, _ = db.Exec(`INSERT INTO sys_op_log(id,user_id,login_name,method,path,action,status_code,created_at)
			VALUES(?,?,?,?,?,?,?,?)`, id, u.ID, u.Name, method, c.Request.URL.Path, method+" "+c.Request.URL.Path, c.Writer.Status(), created)
	}
}
