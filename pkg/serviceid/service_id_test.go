package serviceid

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewDiscoveryName_AdminService 钉住服务发现名称的拼接契约：
// ProjectName + "/" + serviceName。
func TestNewDiscoveryName_AdminService(t *testing.T) {
	got := NewDiscoveryName(AdminService)
	assert.Equal(t, "gowind/admin-service", got)
}

// TestNewDiscoveryName_UsesProjectPrefix 名称必须以 ProjectName + "/" 开头。
func TestNewDiscoveryName_UsesProjectPrefix(t *testing.T) {
	got := NewDiscoveryName("foo")
	assert.Equal(t, "gowind/foo", got)
	assert.True(t, len(got) > len(ProjectName)+1, "结果应长于前缀本身")
}

// TestProjectNameConstant 钉住项目名常量值，防止被误改影响服务发现。
func TestProjectNameConstant(t *testing.T) {
	assert.Equal(t, "gowind", ProjectName)
}

// TestAdminServiceConstant 钉住 AdminService 常量值。
func TestAdminServiceConstant(t *testing.T) {
	assert.Equal(t, "admin-service", AdminService)
}
