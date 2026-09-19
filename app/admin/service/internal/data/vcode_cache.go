package data

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	bLogger "github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/tx7do/kratos-bootstrap/logger"

	"go-wind-admin/pkg/serviceid"
)

// vcodeKeyPrefix 验证码键前缀（与 captcha 同前缀域便于统一清理）
const vcodeKeyPrefix = serviceid.ProjectName + ":vcode:"

// VCodeCache 一次性验证码缓存（Redis）。
// purpose 区分业务场景（reset_password / bind_contact），
// 校验成功即删除（单次有效），TTL 到期自动清理。
type VCodeCache struct {
	rdb *redis.Client
	log *logger.Helper
}

func NewVCodeCache(ctx *bLogger.Context, rdb *redis.Client) *VCodeCache {
	return &VCodeCache{
		rdb: rdb,
		log: ctx.NewLoggerHelper("vcode/cache"),
	}
}

// Save 保存验证码，ttl 到期自动删除。
func (c *VCodeCache) Save(purpose, identifier, code string, ttl time.Duration) error {
	key := vcodeKeyPrefix + purpose + ":" + identifier
	if err := c.rdb.Set(context.Background(), key, code, ttl).Err(); err != nil {
		c.log.Errorf(context.Background(), "save vcode failed: %v", err)
		return err
	}
	return nil
}

// Verify 校验验证码（消费型：匹配即删除，单次有效）。
func (c *VCodeCache) Verify(purpose, identifier, code string) bool {
	key := vcodeKeyPrefix + purpose + ":" + identifier
	v, err := c.rdb.Get(context.Background(), key).Result()
	if err != nil || v != code {
		return false
	}
	c.rdb.Del(context.Background(), key)
	return true
}
