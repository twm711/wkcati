package app

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

// auditList 提供督导/管理员审计查询；默认只返回最近 100 条，不返回请求体。
func auditList(db *store.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := auth.From(c)
		if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
			rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
			return
		}
		limit := 100
		if raw := c.Query("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		q := `SELECT id,user_id,login_name,method,path,action,status_code,created_at FROM sys_op_log`
		args := []interface{}{}
		where := ""
		if method := c.Query("method"); method != "" {
			where = " WHERE method=?"
			args = append(args, method)
		}
		q += where + ` ORDER BY created_at DESC LIMIT ` + strconv.Itoa(limit)
		rows, err := db.Query(q, args...)
		if err != nil {
			rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
			return
		}
		defer rows.Close()
		out := []map[string]interface{}{}
		for rows.Next() {
			var id, login, method, path, action, created string
			var uid int64
			var status int
			if err := rows.Scan(&id, &uid, &login, &method, &path, &action, &status, &created); err != nil {
				continue
			}
			out = append(out, map[string]interface{}{"id": id, "userId": uid, "userName": login,
				"method": method, "path": path, "action": action, "statusCode": status, "createdAt": created})
		}
		rinfo.GinOK(c, gin.H{"total": len(out), "rows": out}, "ok")
	}
}
