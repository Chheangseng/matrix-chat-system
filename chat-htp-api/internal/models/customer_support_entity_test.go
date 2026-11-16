package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestCustomerSupport_BeforeCreate(t *testing.T) {
	// Case 1: Nil UUID generates UUID v7
	cs := &CustomerSupport{}
	err := cs.BeforeCreate(nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cs.ID == uuid.Nil {
		t.Fatal("expected generated UUID, got nil")
	}

	if cs.ID.Version() != 7 {
		t.Fatalf("expected UUID version 7, got version: %d", cs.ID.Version())
	}

	// Case 2: Pre-existing UUID is preserved
	customID := uuid.MustParse("01932f91-5f33-7000-8000-000000000000")
	cs2 := &CustomerSupport{ID: customID}
	err = cs2.BeforeCreate(nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cs2.ID != customID {
		t.Fatalf("expected preserved ID %s, got %s", customID, cs2.ID)
	}
}

func TestCustomerSupport_TableName(t *testing.T) {
	cs := CustomerSupport{}
	if cs.TableName() != "customer_supports" {
		t.Fatalf("expected table name 'customer_supports', got: '%s'", cs.TableName())
	}
}
