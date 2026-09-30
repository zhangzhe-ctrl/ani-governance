package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// ModelDevReleaseBinding is the tenant/preset current pointer. It deliberately
// stores no release catalogue content, generic settings, or actor defaults.
type ModelDevReleaseBinding struct{ ent.Schema }

func (ModelDevReleaseBinding) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{
		Table: "sys_modeldev_release_bindings",
		Checks: map[string]string{
			"sys_modeldev_release_bindings_tenant_positive_ck":     "tenant_id > 0",
			"sys_modeldev_release_bindings_generation_positive_ck": "generation > 0",
		},
	}}
}

func (ModelDevReleaseBinding) Mixin() []ent.Mixin {
	return []ent.Mixin{mixin.AutoIncrementId{}, ModelDevAdmissionTenantID{}}
}

func (ModelDevReleaseBinding) Policy() ent.Policy {
	return &TenantMutationGuardPolicy{}
}

func (ModelDevReleaseBinding) Fields() []ent.Field {
	return []ent.Field{
		field.String("resource_tenant_id").NotEmpty().Immutable(),
		field.String("preset_id").NotEmpty().Immutable(),
		field.String("release_id").NotEmpty(),
		field.String("release_digest").NotEmpty(),
		field.Uint64("generation").Positive(),
		field.Bool("new_submissions_enabled").Default(false),
		field.String("updated_by").NotEmpty(),
		field.Time("updated_at"),
		field.String("reason").NotEmpty(),
		field.String("evidence_reference").NotEmpty(),
	}
}

func (ModelDevReleaseBinding) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("resource_tenant_id", "preset_id").Unique().StorageKey("uix_sys_modeldev_release_bindings_scope"),
		index.Fields("tenant_id", "preset_id").Unique().StorageKey("uix_sys_modeldev_release_bindings_tenant_preset"),
	}
}
