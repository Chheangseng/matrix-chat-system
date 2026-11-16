package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CustomerSupportRoom tracks the 1-on-1 direct message room established between a user and customer support
type CustomerSupportRoom struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	RoomID        string         `gorm:"uniqueIndex;size:255;not null" json:"room_id"`
	UserID        string         `gorm:"index;size:255;not null" json:"user_id"`
	SupportUserID string         `gorm:"index;size:255;not null" json:"support_user_id"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName returns the table name for CustomerSupportRoom
func (CustomerSupportRoom) TableName() string {
	return "customer_support_rooms"
}

// BeforeCreate hook generates a time-sortable UUID v7 if not already set
func (c *CustomerSupportRoom) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		c.ID = id
	}
	return nil
}
