package ginauth

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/park285/shared-go/pkg/httputil"
)

const (
	// APIKeyHeader는 gin 인증 middleware가 읽는 API key 헤더 이름이다.
	APIKeyHeader = httputil.HeaderAPIKey
)

// APIKeyAuthMiddleware는 hololive-shared형 gin API key 인증 middleware를 만든다.
func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			c.Next()
			return
		}

		providedKey := c.GetHeader(APIKeyHeader)
		if providedKey == "" {
			abortWithError(c, http.StatusUnauthorized, "unauthorized", "API key required")
			return
		}

		if !httputil.ConstantTimeStringEqual(providedKey, apiKey) {
			abortWithError(c, http.StatusForbidden, "forbidden", "invalid API key")
			return
		}

		c.Next()
	}
}

// NoRouteAuthHandler는 인증 후에도 not_found JSON을 반환하는 gin NoRoute handler를 만든다.
func NoRouteAuthHandler(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if apiKey == "" {
			respondError(c, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		providedKey := c.GetHeader(APIKeyHeader)
		if providedKey == "" {
			respondError(c, http.StatusUnauthorized, "unauthorized", "API key required")
			return
		}

		if !httputil.ConstantTimeStringEqual(providedKey, apiKey) {
			respondError(c, http.StatusForbidden, "forbidden", "invalid API key")
			return
		}

		respondError(c, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

func errorPayload(code, message string) gin.H {
	payload := gin.H{"error": code}
	if message != "" {
		payload["message"] = message
	}
	return payload
}

func abortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorPayload(code, message))
}

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorPayload(code, message))
}
