package drive

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppxb/miyabi/internal/pan"
)

func (d *Drive) refreshTokens(ctx context.Context, expected snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := fmt.Sprintf("%d:%d", expected.credentialVersion, expected.tokenVersion)
	result := d.refresh.DoChan(key, func() (any, error) {
		done, ok := d.StartWork()
		if !ok {
			return nil, context.Canceled
		}
		defer done()
		current, err := d.credentials(expected)
		if err != nil || current.tokenVersion != expected.tokenVersion {
			return nil, err
		}
		tokenContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamTimeout)
		defer cancel()
		tokens, err := d.client.RefreshToken(tokenContext, current.tokens.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("refresh 115 credentials: %w", err)
		}
		if err := d.commit.Lock(tokenContext); err != nil {
			return nil, err
		}
		defer d.commit.Unlock()
		current, err = d.credentials(expected)
		if err != nil || current.tokenVersion != expected.tokenVersion {
			return nil, err
		}
		if err := saveSetting(tokenContext, d.database, credentialsSetting, tokens); err != nil {
			return nil, err
		}
		d.mu.Lock()
		d.tokens = tokens
		d.tokenVersion++
		d.mu.Unlock()
		return nil, nil
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case completed := <-result:
		return completed.Err
	}
}

func withPanToken[T any](ctx context.Context, d *Drive, expected snapshot, request func(string) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	current, err := d.credentials(expected)
	if err != nil {
		return zero, err
	}
	if current.closed {
		return zero, context.Canceled
	}
	refreshed := !current.tokens.ExpiresAt.IsZero() && time.Until(current.tokens.ExpiresAt) <= 30*time.Second
	if refreshed {
		if err := d.refreshTokens(ctx, current); err != nil {
			return zero, err
		}
		current, err = d.credentials(expected)
		if err != nil {
			return zero, err
		}
	}
	if current.closed {
		return zero, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	value, err := request(current.tokens.AccessToken)
	if errors.Is(err, pan.ErrUnauthorized) && !refreshed {
		if err := d.refreshTokens(ctx, current); err != nil {
			return zero, err
		}
		current, err = d.credentials(expected)
		if err != nil {
			return zero, err
		}
		if current.closed {
			return zero, context.Canceled
		}
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return request(current.tokens.AccessToken)
	}
	return value, err
}

func withPanSourceToken[T any](ctx context.Context, d *Drive, expected snapshot, request func(string) (T, error)) (T, error) {
	return withPanToken(ctx, d, expected, func(token string) (T, error) {
		if _, err := d.sourceState(expected.source(), expected.authorizationVersion); err != nil {
			var zero T
			return zero, err
		}
		return request(token)
	})
}
