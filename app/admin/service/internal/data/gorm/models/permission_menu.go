package models

import (
	"github.com/tx7do/go-crud/gorm/mixin"
)

// PermissionMenu 对应表 sys_permission_menus
type PermissionMenu struct {
	mixin.AutoIncrementID

	PermissionId *uint32 `gorm:"column:permission_id;type:int unsigned;comment:权限ID"`
	MenuId       *uint32 `gorm:"column:menu_id;type:int unsigned;comment:菜单ID"`

	mixin.TimeAt
	mixin.OperatorID
}

// TableName 指定表名
func (PermissionMenu) TableName() string {
	return "sys_permission_menus"
}
