package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DailyQuotaRetryThreshold is the RetryInfo delay above which a 429 is treated
// as an exhausted daily quota even when the quotaId does not contain "PerDay".
const DailyQuotaRetryThreshold = 120 * time.Second

// QuotaInfo is the provider-side classification of an HTTP 429. Daily means
// retrying the same model cannot help until RetryDelay elapses.
type QuotaInfo struct {
	Daily          bool
	QuotaID        string
	QuotaValue     string
	QuotaModel     string
	RequestedModel string
	RetryDelay     time.Duration
}

type quotaError struct {
	info QuotaInfo
}

func (e *quotaError) Error() string {
	return fmt.Sprintf("quota daily=%t id=%q value=%q model=%q retry_delay=%s",
		e.info.Daily, e.info.QuotaID, e.info.QuotaValue, e.info.QuotaModel, e.info.RetryDelay)
}

// NewQuotaError attaches a QuotaInfo to a provider error. The caller usually
// wraps the result in an AIProviderReason error so the client-safe message is
// preserved while QuotaErrorInfo can still recover the classification.
func NewQuotaError(info QuotaInfo) error {
	return &quotaError{info: info}
}

// QuotaErrorInfo recovers the 429 classification from an error chain.
func QuotaErrorInfo(err error) (QuotaInfo, bool) {
	var qe *quotaError
	if !errors.As(err, &qe) {
		return QuotaInfo{}, false
	}
	return qe.info, true
}

// geminiErrorEnvelope mirrors the subset of Google RPC error details needed to
// distinguish a daily quota exhaustion from a short-lived per-minute limit.
type geminiErrorEnvelope struct {
	Error struct {
		Code    int                 `json:"code"`
		Message string              `json:"message"`
		Status  string              `json:"status"`
		Details []geminiErrorDetail `json:"details"`
	} `json:"error"`
}

type geminiErrorDetail struct {
	Type            string                 `json:"@type"`
	QuotaID         string                 `json:"quotaId"`
	QuotaValue      interface{}            `json:"quotaValue"`
	QuotaDimensions map[string]string      `json:"quotaDimensions"`
	RetryDelay      string                 `json:"retryDelay"`
	Violations      []geminiQuotaViolation `json:"violations"`
}

type geminiQuotaViolation struct {
	QuotaID         string            `json:"quotaId"`
	QuotaValue      interface{}       `json:"quotaValue"`
	QuotaDimensions map[string]string `json:"quotaDimensions"`
}

// parseQuotaInfo extracts quotaId/quotaValue/quotaDimensions and RetryInfo from
// an HTTP 429 body. Malformed bodies yield a non-daily classification with no
// retry delay, which keeps the existing 429 retry policy.
func parseQuotaInfo(body []byte, requestedModel string) QuotaInfo {
	info := QuotaInfo{RequestedModel: requestedModel}
	var env geminiErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return info
	}

	for _, detail := range env.Error.Details {
		if delay := parseRetryDelay(detail.RetryDelay); delay > 0 {
			info.RetryDelay = delay
		}
		applyQuotaViolation(&info, detail.QuotaID, detail.QuotaValue, detail.QuotaDimensions)
		for _, violation := range detail.Violations {
			applyQuotaViolation(&info, violation.QuotaID, violation.QuotaValue, violation.QuotaDimensions)
		}
	}

	info.Daily = strings.Contains(strings.ToLower(info.QuotaID), "perday") ||
		info.RetryDelay > DailyQuotaRetryThreshold
	return info
}

func applyQuotaViolation(info *QuotaInfo, quotaID string, value interface{}, dimensions map[string]string) {
	quotaID = strings.TrimSpace(quotaID)
	if quotaID == "" {
		return
	}
	model := dimensions["model"]
	isDaily := strings.Contains(strings.ToLower(quotaID), "perday")
	if info.QuotaID != "" {
		existingDaily := strings.Contains(strings.ToLower(info.QuotaID), "perday")
		if existingDaily || !isDaily {
			return
		}
	}
	info.QuotaID = quotaID
	info.QuotaValue = quotaValueString(value)
	info.QuotaModel = model
}

func quotaValueString(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func parseRetryDelay(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	delay, err := time.ParseDuration(raw)
	if err != nil || delay < 0 {
		return 0
	}
	return delay
}
