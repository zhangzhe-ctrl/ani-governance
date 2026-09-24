package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// AccessKey holds the schema definition for the AccessKey entity.
// OpenAPI 访问凭证（AK/SK）：租户级机器凭证，SK 以 AES-256-GCM 密文存储。
type AccessKey struct {
	ent.Schema
}

func (AccessKey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "sys_access_keys",
			Checks:    map[string]string{"sys_access_keys_tenant_positive": "tenant_id > 0"},
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
		},
		entsql.WithComments(true),
		schema.Comment("OpenAPI访问凭证表"),
	}
}

// The tenant-preserving (tenant_id, role_id) foreign key is maintained by
// cmd/schema, the Atlas source exporter, because Ent edges are single-column.
// Fields of the AccessKey.
func (AccessKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Comment("凭证名称（用途说明）").Optional().Nillable(),
		field.String("access_key").Comment("访问键（AK，公开标识）").NotEmpty(),
		field.String("secret_ciphertext").Comment("AES-256-GCM密文").NotEmpty().Sensitive(),
		field.Uint32("role_id").Positive().Comment("本租户绑定角色"),
		field.Time("expires_at").Comment("过期时间（空表示长期有效）").Optional().Nillable(),
		field.Time("last_used_at").Comment("最近成功验签时间").Optional().Nillable(),
	}
}

// Mixin of the AccessKey.
func (AccessKey) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.OperatorID{},
		mixin.SwitchStatus{},
		AccessKeyTenantID{},
	}
}

// Policy 租户写隔离冗余防线：与本仓其余带租户列表一致（库层 TenantPrivacy 为主，
// 本守卫为独立第二道，见 schema/tenant_mutation_guard.go 与 docs/tenant_isolation.md）。
// 此前本表独缺此层（2026-09-12 补挂）。
func (AccessKey) Policy() ent.Policy {
	return &TenantMutationGuardPolicy{}
}

func (AccessKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("access_key").Unique().StorageKey("idx_sys_access_keys_access_key"),
		index.Fields("tenant_id").StorageKey("idx_sys_access_keys_tenant_id"),
	}
}

// AccessKeyTenantID retains the shared TenantPrivacy policy while requiring a
// positive immutable tenant on every persisted key (there are no platform keys).
type AccessKeyTenantID struct{ mixin.TenantID[uint32] }

func (AccessKeyTenantID) Fields() []ent.Field {
	return []ent.Field{field.Uint32("tenant_id").Positive().Nillable().Immutable().Comment("租户ID")}
}
