package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// AccessKeyIdempotency records the input-side idempotency of
// POST /api/v1/auth/api-keys. It is deliberately a separate relation rather than
// a column on sys_access_keys: sys_access_keys rows are physically deleted, and a
// deleted row cannot answer "was this key already used".
//
// There is intentionally no foreign key to sys_access_keys: the record must
// outlive the key it points at so that create → delete → replay is still judged
// as a replay instead of silently creating a second key.
type AccessKeyIdempotency struct {
	ent.Schema
}

func (AccessKeyIdempotency) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table: "sys_access_key_idempotency",
			Checks: map[string]string{
				"sys_access_key_idempotency_tenant_positive_ck": "tenant_id > 0",
				"sys_access_key_idempotency_actor_positive_ck":  "actor_id > 0",
				"sys_access_key_idempotency_action_ck":          "action = 'access_key.create'",
			},
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
		},
		entsql.WithComments(true),
		schema.Comment("访问凭证创建幂等记录表"),
	}
}

func (AccessKeyIdempotency) Fields() []ent.Field {
	return []ent.Field{
		field.Uint32("actor_id").Positive().Immutable().Comment("操作人用户ID"),
		field.String("action").NotEmpty().Immutable().Comment("操作标识（固定 access_key.create）"),
		field.String("idempotency_key").NotEmpty().MaxLen(128).Immutable().Comment("客户端提供的幂等键"),
		field.String("request_fingerprint").NotEmpty().Immutable().Comment("规范意图的 sha256 十六进制"),
		field.Uint32("access_key_id").Positive().Immutable().Comment("指向 sys_access_keys.id（回放原始对象）"),
		field.Time("created_at").Immutable().Comment("创建时间"),
	}
}

func (AccessKeyIdempotency) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		AccessKeyIdempotencyTenantID{},
	}
}

// Policy 与仓内其余带租户列表一致：库层 TenantPrivacy 为主，本守卫为独立第二道。
func (AccessKeyIdempotency) Policy() ent.Policy {
	return &TenantMutationGuardPolicy{}
}

func (AccessKeyIdempotency) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "actor_id", "action", "idempotency_key").Unique().StorageKey("uix_sys_access_key_idempotency_key"),
	}
}

// AccessKeyIdempotencyTenantID retains the shared TenantPrivacy policy while
// requiring a positive immutable tenant on every persisted record.
type AccessKeyIdempotencyTenantID struct{ mixin.TenantID[uint32] }

func (AccessKeyIdempotencyTenantID) Fields() []ent.Field {
	return []ent.Field{field.Uint32("tenant_id").Positive().Nillable().Immutable().Comment("租户ID")}
}
