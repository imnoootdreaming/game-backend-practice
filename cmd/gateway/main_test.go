package main

import (
	"net/http"
	"net/http/httptest" // 测试专用：在内存里模拟 HTTP 请求和响应，不用真开端口
	"testing"
)

// TestHealthzOK 验证：GET /healthz 返回 200、JSON 头、{"status":"ok"} 响应体
func TestHealthzOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder() // 记录处理器写出的状态码、响应头和响应体

	newHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := w.Body.String(); got != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", got, `{"status":"ok"}`)
	}
}

// TestHealthzRejectsPost 验证：POST /healthz 被路由层拒绝（405），呼应路由写法 "GET /healthz"
func TestHealthzRejectsPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	w := httptest.NewRecorder()

	newHandler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", w.Code)
	}
}
