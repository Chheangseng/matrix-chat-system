package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestCustomerSupportRoom_BeforeCreate(t *testing.T) {
	csr := &CustomerSupportRoom{}
	err := csr.BeforeCreate(nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if csr.ID == uuid.Nil {
		t.Fatal("expected generated UUID, got nil")
	}

	if csr.ID.Version() != 7 {
		t.Fatalf("expected UUID version 7, got version: %d", csr.ID.Version())
	}

	customID := uuid.MustParse("01932f91-5f33-7000-8000-000000000001")
	csr2 := &CustomerSupportRoom{ID: customID}
	err = csr2.BeforeCreate(nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if csr2.ID != customID {
		t.Fatalf("expected preserved ID %s, got %s", customID, csr2.ID)
	}
}

func TestCustomerSupportRoom_TableName(t *testing.T) {
	csr := CustomerSupportRoom{}
	if csr.TableName() != "customer_support_rooms" {
		t.Fatalf("expected table name 'customer_support_rooms', got: '%s'", csr.TableName())
	}
}
