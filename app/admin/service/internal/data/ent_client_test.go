package data

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestEntClientRejectsStartupMigration(t *testing.T) {
	cfg := &conf.Bootstrap{}
	require.NoError(t, protojson.Unmarshal([]byte(`{"data":{"database":{"migrate":true}}}`), cfg))
	ctx := bootstrap.NewContextWithParam(context.Background(), &conf.AppInfo{}, cfg, bLogger.NopLogger())
	client, cleanup, err := NewEntClient(ctx)
	require.ErrorContains(t, err, "Atlas")
	require.Nil(t, client)
	require.Nil(t, cleanup)
}
