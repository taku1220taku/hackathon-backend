package main

import "testing"

func configureUnavailableTestDatabase(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CLOUD_SQL_CONNECTION_NAME", "")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "1")
}

func TestInitializeStoreRequiresDatabaseByDefault(t *testing.T) {
	configureUnavailableTestDatabase(t)
	t.Setenv("ALLOW_IN_MEMORY_STORE", "false")

	if _, err := initializeStore(); err == nil {
		t.Fatal("expected initialization to fail without MySQL")
	}
}

func TestInitializeStoreAllowsExplicitDevelopmentMemoryMode(t *testing.T) {
	configureUnavailableTestDatabase(t)
	t.Setenv("ALLOW_IN_MEMORY_STORE", "true")

	store, err := initializeStore()
	if err != nil {
		t.Fatalf("expected explicit in-memory mode to start: %v", err)
	}
	if store.db != nil {
		t.Fatal("expected in-memory store")
	}
	if len(store.users) == 0 {
		t.Fatal("expected demo seed data")
	}
}
