package plugin

import (
	"encoding/base64"
	"encoding/json"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

type OracleDatasourceSettings struct {
	O_connStr    string
	O_hostname   string
	O_password   string `json:"-"`
	O_port       int
	O_service    string
	O_sid        string
	O_tlsEnabled bool `json:"o_tlsEnabled"`
	O_user       string

	// Wallet settings
	O_walletMode     bool   `json:"o_walletMode"`
	O_walletPassword string `json:"-"`
	O_walletPath     string `json:"-"`
	O_walletTnsAlias string `json:"o_walletTnsAlias"`
	O_walletZip      []byte `json:"-"`
}

func ParseDatasourceSettings(rawOptions json.RawMessage, decryptedOptions map[string]string) OracleDatasourceSettings {
	settings := OracleDatasourceSettings{}
	err := json.Unmarshal(rawOptions, &settings)
	if err != nil {
		log.DefaultLogger.Error("Error parsing Oracle datasource settings: ", err)
	}
	// Set passwords AFTER unmarshal to ensure decrypted secure values are never overwritten
	settings.O_password = decryptedOptions["o_password"]
	settings.O_walletPassword = decryptedOptions["o_walletPassword"]

	if raw := decryptedOptions["o_walletZip"]; raw != "" {
		if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
			settings.O_walletZip = decoded
		} else {
			log.DefaultLogger.Warn("Failed to decode o_walletZip from secureJsonData", "error", err)
		}
	}

	log.DefaultLogger.Debug("Parsed datasource settings",
		"user", settings.O_user,
		"hasPassword", len(settings.O_password) > 0,
		"passwordLen", len(settings.O_password),
		"walletMode", settings.O_walletMode,
		"walletAlias", settings.O_walletTnsAlias,
	)
	return settings
}
