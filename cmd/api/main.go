package main

import (
	"log"
	"net/http"
)

func main() {
	s := newStore()
	if db, err := openDatabase(); err != nil {
		log.Printf("MySQL disabled: %v", err)
	} else {
		s.db = db
		if err := migrateDB(db); err != nil {
			log.Fatalf("failed to migrate database: %v", err)
		}
		if err := loadStore(db, s); err != nil {
			log.Fatalf("failed to load database: %v", err)
		}
		if err := s.purgeItemByTitle("Persistence Test Jacket"); err != nil {
			log.Fatalf("failed to remove obsolete item: %v", err)
		}
		log.Printf("MySQL persistence enabled")
	}
	if len(s.users) == 0 {
		seed(s)
		if err := s.persistAll(); err != nil {
			log.Fatalf("failed to seed database: %v", err)
		}
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
