package main

import (
	"encoding/base64"
	"os"
	"strings"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func b64(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
