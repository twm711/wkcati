// Package rinfo 统一响应封装（与《ITACATI_NK3C_API接口规格》ResultInfo 逐字一致）
package rinfo

import "github.com/gin-gonic/gin"

// 错误码表（规格定义）
const (
	CodeOK          = "0"
	CodeParam       = "4001" // 参数错误
	CodeUnauth      = "4010" // 未登录/会话失效
	CodeState       = "4031" // 状态冲突（非法跳转）
	CodePermission  = "4032" // 权限不足
	CodeNotFound    = "4041" // 资源不存在
	CodeConflict    = "4091" // 业务冲突
	CodeDuplicate   = "4092" // 重复
	CodeInternal    = "5000" // 内部错误
)

type ResultInfo struct {
	Success bool        `json:"success"`
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func OK(data interface{}, message string) ResultInfo {
	return ResultInfo{Success: true, Code: CodeOK, Message: message, Data: data}
}
func Fail(code, message string) ResultInfo {
	return ResultInfo{Success: false, Code: code, Message: message, Data: nil}
}

func GinOK(c *gin.Context, data interface{}, message string) {
	c.JSON(200, OK(data, message))
}
func GinFail(c *gin.Context, code, message string) {
	c.JSON(200, Fail(code, message))
}
