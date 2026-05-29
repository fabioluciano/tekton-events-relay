package httpx

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

const maxBodyLogSize = 4 * 1024 // 4KB

// debugTransport wraps http.RoundTripper with debug logging.
type debugTransport struct {
	base   http.RoundTripper
	logger *zap.Logger
	name   string
}

// RoundTrip implements http.RoundTripper with before/after logging.
func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Log request
	reqBody := readBodyTruncated(req.Body, maxBodyLogSize)
	if req.Body != nil {
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	d.logger.Debug("http request",
		zap.String("provider", d.name),
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Any("headers", req.Header),
		zap.ByteString("body", reqBody))

	// Execute request
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

	// Log response
	respBody := readBodyTruncated(resp.Body, maxBodyLogSize)
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	d.logger.Debug("http response",
		zap.String("provider", d.name),
		zap.String("method", req.Method),
		zap.String("url", req.URL.String()),
		zap.Int("status", resp.StatusCode),
		zap.Duration("latency", latency),
		zap.Any("headers", resp.Header),
		zap.ByteString("body", respBody))

	return resp, nil
}

// readBodyTruncated reads up to maxSize bytes from body, then restores it.
func readBodyTruncated(body io.ReadCloser, maxSize int) []byte {
	if body == nil {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(body, int64(maxSize)))
	return data
}
