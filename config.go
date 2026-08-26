package main

import (
	"encoding/hex"
	"log"
	"os"
	"strconv"
	"strings"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func decodePATSecret(raw string) []byte {
	if raw == "" {
		log.Printf("warning: KAN_PAT_SECRET unset — personal access token issuance disabled")
		return nil
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		log.Fatalf("KAN_PAT_SECRET must be a hex-encoded string: %v", err)
	}
	return b
}
