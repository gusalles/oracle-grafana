package plugin

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	go_ora "github.com/sijms/go-ora/v2"
)

type OracleDatasourceConnection struct {
	connection *go_ora.Connection
}

func (c *OracleDatasourceConnection) Connect(settings *OracleDatasourceSettings) error {
	var connectionString string
	var err error
	if !c.IsConnected() {
		if settings.O_walletMode {
			connectionString, err = buildWalletConnectionString(settings)
			if err != nil {
				return err
			}
		} else {
			connectionString = buildStandardConnectionString(settings)
		}

		log.DefaultLogger.Debug("Connecting to Oracle:",
			"connStr", sanitizeConnStr(connectionString, settings),
			"tlsEnabled", settings.O_tlsEnabled,
			"hasConnStr", len(settings.O_connStr) > 0,
			"walletMode", settings.O_walletMode)
		connection, conErr := go_ora.NewConnection(connectionString, nil)
		if conErr != nil {
			log.DefaultLogger.Error("Error creating Oracle connection: ", conErr)
			err = conErr
		} else {
			conErr = connection.Open()
			if conErr != nil {
				log.DefaultLogger.Error("Error opening Oracle connection: ", conErr)
				err = conErr
			} else {
				c.connection = connection
				err = c.Ping()
			}
		}
	}
	return err
}

// buildStandardConnectionString creates a connection string using host/port/service or connStr.
func buildStandardConnectionString(settings *OracleDatasourceSettings) string {
	urlOptions := map[string]string{
		"SSL Verify": "false",
	}
	if len(settings.O_sid) > 0 {
		urlOptions["SID"] = settings.O_sid
	}
	if settings.O_tlsEnabled {
		urlOptions["SSL"] = "enable"
	}

	if len(settings.O_connStr) > 0 {
		if strings.Contains(strings.ToUpper(settings.O_connStr), "TCPS") {
			urlOptions["SSL"] = "enable"
		}
		// go-ora's BuildJDBC delegates to BuildUrl, which splits every option value
		// on commas before URL-encoding. TNS descriptors with SSL_SERVER_CERT_DN
		// contain comma-separated DN fields (O=, L=, ST=, C=), so BuildJDBC
		// silently truncates the descriptor at the first comma. Oracle then
		// receives a malformed descriptor and refuses with ORA-12564.
		// Build the URL manually to encode the full descriptor as one parameter.
		connURL := fmt.Sprintf("oracle://%s:%s@:0/?connStr=%s",
			url.PathEscape(settings.O_user),
			url.PathEscape(settings.O_password),
			url.QueryEscape(settings.O_connStr))
		for k, v := range urlOptions {
			connURL += fmt.Sprintf("&%s=%s", url.QueryEscape(k), url.QueryEscape(v))
		}
		return connURL
	}
	return go_ora.BuildUrl(settings.O_hostname, settings.O_port, settings.O_service, settings.O_user, settings.O_password, urlOptions)
}

// buildWalletConnectionString creates a connection string using the Oracle wallet.
// If username/password are provided, they override wallet-embedded credentials.
func buildWalletConnectionString(settings *OracleDatasourceSettings) (string, error) {
	walletPath := settings.O_walletPath
	if walletPath == "" {
		return "", fmt.Errorf("wallet mode is enabled but no wallet has been uploaded")
	}

	// Verify wallet directory exists
	if _, err := os.Stat(walletPath); os.IsNotExist(err) {
		return "", fmt.Errorf("wallet directory does not exist: %s", walletPath)
	}

	alias := settings.O_walletTnsAlias
	if alias == "" {
		return "", fmt.Errorf("wallet mode requires a TNS alias to be selected")
	}

	// Resolve the TNS alias to a full connection descriptor from tnsnames.ora.
	// go-ora's BuildJDBC expects a descriptor like (DESCRIPTION=(ADDRESS=...)),
	// not a plain alias name.
	descriptor, err := ResolveTnsAlias(walletPath, alias)
	if err != nil {
		return "", fmt.Errorf("failed to resolve TNS alias %q: %w", alias, err)
	}

	log.DefaultLogger.Debug("Resolved TNS alias", "alias", alias, "descriptor", descriptor)

	// Use username/password if provided, otherwise fall back to wallet credentials
	user := settings.O_user
	password := settings.O_password

	urlOptions := map[string]string{
		"wallet":     walletPath,
		"SSL":        "enable",
		"SSL Verify": "false",
	}

	if len(user) > 0 && len(password) > 0 {
		// User/password override: connect with explicit credentials + wallet for encryption
		connStr := go_ora.BuildJDBC(user, password, descriptor, urlOptions)
		return connStr, nil
	}

	// Wallet-only auth: use /@alias style (empty user/password)
	connStr := go_ora.BuildJDBC("", "", descriptor, urlOptions)
	return connStr, nil
}

// sanitizeConnStr removes passwords from connection strings for logging.
func sanitizeConnStr(connStr string, settings *OracleDatasourceSettings) string {
	result := connStr
	if len(settings.O_password) > 0 {
		result = strings.Replace(result, settings.O_password, "********", 1)
	}
	if len(settings.O_walletPassword) > 0 {
		result = strings.Replace(result, settings.O_walletPassword, "********", 1)
	}
	return result
}

func (c *OracleDatasourceConnection) Disconnect() error {
	var err error
	if c.IsConnected() {
		err = c.connection.Close()
		if err != nil {
			log.DefaultLogger.Error("Error closing Oracle connection: ", err)
		}
		c.connection = nil
	}
	return err
}

func (c *OracleDatasourceConnection) IsConnected() bool {
	if c.connection == nil {
		return false
	}
	// Wrap Ping in a recover: go-ora may panic on broken connections
	ok := make(chan bool, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.DefaultLogger.Warn("Connection ping panicked, marking as disconnected", "panic", fmt.Sprintf("%v", r))
				c.connection = nil
				ok <- false
			}
		}()
		err := c.connection.Ping(context.Background())
		if err != nil {
			log.DefaultLogger.Warn("Connection ping failed, marking as disconnected", "error", err)
			c.connection = nil
			ok <- false
			return
		}
		ok <- true
	}()
	return <-ok
}

func (c *OracleDatasourceConnection) Ping() error {
	var err error
	if c.IsConnected() {
		err = c.connection.Ping(context.Background())
		if err != nil {
			log.DefaultLogger.Error("Error pinging Oracle connection: ", err)
		}
	}
	return err
}

func (c *OracleDatasourceConnection) Reconnect(settings *OracleDatasourceSettings) error {
	var err error
	if c.IsConnected() {
		err = c.Disconnect()
	}
	err = c.Connect(settings)
	return err
}
