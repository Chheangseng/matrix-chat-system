package support

import (
	"context"
	"fmt"
	"log"
	"time"

	"chat-htp-api/internal/config"
	"chat-htp-api/internal/synapse"
)

// Provisioner handles on-mount verification and provisioning of the customer support Matrix account
type Provisioner struct {
	adminClient *synapse.AdminClient
	cfg         *config.Config
	maxRetries  int
	retryDelay  time.Duration
}

// NewProvisioner creates a new customer support account provisioner instance
func NewProvisioner(adminClient *synapse.AdminClient, cfg *config.Config) *Provisioner {
	return &Provisioner{
		adminClient: adminClient,
		cfg:         cfg,
		maxRetries:  10,
		retryDelay:  1 * time.Second,
	}
}

// SetRetryPolicy customizes retry attempts and delay for testing or slow startup environments
func (p *Provisioner) SetRetryPolicy(maxRetries int, delay time.Duration) {
	p.maxRetries = maxRetries
	p.retryDelay = delay
}

// EnsureSupportAccount checks and provisions the customer support account in Synapse with retry logic.
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

		user, err := p.adminClient.CreateOrUpdateUser(ctx, "", supportUserID, payload)
		if err == nil {
			log.Printf("[INFO] Customer support account ready in Matrix: %s (\"%s\")", supportUserID, p.cfg.SupportDisplayName)
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
