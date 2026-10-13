package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Config defines retry parameters
type Config struct {
	MaxRetries      int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	Jitter          float64 // 0-1, percentage of jitter to add
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		MaxRetries:      3,
		InitialInterval: 1 * time.Second,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		Jitter:          0.1,
	}
}

// Operation is a function that may be retried
type Operation func() error

// RetryableError indicates an error that can be retried
type RetryableError interface {
	IsRetryable() bool
}

// Do executes operation with retry logic
func Do(ctx context.Context, config Config, operation Operation) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if retryable, ok := err.(RetryableError); ok && !retryable.IsRetryable() {
			return err
		}

		// Don't sleep after the last attempt
		if attempt < config.MaxRetries {
			sleepDuration := calculateBackoff(attempt, config)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleepDuration):
				continue
			}
		}
	}

	return lastErr
}

// calculateBackoff calculates the backoff duration for a given attempt
func calculateBackoff(attempt int, config Config) time.Duration {
	// Calculate base backoff
	backoff := float64(config.InitialInterval) * math.Pow(config.Multiplier, float64(attempt))

	// Cap at max interval
	if backoff > float64(config.MaxInterval) {
		backoff = float64(config.MaxInterval)
	}

	// Add jitter
	if config.Jitter > 0 {
		jitterRange := backoff * config.Jitter
		jitter := (rand.Float64() * 2 * jitterRange) - jitterRange
		backoff += jitter
	}

	return time.Duration(backoff)
}

// DoWithResult executes operation with retry logic and returns result
func DoWithResult[T any](ctx context.Context, config Config, operation func() (T, error)) (T, error) {
	var lastErr error
	var zero T

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		default:
		}

		result, err := operation()
		if err == nil {
			return result, nil
		}

		lastErr = err

		// Check if error is retryable
		if retryable, ok := err.(RetryableError); ok && !retryable.IsRetryable() {
			return zero, err
		}

		// Don't sleep after the last attempt
		if attempt < config.MaxRetries {
			sleepDuration := calculateBackoff(attempt, config)
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(sleepDuration):
				continue
			}
		}
	}

	return zero, lastErr
}
