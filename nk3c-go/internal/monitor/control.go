package monitor

import (
	"context"
	"net"
	"strconv"

	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/pkg/rinfo"
)

type controlReq struct {
	CallID        int64  `json:"callId" binding:"required"`
	Action        string `json:"action" binding:"required"`
	SupervisorURI string `json:"supervisorUri"`
}

// Control 当前实际执行 HANGUP；其余 CTI 动作明确返回未实现，避免把“记录事件”冒充成媒体控制。
func (s *Service) Control(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	var req controlReq
	if err := c.ShouldBindJSON(&req); err != nil || req.CallID <= 0 {
		rinfo.GinFail(c, rinfo.CodeParam, "callId/action 参数错误")
		return
	}
	if req.Action == "BARGE" {
		host, portText, err := net.SplitHostPort(req.SupervisorURI)
		if err != nil || host == "" {
			rinfo.GinFail(c, rinfo.CodeParam, "BARGE 需要 supervisorUri=host:port")
			return
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port <= 0 {
			rinfo.GinFail(c, rinfo.CodeParam, "supervisorUri 端口非法")
			return
		}
		if s.cti == nil {
			rinfo.GinFail(c, rinfo.CodeState, "话务域控制器未启动")
			return
		}
		if err := s.cti.AddSupervisor(context.Background(), req.CallID, host, port); err != nil {
			rinfo.GinFail(c, rinfo.CodeState, "加入督导腿失败："+err.Error())
			return
		}
		if s.qcHub != nil {
			s.qcHub.Publish("BARGE", gin.H{"callId": req.CallID, "supervisorUri": req.SupervisorURI, "byUserId": u.ID})
		}
		rinfo.GinOK(c, gin.H{"callId": req.CallID, "action": req.Action}, "督导已加入三方通话")
		return
	}
	if req.Action != "HANGUP" {
		rinfo.GinFail(c, rinfo.CodeState, "当前仅支持 HANGUP/BARGE；LISTEN/MESSAGE/强制状态切换待接入媒体控制")
		return
	}
	if s.cti == nil {
		rinfo.GinFail(c, rinfo.CodeState, "话务域控制器未启动")
		return
	}
	if err := s.cti.Hangup(req.CallID); err != nil {
		rinfo.GinFail(c, rinfo.CodeNotFound, "活动通话不存在："+strconv.FormatInt(req.CallID, 10))
		return
	}
	if s.qcHub != nil {
		s.qcHub.Publish("HANGUP", gin.H{"callId": req.CallID, "byUserId": u.ID, "byAgentNo": u.AgentNo})
	}
	rinfo.GinOK(c, gin.H{"callId": req.CallID, "action": req.Action}, "已发送强制挂断")
}
