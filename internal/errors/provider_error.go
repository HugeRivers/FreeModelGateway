package errors

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// ErrorCategory 错误大类
type ErrorCategory int

const (
	CategoryNetwork ErrorCategory = iota
	CategoryHTTP
	CategoryApplication
)

// RateLimitType 429 细分类型
type RateLimitType int

const (
	RateLimitUnknown         RateLimitType = iota
	RateLimitPerMinute                     // RPM 限流（瞬时的）
	RateLimitPerDay                        // RPD 限流（日配额）
	RateLimitPerMinuteTokens               // TPM 限流
	RateLimitPerDayTokens                  // TPD 限流
	RateLimitProviderWide                  // Provider 级日配额
	RateLimitConcurrent                    // 并发限制
)

func (r RateLimitType) String() string {
	switch r {
	case RateLimitPerMinute:
		return "per_minute"
	case RateLimitPerDay:
		return "per_day"
	case RateLimitPerMinuteTokens:
		return "per_minute_tokens"
	case RateLimitPerDayTokens:
		return "per_day_tokens"
	case RateLimitProviderWide:
		return "provider_wide"
	case RateLimitConcurrent:
		return "concurrent"
	default:
		return "unknown"
	}
}

// ProviderError 统一的提供商错误类型
type ProviderError struct {
	Code    int    // HTTP 状态码（网络错误为 0）
	Message string // 错误消息
	RawBody []byte // 原始响应体

	// Fallback 决策
	Retryable      bool          // 是否可重试
	Cooldown       time.Duration // 冷却时长（0 = 不冷却）
	SkipModel      bool          // 是否跳过当前 model
	SkipProvider   bool          // 是否跳过整个 provider
	MarkKeyInvalid bool          // 是否标记 key 为 invalid

	// 分类
	Category      ErrorCategory
	RateLimitType RateLimitType // 429 细分类型
}

func (e *ProviderError) Error() string {
	return e.Message
}

// ParseProviderError 从原始错误解析出统一的 ProviderError
func ParseProviderError(err error, statusCode int, body []byte) *ProviderError {
	pe := &ProviderError{
		Code:    statusCode,
		Message: "",
		RawBody: body,
	}

	if err != nil {
		pe.Message = err.Error()
	}

	switch statusCode {
	case http.StatusTooManyRequests: // 429
		pe.Category = CategoryHTTP
		pe.Retryable = true
		pe.SkipModel = true
		pe.RateLimitType = classifyRateLimit(pe.Message)
		pe.Cooldown = getCooldownForRateLimit(pe.RateLimitType)
		// Provider-wide / 日配额 → 跳过整个 provider
		if pe.RateLimitType == RateLimitProviderWide ||
			pe.RateLimitType == RateLimitPerDay ||
			pe.RateLimitType == RateLimitPerDayTokens {
			pe.SkipProvider = true
		}

	case http.StatusPaymentRequired: // 402
		pe.Category = CategoryHTTP
		pe.Retryable = true
		pe.SkipModel = true
		pe.SkipProvider = true
		pe.Cooldown = 24 * time.Hour

	case http.StatusUnauthorized, http.StatusForbidden: // 401/403
		pe.Category = CategoryHTTP
		pe.Retryable = false
		pe.MarkKeyInvalid = true

	case http.StatusNotFound: // 404
		pe.Category = CategoryHTTP
		pe.Retryable = true
		pe.SkipModel = true
		pe.Cooldown = 2 * time.Minute

	case http.StatusRequestEntityTooLarge: // 413
		pe.Category = CategoryHTTP
		pe.Retryable = true
		pe.SkipModel = true
		pe.Cooldown = 0

	case http.StatusBadRequest: // 400
		pe.Category = CategoryHTTP
		if isParamIncompatibility(pe.Message) {
			pe.Retryable = true
			pe.SkipModel = true
			pe.Cooldown = 90 * time.Second
		} else {
			pe.Retryable = false
		}

	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		http.StatusRequestTimeout: // 500/502/503/504/408
		pe.Category = CategoryHTTP
		pe.Retryable = true
		pe.SkipModel = true
		pe.Cooldown = 90 * time.Second

	case 0:
		// 网络层错误（无 HTTP 状态码）
		pe.Category = CategoryNetwork
		retryable, cooldown, skipProvider := classifyNetworkError(pe.Message)
		pe.Retryable = retryable
		pe.Cooldown = cooldown
		pe.SkipProvider = skipProvider
		if !skipProvider {
			pe.SkipModel = true
		}
	}

	return pe
}

// IsRetryableStatus 判断 HTTP 状态码是否可重试
func IsRetryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly,
		http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// IsClientErrorStatus 判断是否是客户端错误（不重试）
func IsClientErrorStatus(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// classifyRateLimit 根据错误消息判断 429 类型
func classifyRateLimit(msg string) RateLimitType {
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "provider") && strings.Contains(lower, "quota"):
		return RateLimitProviderWide
	case strings.Contains(lower, "per minute") || strings.Contains(lower, "rpm"):
		return RateLimitPerMinute
	case strings.Contains(lower, "per day") || strings.Contains(lower, "rpd"):
		return RateLimitPerDay
	case strings.Contains(lower, "tokens per minute") || strings.Contains(lower, "tpm"):
		return RateLimitPerMinuteTokens
	case strings.Contains(lower, "tokens per day") || strings.Contains(lower, "tpd"):
		return RateLimitPerDayTokens
	case strings.Contains(lower, "concurrent") || strings.Contains(lower, "too many connections"):
		return RateLimitConcurrent
	default:
		return RateLimitUnknown
	}
}

// getCooldownForRateLimit 根据 429 类型返回冷却时长
func getCooldownForRateLimit(rt RateLimitType) time.Duration {
	switch rt {
	case RateLimitPerMinute, RateLimitPerMinuteTokens:
		return 90 * time.Second // 瞬时限流 → 短冷却
	case RateLimitPerDay, RateLimitPerDayTokens:
		return 2 * time.Minute // 日配额 → 递增长冷却（起始）
	case RateLimitProviderWide:
		return 24 * time.Hour // Provider-wide → 长冷却
	case RateLimitConcurrent:
		return 30 * time.Second // 并发限制 → 最短冷却
	default:
		return 90 * time.Second // 未知 429 → 默认短冷却
	}
}

// classifyNetworkError 分类网络层错误
func classifyNetworkError(msg string) (retryable bool, cooldown time.Duration, skipProvider bool) {
	lower := strings.ToLower(msg)

	switch {
	// 超时类 → 重试，短冷却
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		return true, 30 * time.Second, false

	// 连接被拒绝 → provider 完全不可用，跳过 provider
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "econnrefused"):
		return true, 2 * time.Minute, true

	// 连接被重置 → 可能是网络抖动，只换 model
	case strings.Contains(lower, "connection reset") || strings.Contains(lower, "econnreset"):
		return true, 30 * time.Second, false

	// DNS 失败 → provider 域名问题，跳过 provider
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "dns") ||
		strings.Contains(lower, "notfound") || strings.Contains(lower, "enotfound"):
		return true, 5 * time.Minute, true

	// TLS/SSL 错误 → 证书问题，不重试（安全问题）
	case strings.Contains(lower, "tls") || strings.Contains(lower, "ssl") ||
		strings.Contains(lower, "certificate"):
		return false, 0, false

	// 请求被中止（客户端取消）→ 不重试
	case strings.Contains(lower, "aborted") || strings.Contains(lower, "canceled"):
		return false, 0, false

	default:
		return true, 30 * time.Second, false
	}
}

// isParamIncompatibility 判断是否是因为参数不兼容导致的 400
func isParamIncompatibility(msg string) bool {
	lower := strings.ToLower(msg)
	// 常见参数不兼容错误
	patterns := []string{
		"api error 400",
		"parameter",
		"unsupported",
		"not supported",
		"invalid parameter",
		"model does not support",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// AsProviderError 尝试将 error 转为 ProviderError，如果不是则包装
func AsProviderError(err error) *ProviderError {
	if err == nil {
		return nil
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe
	}
	return nil
}
