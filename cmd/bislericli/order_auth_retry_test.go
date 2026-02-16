package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bislericli/internal/store"
)

func TestConfirmLoginPromptAcceptsYes(t *testing.T) {
	confirmed, timedOut, err := confirmLoginPrompt(strings.NewReader("y\n"), io.Discard, time.Second)
	if err != nil {
		t.Fatalf("confirmLoginPrompt returned error: %v", err)
	}
	if timedOut {
		t.Fatalf("expected no timeout")
	}
	if !confirmed {
		t.Fatalf("expected confirmation=true")
	}
}

func TestConfirmLoginPromptTimesOut(t *testing.T) {
	reader, writer := io.Pipe()
	confirmed, timedOut, err := confirmLoginPrompt(reader, io.Discard, 25*time.Millisecond)
	_ = writer.Close()
	if err != nil {
		t.Fatalf("confirmLoginPrompt returned error: %v", err)
	}
	if confirmed {
		t.Fatalf("expected confirmation=false")
	}
	if !timedOut {
		t.Fatalf("expected timeout=true")
	}
}

func TestRefreshSessionForOrderPersistsCookies(t *testing.T) {
	oldLoginFn := otpLoginFn
	t.Cleanup(func() {
		otpLoginFn = oldLoginFn
	})

	expectedPhone := "9876543210"
	expectedCookies := []store.Cookie{
		{Name: "dwsid", Value: "new-session", Domain: ".bisleri.com", Path: "/"},
	}
	otpLoginFn = func(ctx context.Context, phone string) ([]store.Cookie, error) {
		if phone != expectedPhone {
			t.Fatalf("unexpected phone passed to OTP login: %s", phone)
		}
		return expectedCookies, nil
	}

	profilePath := filepath.Join(t.TempDir(), "default.json")
	profile := store.Profile{
		Name:        "default",
		PhoneNumber: expectedPhone,
		Cookies: []store.Cookie{
			{Name: "dwsid", Value: "old-session", Domain: ".bisleri.com", Path: "/"},
		},
	}
	if err := store.SaveProfile(profilePath, profile); err != nil {
		t.Fatalf("failed to seed profile: %v", err)
	}

	if err := refreshSessionForOrder(context.Background(), profilePath, &profile, strings.NewReader("\n"), io.Discard); err != nil {
		t.Fatalf("refreshSessionForOrder returned error: %v", err)
	}

	if len(profile.Cookies) != 1 || profile.Cookies[0].Value != "new-session" {
		t.Fatalf("profile cookies were not updated in memory: %#v", profile.Cookies)
	}
	if profile.LastLogin.IsZero() {
		t.Fatalf("expected LastLogin to be set")
	}

	saved, err := store.LoadProfile(profilePath)
	if err != nil {
		t.Fatalf("failed to reload profile: %v", err)
	}
	if len(saved.Cookies) != 1 || saved.Cookies[0].Value != "new-session" {
		t.Fatalf("profile cookies were not persisted: %#v", saved.Cookies)
	}
	if saved.LastLogin.IsZero() {
		t.Fatalf("expected persisted LastLogin to be set")
	}
}
