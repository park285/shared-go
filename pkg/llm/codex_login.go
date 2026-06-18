package llm

import (
	"context"
	"fmt"
	"time"
)

func (c *CodexJSONGenerator) EnsureLogin(ctx context.Context) error {
	if c == nil {
		return ErrNilJSONGenerator
	}
	if !c.loginCheck {
		return nil
	}

	c.loginMu.Lock()
	defer c.loginMu.Unlock()

	if c.loggedIn {
		return nil
	}
	if err := c.loginStatus(ctx); err == nil {
		c.loggedIn = true
		return nil
	} else if c.accessToken == "" {
		return fmt.Errorf("%w: run `codex login --device-auth` in CODEX_HOME=%q or mount an existing auth.json", ErrCodexAuthRequired, c.effectiveHomeForMessage())
	}

	if err := c.loginWithAccessToken(ctx); err != nil {
		return fmt.Errorf("codex login with access token failed: %w", err)
	}
	if err := c.loginStatus(ctx); err != nil {
		return fmt.Errorf("%w: codex login status failed after token login: %w", ErrCodexAuthRequired, err)
	}
	c.loggedIn = true
	return nil
}

func (c *CodexJSONGenerator) loginStatus(ctx context.Context) error {
	ctx, cancel := c.commandContext(ctx, minDuration(c.timeout, 15*time.Second))
	defer cancel()

	_, _, err := c.runRaw(ctx, []string{"login", "status"}, "")
	return err
}

func (c *CodexJSONGenerator) loginWithAccessToken(ctx context.Context) error {
	if c.accessToken == "" {
		return ErrCodexAuthRequired
	}

	ctx, cancel := c.commandContext(ctx, minDuration(c.timeout, 30*time.Second))
	defer cancel()

	_, _, err := c.runRaw(ctx, []string{"login", "--with-access-token"}, c.accessToken+"\n")
	return err
}
