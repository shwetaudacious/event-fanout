package redisutil

import "testing"

func TestNewClient_DefaultPort(t *testing.T) {
	client, err := NewClient("redis://localhost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Options().Addr != "localhost:6379" {
		t.Fatalf("expected default port, got %s", client.Options().Addr)
	}
}

func TestNewClient_WithPasswordAndDB(t *testing.T) {
	client, err := NewClient("redis://:secret@redis.example.com:6380/2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Options().Addr != "redis.example.com:6380" {
		t.Fatalf("unexpected addr: %s", client.Options().Addr)
	}
	if client.Options().Password != "secret" {
		t.Fatal("expected password to be set")
	}
	if client.Options().DB != 2 {
		t.Fatalf("expected db 2, got %d", client.Options().DB)
	}
}
