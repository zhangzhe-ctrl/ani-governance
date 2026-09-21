package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tx7do/go-utils/captcha"
	"github.com/tx7do/go-utils/timeutil"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/ent/privacy"

	ktransport "github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"

	"go-wind-admin/pkg/constants"
	appViewer "go-wind-admin/pkg/entgo/viewer"
	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/netutil"
)

// 验证码相关请求头（H5：登录强制验证码，通过 header 传递以避免改动 proto 与三套前端生成代码）。
const (
	headerCaptchaID    = "X-Captcha-Id"
	headerCaptchaValue = "X-Captcha-Value"
)

// CaptchaEnabled 控制登录是否强制校验验证码。
// 开发/无 Redis 等环境可改为 false 跳过验证码校验，避免登录被 400 invalid or missing captcha 阻断。
const CaptchaEnabled = true

// refresh token cookie 相关常量。
// refresh token 以 HttpOnly Cookie 传输，Path 收窄到刷新端点，SameSite=Lax 阻断跨站 POST。
// refresh_exp 为非 HttpOnly 的过期时间戳 cookie，供前端定时器读取以调度主动刷新。
const (
	refreshTokenCookieName = "refresh_token"
	refreshExpCookieName   = "refresh_exp"
	refreshCookiePath      = "/api/v1/auth/refresh"
)

// resolveCookieSecure 按请求的实际传输层判断是否加 Secure 属性：
// TLS 直连或经可信反代（X-Forwarded-Proto: https）→ true；明文 HTTP → false。
// 依据：带 Secure 的 cookie 在明文 HTTP 下会被浏览器拒收，导致 refresh token 无法落地。
// 伪造 X-Forwarded-Proto 只会让 cookie 多加 Secure（存不下而非泄露），失败方向安全。
func resolveCookieSecure(htr *khttp.Transport) bool {
	req := htr.Request()
	if req == nil {
		return true
	}
	if req.TLS != nil {
		return true
	}
	return strings.EqualFold(req.Header.Get("X-Forwarded-Proto"), "https")
}

// setRefreshCookies 将 refresh token 及其过期时间戳写入 Set-Cookie 响应头。
// refresh_token cookie 为 HttpOnly（JS 不可读）+ Path 收窄到刷新端点；
// refresh_exp cookie 非 HttpOnly（前端定时器可读）+ Path=/（任意页面 JS 可读）。
// 两者均 SameSite=Lax（按站点判断，localhost 不同端口属同站，dev 直连后端可落地）；
// Secure 按 TLS 自适应（明文 HTTP 下省略，否则浏览器拒收）。
func setRefreshCookies(ctx context.Context, refreshToken string, refreshExpiresInSeconds int64) {
	tr, ok := ktransport.FromServerContext(ctx)
	if !ok {
		return
	}
	htr, hok := tr.(*khttp.Transport)
	if !hok {
		return
	}
	header := htr.ReplyHeader()
	cookieSecure := resolveCookieSecure(htr)

	// HttpOnly refresh token cookie：Path 收窄到刷新端点（纵深防御，仅刷新请求携带）
	rtCookie := &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    refreshToken,
		Path:     refreshCookiePath,
		MaxAge:   int(refreshExpiresInSeconds),
		Secure:   cookieSecure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	header.Add("Set-Cookie", rtCookie.String())

	// 非 HttpOnly 过期时间戳 cookie（只含 Unix 秒，无敏感信息）。
	// Path 必须为 /：前端 JS（bootstrap 静默恢复/刷新定时器）在任意页面路径读取
	// document.cookie 判断续期窗口——收窄到刷新端点会导致普通页面读不到、刷新即丢会话。
	expCookie := &http.Cookie{
		Name:     refreshExpCookieName,
		Value:    fmt.Sprintf("%d", time.Now().Unix()+refreshExpiresInSeconds),
		Path:     "/",
		MaxAge:   int(refreshExpiresInSeconds),
		Secure:   cookieSecure,
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	}
	header.Add("Set-Cookie", expCookie.String())
}

// clearRefreshCookies 向响应头写入 Max-Age=0 的清除 cookie，使浏览器立即删除 refresh token 相关 cookie。
// Secure 按 TLS 自适应，与 setRefreshCookies 同逻辑，保证属性匹配可删除。
func clearRefreshCookies(ctx context.Context) {
	tr, ok := ktransport.FromServerContext(ctx)
	if !ok {
		return
	}
	htr, hok := tr.(*khttp.Transport)
	if !hok {
		return
	}
	header := htr.ReplyHeader()
	cookieSecure := resolveCookieSecure(htr)

	// 两个 cookie 的 Path 不同（refresh_token 收窄到刷新端点、refresh_exp 为 /），
	// 清除时须各自匹配原 Path，否则浏览器因属性不匹配拒绝删除。
	for name, path := range map[string]string{
		refreshTokenCookieName: refreshCookiePath,
		refreshExpCookieName:   "/",
	} {
		cookie := &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     path,
			MaxAge:   -1,
			Secure:   cookieSecure,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		header.Add("Set-Cookie", cookie.String())
	}
}

// normalizeLoginVerifyError 将登录凭证校验的多种细分错误统一对外成 INVALID_CREDENTIALS，
// 防止攻击者通过区分"用户不存在/账号冻结/密码错误"来枚举有效用户名。
// 真实原因仍保留在服务端日志与审计中间件的 FailureReason 中，不影响可观测性。
//
// 选用 INVALID_CREDENTIALS（401）而非 INVALID_PASSWORD（400）：一是对齐 ANI 契约的
// 401 INVALID_CREDENTIALS，二是"凭据无效"比"密码错误"更准确——用户不存在时并非密码错。
func normalizeLoginVerifyError(err error) error {
	switch {
	case authenticationV1.IsUserNotFound(err),
		authenticationV1.IsUserFreeze(err),
		authenticationV1.IsInvalidPassword(err):
		return authenticationV1.ErrorInvalidCredentials("invalid username or password")
	default:
		return err
	}
}

// stripLocalIdentityPrefix 剥离 ANI 形态用户名可能携带的 "local:" 身份命名空间前缀。
//
// 本仓的凭证表是"多提供商"模型：provider / identity_type 是独立列
// （sys_user_credentials 的 provider、identity_type，见 ent/schema/user_credential.go），
// 裸用户名才是 identifier 的存储形态。ANI 用单列扁平 identifier，只能把提供商信息
// 拼进字符串值里，故其契约描述"服务端自动拼接 local:<username>"。
// 这里只做入参容错（带前缀则剥离、不带也接受），不迁移存储、不引入前缀，
// 否则会把已有列的信息冗余进值里，并使 identifier 的等值索引失效。
func stripLocalIdentityPrefix(username string) string {
	const prefix = "local:"
	if strings.HasPrefix(username, prefix) {
		return strings.TrimPrefix(username, prefix)
	}
	return username
}

// buildTokenPairResponse 把内部登录引擎的 LoginResponse 映射为对外的 TokenPairResponse。
//
// refresh token 不在响应体中返回：本实现经 HttpOnly Cookie 下发（见 setRefreshCookies）。
// 非浏览器客户端不走此端点，而用 AK/SK 换短期机器令牌
// （AccessKeyService.IssueToken），其令牌本就没有 refresh。
//
// MFA 中间态：用户已绑定启用的 TOTP 因子时引擎不签发令牌，改为返回 operation_id。
// 该中间态为 ANI 契约所无，属本仓扩展字段。
func buildTokenPairResponse(resp *authenticationV1.LoginResponse) *authenticationV1.TokenPairResponse {
	if resp == nil {
		return nil
	}

	out := &authenticationV1.TokenPairResponse{
		AccessToken: resp.GetAccessToken(),
		ExpiresIn:   resp.GetExpiresIn(),
		// 签发时间取响应组装时刻。引擎未回传 iat，此处存在毫秒级偏差，
		// 仅作展示用途，不用于任何校验。
		IssuedAt: timestamppb.Now(),
	}

	if opId := resp.GetMfaOperationId(); opId != "" {
		out.MfaRequired = trans.Ptr(true)
		out.MfaOperationId = trans.Ptr(opId)
	}

	return out
}

type AuthenticationService struct {
	adminV1.AuthenticationServiceHTTPServer

	log *bLogger.Helper

	userRepo           data.UserRepo
	userCredentialRepo *data.UserCredentialRepo

	roleRepo                *data.RoleRepo
	tenantRepo              *data.TenantRepo
	membershipRepo          *data.MembershipRepo
	orgUnitRepo             *data.OrgUnitRepo
	roleOrgUnitRepo         *data.RoleOrgUnitRepo
	roleFieldPermissionRepo *data.RoleFieldPermissionRepo
	permissionRepo          *data.PermissionRepo

	authenticator *data.Authenticator
	clientType    authenticationV1.ClientType

	captchaClient *captcha.Captcha
	rateLimiter   *data.LoginRateLimiter

	loginPolicyRepo *data.LoginPolicyRepo

	mfaFactorRepo     *data.UserMfaFactorRepo
	mfaChallengeCache *data.MfaChallengeCache

	vcodeCache              *data.VCodeCache
	notificationChannelRepo *data.NotificationChannelRepo
}

func NewAuthenticationService(
	ctx *bootstrap.Context,
	userRepo data.UserRepo,
	userCredentialRepo *data.UserCredentialRepo,
	roleRepo *data.RoleRepo,
	tenantRepo *data.TenantRepo,
	membershipRepo *data.MembershipRepo,
	orgUnitRepo *data.OrgUnitRepo,
	roleOrgUnitRepo *data.RoleOrgUnitRepo,
	roleFieldPermissionRepo *data.RoleFieldPermissionRepo,
	permissionRepo *data.PermissionRepo,
	authenticator *data.Authenticator,
	clientType authenticationV1.ClientType,
	captchaClient *captcha.Captcha,
	rateLimiter *data.LoginRateLimiter,
	loginPolicyRepo *data.LoginPolicyRepo,
	mfaFactorRepo *data.UserMfaFactorRepo,
	mfaChallengeCache *data.MfaChallengeCache,
	vcodeCache *data.VCodeCache,
	notificationChannelRepo *data.NotificationChannelRepo,
) *AuthenticationService {
	return &AuthenticationService{
		log:                     ctx.NewLoggerHelper("authn/service/admin-service"),
		userRepo:                userRepo,
		userCredentialRepo:      userCredentialRepo,
		tenantRepo:              tenantRepo,
		roleRepo:                roleRepo,
		membershipRepo:          membershipRepo,
		orgUnitRepo:             orgUnitRepo,
		roleOrgUnitRepo:         roleOrgUnitRepo,
		roleFieldPermissionRepo: roleFieldPermissionRepo,
		permissionRepo:          permissionRepo,
		authenticator:           authenticator,
		clientType:              clientType,
		captchaClient:           captchaClient,
		rateLimiter:             rateLimiter,
		loginPolicyRepo:         loginPolicyRepo,
		mfaFactorRepo:           mfaFactorRepo,
		mfaChallengeCache:       mfaChallengeCache,
		vcodeCache:              vcodeCache,
		notificationChannelRepo: notificationChannelRepo,
	}
}

// checkLoginPolicies 拉取租户登录策略并按当前上下文匹配。
// userId 传 0 时只匹配全局条目（target_id 为空）；密码校验前与取到 user 后各调用一次。
// 匹配逻辑见 data.MatchLoginPolicy（纯函数，含单测）。
// 策略查询失败时 fail-open（仅告警）——登录可用性优先于策略拦截，与验证码开关的容错取向一致。
func (s *AuthenticationService) checkLoginPolicies(ctx context.Context, tenantID, userId uint32, clientIP, deviceId string) (bool, string) {
	policies, err := s.loginPolicyRepo.ListForLogin(ctx, tenantID)
	if err != nil {
		s.log.Errorf(ctx, "list login policies failed for tenant [%d]: %s", tenantID, err.Error())
		return false, ""
	}
	return data.MatchLoginPolicy(policies, userId, clientIP, deviceId, time.Now())
}

func (s *AuthenticationService) resetContextForLogin(ctx context.Context) context.Context {
	// 登录前没有任何已认证身份，但凭证/用户查询必须绕过租户隔离：
	// go-crud TenantPrivacy.EvalQuery 对"非平台/非系统"的 viewer 一律注入
	// WHERE tenant_id = viewer.TenantID()，而 NoopContext 的 TenantID 恒为 0
	// 且 IsPlatformContext/IsSystemContext 均为 false——privacy.Allow 决策
	// 无法阻止该谓词注入（注入发生在规则内部，不受决策短路影响），
	// 租户用户（tenant_id>0）的凭证行因此永远查不到（USER_NOT_FOUND）。
	// 这里改用系统视图：TenantPrivacy 对系统上下文直接放行、不注入谓词，
	// 登录期的租户限定由显式的 tenantID 查询参数自行完成（见 doGrantTypePassword）。
	ctx = appViewer.NewSystemViewerContext(ctx)
	// 绕过隐私保护中间件
	ctx = privacy.DecisionContext(ctx, privacy.Allow)

	return ctx
}

// Login 登录（内部授权类型分发入口）。
//
// 注意：本方法已不是对外 HTTP 端点——对外入口是 PasswordLogin / PlatformPasswordLogin
// （POST /api/v1/auth/password/login 与 /api/v1/auth/platform/password/login）。
// 保留它是因为：（1）grant_type 分发仍是引擎的合法内部语义；
// （2）doGrantTypePassword 是全部登录能力的唯一实现，适配层经由它复用；
// （3）既有测试以它为引擎入口，删改会造成大面积无收益的测试改写。
func (s *AuthenticationService) Login(ctx context.Context, req *authenticationV1.LoginRequest) (*authenticationV1.LoginResponse, error) {
	switch req.GetGrantType() {
	case authenticationV1.GrantType_password:
		return s.doGrantTypePassword(ctx, req)

	case authenticationV1.GrantType_refresh_token:
		// refresh token 刷新已迁移到 /api/v1/auth/refresh 端点（HttpOnly Cookie 传输），
		// 本入口不再处理 refresh_token grant type。
		return nil, authenticationV1.ErrorInvalidGrantType("use /api/v1/auth/refresh for token refresh")

	case authenticationV1.GrantType_client_credentials:
		return s.doGrantTypeClientCredentials(ctx, req)

	default:
		return nil, authenticationV1.ErrorInvalidGrantType("invalid grant type")
	}
}

// PasswordLogin 租户账密登录（ANI 契约形态，POST /api/v1/auth/password/login）。
//
// 本方法只做报文翻译，登录能力完全复用 doGrantTypePassword，不重复实现：
// 强制验证码、IP+用户名限流、登录策略闸门、identifier 反查、bcrypt 校验、
// MFA 闸门、会话元数据、权限聚合、令牌签发与 refresh Cookie 下发均沿用原实现。
//
// 租户标识：tenant_name 的值取 sys_tenants.code（"租户编号"）。这里不做预解析，
// 直接透传给引擎——引擎内的租户解析已具备"查不到/非启用即拒绝"的语义，
// 预解析只会多一次查询而不增加保障。
func (s *AuthenticationService) PasswordLogin(ctx context.Context, req *authenticationV1.PasswordLoginRequest) (*authenticationV1.TokenPairResponse, error) {
	if req == nil {
		return nil, authenticationV1.ErrorBadRequest("invalid parameter")
	}

	// 显式闸门：tenant_name 为空必须拒绝。
	// 引擎对"tenant_code 留空"的既有语义是解析为平台租户（tenantID=0），
	// 若不在入口拦住，租户登录漏传租户名会静默降级为平台登录——
	// 这正是把租户/平台拆成两个端点的理由，不能只靠 proto validate 单点保障。
	tenantName := strings.TrimSpace(req.GetTenantName())
	if tenantName == "" {
		return nil, authenticationV1.ErrorBadRequest("tenant_name is required")
	}

	inner := &authenticationV1.LoginRequest{
		GrantType:  authenticationV1.GrantType_password,
		ClientType: authenticationV1.ClientType_admin.Enum(),
		TenantCode: trans.Ptr(tenantName),
		Password:   trans.Ptr(req.GetPassword()),
	}
	// identifier 是 proto3 oneof，Go 侧必须经包装类型赋值，不能直接写 inner.Username
	inner.Identifier = &authenticationV1.LoginRequest_Username{
		Username: stripLocalIdentityPrefix(req.GetUsername()),
	}

	resp, err := s.doGrantTypePassword(ctx, inner)
	if err != nil {
		return nil, err
	}

	return buildTokenPairResponse(resp), nil
}

// PlatformPasswordLogin 平台账密登录（ANI 契约形态，POST /api/v1/auth/platform/password/login）。
//
// 与租户登录拆为独立端点而非复用 tenant_name 留空，是为消除静默降级：
// 单端点下"漏传租户"会被当成平台登录，无法区分"忘记传参"与"有意平台登录"；
// 独立端点同时为平台登录单独施加风控/审计策略留出位置。
//
// 引擎语义：tenant_code 留空即 tenantID=0（平台作用域），这是 doGrantTypePassword 既有行为，
// 故此处零新增逻辑。平台身份的准入校验在授权阶段完成
// （见 authorizeAndEnrichUserTokenPayload 的平台角色闸门）。
func (s *AuthenticationService) PlatformPasswordLogin(ctx context.Context, req *authenticationV1.PlatformPasswordLoginRequest) (*authenticationV1.TokenPairResponse, error) {
	if req == nil {
		return nil, authenticationV1.ErrorBadRequest("invalid parameter")
	}

	inner := &authenticationV1.LoginRequest{
		GrantType:  authenticationV1.GrantType_password,
		ClientType: authenticationV1.ClientType_admin.Enum(),
		Password:   trans.Ptr(req.GetPassword()),
	}
	// 不设置 TenantCode：引擎据"留空"解析为平台租户（tenantID=0）
	inner.Identifier = &authenticationV1.LoginRequest_Username{
		Username: stripLocalIdentityPrefix(req.GetUsername()),
	}

	resp, err := s.doGrantTypePassword(ctx, inner)
	if err != nil {
		return nil, err
	}

	return buildTokenPairResponse(resp), nil
}

// containsPermission 检查权限代码列表中是否包含指定权限代码
func containsPermission(perms []string, target string) bool {
	for _, p := range perms {
		if p == target {
			return true
		}
	}
	return false
}

// authorizeAndEnrichUserTokenPayloadUserTenantRelationOneToOne 一对一用户-租户关系的授权与丰富
func (s *AuthenticationService) authorizeAndEnrichUserTokenPayloadUserTenantRelationOneToOne(ctx context.Context, userID, tenantID uint32, tokenPayload *authenticationV1.UserTokenPayload) error {
	hasBackendAccess := false

	if tenantID > 0 {
		// 检查租户状态
		tenant, _ := s.tenantRepo.Get(ctx, &identityV1.GetTenantRequest{
			QueryBy: &identityV1.GetTenantRequest_Id{Id: tenantID},
		})
		if tenant == nil || tenant.GetStatus() != identityV1.Tenant_ON {
			return authenticationV1.ErrorForbidden("insufficient authority")
		}
	}

	// 获取角色 ID 列表
	roleIDs, err := s.userRepo.ListRoleIDsByUserID(ctx, userID)
	if err != nil || len(roleIDs) == 0 {
		s.log.Errorf(ctx, "get roles by user [%d] failed [%v]", userID, err)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}

	// 获取权限 ID 列表
	permissionIDs, err := s.roleRepo.ListPermissionIDsByRoleIDs(ctx, roleIDs)
	if err != nil || len(permissionIDs) == 0 {
		s.log.Errorf(ctx, "get permissions by role ids failed [%v]", err)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}

	// 获取权限代码列表
	permissionCodes, err := s.permissionRepo.GetPermissionCodesByIDs(ctx, permissionIDs)
	if err != nil || len(permissionCodes) == 0 {
		s.log.Errorf(ctx, "get permission codes by ids failed [%v]", err)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}

	// 检查是否包含系统访问后台权限
	if containsPermission(permissionCodes, constants.SystemAccessBackendPermissionCode) {
		hasBackendAccess = true
	}

	// 授权决策
	if !hasBackendAccess {
		s.log.Errorf(ctx, "user [%d] has no backend access permission", userID)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}

	// 获取角色代码列表
	roleCodes, err := s.roleRepo.ListRoleCodesByRoleIds(ctx, roleIDs)
	if err != nil || len(roleCodes) == 0 {
		s.log.Errorf(ctx, "list role codes by role ids failed [%v]", err)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}
	tokenPayload.Roles = roleCodes
	// 权限码随令牌下发：服务层的"跨租户能力"判定无法只靠 API 级 authz 表达，
	// 需按能力区分（见 UserService / MfaService 的 reset_others_* 检查）。
	tokenPayload.Permissions = permissionCodes
	fillAdminFlags(tokenPayload, permissionCodes)

	// 平台登录闸门：tenantID == 0 表示该凭证处于平台作用域，必须是平台角色才放行。
	// 判据用角色码前缀而非具体 platform:admin —— 多平台角色（平台运维、平台只读）
	// 都应以平台身份登录，能力差异由权限矩阵决定，不靠登录入口区分。
	// 放在此处而非函数开头：角色码要到这一步才取到。
	if tenantID == 0 && !hasPlatformRole(roleCodes) {
		s.log.Warnf(ctx, "platform login rejected for user [%d]: no platform role, roles=%v", userID, roleCodes)
		return authenticationV1.ErrorForbidden("not a platform account")
	}

	// 聚合角色级数据范围配置进令牌（dss/dsu 轨道；语义见 aggregateDataScopes）。
	s.aggregateDataScopes(ctx, tokenPayload.GetTenantId(), userID, roleIDs, tokenPayload)
	// 聚合角色级字段权限配置进令牌（hfs 轨道；语义见 aggregateHiddenFields）。
	s.aggregateHiddenFields(ctx, tokenPayload.GetTenantId(), roleIDs, tokenPayload)

	return nil
}

// authorizeAndEnrichUserTokenPayloadUserTenantRelationOneToMany 一对多用户-租户关系的授权与丰富
func (s *AuthenticationService) authorizeAndEnrichUserTokenPayloadUserTenantRelationOneToMany(ctx context.Context, userID, tenantID uint32, tokenPayload *authenticationV1.UserTokenPayload) error {
	var memberships []*identityV1.Membership
	if tenantID > 0 {
		// 指定租户
		membership, err := s.membershipRepo.GetMembershipByUserTenant(ctx, userID, tenantID)
		if err != nil {
			s.log.Errorf(ctx, "get user [%d] membership for tenant [%d] failed [%s]", userID, tenantID, err.Error())
			return authenticationV1.ErrorForbidden("insufficient authority")
		}
		memberships = []*identityV1.Membership{membership}
	} else {
		var err error
		// 获取所有活跃成员身份
		memberships, err = s.membershipRepo.GetUserActiveMemberships(ctx, userID)
		if err != nil || len(memberships) == 0 {
			s.log.Errorf(ctx, "list user [%d] active memberships failed [%v]", userID, err)
			return authenticationV1.ErrorForbidden("insufficient authority")
		}
	}

	hasBackendAccess := false
	var validRoleIDs []uint32
	// 权限码并集：与 validRoleIDs 同步累积，只收"具备后台访问权限"的成员身份，
	// 避免把无后台访问权身份的权限也带进令牌（最小权限）。
	var allPermissionCodes []string
	for _, m := range memberships {
		if m.GetTenantId() > 0 {
			// 检查租户状态
			tenant, _ := s.tenantRepo.Get(ctx, &identityV1.GetTenantRequest{
				QueryBy: &identityV1.GetTenantRequest_Id{Id: m.GetTenantId()},
			})
			if tenant == nil || tenant.GetStatus() != identityV1.Tenant_ON {
				continue
			}
		}

		// 获取角色 ID 列表
		roleIDs, err := s.membershipRepo.GetRoleIDsByMembership(ctx, m.GetId())
		if err != nil || len(roleIDs) == 0 {
			s.log.Errorf(ctx, "get roles by membership [%d] failed [%v]", m.GetId(), err)
			continue
		}

		// 获取权限 ID 列表
		permissionIDs, err := s.roleRepo.ListPermissionIDsByRoleIDs(ctx, roleIDs)
		if err != nil || len(permissionIDs) == 0 {
			s.log.Errorf(ctx, "get permissions by role ids failed [%v]", err)
			continue
		}

		// 获取权限代码列表
		permissionCodes, _ := s.permissionRepo.GetPermissionCodesByIDs(ctx, permissionIDs)

		s.log.Infof(ctx, "user [%d] membership [%d] permission codes: %v", userID, m.GetId(), permissionCodes)

		// 检查是否包含系统访问后台权限
		if containsPermission(permissionCodes, constants.SystemAccessBackendPermissionCode) {
			hasBackendAccess = true
			validRoleIDs = append(validRoleIDs, roleIDs...)
			allPermissionCodes = append(allPermissionCodes, permissionCodes...)
		}
	}

	// 授权决策
	if !hasBackendAccess {
		s.log.Errorf(ctx, "user [%d] has no backend access permission", userID)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}

	// 获取角色代码列表
	roleCodes, err := s.roleRepo.ListRoleCodesByRoleIds(ctx, validRoleIDs)
	if err != nil || len(roleCodes) == 0 {
		s.log.Errorf(ctx, "list role codes by role ids failed [%v]", err)
		return authenticationV1.ErrorForbidden("insufficient authority")
	}
	tokenPayload.Roles = roleCodes
	// 一对多模式下 tenantID == 0 表示"该用户的全部活跃成员身份"，属于跨租户聚合，
	// 不是平台登录，因此这里不设平台角色闸门（闸门只作用于一对一的平台登录路径）。
	tokenPayload.Permissions = dedupeStrings(allPermissionCodes)
	fillAdminFlags(tokenPayload, allPermissionCodes)

	// 聚合角色级数据范围配置进令牌（dss/dsu 轨道；语义见 aggregateDataScopes）。
	s.aggregateDataScopes(ctx, tokenPayload.GetTenantId(), userID, validRoleIDs, tokenPayload)
	// 聚合角色级字段权限配置进令牌（hfs 轨道；语义见 aggregateHiddenFields）。
	s.aggregateHiddenFields(ctx, tokenPayload.GetTenantId(), validRoleIDs, tokenPayload)

	return nil
}

// authorizeAndEnrichUserTokenPayload 授权并丰富用户令牌载荷
func (s *AuthenticationService) authorizeAndEnrichUserTokenPayload(ctx context.Context, userID, tenantID uint32, tokenPayload *authenticationV1.UserTokenPayload) error {
	switch constants.DefaultUserTenantRelationType {
	case constants.UserTenantRelationOneToOne:
		return s.authorizeAndEnrichUserTokenPayloadUserTenantRelationOneToOne(ctx, userID, tenantID, tokenPayload)

	case constants.UserTenantRelationOneToMany:
		return s.authorizeAndEnrichUserTokenPayloadUserTenantRelationOneToMany(ctx, userID, tenantID, tokenPayload)

	default:
		s.log.Errorf(ctx, "unsupported user-tenant relation type: %d", constants.DefaultUserTenantRelationType)
		return authenticationV1.ErrorServiceUnavailable("unsupported user-tenant relation type")
	}
}

// resolveUserAuthority 解析用户权限信息
func (s *AuthenticationService) resolveUserAuthority(ctx context.Context, user *identityV1.User, tokenPayload *authenticationV1.UserTokenPayload) error {
	if user.GetStatus() != identityV1.User_NORMAL {
		s.log.Errorf(ctx, "user [%d] is [%v]", user.GetId(), user.GetStatus())
		return authenticationV1.ErrorForbidden("user is disabled")
	}

	if err := s.authorizeAndEnrichUserTokenPayload(ctx, user.GetId(), user.GetTenantId(), tokenPayload); err != nil {
		return err
	}

	return nil
}

// doGrantTypePassword 处理授权类型 - 密码
func (s *AuthenticationService) doGrantTypePassword(ctx context.Context, req *authenticationV1.LoginRequest) (*authenticationV1.LoginResponse, error) {
	ctx = s.resetContextForLogin(ctx)

	// 取客户端 IP（供限流维度使用），失败不阻断登录
	clientIP := netutil.ClientIPFromContext(ctx)
	// 剥离 CR/LF，防止含换行的用户名注入文本日志行（伪造条目/行内注入）。
	username := strings.NewReplacer("\r", "", "\n", "").Replace(req.GetUsername())

	// ===== H5 闸门 1：登录限流预检（按 IP + 用户名双维度）=====
	if s.rateLimiter != nil {
		if locked, lerr := s.rateLimiter.IsLocked(ctx, clientIP, username); lerr != nil {
			s.log.Errorf(ctx, "login rate limiter pre-check failed: %s", lerr.Error())
		} else if locked {
			s.log.Warnf(ctx, "login blocked by rate limiter: ip=%s, username=%s", clientIP, username)
			return nil, authenticationV1.ErrorBadRequest("too many login failures, please try again later")
		}
	}

	// ===== H5 闸门 2：强制验证码（始终启用；通过 HTTP Header 传递，避免改动 proto/前端生成代码）=====
	if !s.verifyLoginCaptcha(ctx) {
		return nil, authenticationV1.ErrorBadRequest("invalid or missing captcha")
	}

	// ===== 租户解析：tenant_code 留空视为平台（tenant 0），非空则按编号定位租户 =====
	// 解析后的 tenantID 限定后续凭证查询范围，消除同名 identifier 跨租户歧义。
	var tenantID uint32 = 0
	if code := req.GetTenantCode(); strings.TrimSpace(code) != "" {
		tenant, _ := s.tenantRepo.Get(ctx, &identityV1.GetTenantRequest{
			QueryBy: &identityV1.GetTenantRequest_Code{Code: code},
		})
		// 查不到、或租户非启用状态，统一并入登录失败文案与错误码。
		// 不用 404 TENANT_NOT_FOUND：区分"租户不存在"会让攻击者据此枚举有效租户编号，
		// 与 normalizeLoginVerifyError 的用户名防枚举取向必须一致。
		if tenant == nil || tenant.GetStatus() != identityV1.Tenant_ON {
			s.log.Warnf(ctx, "login tenant resolve failed for tenant code [%s]", code)
			return nil, authenticationV1.ErrorInvalidCredentials("invalid username or password")
		}
		tenantID = tenant.GetId()
	}

	// ===== 登录策略闸门（全局部分）：target_id 为空的策略不依赖用户身份， =====
	// ===== 在 identifier 反查与密码校验之前拦截，被封锁的 IP 连 user 表查询都省掉。
	// ===== 用户定向策略（target_id = userId）在取到 user 后二次检查。
	if s.loginPolicyRepo != nil {
		if blocked, reason := s.checkLoginPolicies(ctx, tenantID, 0, clientIP, req.GetDeviceId()); blocked {
			s.log.Warnf(ctx, "login blocked by policy: ip=%s username=%s reason=%s", clientIP, username, reason)
			return nil, authenticationV1.ErrorForbidden("login blocked by security policy")
		}
	}

	// ===== identifier 智能解析：输入含 @ 视为 email、纯数字视为 mobile， =====
	// ===== 经 user 表反查得到真实 username 后仍走 USERNAME 维度凭证校验。
	// 未命中时原样返回，交由凭证校验走统一失败路径（防枚举）；mobile 多行歧义直接拒绝。
	if resolved, _, rerr := s.userRepo.FindUsernameByIdentifier(ctx, tenantID, username); rerr != nil {
		return nil, rerr
	} else if resolved != username {
		username = resolved
	}

	// ===== 凭证校验：在解析出的 tenant 范围内查单条凭证并校验密码 =====
	// 注意用解析后的 username 局部变量（可能是 email/mobile 反查所得），而非 req 原始输入
	var matchedUserID uint32
	var err error
	matchedUserID, err = s.userCredentialRepo.FindUserCredential(ctx, tenantID, authenticationV1.UserCredential_USERNAME, username, req.GetPassword(), true)
	if err != nil {
		// 服务端日志保留真实原因（USER_NOT_FOUND / USER_FREEZE / INVALID_PASSWORD），便于运维排查
		s.log.Errorf(ctx, "verify user credential failed for username [%s]: %s", username, err.Error())

		// H5：登录失败时自增失败计数（按 IP + 用户名双维度）
		if s.rateLimiter != nil {
			if _, _, _, cerr := s.rateLimiter.CheckAndIncr(ctx, clientIP, username); cerr != nil {
				s.log.Errorf(ctx, "login rate limiter incr failed: %s", cerr.Error())
			}
		}

		// 对客户端统一返回同一文案，避免通过 HTTP 状态码/reason 枚举有效用户名（H11）
		return nil, normalizeLoginVerifyError(err)
	}

	// 获取用户信息（按凭证归属的 user_id 精确查找，避免同 identifier 多租户歧义）
	var user *identityV1.User
	user, err = s.userRepo.Get(ctx, &identityV1.GetUserRequest{QueryBy: &identityV1.GetUserRequest_Id{Id: matchedUserID}})
	if err != nil {
		s.log.Errorf(ctx, "get user by id [%d] failed [%s]", matchedUserID, err.Error())
		return nil, err
	}

	// 纵深防御：凭证行的 tenant 必须与用户行的 tenant 一致，否则拒绝登录
	if user.GetTenantId() != tenantID {
		s.log.Errorf(ctx, "tenant mismatch for user [%d]: credential tenant [%d] vs user tenant [%d]",
			matchedUserID, tenantID, user.GetTenantId())
		// 同样并入登录失败文案：凭证归属租户与用户归属租户不一致属异常数据，
		// 对外不暴露"哪一侧不匹配"，避免成为租户探测信号。
		return nil, authenticationV1.ErrorInvalidCredentials("invalid username or password")
	}

	// ===== 登录策略闸门（用户定向部分）：密码已通过、userId 已知， =====
	// ===== 检查 target_id 约束到该用户的策略条目。
	if s.loginPolicyRepo != nil {
		if blocked, reason := s.checkLoginPolicies(ctx, tenantID, user.GetId(), clientIP, req.GetDeviceId()); blocked {
			s.log.Warnf(ctx, "login blocked by user-targeted policy: uid=%d ip=%s reason=%s", user.GetId(), clientIP, reason)
			return nil, authenticationV1.ErrorForbidden("login blocked by security policy")
		}
	}

	tokenPayload := &authenticationV1.UserTokenPayload{
		UserId:   user.GetId(),
		TenantId: user.TenantId,
		Username: user.Username,
		ClientId: req.ClientId,
		DeviceId: req.DeviceId,
	}

	// 解析用户权限信息
	err = s.resolveUserAuthority(ctx, user, tokenPayload)
	if err != nil {
		s.log.Errorf(ctx, "resolve user [%d] authority failed [%s]", user.GetId(), err.Error())
		return nil, err
	}

	// ===== MFA 闸门：若该用户绑定了 ENABLED 的 TOTP 因子，则不签发 token， =====
	// ===== 改为签发 operation_id，要求前端走二次验证（MfaService.VerifyMFAChallenge）。
	// 注意：此处不清零限流计数——认证尚未完成；真正清零在 VerifyMFAChallenge 通过后。
	if s.mfaFactorRepo != nil {
		needMfa, merr := s.mfaFactorRepo.HasEnabledTotp(ctx, user.GetTenantId(), user.GetId())
		if merr != nil {
			// fail-closed：MFA 状态查询失败时拒绝登录，避免已绑定用户在 DB 故障时
			// 被降级为单因子放行（与上方凭证校验出错即拒登的行为一致）。
			s.log.Errorf(ctx, "check mfa factor failed for user [%d]: %s", user.GetId(), merr.Error())
			return nil, authenticationV1.ErrorInternalServerError("mfa check failed")
		}
		if needMfa && s.mfaChallengeCache != nil {
			opId, cerr := s.mfaChallengeCache.SetLoginChallenge(ctx, tokenPayload, req.GetClientType())
			if cerr != nil {
				s.log.Errorf(ctx, "set mfa login challenge failed for user [%d]: %s", user.GetId(), cerr.Error())
				return nil, authenticationV1.ErrorInternalServerError("mfa challenge failed")
			}
			return &authenticationV1.LoginResponse{
				TokenType:      authenticationV1.TokenType_bearer,
				AccessToken:    "",
				MfaOperationId: trans.Ptr(opId),
			}, nil
		}
	}

	// 生成令牌
	accessToken, refreshToken, err := s.authenticator.CreateUserToken(ctx, req.GetClientType(), tokenPayload)
	if err != nil {
		return nil, err
	}

	// 记录会话元数据（在线会话列表展示用）；失败不阻断登录
	recordSessionMeta(ctx, s.log, s.authenticator, req.GetClientType(), tokenPayload)

	// 记录最后登录时间与 IP（用户列表/个人中心展示用）；失败不阻断登录
	recordUserLastLogin(ctx, s.log, s.userRepo, user.GetId(), clientIP)

	// H5：登录成功后清零失败计数
	if s.rateLimiter != nil {
		s.rateLimiter.Reset(ctx, clientIP, username)
	}

	// refresh token 通过 HttpOnly Cookie 下发，不再放入响应体
	refreshExpiresIn := int64(s.authenticator.GetRefreshTokenExpires(req.GetClientType()).Seconds())
	setRefreshCookies(ctx, refreshToken, refreshExpiresIn)

	return &authenticationV1.LoginResponse{
		TokenType:   authenticationV1.TokenType_bearer,
		AccessToken: accessToken,
		ExpiresIn:   int64(s.authenticator.GetAccessTokenExpires(req.GetClientType()).Seconds()),
	}, nil
}

// recordUserLastLogin 记录用户最后登录时间与 IP。
// 仅用于展示（用户列表"最后登录"列、个人中心），更新失败不阻断登录流程。
// 密码登录与 MFA 挑战通过两条成功路径都调用。
func recordUserLastLogin(ctx context.Context, log *bLogger.Helper, userRepo data.UserRepo, userID uint32, clientIP string) {
	if userID == 0 || userRepo == nil {
		return
	}
	mask, err := fieldmaskpb.New(&identityV1.User{}, "last_login_at", "last_login_ip")
	if err != nil {
		log.Errorf(ctx, "build last-login fieldmask failed: %s", err.Error())
		return
	}
	uerr := userRepo.Update(ctx, &identityV1.UpdateUserRequest{
		Id: userID,
		Data: &identityV1.User{
			LastLoginAt: timeutil.TimeToTimestamppb(trans.Ptr(time.Now())),
			LastLoginIp: trans.Ptr(clientIP),
		},
		UpdateMask: mask,
	})
	if uerr != nil {
		log.Errorf(ctx, "record last login for user [%d] failed: %s", userID, uerr.Error())
	}
}

// fillAdminFlags 按权限码填充 token 的平台/租户管理员标志。
// 该标志此前从未被赋值（全仓库仅消费无生产），导致下游
// GetIsPlatformAdmin 判定（MFA 重置、用户管理越权校验等）形同虚设。
//
// 判据由"角色码 == platform:admin"改为"持有 sys:platform_admin 权限"：
// 角色码是身份标识，权限才是能力。多平台角色（平台运维、平台只读）下，
// 布尔标志无法表达能力差异，而权限矩阵可以；默认 platform:admin 角色
// 已绑定该权限，故改动对既有部署行为一致。
//
// 注意：该标志仅用于展示与兼容，跨租户授权已改为按 reset_others_* 能力权限判定
// （见 UserService / MfaService），不再依赖此布尔值。
func fillAdminFlags(tokenPayload *authenticationV1.UserTokenPayload, permissionCodes []string) {
	if containsPermission(permissionCodes, constants.SystemPlatformAdminPermissionCode) {
		tokenPayload.IsPlatformAdmin = trans.Ptr(true)
	}
	if containsPermission(permissionCodes, constants.SystemTenantManagerPermissionCode) {
		tokenPayload.IsTenantAdmin = trans.Ptr(true)
	}
}

// hasPlatformRole 判断角色码列表中是否存在平台角色（platform: 前缀）。
//
// 用于平台登录闸门：区分"是不是平台身份"用角色码前缀，"能做什么"用权限矩阵。
// 二者分层，避免为每类平台角色新增角色码判断分支。
// 本函数接上了此前全仓无生产调用的 constants.IsPlatformRoleCode。
func hasPlatformRole(roleCodes []string) bool {
	for _, rc := range roleCodes {
		if constants.IsPlatformRoleCode(rc) {
			return true
		}
	}
	return false
}

// hasPermission 判断令牌载荷中是否持有指定权限码。
// 服务层的跨租户能力判定统一走此入口，不直接读 IsPlatformAdmin 布尔。
func hasPermission(operator *authenticationV1.UserTokenPayload, permissionCode string) bool {
	if operator == nil {
		return false
	}
	return containsPermission(operator.GetPermissions(), permissionCode)
}

// dedupeStrings 去重并保持首次出现顺序。
// 一对多模式下多个成员身份的权限码会重复，去重避免 JWT 体积无谓膨胀。
func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// verifyLoginCaptcha 校验登录请求携带的验证码。
// 验证码 id/value 通过 HTTP Header（X-Captcha-Id / X-Captcha-Value）传递。
// captchaClient.Verify 已是 verify-and-delete 单次有效语义。
// 注意：refresh_token / client_credentials 等非密码授权不走此校验（仅 doGrantTypePassword 调用）。
func (s *AuthenticationService) verifyLoginCaptcha(ctx context.Context) bool {
	if !CaptchaEnabled {
		// 验证码开关关闭，跳过校验
		return true
	}
	if s.captchaClient == nil {
		// captcha 未配置时 fail-open（仅记录告警），避免影响登录基本功能
		return true
	}
	header := netutil.HeaderFromContext(ctx)
	if header == nil {
		return false
	}
	captchaID := strings.TrimSpace(header.Get(headerCaptchaID))
	captchaValue := strings.TrimSpace(header.Get(headerCaptchaValue))
	if captchaID == "" || captchaValue == "" {
		return false
	}
	ok, err := s.captchaClient.Verify(ctx, captchaID, captchaValue)
	if err != nil {
		s.log.Errorf(ctx, "verify captcha failed: %s", err.Error())
		return false
	}
	return ok
}

// doGrantTypeRefreshToken 处理授权类型 - 刷新令牌
func (s *AuthenticationService) doGrantTypeRefreshToken(ctx context.Context, req *authenticationV1.LoginRequest, refreshToken string) (*authenticationV1.LoginResponse, error) {
	// refresh token 为自描述 JWT，VerifyRefreshToken 验签并原子吊销旧令牌对，返回 uid/jti。
	// 不再依赖 access token 提供的身份信息。旧 jti 用于继承会话元数据的登录时间。
	userId, oldJti, err := s.authenticator.VerifyRefreshToken(ctx, req.GetClientType(), refreshToken)
	if err != nil {
		s.log.Errorf(ctx, "verify refresh token failed: [%s]", err)
		return nil, authenticationV1.ErrorIncorrectRefreshToken("invalid refresh token")
	}

	// 本端点在鉴权白名单内（refresh token 独立鉴权），ctx 无 auth 中间件注入的
	// ViewerContext，而下方 userRepo.Get 等查询走 ent privacy（缺 viewer 直接 500
	// "missing ViewerContext"）。uid 来自已验签的自描述 JWT 且按主键精确查询，
	// 注入系统级 viewer 查询不会越权。
	ctx = appViewer.NewSystemViewerContext(ctx)

	// 获取用户信息
	user, err := s.userRepo.Get(ctx, &identityV1.GetUserRequest{
		QueryBy: &identityV1.GetUserRequest_Id{
			Id: userId,
		},
	})
	if err != nil {
		return nil, err
	}

	tokenPayload := &authenticationV1.UserTokenPayload{
		UserId:   user.GetId(),
		TenantId: user.TenantId,
		Username: user.Username,
		ClientId: req.ClientId,
		DeviceId: req.DeviceId,
	}

	// 解析用户权限信息
	err = s.resolveUserAuthority(ctx, user, tokenPayload)
	if err != nil {
		s.log.Errorf(ctx, "resolve user [%d] authority failed [%s]", user.GetId(), err.Error())
		return nil, err
	}

	// 生成令牌
	accessToken, newRefreshToken, err := s.authenticator.CreateUserToken(ctx, req.GetClientType(), tokenPayload)
	if err != nil {
		return nil, err
	}

	// 记录新令牌对的会话元数据：刷新轮换是同一客户端续期而非重新登录，
	// 登录时间继承旧会话（旧元数据已随旧令牌对在 Lua 脚本中原子删除）。
	oldMeta, gerr := s.authenticator.GetSessionMeta(ctx, req.GetClientType(), userId, oldJti)
	if gerr != nil {
		s.log.Errorf(ctx, "get old session meta failed for user [%d]: %v", userId, gerr)
	}
	var loginAt int64
	if oldMeta != nil {
		loginAt = oldMeta.LoginAt
	}
	recordSessionMetaAt(ctx, s.log, s.authenticator, req.GetClientType(), tokenPayload, loginAt)

	// refresh token 通过 HttpOnly Cookie 下发，不再放入响应体
	refreshExpiresIn := int64(s.authenticator.GetRefreshTokenExpires(req.GetClientType()).Seconds())
	setRefreshCookies(ctx, newRefreshToken, refreshExpiresIn)

	return &authenticationV1.LoginResponse{
		TokenType:   authenticationV1.TokenType_bearer,
		AccessToken: accessToken,
		ExpiresIn:   int64(s.authenticator.GetAccessTokenExpires(req.GetClientType()).Seconds()),
	}, nil
}

// doGrantTypeClientCredentials 处理授权类型 - 客户端凭据
func (s *AuthenticationService) doGrantTypeClientCredentials(_ context.Context, _ *authenticationV1.LoginRequest) (*authenticationV1.LoginResponse, error) {
	return nil, authenticationV1.ErrorInvalidGrantType("invalid grant type")
}

// RevokeJti 登出（POST /api/v1/auth/logout）。
//
// 吊销范围是该用户的全部会话，而非请求上报的单个 jti：
// 全域吊销比单令牌吊销更彻底（浏览器登出后其他设备/标签页的令牌一并失效），
// 且与既有 authenticator.RevokeUserToken 的语义一致，故不改为按 jti 吊销。
// jti 仅用于审计留痕——调用方从当前 AccessToken 的 claims 读取后上报。
func (s *AuthenticationService) RevokeJti(ctx context.Context, req *authenticationV1.RevokeJtiRequest) (*authenticationV1.RevokeStatusResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	if jti := req.GetJti(); jti != "" {
		s.log.Infof(ctx, "logout: user [%d] reported jti [%s]", operator.GetUserId(), jti)
	}

	if err = s.authenticator.RevokeUserToken(ctx, s.clientType, operator.GetUserId()); err != nil {
		return nil, err
	}

	// 清除 refresh token 相关 cookie
	clearRefreshCookies(ctx)

	return &authenticationV1.RevokeStatusResponse{Status: "revoked"}, nil
}

// RefreshAccessToken 刷新认证令牌（POST /api/v1/auth/refresh）。
// refresh token 以 HttpOnly Cookie 传输，本端点已加入白名单（无需 access token）。
// refresh token 为自描述 JWT，VerifyRefreshToken 从中解析 uid/jti 完成独立鉴权。
//
// 请求体的 client_id / device_id 为可选：用于让新令牌对延续审计留痕与会话元数据。
// 二者均不参与鉴权判定，缺失只影响审计与会话列表的信息完整度。
func (s *AuthenticationService) RefreshAccessToken(ctx context.Context, req *authenticationV1.RefreshAccessTokenRequest) (*authenticationV1.TokenPairResponse, error) {
	if req == nil {
		req = &authenticationV1.RefreshAccessTokenRequest{}
	}

	// refresh token 从 HttpOnly Cookie 读取，不从请求体获取
	refreshToken := netutil.CookieFromContext(ctx, refreshTokenCookieName)
	if refreshToken == "" {
		return nil, authenticationV1.ErrorIncorrectRefreshToken("refresh token cookie is missing")
	}

	// admin 客户端类型固定；doGrantTypeRefreshToken 只用到 client_type 与 client_id/device_id
	inner := &authenticationV1.LoginRequest{
		ClientType: trans.Ptr(authenticationV1.ClientType_admin),
		ClientId:   req.ClientId,
		DeviceId:   req.DeviceId,
	}

	resp, err := s.doGrantTypeRefreshToken(ctx, inner, refreshToken)
	if err != nil {
		return nil, err
	}

	return buildTokenPairResponse(resp), nil
}

// ValidateToken 验证令牌
func (s *AuthenticationService) ValidateToken(ctx context.Context, req *authenticationV1.ValidateTokenRequest) (*authenticationV1.ValidateTokenResponse, error) {
	return s.authenticator.Authenticate(ctx, req)
}

// 注册流程已整体移除：管理底座的账号一律由管理员在用户管理创建，
// 不提供自主注册（原 RegisterUser 的"先建用户后验密码"还会留下半注册孤儿）。

func (s *AuthenticationService) WhoAmI(ctx context.Context, _ *emptypb.Empty) (*authenticationV1.WhoAmIResponse, error) {
	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	return &authenticationV1.WhoAmIResponse{
		UserId:   operator.GetUserId(),
		Username: operator.GetUsername(),
	}, nil
}

func (s *AuthenticationService) GenerateCaptcha(ctx context.Context, _ *emptypb.Empty) (*authenticationV1.GenerateCaptchaResponse, error) {
	captchaId, captchaImage, answer, err := s.captchaClient.Generate()
	if err != nil {
		s.log.Errorf(ctx, "generate captcha failed: %s", err.Error())
		return nil, authenticationV1.ErrorInternalServerError("generate captcha failed")
	}

	// Generate() 只生成验证码但不落盘，必须手动 Save 到 Redis，否则 Verify 时查不到。
	if err = s.captchaClient.Save(ctx, captchaId, answer); err != nil {
		s.log.Errorf(ctx, "save captcha failed: %s", err.Error())
		return nil, authenticationV1.ErrorInternalServerError("save captcha failed")
	}

	return &authenticationV1.GenerateCaptchaResponse{
		CaptchaId:   captchaId,
		ImageBase64: captchaImage,
	}, nil
}

func (s *AuthenticationService) VerifyCaptcha(ctx context.Context, req *authenticationV1.VerifyCaptchaRequest) (*authenticationV1.VerifyCaptchaResponse, error) {
	ok, err := s.captchaClient.Verify(ctx, req.GetCaptchaId(), req.GetUserInput())
	if err != nil {
		s.log.Errorf(ctx, "verify captcha failed: %s", err.Error())
		return nil, authenticationV1.ErrorInternalServerError("verify captcha failed")
	}

	return &authenticationV1.VerifyCaptchaResponse{
		Valid: ok,
	}, nil
}
