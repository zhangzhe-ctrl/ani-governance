package main

import (
	"fmt"

	"github.com/go-kratos/kratos/v2"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/server"
	"go-wind-admin/app/admin/service/internal/service"
	"go-wind-admin/pkg/authorizer"
)

// initApp 手写装配整个应用,是唯一后端构建的依赖注入点。
//
// 装配严格单向分层,自上而下的阅读顺序即依赖方向:
// The wiring is strictly layered; reading top-down follows the dependency direction:
//
//	基础设施 → 仓储层(data) → 认证与鉴权 → 服务层(service) → 传输层(server)
//
// 约定 / Conventions:
//   - 新增仓储/服务:在对应分层小节追加一行构造,并传给下游消费者;漏接由编译器在调用处报错。
//     To add a repo/service: append one constructor line in its layer section, then pass it to
//     downstream consumers; a missing connection is a compile error at the call site.
//   - 持有 cleanup 的资源创建成功后立即注册进 cleanups;任何一步失败,rollback 逆序执行已注册
//     的清理(shutdown 与中途失败共用同一条 LIFO 路径)。
//     Resources owning a cleanup register it immediately; rollback runs them LIFO both on
//     mid-way failure and on shutdown.
//   - 本文件只做构造与传参,不写业务逻辑。
//     Construction and parameter passing only; no business logic in this file.
func initApp(ctx *bootstrap.Context) (*kratos.App, func(), error) {
	// Downstream catalog queries must never run with the upstream development noop authorizer.
	if ctx.GetConfig() == nil || ctx.GetConfig().GetAuthz() == nil || ctx.GetConfig().GetAuthz().GetType() != "casbin" {
		return nil, nil, fmt.Errorf("ani-governance downstream catalog access requires tenant-domain casbin authorization")
	}
	// cleanup 注册表:rollback 时逆序执行。
	// Cleanup registry; rollback runs entries in reverse order.
	var cleanups []func()
	rollback := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// ═══════════════════════ 一、基础设施 ═══════════════════════

	redisClient, cleanupRedis, err := data.NewRedisClient(ctx)
	if err != nil {
		return nil, nil, err
	}
	cleanups = append(cleanups, cleanupRedis)

	entClient, cleanupEnt, err := data.NewEntClient(ctx)
	if err != nil {
		rollback()
		return nil, nil, err
	}
	cleanups = append(cleanups, cleanupEnt)

	minioClient := data.NewMinIoClient(ctx)

	// 认证基建:令牌缓存 → 认证器 → 访问令牌校验器。
	clientType := data.NewClientType()
	passwordCrypto := data.NewPasswordCrypto()
	captcha := data.NewCaptcha(redisClient)
	userTokenCache := data.NewUserTokenCache(ctx, redisClient)
	authenticator := data.NewAuthenticator(ctx, userTokenCache)
	accessTokenChecker := data.NewTokenChecker(ctx, authenticator, clientType)
	loginRateLimiter := data.NewLoginRateLimiter(ctx, redisClient)
	mfaChallengeCache := data.NewMfaChallengeCache(ctx, redisClient)
	vcodeCache := data.NewVCodeCache(ctx, redisClient)

	// ═══════════════════════ 二、仓储层(internal/data) ═══════════════════════

	// 身份与账号
	userRoleRepo := data.NewUserRoleRepo(ctx, entClient)
	userOrgUnitRepo := data.NewUserOrgUnitRepo(ctx, entClient)
	userPositionRepo := data.NewUserPositionRepo(ctx, entClient)
	membershipRoleRepo := data.NewMembershipRoleRepo(ctx, entClient)
	membershipPositionRepo := data.NewMembershipPositionRepo(ctx, entClient)
	membershipOrgUnitRepo := data.NewMembershipOrgUnitRepo(ctx, entClient)
	membershipRepo := data.NewMembershipRepo(ctx, entClient, membershipRoleRepo, membershipPositionRepo, membershipOrgUnitRepo)
	userRepo := data.NewUserRepo(ctx, entClient, userRoleRepo, userOrgUnitRepo, userPositionRepo, membershipRepo)
	configRepo := data.NewConfigRepo(ctx, entClient, redisClient)
	userCredentialRepo := data.NewUserCredentialRepo(ctx, entClient, passwordCrypto, configRepo)
	userMfaFactorRepo := data.NewUserMfaFactorRepo(ctx, entClient)
	loginPolicyRepo := data.NewLoginPolicyRepo(ctx, entClient)

	// 组织架构与租户
	orgUnitRepo := data.NewOrgUnitRepo(ctx, entClient, userOrgUnitRepo)
	positionRepo := data.NewPositionRepo(ctx, entClient)
	tenantRepo := data.NewTenantRepo(ctx, entClient)
	tenantUsageRepo := data.NewTenantUsageRepo(ctx, entClient, authenticator)

	// RBAC:权限 → 角色(聚合权限),Api/Menu 为权限挂载点
	permissionApiRepo := data.NewPermissionApiRepo(ctx, entClient)
	permissionMenuRepo := data.NewPermissionMenuRepo(ctx, entClient)
	rolePermissionRepo := data.NewRolePermissionRepo(ctx, entClient)
	roleOrgUnitRepo := data.NewRoleOrgUnitRepo(ctx, entClient)
	roleFieldPermissionRepo := data.NewRoleFieldPermissionRepo(ctx, entClient)
	roleMetadataRepo := data.NewRoleMetadataRepo(ctx, entClient)
	permissionRepo := data.NewPermissionRepo(ctx, entClient, permissionApiRepo, permissionMenuRepo)
	roleRepo := data.NewRoleRepo(ctx, entClient, rolePermissionRepo, roleOrgUnitRepo, permissionRepo, roleMetadataRepo, roleFieldPermissionRepo)
	permissionGroupRepo := data.NewPermissionGroupRepo(ctx, entClient)
	apiRepo := data.NewApiRepo(ctx, entClient)
	menuRepo := data.NewMenuRepo(ctx, entClient)

	// 审计日志
	apiAuditLogRepo := data.NewApiAuditLogRepo(ctx, entClient)
	loginAuditLogRepo := data.NewLoginAuditLogRepo(ctx, entClient)
	operationAuditLogRepo := data.NewOperationAuditLogRepo(ctx, entClient)
	permissionAuditLogRepo := data.NewPermissionAuditLogRepo(ctx, entClient)
	dataAccessAuditLogRepo := data.NewDataAccessAuditLogRepo(ctx, entClient)
	policyEvaluationLogRepo := data.NewPolicyEvaluationLogRepo(ctx, entClient)
	auditLogArchiveRepo := data.NewAuditLogArchiveRepo(ctx, entClient)

	// 字典与多语言
	dictTypeRepo := data.NewDictTypeRepo(ctx, entClient)
	dictEntryI18nRepo := data.NewDictEntryI18nRepo(ctx, entClient)
	dictEntryRepo := data.NewDictEntryRepo(ctx, entClient, dictEntryI18nRepo)
	languageRepo := data.NewLanguageRepo(ctx, entClient)

	// 套餐与计费
	planRepo := data.NewPlanRepo(ctx, entClient)
	planQuotaRepo := data.NewPlanQuotaRepo(ctx, entClient)
	planModuleRepo := data.NewPlanModuleRepo(ctx, entClient)

	// 任务 / 文件 / 运维观测
	taskRepo := data.NewTaskRepo(ctx, entClient)
	backupRepo := data.NewBackupRepo(ctx, entClient)
	fileRepo := data.NewFileRepo(ctx, entClient)
	redisCacheMonitorRepo := data.NewRedisCacheMonitorRepo(ctx, redisClient)
	serverMonitorRepo := data.NewServerMonitorRepo(ctx, entClient)
	notificationChannelRepo := data.NewNotificationChannelRepo(ctx, entClient)
	dashboardRepo := data.NewDashboardRepo(ctx, entClient)

	// 站内信
	internalMessageRepo := data.NewInternalMessageRepo(ctx, entClient)
	internalMessageCategoryRepo := data.NewInternalMessageCategoryRepo(ctx, entClient)
	internalMessageRecipientRepo := data.NewInternalMessageRecipientRepo(ctx, entClient)

	// ── register:repo ── 新模块仓储在此行后注册(make register 工具锚点,勿删)
	accessKeyRepo := data.NewAccessKeyRepo(ctx, entClient)

	// ═══════════════════════ 三、认证与鉴权 ═══════════════════════

	tenantAccessChecker := data.NewTenantAccessCheckerImpl(ctx, entClient)
	authorizerProvider := data.NewAuthorizerProvider(ctx, roleRepo, apiRepo)
	authz := authorizer.NewAuthorizer(ctx, authorizerProvider)

	// ═══════════════════════ 四、服务层(internal/service) ═══════════════════════

	// 认证与登录策略
	authenticationService := service.NewAuthenticationService(ctx, userRepo, userCredentialRepo, roleRepo, tenantRepo, membershipRepo, orgUnitRepo, roleOrgUnitRepo, roleFieldPermissionRepo, permissionRepo, authenticator, clientType, captcha, loginRateLimiter, loginPolicyRepo, userMfaFactorRepo, mfaChallengeCache, vcodeCache, notificationChannelRepo)
	mfaService := service.NewMfaService(ctx, userMfaFactorRepo, mfaChallengeCache, authenticator, loginRateLimiter, userRepo)
	loginPolicyService := service.NewLoginPolicyService(ctx, loginPolicyRepo)

	// 身份与组织
	userService := service.NewUserService(ctx, userRepo, roleRepo, userCredentialRepo, positionRepo, orgUnitRepo, tenantRepo, membershipRepo, authenticator)
	userProfileService := service.NewUserProfileService(ctx, userRepo, roleRepo, userCredentialRepo, authenticator, notificationChannelRepo, vcodeCache, minioClient)
	positionService := service.NewPositionService(ctx, positionRepo, orgUnitRepo)
	orgUnitService := service.NewOrgUnitService(ctx, orgUnitRepo, userRepo)

	// RBAC
	roleService := service.NewRoleService(ctx, authz, roleRepo, tenantRepo)
	menuService := service.NewMenuService(ctx, menuRepo)
	apiService := service.NewApiService(ctx, apiRepo, authz)
	permissionService := service.NewPermissionService(ctx, permissionRepo, permissionGroupRepo, menuRepo, apiRepo, roleRepo, authz)
	permissionGroupService := service.NewPermissionGroupService(ctx, permissionGroupRepo, permissionRepo)

	// 租户与套餐
	tenantService := service.NewTenantService(ctx, tenantRepo, tenantUsageRepo, userRepo, userCredentialRepo, roleRepo, authz)
	planService := service.NewPlanService(ctx, planRepo)
	planQuotaService := service.NewPlanQuotaService(ctx, planQuotaRepo)
	planModuleService := service.NewPlanModuleService(ctx, planModuleRepo)

	// 字典与多语言
	dictTypeService := service.NewDictTypeService(ctx, dictTypeRepo)
	dictEntryService := service.NewDictEntryService(ctx, dictEntryRepo)
	languageService := service.NewLanguageService(ctx, languageRepo)

	// 文件与任务
	fileService := service.NewFileService(ctx, fileRepo, minioClient)
	fileTransferService := service.NewFileTransferService(ctx, minioClient, fileRepo)
	taskService := service.NewTaskService(ctx, taskRepo, userRepo, backupRepo, tenantUsageRepo, auditLogArchiveRepo, minioClient)

	// 审计日志
	loginAuditLogService := service.NewLoginAuditLogService(ctx, loginAuditLogRepo)
	apiAuditLogService := service.NewApiAuditLogService(ctx, apiAuditLogRepo, apiRepo)
	operationAuditLogService := service.NewOperationAuditLogService(ctx, operationAuditLogRepo)
	dataAccessAuditLogService := service.NewDataAccessAuditLogService(ctx, dataAccessAuditLogRepo)
	permissionAuditLogService := service.NewPermissionAuditLogService(ctx, permissionAuditLogRepo)
	policyEvaluationLogService := service.NewPolicyEvaluationLogService(ctx, policyEvaluationLogRepo)

	// 运维观测与门户
	redisCacheMonitorService := service.NewRedisCacheMonitorService(ctx, redisCacheMonitorRepo)
	serverMonitorService := service.NewServerMonitorService(ctx, serverMonitorRepo)
	notificationChannelService := service.NewNotificationChannelService(ctx, notificationChannelRepo)
	onlineSessionService := service.NewOnlineSessionService(ctx, authenticator)
	dashboardService := service.NewDashboardService(ctx, dashboardRepo)
	adminPortalService := service.NewAdminPortalService(ctx, menuRepo, roleRepo, userRepo, permissionRepo, planModuleRepo, tenantRepo)

	// 站内信
	internalMessageService := service.NewInternalMessageService(ctx, internalMessageRepo, internalMessageCategoryRepo, internalMessageRecipientRepo, userRepo, authenticator, clientType)
	internalMessageCategoryService := service.NewInternalMessageCategoryService(ctx, internalMessageCategoryRepo)
	internalMessageRecipientService := service.NewInternalMessageRecipientService(ctx, internalMessageRepo, internalMessageRecipientRepo)

	// ── register:service ── 新模块服务在此行后注册(make register 工具锚点,勿删)
	accessKeyService := service.NewAccessKeyService(ctx, accessKeyRepo, authenticator, loginRateLimiter)
	configService := service.NewConfigService(ctx, configRepo)
	networkConfig, err := data.NetworkConfigFromEnv()
	if err != nil {
		rollback()
		return nil, nil, err
	}
	var networkClient service.VPCGetter
	if networkConfig.Address != "" {
		client, closeNetwork, err := data.NewNetworkClient(networkConfig)
		if err != nil {
			rollback()
			return nil, nil, err
		}
		cleanups = append(cleanups, closeNetwork)
		networkClient = client
	}
	networkService := service.NewNetworkService(networkClient, tenantRepo)

	// ═══════════════════════ 五、传输层(internal/server) ═══════════════════════

	restMiddlewares := server.NewRestMiddleware(ctx, accessTokenChecker, tenantAccessChecker, authz,
		apiAuditLogRepo, loginAuditLogRepo, operationAuditLogRepo, permissionAuditLogRepo, dataAccessAuditLogRepo, policyEvaluationLogRepo)

	restServer, err := server.NewRestServer(ctx, restMiddlewares, authz,
		authenticationService, mfaService, loginPolicyService,
		adminPortalService, taskService,
		fileService, fileTransferService,
		dictTypeService, dictEntryService, languageService,
		tenantService, planService, planQuotaService, planModuleService,
		userService, userProfileService, roleService, positionService, orgUnitService,
		menuService, apiService, permissionService, permissionGroupService,
		permissionAuditLogService, policyEvaluationLogService,
		loginAuditLogService, apiAuditLogService, operationAuditLogService, dataAccessAuditLogService,
		redisCacheMonitorService, serverMonitorService, notificationChannelService,
		onlineSessionService, dashboardService,
		internalMessageService, internalMessageCategoryService, internalMessageRecipientService,
		// register:rest-arg ── 新模块服务实参在此行后追加(make register 工具锚点,勿删)
		accessKeyService,
		configService,
		networkService,
	)
	if err != nil {
		rollback()
		return nil, nil, err
	}

	asynqServer, err := server.NewAsynqServer(ctx, taskService, internalMessageService)
	if err != nil {
		rollback()
		return nil, nil, err
	}

	sseServer := server.NewSseServer(ctx, internalMessageService)

	return newApp(ctx, restServer, asynqServer, sseServer), rollback, nil
}
