package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"

	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
)

// TestComponentToModule 组件路径前缀 → 业务模块的归类必须与登记的前缀表
// 完全一致。该函数用于默认菜单与存量菜单的 module 字段回填，模块白名单
// 依赖该字段过滤租户可见菜单；前缀匹配错位会导致菜单在租户侧整页消失。
func TestComponentToModule(t *testing.T) {
	cases := []struct {
		name      string
		component string
		want      identityV1.Module
	}{
		// catalog 容器节点：不参与白名单过滤
		{"layout container", "BasicLayout", identityV1.Module_MODULE_UNSPECIFIED},
		{"empty component", "", identityV1.Module_MODULE_UNSPECIFIED},

		// 登记过的前缀，路径带具体页面
		{"dashboard page", "dashboard/analytics/index.vue", identityV1.Module_DASHBOARD},
		{"dashboard bare prefix", "dashboard/", identityV1.Module_DASHBOARD},
		{"opm page", "app/opm/user/list/index.vue", identityV1.Module_OPM},
		{"system page", "app/system/config/index.vue", identityV1.Module_SYSTEM},
		{"dict page", "app/dict/entry/index.vue", identityV1.Module_DICT},
		{"tenant page", "app/tenant/tenant/index.vue", identityV1.Module_TENANT},
		{"permission page", "app/permission/menu/index.vue", identityV1.Module_PERMISSION},
		{"log page", "app/log/api_audit_log/index.vue", identityV1.Module_LOG},
		{"log bare prefix", "app/log/", identityV1.Module_LOG},
		{"internal message page", "app/internal_message/inbox/index.vue", identityV1.Module_INTERNAL_MESSAGE},
		{"internal message bare prefix", "app/internal_message/", identityV1.Module_INTERNAL_MESSAGE},
		{"file page", "app/file/list/index.vue", identityV1.Module_FILE},
		{"task page", "app/task/list/index.vue", identityV1.Module_TASK},

		// 复合前缀：switch 顺序决定归入 system 而非 dict/file/task
		{"system dict page hits system first", "app/system/dict/index.vue", identityV1.Module_SYSTEM},
		{"system file page hits system first", "app/system/file/index.vue", identityV1.Module_SYSTEM},
		{"system task page hits system first", "app/system/task/index.vue", identityV1.Module_SYSTEM},

		// 未登记的前缀 / 前缀变体一律 UNSPECIFIED
		{"unknown top dir", "app/unknown/thing/index.vue", identityV1.Module_MODULE_UNSPECIFIED},
		{"prefix without slash", "app/opm", identityV1.Module_MODULE_UNSPECIFIED},
		{"prefix with extra letter", "app/opmx/user/index.vue", identityV1.Module_MODULE_UNSPECIFIED},
		{"dashboard with suffix letter", "dashboardx/index.vue", identityV1.Module_MODULE_UNSPECIFIED},
		{"case sensitive", "App/OPM/user/index.vue", identityV1.Module_MODULE_UNSPECIFIED},
		{"leading space not trimmed", " app/opm/user/index.vue", identityV1.Module_MODULE_UNSPECIFIED},
		{"plain text", "just-a-name", identityV1.Module_MODULE_UNSPECIFIED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ComponentToModule(tc.component))
		})
	}
}

// TestDefaultMenusModuleBackfillInvariant 默认菜单种子数据与归类函数的
// 一致性：非容器菜单的组件必须能归入一个已定义的业务模块，否则该菜单
// 会被模块白名单当作 UNSPECIFIED 过滤掉，租户侧凭空丢页面；
// 容器节点必须归为 UNSPECIFIED。
func TestDefaultMenusModuleBackfillInvariant(t *testing.T) {
	for _, menu := range DefaultMenus {
		component := menu.GetComponent()
		module := ComponentToModule(component)
		if component == "" || component == "BasicLayout" {
			assert.Equal(t, identityV1.Module_MODULE_UNSPECIFIED, module,
				"容器菜单 %q 的组件应归为 UNSPECIFIED", component)
			continue
		}
		_, defined := identityV1.Module_name[int32(module)]
		assert.True(t, defined,
			"菜单组件 %q 归类到未定义模块值 %d", component, module)
		assert.NotEqual(t, identityV1.Module_MODULE_UNSPECIFIED, module,
			"非容器菜单组件 %q 未被 ComponentToModule 登记，租户白名单会过滤掉该菜单；请登记前缀或修正组件路径", component)
	}
}
