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
	CallID        int64  `json:"callId"`
	Action        string `json:"action" binding:"required"`
	SupervisorURI string `json:"supervisorUri"`
	UserID        int64  `json:"userId"`
	Text          string `json:"text"`
}

// Control executes real media controls only; unsupported actions never return fake success.
func (s *Service) Control(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	var req controlReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Action == "" {
		rinfo.GinFail(c, rinfo.CodeParam, "callId/action 参数错误")
		return
	}
	if req.Action == "FORCE_BUSY" || req.Action == "FORCE_READY" {
		if req.UserID <= 0 {
			rinfo.GinFail(c, rinfo.CodeParam, "FORCE_BUSY/FORCE_READY 需要 userId")
			return
		}
		state := "BUSY"
		if req.Action == "FORCE_READY" {
			state = "READY"
		}
		if err := s.setAgentState(req.UserID, state, "supervisor:"+u.AgentNo); err != nil {
			rinfo.GinFail(c, rinfo.CodeState, err.Error())
			return
		}
		if s.qcHub != nil {
			s.qcHub.Publish(req.Action, gin.H{"userId": req.UserID, "byUserId": u.ID})
		}
		rinfo.GinOK(c, gin.H{"userId": req.UserID, "state": state}, "坐席状态已强制更新")
		return
	}
	if req.Action == "MESSAGE" {
		if req.UserID <= 0 || req.Text == "" || len(req.Text) > 1000 {
			rinfo.GinFail(c, rinfo.CodeParam, "MESSAGE 需要 userId 和 1-1000 字符 text")
			return
		}
		if s.msgHub == nil {
			rinfo.GinFail(c, rinfo.CodeState, "坐席消息通道未启动")
			return
		}
		s.msgHub.Publish(req.UserID, req.Text, u.ID)
		if s.qcHub != nil {
			s.qcHub.Publish("MESSAGE", gin.H{"userId": req.UserID, "byUserId": u.ID})
		}
		rinfo.GinOK(c, gin.H{"userId": req.UserID}, "消息已发送")
		return
	}
	if req.Action == "LISTEN" || req.Action == "BARGE" {
		if req.CallID <= 0 {
			rinfo.GinFail(c, rinfo.CodeParam, "LISTEN/BARGE 需要 callId")
			return
		}
		host, portText, err := net.SplitHostPort(req.SupervisorURI)
		if err != nil || host == "" {
			rinfo.GinFail(c, rinfo.CodeParam, "LISTEN/BARGE 需要 supervisorUri=host:port")
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
		listenOnly := req.Action == "LISTEN"
		if err := s.cti.SetSupervisorMode(req.CallID, listenOnly); err != nil {
			if err := s.cti.AddSupervisor(context.Background(), req.CallID, host, port, listenOnly); err != nil {
				rinfo.GinFail(c, rinfo.CodeState, "加入督导腿失败："+err.Error())
				return
			}
		}
		if s.qcHub != nil {
			s.qcHub.Publish(req.Action, gin.H{"callId": req.CallID, "supervisorUri": req.SupervisorURI, "byUserId": u.ID})
		}
		msg := "督导已加入三方通话"
		if listenOnly {
			msg = "督导已进入监听"
		}
		rinfo.GinOK(c, gin.H{"callId": req.CallID, "action": req.Action}, msg)
		return
	}
	if req.CallID <= 0 {
		rinfo.GinFail(c, rinfo.CodeParam, "HANGUP 需要 callId")
		return
	}
	if req.Action != "HANGUP" {
		rinfo.GinFail(c, rinfo.CodeState, "当前仅支持 HANGUP/LISTEN/BARGE；MESSAGE/强制状态切换待接入媒体控制")
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
