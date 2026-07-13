package ginauth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/park285/shared-go/pkg/httputil"
)

const (
	// APIKeyHeader는 gin 인증 middleware가 읽는 API key 헤더 이름이다.
	APIKeyHeader = httputil.HeaderAPIKey
)

// AuthConfig는 Gin API key 인증 설정이다. zero value는 인증을 강제하고 빈 key를 fail-closed로 처리한다.
type AuthConfig = httputil.AdminAuthConfig

// AuthMiddleware는 명시적인 설정으로 Gin API key 인증 middleware를 만든다.
func AuthMiddleware(cfg AuthConfig) gin.HandlerFunc {
	expected := strings.TrimSpace(cfg.APIKey)

	return func(c *gin.Context) {
		if cfg.Disabled {
			c.Next()
			return
		}
		if expected == "" {
			abortWithError(c, http.StatusServiceUnavailable, "auth_not_configured", "API key not configured")
			return
		}

		providedKey := c.GetHeader(APIKeyHeader)
		if providedKey == "" {
			abortWithError(c, http.StatusUnauthorized, "unauthorized", "API key required")
			return
		}

		if !httputil.ConstantTimeStringEqual(providedKey, expected) {
			abortWithError(c, http.StatusForbidden, "forbidden", "invalid API key")
			return
		}

		c.Next()
	}
}

// APIKeyAuthMiddleware는 hololive-shared형 gin API key 인증 middleware를 만든다.
//
// Deprecated: 인증 비활성화가 필요하면 AuthMiddleware와 AuthConfig.Disabled를 명시하십시오.
func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	return AuthMiddleware(AuthConfig{APIKey: apiKey})
}

// NoRouteHandler는 명시적인 설정으로 인증 후 not_found JSON을 반환하는 Gin NoRoute handler를 만든다.
func NoRouteHandler(cfg AuthConfig) gin.HandlerFunc {
	expected := strings.TrimSpace(cfg.APIKey)

	return func(c *gin.Context) {
		if cfg.Disabled {
			respondError(c, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		if expected == "" {
			respondError(c, http.StatusServiceUnavailable, "auth_not_configured", "API key not configured")
			return
		}

		providedKey := c.GetHeader(APIKeyHeader)
		if providedKey == "" {
			respondError(c, http.StatusUnauthorized, "unauthorized", "API key required")
			return
		}

		if !httputil.ConstantTimeStringEqual(providedKey, expected) {
			respondError(c, http.StatusForbidden, "forbidden", "invalid API key")
			return
		}

		respondError(c, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

// NoRouteAuthHandler는 인증 후에도 not_found JSON을 반환하는 gin NoRoute handler를 만든다.
//
// Deprecated: 인증 비활성화가 필요하면 NoRouteHandler와 AuthConfig.Disabled를 명시하십시오.
func NoRouteAuthHandler(apiKey string) gin.HandlerFunc {
	return NoRouteHandler(AuthConfig{APIKey: apiKey})
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
