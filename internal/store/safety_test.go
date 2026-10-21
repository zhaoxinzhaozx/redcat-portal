package store

import (
	"strings"
	"testing"
)

func TestRechargeSQLOnlyReadsPayments(t *testing.T) {
	sql := strings.ToUpper(RechargeSQL)
	if !strings.Contains(sql, "FROM PAYMENT_ORDERS") {
		t.Fatal("query should read payment orders")
	}
	for _, word := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "TRUNCATE"} {
		if strings.Contains(sql, word) {
			t.Fatalf("recharge query contains %s", word)
		}
	}
}

func TestMigrateStaysInsidePortalSchema(t *testing.T) {
	sql := strings.ToUpper(migrateSQL)
	for _, word := range []string{"PUBLIC.", "USERS", "PAYMENT_ORDERS", "DROP TABLE", "TRUNCATE", "DELETE FROM"} {
		if strings.Contains(sql, word) {
			t.Fatalf("migration contains %s", word)
		}
	}
}

func TestSchemaOnlyCreatesPortalTables(t *testing.T) {
	sql := strings.ToUpper(schemaSQL)
	if !strings.Contains(sql, "CREATE SCHEMA IF NOT EXISTS {{SCHEMA}}") {
		t.Fatal("schema name should come from configuration")
	}
	if strings.Contains(sql, "RED"+"CAT") {
		t.Fatal("schema SQL should not contain a fixed brand name")
	}
	for _, word := range []string{"DROP ", "TRUNCATE", "DELETE FROM", "UPDATE PUBLIC", "UPDATE USERS", "UPDATE PAYMENT_ORDERS"} {
		if strings.Contains(sql, word) {
			t.Fatalf("schema SQL contains %s", word)
		}
	}
}
