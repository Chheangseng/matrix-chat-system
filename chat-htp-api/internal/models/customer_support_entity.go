package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CustomerSupport represents the synchronized customer support account entity in the API database
type CustomerSupport struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	MatrixUserID string         `gorm:"uniqueIndex;size:255;not null" json:"support_user_id"`
	Username     string         `gorm:"uniqueIndex;size:255;not null" json:"username"`
	DisplayName  string         `gorm:"size:255" json:"display_name"`
	AvatarURL    string         `gorm:"type:text" json:"avatar_url"`
	IsAdmin      bool           `gorm:"default:false" json:"is_admin"`
	Status       string         `gorm:"size:50;default:'active'" json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName returns the table name for CustomerSupport
func (CustomerSupport) TableName() string {
	return "customer_supports"
}

// BeforeCreate hook generates a time-sortable UUID v7 if not already set
func (c *CustomerSupport) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		c.ID = id
	}
	return nil
}
