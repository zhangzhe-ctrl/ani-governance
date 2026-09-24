package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// QuotaCharge holds the schema definition for the QuotaCharge entity.
// 占额明细：一次占额事务中对一项配额的一笔占用；退额按累计释放推进
// released_units，恒等式 account.occupied_units=SUM(original-released) 由验收重算。
type QuotaCharge struct {
	ent.Schema
}

func (QuotaCharge) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "sys_quota_charges",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
			Checks: map[string]string{
				"sys_quota_charges_tenant_positive_ck":   `tenant_id > 0`,
				"sys_quota_charges_original_positive_ck": `original_units >= 0`,
				"sys_quota_charges_release_range_ck":     `released_units >= 0 AND released_units <= original_units`,
			},
		},
		entsql.WithComments(true),
		schema.Comment("配额占额明细表"),
	}
}

// Fields of the QuotaCharge.
func (QuotaCharge) Fields() []ent.Field {
	return []ent.Field{
		field.String("charge_id").
			Comment("占额明细ID（UUID）").
			NotEmpty().
			Immutable().
			Unique(),

		// 引用 sys_quota_operations.operation_id（UUID 字符串）；
		// 复合 FK (tenant_id,operation_id) 由 schema 导出器补充。
		field.String("operation_id").
			Comment("原创建操作ID（UUID）").
			NotEmpty().
			Immutable(),

		field.String("quota_code").
			Comment("配额编码").
			NotEmpty().
			Immutable(),

		field.Int64("original_units").
			Comment("原始占额数量").
			Immutable(),

		// 累计已释放数量；0<=released_units<=original_units，检查约束见迁移。
		field.Int64("released_units").
			Comment("累计已释放数量").
			Default(0),
	}
}

// Mixin of the QuotaCharge.
func (QuotaCharge) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.TenantID[uint32]{},
	}
}

// Indexes of the QuotaCharge.
func (QuotaCharge) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "charge_id").Unique().
			StorageKey("uix_sys_quota_charges_tenant_id_charge_id"),
		// 每个 operation 每个配额编码只有一笔 charge。
		index.Fields("tenant_id", "operation_id", "quota_code").Unique().
			StorageKey("uix_sys_quota_charges_tenant_operation_code"),
		index.Fields("quota_code").StorageKey("idx_sys_quota_charges_quota_code"),
	}
}
