package server

import "net/http"

// Capture status for debug metadata without recording response bodies or headers.
type debugResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *debugResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *debugResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
func (w *debugResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
