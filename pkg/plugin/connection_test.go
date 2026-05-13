package plugin

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildStandardConnectionString_TcpsEnablesSSL(t *testing.T) {
	connStr := `(DESCRIPTION=(ADDRESS=(PROTOCOL=TCPS)(HOST=myhost.example.com)(PORT=1523))(CONNECT_DATA=(SERVER=DEDICATED)(SERVICE_NAME=mysvc))(SECURITY=(SSL_SERVER_CERT_DN="CN=myhost.example.com,O=Acme,C=US")))`
	settings := &OracleDatasourceSettings{
		O_connStr:  connStr,
		O_user:     "user",
		O_password: "pass",
	}

	result := buildStandardConnectionString(settings)

	if !strings.Contains(result, "SSL=enable") && !strings.Contains(strings.ToLower(result), "ssl=enable") {
		t.Errorf("expected SSL=enable in connection string for TCPS descriptor, got: %s", result)
	}
}

func TestBuildStandardConnectionString_PlainTcpNoSSL(t *testing.T) {
	connStr := `(DESCRIPTION=(ADDRESS=(PROTOCOL=TCP)(HOST=myhost.example.com)(PORT=1521))(CONNECT_DATA=(SERVER=DEDICATED)(SERVICE_NAME=mysvc)))`
	settings := &OracleDatasourceSettings{
		O_connStr:  connStr,
		O_user:     "user",
		O_password: "pass",
	}

	result := buildStandardConnectionString(settings)

	if strings.Contains(strings.ToLower(result), "ssl=enable") {
		t.Errorf("expected no SSL=enable in connection string for plain TCP descriptor, got: %s", result)
	}
}

func TestBuildStandardConnectionString_TcpsLowercaseEnablesSSL(t *testing.T) {
	connStr := `(DESCRIPTION=(ADDRESS=(PROTOCOL=tcps)(HOST=myhost.example.com)(PORT=1523))(CONNECT_DATA=(SERVICE_NAME=mysvc)))`
	settings := &OracleDatasourceSettings{
		O_connStr:  connStr,
		O_user:     "user",
		O_password: "pass",
	}

	result := buildStandardConnectionString(settings)

	if !strings.Contains(result, "SSL=enable") && !strings.Contains(strings.ToLower(result), "ssl=enable") {
		t.Errorf("expected SSL=enable in connection string for lowercase tcps descriptor, got: %s", result)
	}
}

func TestBuildStandardConnectionString_TlsToggleEnablesSSL_HostPortService(t *testing.T) {
	settings := &OracleDatasourceSettings{
		O_hostname:   "myhost.example.com",
		O_port:       1523,
		O_service:    "mysvc",
		O_user:       "user",
		O_password:   "pass",
		O_tlsEnabled: true,
	}

	result := buildStandardConnectionString(settings)

	if !strings.Contains(strings.ToLower(result), "ssl=enable") {
		t.Errorf("expected SSL=enable when O_tlsEnabled is true for host/port/service, got: %s", result)
	}
}

func TestBuildStandardConnectionString_NoTlsToggle_HostPortService(t *testing.T) {
	settings := &OracleDatasourceSettings{
		O_hostname: "myhost.example.com",
		O_port:     1521,
		O_service:  "mysvc",
		O_user:     "user",
		O_password: "pass",
	}

	result := buildStandardConnectionString(settings)

	if strings.Contains(strings.ToLower(result), "ssl=enable") {
		t.Errorf("expected no SSL=enable when O_tlsEnabled is false for host/port/service, got: %s", result)
	}
}

func TestBuildStandardConnectionString_TcpsWithCommaDN_NotTruncated(t *testing.T) {
	// go-ora's BuildJDBC/BuildUrl splits option values on commas. A TNS descriptor
	// with SSL_SERVER_CERT_DN containing comma-separated DN fields (O=,L=,ST=,C=)
	// would be truncated at the first comma, sending a malformed descriptor to Oracle.
	// This test verifies the full descriptor is preserved intact.
	connStr := `(DESCRIPTION=(ADDRESS=(PROTOCOL=TCPS)(HOST=test123.test.com)(PORT=1234))(CONNECT_DATA=(SERVER=DEDICATED)(SERVICE_NAME=test_db))(SECURITY=(SSL_SERVER_CERT_DN="CN=test123.test.com,O=Test Inc.,L=Round Rock,ST=Texas,C=US")))`
	settings := &OracleDatasourceSettings{
		O_connStr:  connStr,
		O_user:     "user",
		O_password: "pass",
	}

	result := buildStandardConnectionString(settings)

	u, err := url.Parse(result)
	if err != nil {
		t.Fatalf("failed to parse returned URL: %v", err)
	}
	got := u.Query().Get("connStr")
	if got != connStr {
		t.Errorf("connStr was truncated or modified.\nexpected: %s\n     got: %s", connStr, got)
	}
}
