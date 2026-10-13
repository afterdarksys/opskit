package ratelimit

import (
	"context"
	"sync"
	"time"
)

// Limiter implements token bucket rate limiting
type Limiter struct {
	mu              sync.Mutex
	tokens          float64
	maxTokens       float64
	refillRate      float64 // tokens per second
	lastRefillTime  time.Time
	timeout         time.Duration
}

// NewLimiter creates a rate limiter
func NewLimiter(requestsPerSecond float64, burst int, timeout time.Duration) *Limiter {
	return &Limiter{
		tokens:         float64(burst),
		maxTokens:      float64(burst),
		refillRate:     requestsPerSecond,
		lastRefillTime: time.Now(),
		timeout:        timeout,
	}
}

// Wait blocks until request can proceed or context is done
func (l *Limiter) Wait(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()

	for {
		if l.Allow() {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(time.Millisecond * 50):
			// Retry after short delay
			continue
		}
	}
}

// Allow returns true if request can proceed immediately
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.refill()

	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// refill adds tokens based on elapsed time
func (l *Limiter) refill() {
	now := time.Now()
	elapsed := now.Sub(l.lastRefillTime).Seconds()
	l.lastRefillTime = now

	l.tokens += elapsed * l.refillRate
	if l.tokens > l.maxTokens {
		l.tokens = l.maxTokens
	}
}

// Available returns the current number of available tokens
func (l *Limiter) Available() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refill()
	return l.tokens
}

// Reset resets the limiter to full capacity
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tokens = l.maxTokens
	l.lastRefillTime = time.Now()
}
