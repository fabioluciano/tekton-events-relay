package httpx

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

const maxBodyLogSize = 4 * 1024 // 4KB

var sensitiveHeaders = map[string]bool{
	"authorization": true,
	"x-api-key":     true,
	"private-token": true,
	"x-auth-token":  true,
	"cookie":        true,
	"set-cookie":    true,
}

func redactHeaders(h http.Header) http.Header {
	out := h.Clone()
	for k := range out {
		if sensitiveHeaders[strings.ToLower(k)] {
			out[k] = []string{"[REDACTED]"}
		}
	}
	return out
}

// debugTransport wraps http.RoundTripper with debug logging.
type debugTransport struct {
	base   http.RoundTripper
	logger *zap.Logger
	name   string
}

// RoundTrip implements http.RoundTripper with before/after logging.
func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Read full body, restore full body, truncate only for log.
	var reqLogBody []byte
	if req.Body != nil {
		fullBody, readErr := io.ReadAll(req.Body)
		if readErr != nil {
			d.logger.Warn("failed to read request body for debug logging", zap.Error(readErr))
		}
		req.Body = io.NopCloser(bytes.NewReader(fullBody))
		if len(fullBody) > maxBodyLogSize {
			reqLogBody = fullBody[:maxBodyLogSize]
		} else {
			reqLogBody = fullBody
		}
	}

	d.logger.Debug("http request",
		zap.String("provider", d.name),
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Any("headers", redactHeaders(req.Header)),
		zap.ByteString("body", reqLogBody))

	resp, err := d.base.RoundTrip(req)
	latency := time.Since(start)

	if err != nil {
		d.logger.Debug("http request failed",
			zap.String("method", req.Method),
			zap.String("url", req.URL.String()),
			zap.Duration("latency", latency),
			zap.Error(err))
		return nil, err
	}

	// Read full response body, restore full body, truncate only for log.
	fullRespBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		d.logger.Warn("failed to read response body for debug logging", zap.Error(readErr))
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(fullRespBody))

	var respLogBody []byte
	if len(fullRespBody) > maxBodyLogSize {
		respLogBody = fullRespBody[:maxBodyLogSize]
	} else {
		respLogBody = fullRespBody
	}

	d.logger.Debug("http response",
		zap.String("provider", d.name),
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Int("status", resp.StatusCode),
		zap.Duration("latency", latency),
		zap.Any("headers", redactHeaders(resp.Header)),
		zap.ByteString("body", respLogBody))

	return resp, nil
}
