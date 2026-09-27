package data

import (
	"time"

	"github.com/redis/go-redis/v9"
	"go-wind-admin/pkg/localdeps/go-utils/captcha"
	"go-wind-admin/pkg/localdeps/go-utils/password"

	klog "github.com/go-kratos/kratos/v2/log"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	redisClient "go-wind-admin/pkg/localdeps/kratos-bootstrap/cache/redis"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"

	"go-wind-admin/pkg/serviceid"
)

func NewClientType() authenticationV1.ClientType {
	return authenticationV1.ClientType_admin
}

// NewRedisClient 创建Redis客户端
func NewRedisClient(ctx *bootstrap.Context) (*redis.Client, func(), error) {
	cfg := ctx.GetConfig()
	if cfg == nil {
		return nil, func() {}, nil
	}

	l := ctx.NewLoggerHelper("redis/data/admin-service")

	cli := redisClient.NewClient(cfg.Data, klog.NewHelper(bLogger.AsKratosLogger(l)))

	return cli, func() {
		if err := cli.Close(); err != nil {
			l.Error(ctx.Context(), err.Error())
		}
	}, nil
}

func NewPasswordCrypto() password.Crypto {
	crypto, err := password.CreateCrypto("bcrypt")
	if err != nil {
		panic(err)
	}
	return crypto
}

func NewCaptcha(rdb *redis.Client) *captcha.Captcha {
	captchaInstance := captcha.NewCaptcha(rdb,
		captcha.WithDriverType(captcha.DriverString),
		captcha.WithExpire(10*time.Minute),
		captcha.WithKeyPrefix(serviceid.ProjectName+":captcha"),
		captcha.WithStringCount(6),
		captcha.WithStringSource("ABCDEFGHJKLMNPQRSTUVWXYZ23456789"),
	)
	return captchaInstance
}
