package service

import (
	"context"
	"github.com/stretchr/testify/require"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"testing"
)

func TestConstructorsDoNotAccessDatabase(t *testing.T) {
	ctx := bootstrap.NewContextWithParam(context.Background(), &conf.AppInfo{}, &conf.Bootstrap{}, bLogger.NopLogger())
	require.NotPanics(t, func() {
		NewApiService(ctx, nil, nil)
		NewConfigService(ctx, nil)
		NewLanguageService(ctx, nil)
		NewMenuService(ctx, nil)
		NewPermissionGroupService(ctx, nil, nil)
		NewPermissionService(ctx, nil, nil, nil, nil, nil, nil)
		NewRoleService(ctx, nil, nil, nil)
		NewUserService(ctx, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	})
}
