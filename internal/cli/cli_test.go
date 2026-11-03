package cli

import (
	"strings"
	"testing"

	"ctrldown/cli/internal/auth"
	"ctrldown/cli/internal/fs"
)

func TestMatchReleaseAssetMultiOS(t *testing.T) {
	assets := []githubAsset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		{Name: "ctrldown_darwin_amd64", BrowserDownloadURL: "https://example.com/ctrldown_darwin_amd64"},
		{Name: "ctrldown_darwin_arm64", BrowserDownloadURL: "https://example.com/ctrldown_darwin_arm64"},
		{Name: "ctrldown_linux_amd64", BrowserDownloadURL: "https://example.com/ctrldown_linux_amd64"},
		{Name: "ctrldown_linux_arm64", BrowserDownloadURL: "https://example.com/ctrldown_linux_arm64"},
		{Name: "ctrldown_windows_amd64.exe", BrowserDownloadURL: "https://example.com/ctrldown_windows_amd64.exe"},
		{Name: "ctrldown_windows_arm64.exe", BrowserDownloadURL: "https://example.com/ctrldown_windows_arm64.exe"},
	}

	cases := []struct {
		goos     string
		goarch   string
		expected string
	}{
		{"windows", "amd64", "ctrldown_windows_amd64.exe"},
		{"windows", "arm64", "ctrldown_windows_arm64.exe"},
		{"linux", "amd64", "ctrldown_linux_amd64"},
		{"linux", "arm64", "ctrldown_linux_arm64"},
		{"darwin", "amd64", "ctrldown_darwin_amd64"},
		{"darwin", "arm64", "ctrldown_darwin_arm64"},
	}

	for _, tc := range cases {
		got, ok := MatchReleaseAsset(assets, tc.goos, tc.goarch)
		if !ok {
			t.Fatalf("expected asset match for %s/%s", tc.goos, tc.goarch)
		}
		if got.Name != tc.expected {
			t.Fatalf("for %s/%s expected %s, got %s", tc.goos, tc.goarch, tc.expected, got.Name)
		}
	}
}

func TestVersionConfigAndAuthEntitlements(t *testing.T) {
	t.Setenv("CTRLDOWN_HOME", "/test-home")
	memFS := fs.NewMemory()

	verStr := FormatVersionString()
	if !strings.Contains(verStr, "ctrldown v") || !strings.Contains(verStr, "schema: v2") {
		t.Fatalf("unexpected version string: %s", verStr)
	}

	// 1. Anonymous user: free microfeatures (bloub, auth) are unlocked, pro (ctrlweb) is locked
	if !auth.IsEntitled(nil, "bloub", "free") {
		t.Fatalf("expected 'bloub' (free) to be unlocked for anonymous user")
	}
	if auth.IsEntitled(nil, "ctrlweb", "pro") {
		t.Fatalf("expected 'ctrlweb' (pro) to be locked for anonymous user")
	}

	// 2. Login via CLI with Pro tier and verify ctrlweb is unlocked
	root := NewRootCmd(memFS)
	root.SetArgs([]string{"login", "--token", "cf_edge_tok_123", "--email", "dev@ctrldown.dev", "--tier", "pro"})
	if err := root.Execute(); err != nil {
		t.Fatalf("login command failed: %v", err)
	}

	creds, err := auth.LoadCredentials(memFS)
	if err != nil {
		t.Fatalf("LoadCredentials failed: %v", err)
	}
	if !auth.IsEntitled(creds, "ctrlweb", "pro") {
		t.Fatalf("expected 'ctrlweb' to be unlocked for pro tier user")
	}
	if auth.IsEntitled(creds, "billing-worker", "enterprise") {
		t.Fatalf("expected 'billing-worker' (enterprise) to remain locked for pro tier user")
	}

	// 3. Logout clears credentials
	root.SetArgs([]string{"logout"})
	if err := root.Execute(); err != nil {
		t.Fatalf("logout command failed: %v", err)
	}
	if _, err := auth.LoadCredentials(memFS); err == nil {
		t.Fatalf("expected error loading credentials after logout")
	}
}
