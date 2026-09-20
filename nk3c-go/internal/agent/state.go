package agent

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/internal/store"
	"nk3c/pkg/rinfo"
)

var allowedStates = map[string]bool{"READY": true, "BUSY": true, "PAUSE": true}

type stateReq struct {
	State  string `json:"state" binding:"required"`
	Reason string `json:"reason"`
}

// State updates the authenticated agent's availability state.
func (s *Service) State(c *gin.Context) {
	u := auth.From(c)
	var req stateReq
	if err := c.ShouldBindJSON(&req); err != nil || !allowedStates[req.State] {
		rinfo.GinFail(c, rinfo.CodeParam, "state 须为 READY/BUSY/PAUSE")
		return
	}
	var err error
	if s.db.Driver == "mysql" {
		_, err = s.db.Exec(`INSERT INTO cti_agent_state(user_id,state,reason,updated_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE state=VALUES(state),reason=VALUES(reason),updated_at=VALUES(updated_at)`, u.ID, req.State, req.Reason, store.NowISO())
	} else {
		_, err = s.db.Exec(`INSERT INTO cti_agent_state(user_id,state,reason,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET state=excluded.state,reason=excluded.reason,updated_at=excluded.updated_at`, u.ID, req.State, req.Reason, store.NowISO())
	}
	if err != nil {
		rinfo.GinFail(c, rinfo.CodeInternal, err.Error())
		return
	}
	rinfo.GinOK(c, gin.H{"userId": u.ID, "state": req.State, "reason": req.Reason}, "坐席状态已更新")
}

func SetState(db *store.DB, userID int64, state, reason string) error {
	if !allowedStates[state] {
		return fmt.Errorf("invalid agent state")
	}
	if db.Driver == "mysql" {
		_, err := db.Exec(`INSERT INTO cti_agent_state(user_id,state,reason,updated_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE state=VALUES(state),reason=VALUES(reason),updated_at=VALUES(updated_at)`, userID, state, reason, store.NowISO())
		return err
	}
	_, err := db.Exec(`INSERT INTO cti_agent_state(user_id,state,reason,updated_at) VALUES(?,?,?,?) ON CONFLICT(user_id) DO UPDATE SET state=excluded.state,reason=excluded.reason,updated_at=excluded.updated_at`, userID, state, reason, store.NowISO())
	return err
}
