package api

import (
	"errors"
	"testing"
	"time"
)

func TestLoginRateLimiter_BlocksAfterMaxFailures(t *testing.T) {
	limiter := newLoginRateLimiter(3, 10*time.Minute, 10*time.Minute)
	ip := "192.168.1.100"

	// 1st failure
	limiter.recordFailure(ip)
	if err := limiter.check(ip); err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}

	// 2nd failure
	limiter.recordFailure(ip)
	if err := limiter.check(ip); err != nil {
		t.Fatalf("expected allowed, got %v", err)
	}

	// 3rd failure (triggers block)
	limiter.recordFailure(ip)
	if err := limiter.check(ip); !errors.Is(err, ErrTooManyLoginAttempts) {
		t.Fatalf("expected ErrTooManyLoginAttempts, got %v", err)
	}

	// Different IP should still be allowed
	if err := limiter.check("10.0.0.1"); err != nil {
		t.Fatalf("expected other IP to be allowed, got %v", err)
	}

	// Success clears failure count
	limiter.recordSuccess(ip)
	if err := limiter.check(ip); err != nil {
		t.Fatalf("expected allowed after success, got %v", err)
	}
}
