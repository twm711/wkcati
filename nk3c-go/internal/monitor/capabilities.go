package monitor

import (
	"github.com/gin-gonic/gin"
	"nk3c/internal/auth"
	"nk3c/pkg/rinfo"
)

// Capabilities exposes the actually wired CTI controls so the UI never presents
// a button that only writes an event without changing media.
func (s *Service) Capabilities(c *gin.Context) {
	u := auth.From(c)
	if u == nil || !auth.HasRoleP(u, "groupAdmin", "orgAdmin", "domainAdmin") {
		rinfo.GinFail(c, rinfo.CodePermission, "需要督导及以上权限")
		return
	}
	rinfo.GinOK(c, gin.H{
		"HANGUP":      s.cti != nil,
		"LISTEN":      s.cti != nil,
		"BARGE":       s.cti != nil,
		"MESSAGE":     s.msgHub != nil,
		"FORCE_BUSY":  true,
		"FORCE_READY": true,
	}, "当前 CTI 控制能力")
}
