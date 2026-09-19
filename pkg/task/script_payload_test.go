// 本文件补 pkg/task 的 ScriptTaskPayloadFromRaw 测试：
// sys_tasks.task_payload 的原始 JSON 到任务载荷的解析契约——空载荷给零值结构、
// 合法载荷带可选 params 解出、带 handler 空串、非法 JSON 报错。
// 分发类型常量 ScriptTaskDispatchType 一并钉死（asynq 固定分发类型的桥接约定，
// 见 pkg/task/script.go 的设计约束注释与 docs/script_system.md）。
package task

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScriptTaskPayloadFromRaw(t *testing.T) {
	t.Run("空载荷给零值结构", func(t *testing.T) {
		d, err := ScriptTaskPayloadFromRaw(nil)
		require.NoError(t, err)
		require.NotNil(t, d)
		require.Empty(t, d.Handler)
		require.Empty(t, d.Params)
	})

	t.Run("合法载荷带params", func(t *testing.T) {
		d, err := ScriptTaskPayloadFromRaw([]byte(`{"handler":"cleanup_tmp","params":{"older_than":86400}}`))
		require.NoError(t, err)
		require.Equal(t, "cleanup_tmp", d.Handler)
		require.Equal(t, map[string]any{"older_than": float64(86400)}, d.Params)
	})

	t.Run("合法载荷省略params", func(t *testing.T) {
		d, err := ScriptTaskPayloadFromRaw([]byte(`{"handler":"noop"}`))
		require.NoError(t, err)
		require.Equal(t, "noop", d.Handler)
		require.Empty(t, d.Params)
	})

	t.Run("handler为空串", func(t *testing.T) {
		d, err := ScriptTaskPayloadFromRaw([]byte(`{"handler":""}`))
		require.NoError(t, err)
		require.Empty(t, d.Handler)
	})

	t.Run("非法JSON报错", func(t *testing.T) {
		d, err := ScriptTaskPayloadFromRaw([]byte(`{"handler":`))
		require.Error(t, err)
		require.Nil(t, d)
	})

	t.Run("非对象JSON报错", func(t *testing.T) {
		d, err := ScriptTaskPayloadFromRaw([]byte(`[1,2,3]`))
		require.Error(t, err)
		require.Nil(t, d)
	})
}

// TestScriptTaskDispatchTypePinned 分发类型常量钉死。
func TestScriptTaskDispatchTypePinned(t *testing.T) {
	require.Equal(t, "script_task", ScriptTaskDispatchType)
}
