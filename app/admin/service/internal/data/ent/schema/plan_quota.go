package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/tx7do/go-crud/entgo/mixin"
)

// PlanQuota holds the schema definition for the PlanQuota entity.
type PlanQuota struct {
	ent.Schema
}

func (PlanQuota) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "sys_plan_quotas",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
			Checks: map[string]string{
				"sys_plan_quotas_quota_value_nonnegative_ck": `quota_value >= 0`,
			},
		},
		entsql.WithComments(true),
		schema.Comment("套餐配额表"),
	}
}

// Fields of the PlanQuota.
func (PlanQuota) Fields() []ent.Field {
	return []ent.Field{
		// 稳定配额编码（权威业务字段）。约束迁移后 NOT NULL，UNIQUE(plan_id,quota_code)；
		// 旧 quota_type 保留 nullable 仅供兼容映射器读写。
		field.String("quota_code").
			Comment("配额编码").
			NotEmpty(),

		field.Enum("quota_type").
			Comment("配额类型（deprecated：仅旧三项兼容投影）").
			NamedValues(
				"UserLimit", "USER_LIMIT",
				"Storage", "STORAGE",
				"ApiCall", "API_CALL",
			).
			Optional().
			Nillable(),

		field.Uint64("quota_value").
			Comment("配额值").
			Optional().
			Nillable(),
	}
}

// Mixin of the PlanQuota.
func (PlanQuota) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.OperatorID{},
	}
}

// Edges of the PlanQuota.
func (PlanQuota) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("plan", Plan.Type).
			Ref("quotas").
			Unique(),
	}
}

// Indexes of the PlanQuota.
func (PlanQuota) Indexes() []ent.Index {
	return []ent.Index{
		// 创建时间索引，用于配额列表的时间区间查询与分页
		index.Fields("created_at").StorageKey("idx_sys_plan_quotas_created_at"),
		// 同一套餐同一配额编码只能有一行政策项
		index.Fields("quota_code").Edges("plan").Unique().StorageKey("uix_sys_plan_quotas_plan_id_quota_code"),
	}
}
