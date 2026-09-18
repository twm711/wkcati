package auth

import "time"

func timeNowNano() int64 { return time.Now().UnixNano() }

// HasRoleP 包级角色判断（供未持有 Service 的模块使用）
func HasRoleP(u *User, roles ...string) bool {
	for _, have := range u.Roles {
		for _, want := range roles {
			if have == want {
				return true
			}
		}
	}
	return false
}
