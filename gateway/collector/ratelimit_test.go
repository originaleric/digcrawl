package collector

import (
	"testing"
	"time"
)

func TestRateLimiterCooldown(t *testing.T) {
	t.Parallel()
	lim := NewRateLimiter(200 * time.Millisecond)
	if !lim.Check("wechat:mp.weixin.qq.com") {
		t.Fatal("expected first check ok")
	}
	lim.Touch("wechat:mp.weixin.qq.com")
	if lim.Check("wechat:mp.weixin.qq.com") {
		t.Fatal("expected cooldown active")
	}
}
