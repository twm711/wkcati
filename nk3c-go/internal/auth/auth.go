// Package auth 登录/会话/RBAC（权限码沿用 S7：domainAdmin/orgAdmin/groupAdmin/phoneAdmin）
package auth

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

type Service struct {
	db       *store.DB
	mu       sync.RWMutex
	sessions map[string]int64 // token -> userID（生产可插 Redis）
}

func New(db *store.DB) *Service { return &Service{db: db, sessions: map[string]int64{}} }

type loginReq struct {
	LoginName string `json:"loginName"`
	Password  string `json:"password"`
}

func (s *Service) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "参数错误")
		return
	}
	var u struct {
		ID       int64
		UserName string
		AgentNo  *string
		Roles    string
	}
	err := s.db.QueryRow(`SELECT id,user_name,agent_no,roles FROM sys_user
		WHERE login_name=? AND password=? AND status=1`, req.LoginName, req.Password).
		Scan(&u.ID, &u.UserName, &u.AgentNo, &u.Roles)
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "登录名或密码错误")
		return
	}
	token := randToken()
	s.mu.Lock()
	s.sessions[token] = u.ID
	s.mu.Unlock()
	agentNo := ""
	if u.AgentNo != nil {
		agentNo = *u.AgentNo
	}
	rinfo.GinOK(c, gin.H{
		"sessionId": token, "userId": u.ID, "userName": u.UserName,
		"agentNo": agentNo, "roles": strings.Split(u.Roles, ","),
	}, "登录成功")
}

func (s *Service) Logout(c *gin.Context) {
	tok := bearer(c)
	s.mu.Lock()
	delete(s.sessions, tok)
	s.mu.Unlock()
	rinfo.GinOK(c, true, "已签出")
}

// User 当前会话用户信息（业务层取用）
type User struct {
	ID      int64
	Name    string
	AgentNo string
	Roles   []string
}

func bearer(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	return strings.TrimPrefix(h, "Bearer ")
}

func (s *Service) user(tok string) *User {
	s.mu.RLock()
	uid, ok := s.sessions[tok]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	var u User
	var roles string
	var agentNo *string
	if err := s.db.QueryRow(`SELECT id,user_name,agent_no,roles FROM sys_user WHERE id=? AND status=1`, uid).
		Scan(&u.ID, &u.Name, &agentNo, &roles); err != nil {
		return nil
	}
	if agentNo != nil {
		u.AgentNo = *agentNo
	}
	u.Roles = strings.Split(roles, ",")
	return &u
}

// LogoutAll 强签：注销某用户全部会话，返回吊销数（督导强签坐席用）
func (s *Service) LogoutAll(userID int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for tok, uid := range s.sessions {
		if uid == userID {
			delete(s.sessions, tok)
			n++
		}
	}
	return n
}

func (s *Service) HasRole(u *User, roles ...string) bool {
	for _, have := range u.Roles {
		for _, want := range roles {
			if have == want {
				return true
			}
		}
	}
	return false
}

// AuthQuery 双通道鉴权：?token= 优先（WebSocket/window.open 无法带头），回退 Authorization 头
func (s *Service) AuthQuery() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := s.user(c.Query("token"))
		if u == nil {
			u = s.user(bearer(c))
		}
		if u == nil {
			c.AbortWithStatus(401)
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

// RequireAuth 鉴权中间件：user 注入 context
func (s *Service) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := s.user(bearer(c))
		if u == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, rinfo.Fail(rinfo.CodeUnauth, "未登录或会话失效"))
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

// RequireRoles 角色中间件（groupAdmin 及以上 = 督导权限）
func (s *Service) RequireRoles(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := c.MustGet("user").(*User)
		if !s.HasRole(u, roles...) {
			rinfo.GinFail(c, rinfo.CodePermission, "需要权限："+strings.Join(roles, "/"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func From(c *gin.Context) *User { return c.MustGet("user").(*User) }

func randToken() string {
	const hex = "0123456789abcdef"
	b := make([]byte, 32)
	now := timeNowNano()
	for i := range b {
		now = now*6364136223846793005 + 1442695040888963407
		b[i] = hex[(now>>33)&0xF]
	}
	return string(b)
}

// UserOf 供非中间件场景取会话用户
func (s *Service) UserOf(token string) *User { return s.user(strings.TrimPrefix(token, "Bearer ")) }

// InvalidateAll 一键重置后失效全部会话
func (s *Service) InvalidateAll() {
	s.mu.Lock()
	s.sessions = map[string]int64{}
	s.mu.Unlock()
}
