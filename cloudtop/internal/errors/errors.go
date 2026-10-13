package errors

import (
	"fmt"
)

// ErrorType represents different categories of errors
type ErrorType int

const (
	ErrorTypeAuth ErrorType = iota
	ErrorTypeNetwork
	ErrorTypeRateLimit
	ErrorTypeNotFound
	ErrorTypePermission
	ErrorTypeValidation
	ErrorTypeInternal
)

func (e ErrorType) String() string {
	switch e {
	case ErrorTypeAuth:
		return "authentication"
	case ErrorTypeNetwork:
		return "network"
	case ErrorTypeRateLimit:
		return "rate_limit"
	case ErrorTypeNotFound:
		return "not_found"
	case ErrorTypePermission:
		return "permission"
	case ErrorTypeValidation:
		return "validation"
	case ErrorTypeInternal:
		return "internal"
	default:
		return "unknown"
	}
}

// CloudtopError is a custom error type with metadata
type CloudtopError struct {
	Type      ErrorType
	Provider  string
	Message   string
	Err       error
	Retryable bool
}

// Error implements the error interface
func (e *CloudtopError) Error() string {
	if e.Provider != "" {
		if e.Err != nil {
			return fmt.Sprintf("[%s] %s: %v", e.Provider, e.Message, e.Err)
		}
		return fmt.Sprintf("[%s] %s", e.Provider, e.Message)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap implements error unwrapping
func (e *CloudtopError) Unwrap() error {
	return e.Err
}

// IsRetryable returns whether the error can be retried
func (e *CloudtopError) IsRetryable() bool {
	return e.Retryable
}

// Constructor functions for common error types

func NewAuthError(provider string, err error) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypeAuth,
		Provider:  provider,
		Message:   "authentication failed",
		Err:       err,
		Retryable: false,
	}
}

func NewNetworkError(provider string, err error) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypeNetwork,
		Provider:  provider,
		Message:   "network error",
		Err:       err,
		Retryable: true,
	}
}

func NewRateLimitError(provider string, err error) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypeRateLimit,
		Provider:  provider,
		Message:   "rate limit exceeded",
		Err:       err,
		Retryable: true,
	}
}

func NewNotFoundError(provider string, resource string) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypeNotFound,
		Provider:  provider,
		Message:   fmt.Sprintf("resource not found: %s", resource),
		Retryable: false,
	}
}

func NewPermissionError(provider string, err error) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypePermission,
		Provider:  provider,
		Message:   "permission denied",
		Err:       err,
		Retryable: false,
	}
}

func NewValidationError(provider string, message string) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypeValidation,
		Provider:  provider,
		Message:   message,
		Retryable: false,
	}
}

func NewInternalError(provider string, err error) *CloudtopError {
	return &CloudtopError{
		Type:      ErrorTypeInternal,
		Provider:  provider,
		Message:   "internal error",
		Err:       err,
		Retryable: false,
	}
}

// ErrorHandler manages error handling strategies
type ErrorHandler struct {
	degradeGracefully bool
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(degradeGracefully bool) *ErrorHandler {
	return &ErrorHandler{
		degradeGracefully: degradeGracefully,
	}
}

// Handle processes an error and returns whether to continue
func (h *ErrorHandler) Handle(err error) bool {
	if err == nil {
		return true
	}

	ctErr, ok := err.(*CloudtopError)
	if !ok {
		// Unknown error type
		return h.degradeGracefully
	}

	switch ctErr.Type {
	case ErrorTypeAuth, ErrorTypePermission:
		// Fatal errors - always fail
		return false
	case ErrorTypeNetwork, ErrorTypeRateLimit:
		// Retryable errors - can continue in degraded mode
		return h.degradeGracefully
	case ErrorTypeNotFound:
		// Resource not found - can continue
		return true
	default:
		return h.degradeGracefully
	}
}

// IsAuthError checks if an error is an authentication error
func IsAuthError(err error) bool {
	ctErr, ok := err.(*CloudtopError)
	return ok && ctErr.Type == ErrorTypeAuth
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) bool {
	ctErr, ok := err.(*CloudtopError)
	return ok && ctErr.Type == ErrorTypeRateLimit
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(err error) bool {
	ctErr, ok := err.(*CloudtopError)
	return ok && ctErr.Retryable
}
