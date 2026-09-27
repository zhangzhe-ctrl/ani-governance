// Package authorizer 白盒单元测试。
//
// 覆盖目标：
//   - newEngine 按 conf.Authorization.Type 选择引擎的分支逻辑
//     （空串/未知串/noop → noop 引擎；nil 配置 → nil；casbin 走真实引擎构造）。
//   - generateCasbinPolicies 的输出形状
//     （多角色多 API 条目下的映射关系，以及空输入的退化形状）。
//   - ResetPolicies 的分支：noop 引擎直接返回 nil；未知引擎名返回错误；
//     provider 出错时原样透传；casbin 名字的引擎收到生成器产物。
//   - NewAuthorizer 在 bootstrap.Context 各配置形态下的引擎初始化结果。
//
// 引擎均通过白盒构造 &Authorizer{...}（NopLogger 日志、stub Provider）实现，
// 不依赖数据库或网络。
package authorizer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authzEngine "go-wind-admin/pkg/localdeps/kratos-authz/engine"
	casbinPolicy "go-wind-admin/pkg/localdeps/kratos-authz/engine/casbin"
	conf "go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1"
	"go-wind-admin/pkg/localdeps/kratos-bootstrap/bootstrap"
	bLogger "go-wind-admin/pkg/localdeps/kratos-bootstrap/logger"
)

// ---------------------------------------------------------------------------
// 测试替身
// ---------------------------------------------------------------------------

// stubProvider 是 Provider 接口的测试替身：按预置数据返回策略，并可注入错误。
type stubProvider struct {
	policyData PermissionDataMap
	provideErr error
}

func (s *stubProvider) ProvidePolicies(_ context.Context) (PermissionDataMap, error) {
	if s.provideErr != nil {
		return nil, s.provideErr
	}
	return s.policyData, nil
}

// stubEngine 是 authzEngine.Engine 的测试替身：
// 返回构造时指定的引擎名，并记录 SetPolicies 收到的入参。
type stubEngine struct {
	name         string
	setErr       error
	setCalls     int
	lastPolicies authzEngine.PolicyMap
	lastRoles    authzEngine.RoleMap
}

func (s *stubEngine) Name() string { return s.name }

func (s *stubEngine) ProjectsAuthorized(_ context.Context, _ authzEngine.Subjects, _ authzEngine.Action, _ authzEngine.Resource, _ authzEngine.Projects) (authzEngine.Projects, error) {
	return nil, nil
}

func (s *stubEngine) FilterAuthorizedPairs(_ context.Context, _ authzEngine.Subjects, _ authzEngine.Pairs) (authzEngine.Pairs, error) {
	return nil, nil
}

func (s *stubEngine) FilterAuthorizedProjects(_ context.Context, _ authzEngine.Subjects) (authzEngine.Projects, error) {
	return nil, nil
}

func (s *stubEngine) IsAuthorized(_ context.Context, _ authzEngine.Subject, _ authzEngine.Action, _ authzEngine.Resource, _ authzEngine.Project) (bool, error) {
	return false, nil
}

func (s *stubEngine) SetPolicies(_ context.Context, policies authzEngine.PolicyMap, roles authzEngine.RoleMap) error {
	s.setCalls++
	s.lastPolicies = policies
	s.lastRoles = roles
	return s.setErr
}

// newTestAuthorizer 构造白盒测试实例：NopLogger 日志 + stub provider，引擎字段留空由用例自行注入。
func newTestAuthorizer(p Provider) *Authorizer {
	return &Authorizer{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		provider: p,
	}
}

// samplePermData 构造两个角色、各两条 API 权限的多角色多条目数据，
// 用于断言生成器的完整映射形状。
func samplePermData() PermissionDataMap {
	return PermissionDataMap{
		"roleA": {
			{Path: "/a", Method: "GET", Domain: "dom1"},
			{Path: "/b", Method: "POST", Domain: "dom1"},
		},
		"roleB": {
			{Path: "/c", Method: "GET", Domain: "dom2"},
			{Path: "/d", Method: "DELETE", Domain: "dom2"},
		},
	}
}

// ---------------------------------------------------------------------------
// newEngine 引擎选择
// ---------------------------------------------------------------------------

// TestNewEngine_EmptyAndNoopTypeSelectNoopEngine 验证 type 为空串与 "noop"
// 时都应得到真实可用的 noop 引擎——这是默认安全兜底引擎。
func TestNewEngine_EmptyAndNoopTypeSelectNoopEngine(t *testing.T) {
	tests := []struct {
		name     string
		authzTyp string
	}{
		{name: "empty type falls to noop", authzTyp: ""},
		{name: "explicit noop type", authzTyp: "noop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAuthorizer(&stubProvider{})
			eng := a.newEngine(context.Background(), &conf.Authorization{Type: tt.authzTyp})
			require.NotNil(t, eng, "noop 分支必须返回真实引擎实例")
			assert.Equal(t, "noop", eng.Name())
		})
	}
}

// TestNewEngine_UnknownTypeFallsThroughToNoop 验证未知的 type 走 default 分支
// fallthrough 到 noop，而不是返回 nil 或报错。
func TestNewEngine_UnknownTypeFallsThroughToNoop(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{})
	eng := a.newEngine(context.Background(), &conf.Authorization{Type: "totally-bogus-engine"})
	require.NotNil(t, eng)
	assert.Equal(t, "noop", eng.Name())
}

// TestNewEngine_NilConfigReturnsNil nil 配置必须返回 nil 引擎，不能构造默认引擎。
func TestNewEngine_NilConfigReturnsNil(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{})
	eng := a.newEngine(context.Background(), nil)
	assert.Nil(t, eng, "nil 配置应返回 nil 引擎")
}

// TestNewEngine_CasbinCreatesRealEngine casbin 类型应构造出真实 casbin 引擎
// （内置默认模型 + 内存 adapter，全程离线）。
func TestNewEngine_CasbinCreatesRealEngine(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{})
	eng := a.newEngine(context.Background(), &conf.Authorization{Type: "casbin"})
	require.NotNil(t, eng)
	assert.Equal(t, "casbin", eng.Name())
}

// TestEngine_ReturnsAssignedEngine Engine() 应原样返回当前引擎字段。
func TestEngine_ReturnsAssignedEngine(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{})
	noopEng := a.newEngine(context.Background(), &conf.Authorization{Type: "noop"})
	require.NotNil(t, noopEng)
	a.engine = noopEng
	assert.Same(t, noopEng, a.Engine())
}

// ---------------------------------------------------------------------------
// 策略生成器输出形状
// ---------------------------------------------------------------------------

// TestGenerateCasbinPolicies_MultiRoleMultiApi 多角色多条目数据必须逐一映射为
// casbin.PolicyRule（PType 固定 "p"，V0-V3 依次为角色/路径/方法/域），
// 且 "projects" 键来自 MakeProjects（空切片）。
func TestGenerateCasbinPolicies_MultiRoleMultiApi(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{})

	policies, err := a.generateCasbinPolicies(samplePermData())
	require.NoError(t, err)
	require.Len(t, policies, 2, "casbin 策略图应只含 policies/projects 两个键")

	rawRules, ok := policies["policies"].([]casbinPolicy.PolicyRule)
	require.True(t, ok, "policies 键应为 []casbin.PolicyRule")
	assert.Len(t, rawRules, 4, "2 角色 × 2 API 应生成 4 条规则")

	got := make(map[string]bool, len(rawRules))
	for _, r := range rawRules {
		assert.Equal(t, "p", r.PType, "PType 必须固定为 p")
		got[r.V0+"|"+r.V1+"|"+r.V2+"|"+r.V3] = true
	}
	expected := map[string]bool{
		"roleA|/a|GET|dom1":    true,
		"roleA|/b|POST|dom1":   true,
		"roleB|/c|GET|dom2":    true,
		"roleB|/d|DELETE|dom2": true,
	}
	assert.Equal(t, expected, got, "每条 API 条目都必须无损映射到 V0-V3")

	projects, ok := policies["projects"].(authzEngine.Projects)
	require.True(t, ok, "projects 键应为 engine.Projects")
	assert.Empty(t, projects, "MakeProjects() 默认不含任何项目")
}

// TestGenerateCasbinPolicies_EmptyInput 空输入下策略图仍应含两个键，
// 规则列表为空。
func TestGenerateCasbinPolicies_EmptyInput(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{})

	for name, data := range map[string]PermissionDataMap{
		"nil map":   nil,
		"empty map": {},
	} {
		t.Run(name, func(t *testing.T) {
			policies, err := a.generateCasbinPolicies(data)
			require.NoError(t, err)
			require.Len(t, policies, 2)

			rawRules, ok := policies["policies"].([]casbinPolicy.PolicyRule)
			require.True(t, ok)
			assert.Empty(t, rawRules, "空输入不应产生任何规则")

			projects, ok := policies["projects"].(authzEngine.Projects)
			require.True(t, ok)
			assert.Empty(t, projects)
		})
	}
}

// ---------------------------------------------------------------------------
// ResetPolicies
// ---------------------------------------------------------------------------

// TestResetPolicies_NoopEngineReturnsNil noop 引擎按约定直接返回 nil，
// 不向引擎灌策略。
func TestResetPolicies_NoopEngineReturnsNil(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{policyData: samplePermData()})
	a.engine = a.newEngine(context.Background(), &conf.Authorization{Type: "noop"})
	require.NotNil(t, a.engine)

	err := a.ResetPolicies(context.Background())
	assert.NoError(t, err, "noop 引擎的重置应无条件成功")
}

// TestResetPolicies_UnknownEngineNameReturnsError 引擎名不在
// casbin/noop 之列时必须报错，不能静默吞掉。
func TestResetPolicies_UnknownEngineNameReturnsError(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{policyData: samplePermData()})
	a.engine = &stubEngine{name: "mystery-engine"}

	err := a.ResetPolicies(context.Background())
	require.Error(t, err)
	assert.EqualError(t, err, "unknown engine name: mystery-engine")
}

// TestResetPolicies_ProviderErrorPassthrough provider 出错时必须原样透传错误，
// 供上层（如策略同步任务）感知失败。
func TestResetPolicies_ProviderErrorPassthrough(t *testing.T) {
	boom := errors.New("db down")
	a := newTestAuthorizer(&stubProvider{provideErr: boom})
	a.engine = &stubEngine{name: "casbin"}

	err := a.ResetPolicies(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom, "provider 错误必须原样透传")
}

// TestResetPolicies_CasbinStubReceivesGeneratedPolicies 引擎名为 casbin 时，
// ResetPolicies 应把 generateCasbinPolicies 的产物交给 SetPolicies。
func TestResetPolicies_CasbinStubReceivesGeneratedPolicies(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{policyData: samplePermData()})
	stub := &stubEngine{name: "casbin"}
	a.engine = stub

	err := a.ResetPolicies(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, stub.setCalls, "SetPolicies 应被调用一次")
	require.NotNil(t, stub.lastPolicies)

	rawRules, ok := stub.lastPolicies["policies"].([]casbinPolicy.PolicyRule)
	require.True(t, ok)
	assert.Len(t, rawRules, 4, "stub 引擎应收到 4 条生成的 casbin 规则")
	assert.Nil(t, stub.lastRoles, "RoleMap 入参应为 nil")
}

// TestResetPolicies_RealCasbinEngineLoadsPolicies 真实 casbin 引擎端到端：
// 生成的策略应能成功灌入内置默认模型（全程内存），ResetPolicies 返回 nil。
func TestResetPolicies_RealCasbinEngineLoadsPolicies(t *testing.T) {
	a := newTestAuthorizer(&stubProvider{policyData: samplePermData()})
	a.engine = a.newEngine(context.Background(), &conf.Authorization{Type: "casbin"})
	require.NotNil(t, a.engine)

	err := a.ResetPolicies(context.Background())
	assert.NoError(t, err, "内存 casbin 引擎应能加载生成的 4 条规则")
}

// TestResetPolicies_EngineSetPoliciesErrorPassthrough 引擎 SetPolicies 失败时
// 错误必须向上透传，不能被吞掉。
func TestResetPolicies_EngineSetPoliciesErrorPassthrough(t *testing.T) {
	boom := errors.New("engine rejected policies")
	a := newTestAuthorizer(&stubProvider{policyData: samplePermData()})
	a.engine = &stubEngine{name: "casbin", setErr: boom}

	err := a.ResetPolicies(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom, "SetPolicies 的错误必须原样透传")
}

// ---------------------------------------------------------------------------
// NewAuthorizer
// ---------------------------------------------------------------------------

// TestNewAuthorizer_NilConfigLeavesEngineNil bootstrap 配置缺失时
// NewAuthorizer 应早退，引擎保持 nil。
func TestNewAuthorizer_NilConfigLeavesEngineNil(t *testing.T) {
	bctx := bootstrap.NewContextWithParam(context.Background(), nil, nil, bLogger.NopLogger())
	a := NewAuthorizer(bctx, &stubProvider{})
	assert.Nil(t, a.Engine(), "nil 配置下引擎必须为 nil")
}

// TestNewAuthorizer_NilAuthzLeavesEngineNil 配置里 Authz 段缺失时同样早退。
func TestNewAuthorizer_NilAuthzLeavesEngineNil(t *testing.T) {
	bctx := bootstrap.NewContextWithParam(
		context.Background(), nil, &conf.Bootstrap{}, bLogger.NopLogger())
	a := NewAuthorizer(bctx, &stubProvider{})
	assert.Nil(t, a.Engine(), "Authz 段缺失时引擎必须为 nil")
}

// TestNewAuthorizer_NoopAuthzInitializesEngine 配置为 noop 时，
// NewAuthorizer 应经 init 构造出真实 noop 引擎。
func TestNewAuthorizer_NoopAuthzInitializesEngine(t *testing.T) {
	bctx := bootstrap.NewContextWithParam(
		context.Background(), nil,
		&conf.Bootstrap{Authz: &conf.Authorization{Type: "noop"}},
		bLogger.NopLogger())
	a := NewAuthorizer(bctx, &stubProvider{})
	require.NotNil(t, a.Engine())
	assert.Equal(t, "noop", a.Engine().Name())
}
