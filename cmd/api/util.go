package main

import (
	"encoding/base64"
	"os"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
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
