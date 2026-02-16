package auth

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"bislericli/internal/store"
)

func TestLoginWithOTPClientResendThenSuccess(t *testing.T) {
	oldGetCSRFTokenFn := getCSRFTokenFn
	oldSendOTPFn := sendOTPFn
	oldVerifyOTPFn := verifyOTPFn
	oldVerifyCookiesFn := verifyCookiesFn
	t.Cleanup(func() {
		getCSRFTokenFn = oldGetCSRFTokenFn
		sendOTPFn = oldSendOTPFn
		verifyOTPFn = oldVerifyOTPFn
		verifyCookiesFn = oldVerifyCookiesFn
	})

	getCSRFTokenFn = func(ctx context.Context, client *http.Client) (string, error) {
		return "csrf-token", nil
	}

	sendCalls := 0
	sendOTPFn = func(ctx context.Context, client *http.Client, phoneNumber, csrfToken string) error {
		sendCalls++
		return nil
	}

	verifyCalls := 0
	verifyOTPFn = func(ctx context.Context, client *http.Client, phoneNumber, otp, csrfToken string) ([]store.Cookie, error) {
		verifyCalls++
		if otp != "123456" {
			return nil, errors.New("unexpected otp")
		}
		return []store.Cookie{
			{Name: "dwsid", Value: "session", Domain: ".bisleri.com", Path: "/"},
		}, nil
	}

	verifyCookiesFn = func(cookies []store.Cookie) error {
		if len(cookies) == 0 {
			return errors.New("missing cookies")
		}
		return nil
	}

	var output bytes.Buffer
	cookies, err := loginWithOTPClient(
		context.Background(),
		&http.Client{},
		"9876543210",
		strings.NewReader("r\n123456\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("loginWithOTPClient returned error: %v", err)
	}
	if len(cookies) != 1 || cookies[0].Value != "session" {
		t.Fatalf("unexpected cookies returned: %#v", cookies)
	}
	if sendCalls != 2 {
		t.Fatalf("expected sendOTP to be called twice, got %d", sendCalls)
	}
	if verifyCalls != 1 {
		t.Fatalf("expected verifyOTP to be called once, got %d", verifyCalls)
	}
}

func TestLoginWithOTPClientResendLimitExceeded(t *testing.T) {
	oldGetCSRFTokenFn := getCSRFTokenFn
	oldSendOTPFn := sendOTPFn
	oldVerifyOTPFn := verifyOTPFn
	oldVerifyCookiesFn := verifyCookiesFn
	t.Cleanup(func() {
		getCSRFTokenFn = oldGetCSRFTokenFn
		sendOTPFn = oldSendOTPFn
		verifyOTPFn = oldVerifyOTPFn
		verifyCookiesFn = oldVerifyCookiesFn
	})

	getCSRFTokenFn = func(ctx context.Context, client *http.Client) (string, error) {
		return "csrf-token", nil
	}

	sendCalls := 0
	sendOTPFn = func(ctx context.Context, client *http.Client, phoneNumber, csrfToken string) error {
		sendCalls++
		return nil
	}

	verifyOTPFn = func(ctx context.Context, client *http.Client, phoneNumber, otp, csrfToken string) ([]store.Cookie, error) {
		return nil, errors.New("should not verify during resend-only sequence")
	}

	verifyCookiesFn = func(cookies []store.Cookie) error { return nil }

	_, err := loginWithOTPClient(
		context.Background(),
		&http.Client{},
		"9876543210",
		strings.NewReader("r\nr\nr\nr\n"),
		&bytes.Buffer{},
	)
	if err == nil {
		t.Fatalf("expected resend limit error")
	}
	if !strings.Contains(err.Error(), "OTP resend limit reached") {
		t.Fatalf("unexpected error: %v", err)
	}
	if sendCalls != 4 {
		t.Fatalf("expected 4 sendOTP calls (1 initial + 3 resend), got %d", sendCalls)
	}
}
