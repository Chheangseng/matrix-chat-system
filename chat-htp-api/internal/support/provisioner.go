package support

import (
	"context"
	"fmt"
	"log"
	"time"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/models"
	"chat-htp-api/internal/synapse"

	"gorm.io/gorm"
)

// Provisioner handles on-mount verification and provisioning of the customer support Matrix account
type Provisioner struct {
	adminClient *synapse.AdminClient
	cfg         *config.Config
	db          *gorm.DB
	maxRetries  int
	retryDelay  time.Duration
}

// NewProvisioner creates a new customer support account provisioner instance
func NewProvisioner(adminClient *synapse.AdminClient, cfg *config.Config, db *gorm.DB) *Provisioner {
	return &Provisioner{
		adminClient: adminClient,
		cfg:         cfg,
		db:          db,
		maxRetries:  10,
		retryDelay:  1 * time.Second,
	}
}

// SetRetryPolicy customizes retry attempts and delay for testing or slow startup environments
func (p *Provisioner) SetRetryPolicy(maxRetries int, delay time.Duration) {
	p.maxRetries = maxRetries
	p.retryDelay = delay
}

// EnsureSupportAccount checks and provisions the customer support account in Synapse with retry logic,
// and synchronizes the account data with the database.
func (p *Provisioner) EnsureSupportAccount(ctx context.Context) (*synapse.SynapseUser, error) {
	supportUserID := p.cfg.GetSupportUserID()
	payload := synapse.CreateUserPayload{
		Password:    p.cfg.SupportPassword,
		DisplayName: p.cfg.SupportDisplayName,
		AvatarURL:   p.cfg.SupportAvatarURL,
		Admin:       p.cfg.SupportAdmin,
	}

	var lastErr error
	for attempt := 1; attempt <= p.maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		user, err := p.adminClient.CreateOrUpdateUser(ctx, "", p.cfg.SupportUser, payload)
		if err == nil {
			log.Printf("[INFO] Customer support account ready in Matrix: %s (\"%s\")", supportUserID, p.cfg.SupportDisplayName)
			if p.db != nil {
				if err := p.syncToDatabase(ctx, user); err != nil {
					log.Printf("[WARN] Failed to sync customer support account to database: %v", err)
				}
			}
			return user, nil
		}

		lastErr = err
		if attempt < p.maxRetries {
			log.Printf("[WARN] Customer support provisioning attempt %d/%d failed: %v. Retrying in %v...",
				attempt, p.maxRetries, err, p.retryDelay)
			time.Sleep(p.retryDelay)
		}
	}

	return nil, fmt.Errorf("failed to provision customer support account after %d attempts: %w", p.maxRetries, lastErr)
}

func (p *Provisioner) syncToDatabase(ctx context.Context, user *synapse.SynapseUser) error {
	matrixUserID := user.Name
	if matrixUserID == "" {
		matrixUserID = p.cfg.GetSupportUserID()
	}
	displayName := user.DisplayName
	if displayName == "" {
		displayName = p.cfg.SupportDisplayName
	}
	avatarURL := user.AvatarURL
	if avatarURL == "" {
		avatarURL = p.cfg.SupportAvatarURL
	}

	var supportRecord models.CustomerSupport
	err := p.db.WithContext(ctx).Where("username = ?", p.cfg.SupportUser).First(&supportRecord).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create new record (BeforeCreate hook generates UUID v7)
			supportRecord = models.CustomerSupport{
				MatrixUserID: matrixUserID,
				Username:     p.cfg.SupportUser,
				DisplayName:  displayName,
				AvatarURL:    avatarURL,
				IsAdmin:      user.Admin,
				Status:       "active",
			}
			if err := p.db.WithContext(ctx).Create(&supportRecord).Error; err != nil {
				return fmt.Errorf("failed to create customer support record: %w", err)
			}
			log.Printf("[INFO] Customer support account created and synced to database: %s (id: %s)", supportRecord.MatrixUserID, supportRecord.ID)
			return nil
		}
		return fmt.Errorf("database query error: %w", err)
	}

	// Record exists, update profile fields
	supportRecord.MatrixUserID = matrixUserID
	supportRecord.DisplayName = displayName
	supportRecord.AvatarURL = avatarURL
	supportRecord.IsAdmin = user.Admin
	supportRecord.Status = "active"

	if err := p.db.WithContext(ctx).Save(&supportRecord).Error; err != nil {
		return fmt.Errorf("failed to update customer support record: %w", err)
	}

	log.Printf("[INFO] Customer support account synced to database: %s (id: %s)", supportRecord.MatrixUserID, supportRecord.ID)
	return nil
}
