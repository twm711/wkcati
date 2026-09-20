package app

import "nk3c/internal/media"

// WireCTI connects the API control plane to the running SIP/media domain.
func (a *App) WireCTI(c media.CallController) {
	a.ctiController = c
	if a.monitor != nil {
		a.monitor.WireCTI(c)
	}
}
