package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func TestResolveWalletPath_PersistentPath(t *testing.T) {
	tmpDir := t.TempDir()

	uploadDir := filepath.Join(tmpDir, WalletBasePath, "ds-uid")
	if err := os.MkdirAll(uploadDir, 0700); err != nil {
		t.Fatalf("failed to create upload dir: %v", err)
	}

	got := resolveWalletPath(tmpDir, "ds-uid")
	if got != uploadDir {
		t.Errorf("expected upload path %s, got %s", uploadDir, got)
	}
}

func TestResolveWalletPath_NeitherExists_ReturnsEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	got := resolveWalletPath(tmpDir, "ds-uid-no-wallet")
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

func TestResolveWalletPath_FallsBackToStaging(t *testing.T) {
	tmpDir := t.TempDir()
	uid := "ds-uid-staging-only"

	stagingDir := GetWalletStagingPath(uid)
	if err := os.MkdirAll(stagingDir, 0700); err != nil {
		t.Fatalf("failed to create staging dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(stagingDir) })

	got := resolveWalletPath(tmpDir, uid)
	if got != stagingDir {
		t.Errorf("expected staging path %s, got %s", stagingDir, got)
	}
}

func TestDispose_DoesNotDeleteWallet(t *testing.T) {
	tmpDir := t.TempDir()
	uid := "dispose-no-delete-uid"

	walletDir := filepath.Join(tmpDir, WalletBasePath, uid)
	if err := os.MkdirAll(walletDir, 0700); err != nil {
		t.Fatalf("failed to create wallet dir: %v", err)
	}

	ds := &OracleDatasource{dataPath: tmpDir, datasourceUID: uid}
	ds.Dispose()

	if !WalletExists(tmpDir, uid) {
		t.Errorf("Dispose should not delete the wallet; wallet was removed unexpectedly")
	}
}

func TestNewDatasource_RestoresWalletFromZip(t *testing.T) {
	tmpDir := t.TempDir()
	uid := "ds-uid-restore-test"

	// Build a minimal valid wallet zip using the shared test helper
	walletZip := createTestZip(t, map[string]string{
		"tnsnames.ora": "MYDB = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=adb.oracle.com)(PORT=1522))(CONNECT_DATA=(SERVICE_NAME=mydb_tp)))",
		"ewallet.sso":  "fake-sso-content",
	})

	// Encode as base64, matching what the frontend btoa() produces
	walletZipB64 := base64.StdEncoding.EncodeToString(walletZip)

	jsonData, _ := json.Marshal(map[string]interface{}{
		"o_walletMode": true,
	})

	// Simulate Grafana calling NewDatasource after a pod restart:
	// wallet files are absent from disk but the zip is in decryptedSecureJsonData
	t.Setenv("GF_PATHS_DATA", tmpDir)
	instance, err := NewDatasource(backend.DataSourceInstanceSettings{
		UID:      uid,
		JSONData: jsonData,
		DecryptedSecureJSONData: map[string]string{
			"o_walletZip": walletZipB64,
		},
	})
	if err != nil {
		t.Fatalf("NewDatasource returned error: %v", err)
	}

	ds := instance.(*OracleDatasource)
	if ds.settings.O_walletPath == "" {
		t.Fatal("expected wallet to be restored from zip, but O_walletPath is empty")
	}
	if !WalletExists(tmpDir, uid) {
		t.Errorf("wallet directory not found on disk after restore: %s", ds.settings.O_walletPath)
	}
}

func TestQueryData(t *testing.T) {
	ds := OracleDatasource{}

	resp, err := ds.QueryData(
		context.Background(),
		&backend.QueryDataRequest{
			Queries: []backend.DataQuery{
				{RefID: "A"},
			},
		},
	)
	if err != nil {
		t.Error(err)
	}

	if len(resp.Responses) != 1 {
		t.Fatal("QueryData must return a response")
	}
}
