package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// QuotaAccount holds the schema definition for the QuotaAccount entity.
// 租户配额账户：每租户每配额编码一行；occupied_units 为权威占用，
// 不维护 reserved/allocated 两套额度。balance 恒等式由账本重算保证。
type QuotaAccount struct {
	ent.Schema
}

func (QuotaAccount) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "sys_quota_accounts",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
			Checks: map[string]string{
				"sys_quota_accounts_tenant_positive_ck":      `tenant_id > 0`,
				"sys_quota_accounts_occupied_nonnegative_ck": `occupied_units >= 0`,
			},
		},
		entsql.WithComments(true),
		schema.Comment("租户配额账户表"),
	}
}

// Fields of the QuotaAccount.
func (QuotaAccount) Fields() []ent.Field {
	return []ent.Field{
		field.String("quota_code").
			Comment("配额编码").
			NotEmpty().
			Immutable(),

		// 当前占用；>=0，由占额/退额事务维护。
		field.Int64("occupied_units").
			Comment("当前占用数量").
			Default(0),

		// 乐观锁版本号，占额/退额时递增。
		field.Int64("version").
			Comment("版本号").
			Default(0),
	}
}

// Mixin of the QuotaAccount.
func (QuotaAccount) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.TimeAt{},
		mixin.TenantID[uint32]{},
	}
}

// Indexes of the QuotaAccount.
func (QuotaAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "quota_code").Unique().StorageKey("uix_sys_quota_accounts_tenant_id_quota_code"),
	}
}
