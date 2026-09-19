package models

import (
	"github.com/tx7do/go-crud/gorm/mixin"
)

// PermissionApi 对应表 sys_permission_apis
type PermissionApi struct {
	mixin.AutoIncrementID

	PermissionId *uint32 `gorm:"column:permission_id;type:int unsigned;comment:权限ID"`
	ApiId        *uint32 `gorm:"column:api_id;type:int unsigned;comment:API ID"`

	mixin.TimeAt
	mixin.OperatorID
}

// TableName 指定表名
func (PermissionApi) TableName() string {
	return "sys_permission_apis"
}
