package server

import (
	"errors"
	"net/http"

	updateapp "xianyu-go/internal/application/systemupdate"
)

// systemUpdateStatus 返回当前版本、最新正式版和宿主机更新能力状态。
func (server *Server) systemUpdateStatus(writer http.ResponseWriter, request *http.Request) {
	// snapshot、statusErr 分别是只读更新状态及其异常错误。
	snapshot, statusErr := server.systemUpdateApplication().Status(request.Context())
	if statusErr != nil {
		writeErrRequest(writer, request, http.StatusServiceUnavailable, "读取更新状态失败")
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}

// systemUpdateCheck 主动检查最新正式版，不触发容器或数据库修改。
func (server *Server) systemUpdateCheck(writer http.ResponseWriter, request *http.Request) {
	// snapshot、checkErr 分别是最新版本检查结果及执行器错误。
	snapshot, checkErr := server.systemUpdateApplication().Check(request.Context())
	if checkErr != nil {
		writeErrRequest(writer, request, http.StatusServiceUnavailable, "检查更新失败")
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}

// systemUpdateApply 请求受限宿主机执行器更新到其重新验证的最新正式版。
func (server *Server) systemUpdateApply(writer http.ResponseWriter, request *http.Request) {
	// snapshot、applyErr 分别是任务受理状态及执行器拒绝或不可用错误。
	snapshot, applyErr := server.systemUpdateApplication().ApplyLatestStable(request.Context())
	if applyErr != nil {
		switch {
		case errors.Is(applyErr, updateapp.ErrUpdaterUnavailable):
			writeErrRequest(writer, request, http.StatusServiceUnavailable, "宿主机更新组件不可用")
			return
		case errors.Is(applyErr, updateapp.ErrUpdateBlocked):
			writeErrRequest(writer, request, http.StatusConflict, "当前部署不允许自动更新")
			return
		default:
			writeErrRequest(writer, request, http.StatusServiceUnavailable, "宿主机更新组件暂时无法执行更新")
			return
		}
	}
	writeJSON(writer, http.StatusAccepted, snapshot)
}
