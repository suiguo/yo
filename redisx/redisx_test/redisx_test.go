package redisx_test

import (
	"context"
	"testing"

	"github.com/suiguo/yo/logger"
	"github.com/suiguo/yo/redisx"
)

var l = logger.GetLogger("redisx")

func TestRedisx(t *testing.T) {
	ctx := context.Background()
	// 1. 创建 Redis 客户端
	client, err := redisx.InitFromFile("redis.yaml")
	if err != nil {
		l.Error("❌ Failed to create Redis client", "err", err)
		t.FailNow()
	}
	l.Info("✅ Redis client created successfully")
	// client.AddHook()
	// 2. 测试 Ping
	if err := client.Ping(ctx).Err(); err != nil {
		l.Error("❌ Failed to ping Redis", "err", err)
		t.FailNow()
	}
	l.Info("✅ Ping Redis successful")

	// 3. 测试 Set
	if err := client.Set(ctx, "key", "value", 0).Err(); err != nil {
		l.Error("❌ Failed to set value:", "err", err)
		t.FailNow()
	}
	l.Info("✅ Set key 'key' = 'value'")

	// 4. 测试 Get
	val, err := client.Get(ctx, "key").Result()
	if err != nil {
		l.Error("❌ Failed to get value", "err", err)
		t.FailNow()
	}
	if val != "value" {
		l.Error("❌ Expected", "val", "value", "got", val)
		t.FailNow()
	}
	l.Info("✅ Got key", "key", "key", "val", val)

	// 5. 测试 Del
	if err := client.Del(ctx, "key").Err(); err != nil {
		l.Error("❌ Failed to delete key", "err", err)
		t.FailNow()
	}
	l.Info("✅ Key 'key' deleted successfully")
}
