package main

import (
	"fmt"
	"log"
	"net/http"
)

func initializeStore() (*store, error) {
	s := newStore()
	if db, err := openDatabase(); err != nil {
		if !envBool("ALLOW_IN_MEMORY_STORE", false) {
			return nil, fmt.Errorf("MySQL is required; set ALLOW_IN_MEMORY_STORE=true only for local development: %w", err)
		}
		log.Printf("WARNING: MySQL disabled; using non-persistent in-memory storage: %v", err)
	} else {
		s.db = db
		if err := migrateDB(db); err != nil {
			return nil, fmt.Errorf("failed to migrate database: %w", err)
		}
		if err := loadStore(db, s); err != nil {
			return nil, fmt.Errorf("failed to load database: %w", err)
		}
		if err := s.purgeItemByTitle("Persistence Test Jacket"); err != nil {
			return nil, fmt.Errorf("failed to remove obsolete item: %w", err)
		}
		log.Printf("MySQL persistence enabled")
	}
	if len(s.users) == 0 {
		seed(s)
		if err := s.persistAll(); err != nil {
			return nil, fmt.Errorf("failed to seed database: %w", err)
		}
	}
	return s, nil
}

func main() {
	s, err := initializeStore()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		store:     s,
		jwtSecret: []byte(env("JWT_SECRET", "dev-secret-change-me")),
		ai:        newAssistant(),
	}

	addr := ":" + env("PORT", "8080")
	log.Printf("CapCycle API listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, withCORS(a.routes())))
}
