package plugin

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: create a zip in memory with given file entries (name -> content)
func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %s: %v", name, err)
		}
		_, err = f.Write([]byte(content))
		if err != nil {
			t.Fatalf("failed to write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestValidateWalletZip_Valid(t *testing.T) {
	data := createTestZip(t, map[string]string{
		"tnsnames.ora": "MYDB = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.us-phoenix-1.oraclecloud.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_tp)))",
		"ewallet.sso":  "fake-wallet-content",
	})
	if err := ValidateWalletZip(data); err != nil {
		t.Errorf("expected valid wallet zip, got error: %v", err)
	}
}

func TestValidateWalletZip_Empty(t *testing.T) {
	if err := ValidateWalletZip([]byte{}); err == nil {
		t.Error("expected error for empty data")
	}
}

func TestValidateWalletZip_NotAZip(t *testing.T) {
	if err := ValidateWalletZip([]byte("this is not a zip")); err == nil {
		t.Error("expected error for non-zip data")
	}
}

func TestValidateWalletZip_MissingTnsnames(t *testing.T) {
	data := createTestZip(t, map[string]string{
		"ewallet.sso": "fake-wallet-content",
	})
	err := ValidateWalletZip(data)
	if err == nil {
		t.Error("expected error for missing tnsnames.ora")
	}
	if err != nil && !strings.Contains(err.Error(), "tnsnames.ora") {
		t.Errorf("error should mention tnsnames.ora, got: %v", err)
	}
}

func TestValidateWalletZip_MissingWalletFile(t *testing.T) {
	data := createTestZip(t, map[string]string{
		"tnsnames.ora": "MYDB = (DESCRIPTION=...)",
	})
	err := ValidateWalletZip(data)
	if err == nil {
		t.Error("expected error for missing wallet file")
	}
	if err != nil && !strings.Contains(err.Error(), "wallet file") {
		t.Errorf("error should mention wallet file, got: %v", err)
	}
}

func TestValidateWalletZip_TooLarge(t *testing.T) {
	// Create data larger than MaxWalletUploadSize
	largeData := make([]byte, MaxWalletUploadSize+1)
	err := ValidateWalletZip(largeData)
	if err == nil {
		t.Error("expected error for oversized data")
	}
	if err != nil && !strings.Contains(err.Error(), "maximum size") {
		t.Errorf("error should mention maximum size, got: %v", err)
	}
}

func TestExtractWallet(t *testing.T) {
	tnsnamesContent := `MYDB_HIGH = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.us-phoenix-1.oraclecloud.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_high)))
MYDB_LOW = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.us-phoenix-1.oraclecloud.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_low)))
`
	data := createTestZip(t, map[string]string{
		"tnsnames.ora": tnsnamesContent,
		"ewallet.sso":  "fake-wallet",
		"sqlnet.ora":   "WALLET_LOCATION = (SOURCE = (METHOD = file))",
		"malicious.sh": "#!/bin/bash\nrm -rf /",
	})

	tmpDir := t.TempDir()
	info, err := ExtractWallet(data, tmpDir, "test-ds-uid")
	if err != nil {
		t.Fatalf("ExtractWallet failed: %v", err)
	}

	// Check path
	expectedDir := filepath.Join(tmpDir, WalletBasePath, "test-ds-uid")
	if info.Path != expectedDir {
		t.Errorf("expected path %s, got %s", expectedDir, info.Path)
	}

	// Check TNS aliases parsed
	if len(info.TnsAliases) != 2 {
		t.Errorf("expected 2 TNS aliases, got %d: %v", len(info.TnsAliases), info.TnsAliases)
	}

	// Check that tnsnames.ora was extracted
	if _, err := os.Stat(filepath.Join(expectedDir, "tnsnames.ora")); err != nil {
		t.Errorf("tnsnames.ora not extracted: %v", err)
	}

	// Check that ewallet.sso was extracted
	if _, err := os.Stat(filepath.Join(expectedDir, "ewallet.sso")); err != nil {
		t.Errorf("ewallet.sso not extracted: %v", err)
	}

	// Check that malicious.sh was NOT extracted (not in allowed list)
	if _, err := os.Stat(filepath.Join(expectedDir, "malicious.sh")); !os.IsNotExist(err) {
		t.Error("malicious.sh should not have been extracted")
	}

	// Check file permissions (non-Windows)
	if fi, err := os.Stat(filepath.Join(expectedDir, "tnsnames.ora")); err == nil {
		perm := fi.Mode().Perm()
		if perm&0077 != 0 && os.Getenv("OS") != "Windows_NT" {
			t.Errorf("wallet file should have restricted permissions, got %o", perm)
		}
	}
}

func TestParseTnsnames(t *testing.T) {
	content := `# Comment line
MYDB_HIGH = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.us-phoenix-1.oraclecloud.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_high)))
MYDB_LOW = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.us-phoenix-1.oraclecloud.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_low)))
MYDB_MEDIUM = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.us-phoenix-1.oraclecloud.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_medium)))

# Duplicate should be deduped
MYDB_HIGH = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=other.host)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_high)))
`
	aliases, err := parseTnsnamesContent(strings.NewReader(content))
	if err != nil {
		t.Fatalf("parseTnsnamesContent failed: %v", err)
	}

	if len(aliases) != 3 {
		t.Errorf("expected 3 unique aliases, got %d: %v", len(aliases), aliases)
	}

	expected := map[string]bool{"MYDB_HIGH": true, "MYDB_LOW": true, "MYDB_MEDIUM": true}
	for _, a := range aliases {
		if !expected[a] {
			t.Errorf("unexpected alias: %s", a)
		}
	}
}

func TestParseTnsnames_Empty(t *testing.T) {
	aliases, err := parseTnsnamesContent(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected 0 aliases, got %d", len(aliases))
	}
}

func TestWalletExists(t *testing.T) {
	tmpDir := t.TempDir()
	uid := "test-uid-exists"

	if WalletExists(tmpDir, uid) {
		t.Error("wallet should not exist yet")
	}

	walletDir := filepath.Join(tmpDir, WalletBasePath, uid)
	if err := os.MkdirAll(walletDir, 0700); err != nil {
		t.Fatalf("failed to create wallet dir: %v", err)
	}

	if !WalletExists(tmpDir, uid) {
		t.Error("wallet should exist after creation")
	}
}

func TestCleanupWallet(t *testing.T) {
	tmpDir := t.TempDir()
	uid := "test-uid-cleanup"

	walletDir := filepath.Join(tmpDir, WalletBasePath, uid)
	if err := os.MkdirAll(walletDir, 0700); err != nil {
		t.Fatalf("failed to create wallet dir: %v", err)
	}

	// Write a file in the wallet dir
	if err := os.WriteFile(filepath.Join(walletDir, "test.ora"), []byte("test"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := CleanupWallet(tmpDir, uid); err != nil {
		t.Fatalf("CleanupWallet failed: %v", err)
	}

	if WalletExists(tmpDir, uid) {
		t.Error("wallet should not exist after cleanup")
	}
}

func TestIsAllowedWalletFile(t *testing.T) {
	allowed := []string{"tnsnames.ora", "sqlnet.ora", "ewallet.sso", "ewallet.p12", "cwallet.sso", "keystore.jks", "ojdbc.properties", "README"}
	for _, name := range allowed {
		if !isAllowedWalletFile(name) {
			t.Errorf("expected %s to be allowed", name)
		}
	}

	disallowed := []string{"malware.exe", "script.sh", "data.csv", "../tnsnames.ora"}
	for _, name := range disallowed {
		if isAllowedWalletFile(name) {
			t.Errorf("expected %s to be disallowed", name)
		}
	}
}
