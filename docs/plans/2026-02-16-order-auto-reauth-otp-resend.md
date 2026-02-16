# Order Auto-Reauth + OTP Resend Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When `bislericli order` hits an expired session, ask for login confirmation; on approval, run OTP login and retry order once. Also add OTP resend on the OTP input screen.

**Architecture:** Keep auth recovery in CLI command orchestration (`cmd/bislericli/main.go`) instead of deep HTTP client hooks to avoid hidden side effects. Add a small prompt-with-timeout helper for explicit user consent. Refactor OTP login into testable functions with injectable IO/client behavior so resend and retry logic can be covered by unit tests.

**Tech Stack:** Go 1.23, standard `flag` CLI, package-local unit tests with `testing` and `httptest`.

---

### Task 1: Add confirmation prompt with 10s timeout (fail closed)

**Files:**
- Modify: `cmd/bislericli/main.go`
- Test: `cmd/bislericli/order_auth_retry_test.go`

**Step 1: Write the failing test**

```go
func TestConfirmLoginPrompt_TimesOutAfter10s(t *testing.T) {
    accepted, timedOut, err := confirmLoginPrompt(
        strings.NewReader(""),
        io.Discard,
        10*time.Millisecond,
    )
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if accepted {
        t.Fatalf("expected declined on timeout")
    }
    if !timedOut {
        t.Fatalf("expected timeout=true")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bislericli -run ConfirmLoginPrompt -v`
Expected: FAIL (helper not implemented).

**Step 3: Write minimal implementation**

- Add `confirmLoginPrompt(r io.Reader, w io.Writer, timeout time.Duration) (accepted bool, timedOut bool, err error)`.
- Prompt text:
  - `Session expired. Would you like to log in now? [y/N]`
  - `Waiting 10s for confirmation...`
- Accept only `y`/`yes` (case-insensitive).
- On timeout: return `accepted=false, timedOut=true`.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bislericli -run ConfirmLoginPrompt -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bislericli/main.go cmd/bislericli/order_auth_retry_test.go
git commit -m "test(cli): add login-confirmation timeout prompt tests"
```

### Task 2: Refactor order execution for single auth-retry path

**Files:**
- Modify: `cmd/bislericli/main.go`
- Test: `cmd/bislericli/order_auth_retry_test.go`

**Step 1: Write the failing test**

```go
func TestRunOrder_ExpiredSession_PromptTimeout_ManualLoginMessage(t *testing.T) {
    // Arrange: first order attempt returns bisleri.ErrNotAuthenticated.
    // Prompt input empty => timeout.
    // Assert returned error contains:
    // "session expired and confirmation timed out; run 'bislericli auth login'"
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bislericli -run ExpiredSession -v`
Expected: FAIL.

**Step 3: Write minimal implementation**

- Split current `runOrder` into:
  - `runOrder(args []string) error` (parsing + retry orchestration)
  - `runOrderOnce(ctx context.Context, opts orderOptions, profile *store.Profile, profilePath string) error`
- In `runOrder`, on `errors.Is(err, bisleri.ErrNotAuthenticated)`:
  - call `confirmLoginPrompt(..., 10*time.Second)`
  - if `accepted == false`:
    - if timeout: return `errors.New("session expired and confirmation timed out; run 'bislericli auth login'")`
    - if explicit no: return `errors.New("session expired; login cancelled; run 'bislericli auth login'")`
  - if accepted: call refresh helper (Task 3), then rerun `runOrderOnce` once.
- Guard with max 1 reauth retry.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bislericli -run ExpiredSession -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bislericli/main.go cmd/bislericli/order_auth_retry_test.go
git commit -m "feat(cli): add consent-gated auth recovery for order command"
```

### Task 3: Implement profile refresh helper for OTP login reuse

**Files:**
- Modify: `cmd/bislericli/main.go`
- Test: `cmd/bislericli/order_auth_retry_test.go`

**Step 1: Write the failing test**

```go
func TestRefreshSessionForProfile_UsesSavedPhoneAndPersistsCookies(t *testing.T) {
    // Arrange profile with PhoneNumber set.
    // Stub auth login function to return new cookies.
    // Assert profile file gets updated with new cookies and LastLogin.
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/bislericli -run RefreshSessionForProfile -v`
Expected: FAIL.

**Step 3: Write minimal implementation**

- Add `refreshSessionForProfile(ctx, profilePath string, profile *store.Profile, in io.Reader, out io.Writer) error`.
- Use `profile.PhoneNumber`; if missing, prompt for phone.
- Call OTP login through injected function variable:
  - `var otpLoginFn = auth.LoginWithOTP`
- Save updated `profile.Cookies` and `profile.LastLogin = time.Now()`.
- Keep existing profile fields untouched.

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/bislericli -run RefreshSessionForProfile -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bislericli/main.go cmd/bislericli/order_auth_retry_test.go
git commit -m "feat(cli): persist refreshed session during order reauth"
```

### Task 4: Add OTP resend loop on OTP screen

**Files:**
- Modify: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

**Step 1: Write the failing test**

```go
func TestLoginWithOTP_ResendThenVerifySuccess(t *testing.T) {
    // Input sequence: "resend\n123456\n"
    // First verify fails; resend triggers second sendOTP; second verify succeeds.
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/auth -run Resend -v`
Expected: FAIL.

**Step 3: Write minimal implementation**

- Refactor OTP flow:
  - keep initial `sendOTP`.
  - replace single prompt with loop:
    - prompt: `Enter OTP (6 digits) or type 'resend': `
    - if `resend`: call `sendOTP` and continue
    - else validate 6-digit OTP and call `verifyOTP`
    - on verify error: print message and stay in loop
- Add caps to avoid infinite loops:
  - max resend attempts: 3
  - max verify attempts: 5
- On caps reached, return actionable error with manual recovery hint.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/auth -run Resend -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go
git commit -m "feat(auth): add otp resend and retry loop for terminal login"
```

### Task 5: End-to-end guard tests for new order behavior

**Files:**
- Test: `cmd/bislericli/order_auth_retry_test.go`

**Step 1: Write failing tests**

```go
func TestRunOrder_ExpiredSession_UserDeclines_ShowsManualLoginMessage(t *testing.T) {}
func TestRunOrder_ExpiredSession_UserAccepts_ReauthAndRetriesOnce(t *testing.T) {}
func TestRunOrder_ExpiredSession_AfterRetryStillExpired_ReturnsError(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/bislericli -run RunOrder_ExpiredSession -v`
Expected: FAIL.

**Step 3: Implement minimal wiring/mocks**

- Inject dependencies in `runOrder` path for testability:
  - `confirmLoginPromptFn`
  - `refreshSessionForProfileFn`
  - `runOrderOnceFn`
- Keep defaults pointing to real functions.

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd/bislericli -run RunOrder_ExpiredSession -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/bislericli/order_auth_retry_test.go cmd/bislericli/main.go
git commit -m "test(cli): cover consent-gated session recovery flow"
```

### Task 6: Update CLI docs/help text

**Files:**
- Modify: `README.md`
- Modify: `cmd/bislericli/main.go` (auth/order help text)

**Step 1: Write doc expectation test (optional lightweight check)**

```go
func TestAuthUsageMentionsOtpResend(t *testing.T) {}
```

**Step 2: Run test/check to verify failure (if added)**

Run: `go test ./cmd/bislericli -run Usage -v`
Expected: FAIL.

**Step 3: Update docs/help**

- `README.md`:
  - `order` auto-detects expired session and asks for login confirmation.
  - if no confirmation within 10s, command exits with manual login hint.
  - OTP screen supports `resend`.
- `printAuthUsage` and/or relevant command help should mention resend capability.

**Step 4: Verify docs/help output**

Run: `go run ./cmd/bislericli help`
Expected: updated text visible.

**Step 5: Commit**

```bash
git add README.md cmd/bislericli/main.go
git commit -m "docs(cli): document consent-based reauth and otp resend"
```

### Task 7: Full verification and final integration commit

**Files:**
- Verify-only task.

**Step 1: Run full test suite**

Run: `go test ./...`
Expected: PASS.

**Step 2: Build binary**

Run: `go build ./cmd/bislericli`
Expected: PASS.

**Step 3: Manual verification script**

Run:

```bash
bislericli order
```

Expected behaviors:
- expired session -> prompt appears immediately
- no response for 10s -> exits and asks manual login
- `y` -> OTP login starts, then order retries once
- OTP screen allows `resend`

**Step 4: Final commit**

```bash
git add cmd/bislericli/main.go internal/auth/auth.go internal/auth/auth_test.go cmd/bislericli/order_auth_retry_test.go README.md
git commit -m "feat(cli): consent-gated order reauth with otp resend flow"
```

