package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// QuotaReleaseReceipt holds the schema definition for the QuotaReleaseReceipt entity.
// 退额回执：对整个已验证批次持久化（不拆明细表），幂等去重按
// UNIQUE(owner_service,release_event_id)。
type QuotaReleaseReceipt struct {
	ent.Schema
}

func (QuotaReleaseReceipt) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{
			Table:     "sys_quota_release_receipts",
			Charset:   "utf8mb4",
			Collation: "utf8mb4_bin",
			Checks: map[string]string{
				"sys_quota_release_receipts_tenant_positive_ck": `tenant_id > 0`,
			},
		},
		entsql.WithComments(true),
		schema.Comment("配额退额回执表"),
	}
}

// Fields of the QuotaReleaseReceipt.
func (QuotaReleaseReceipt) Fields() []ent.Field {
	return []ent.Field{
		field.String("receipt_id").
			Comment("回执ID（UUID）").
			NotEmpty().
			Immutable().
			Unique(),

		field.String("owner_service").
			Comment("owner 服务标识（来自证书精确 SAN）").
			NotEmpty().
			Immutable(),

		field.String("release_event_id").
			Comment("释放事件ID（UUID，重试不变）").
			NotEmpty().
			Immutable(),

		// 基于固定结构、排序后唯一 items/resource_refs 的哈希；
		// 不含传输 request-id。同 ID 不同内容返回冲突。
		field.String("payload_hash").
			Comment("批次负载哈希").
			NotEmpty().
			Immutable(),

		// 整个已验证批次的原文（JSON）。
		field.Text("payload_json").
			Comment("批次负载原文").
			NotEmpty().
			Immutable(),
	}
}

// Mixin of the QuotaReleaseReceipt.
func (QuotaReleaseReceipt) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixin.AutoIncrementId{},
		mixin.CreatedAt{},
		mixin.TenantID[uint32]{},
	}
}

// Indexes of the QuotaReleaseReceipt.
func (QuotaReleaseReceipt) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "owner_service", "release_event_id").Unique().
			StorageKey("uix_sys_quota_release_receipts_owner_event"),
	}
}
