package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/tx7do/go-crud/entgo/mixin"
)

// QuotaDefinition holds the schema definition for the QuotaDefinition entity.
// 配额目录：平台配置元数据，没有 tenant_id；code/unit/kind 本批不可经 API 修改。
// Ent 不支持把 code 声明为原生主键，采用自增 id + code UNIQUE NOT NULL（业务主键），
// 与既有 sys_plan_quotas 的 id 用法一致；唯一性由数据库约束保证。
type QuotaDefinition struct {
	ent.Schema
}

func (QuotaDefinition) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "sys_quota_definitions",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
		},
		entsql.WithComments(true),
		schema.Comment("配额目录表"),
	}
}

// Fields of the QuotaDefinition.
func (QuotaDefinition) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").
			Comment("稳定配额编码（如 gpu.count）").
			NotEmpty().
			Immutable().
			Unique(),

		field.String("display_name").
			Comment("展示名").
			NotEmpty(),

		field.String("unit").
			Comment("单位（user/byte/request/gpu）").
			NotEmpty().
			Immutable(),

		field.Enum("accounting_kind").
			Comment("计数模型").
			NamedValues(
				"Concurrent", "CONCURRENT",
				"Counter", "COUNTER",
			).
			Immutable(),
	}
}

// Mixin of the QuotaDefinition.
func (QuotaDefinition) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.CreatedAt{},
	}
}

// Indexes of the QuotaDefinition.
func (QuotaDefinition) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
	}
}
