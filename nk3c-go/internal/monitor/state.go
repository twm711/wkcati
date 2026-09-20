package monitor

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

func (s *Service) setAgentState(userID int64, state, reason string) error {
	if state != "READY" && state != "BUSY" && state != "PAUSE" {
		return fmt.Errorf("状态非法")
	}
	if s.db.Driver == "mysql" {
		_, err := s.db.Exec(`INSERT INTO cti_agent_state(user_id,state,reason,updated_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE state=VALUES(state),reason=VALUES(reason),updated_at=VALUES(updated_at)`, userID, state, reason, store.NowISO())
		return err
	}
	_, err := s.db.Exec(`INSERT INTO cti_agent_state(user_id,state,reason,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET state=excluded.state,reason=excluded.reason,updated_at=excluded.updated_at`, userID, state, reason, store.NowISO())
	return err
}

func (s *Service) ForceState(c *gin.Context) {
	var req struct {
		UserID int64  `json:"userId" binding:"required"`
		State  string `json:"state" binding:"required"`
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, "userId/state 参数错误")
		return
	}
	if err := s.setAgentState(req.UserID, req.State, req.Reason); err != nil {
		rinfo.GinFail(c, rinfo.CodeParam, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"userId": req.UserID, "state": req.State}, "坐席状态已强制更新")
}
