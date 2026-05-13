package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Make sure Datasource implements required interfaces. This is important to do
// since otherwise we will only get a not implemented error response from plugin in
// runtime. In this example datasource instance implements backend.QueryDataHandler,
// backend.CheckHealthHandler interfaces. Plugin should not implement all these
// interfaces- only those which are required for a particular task.
var (
	_ backend.QueryDataHandler      = (*OracleDatasource)(nil)
	_ backend.CheckHealthHandler    = (*OracleDatasource)(nil)
	_ backend.CallResourceHandler   = (*OracleDatasource)(nil)
	_ instancemgmt.InstanceDisposer = (*OracleDatasource)(nil)
)

// Datasource is an example datasource which can respond to data queries, reports
// its health and has streaming skills.
type OracleDatasource struct {
	connMu        sync.Mutex
	connection    OracleDatasourceConnection
	dataPath      string
	datasourceUID string
	name          string
	settings      OracleDatasourceSettings
}

// NewDatasource creates a new datasource instance.
func NewDatasource(settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	datasourceSettings := ParseDatasourceSettings(settings.JSONData, settings.DecryptedSecureJSONData)
	log.DefaultLogger.Debug("New datasource", "name", settings.Name, "settings", datasourceSettings)

	// Determine data path for wallet storage.
	// NOTE: GF_PATHS_DATA is set in the Grafana process but Grafana spawns the plugin
	// subprocess with a controlled environment and does not forward that env var.
	// Fall back to /var/lib/grafana (Grafana's standard data directory) so wallets
	// are persisted across restarts even when GF_PATHS_DATA is not forwarded.
	dataPath := "/var/lib/grafana"
	if envPath := os.Getenv("GF_PATHS_DATA"); envPath != "" {
		dataPath = envPath
	}
	log.DefaultLogger.Debug("Resolved wallet data path", "GF_PATHS_DATA_env", os.Getenv("GF_PATHS_DATA"), "dataPath", dataPath)

	// Resolve wallet path if wallet mode is enabled
	if datasourceSettings.O_walletMode {
		datasourceSettings.O_walletPath = resolveWalletPath(dataPath, settings.UID)
		if datasourceSettings.O_walletPath == "" && len(datasourceSettings.O_walletZip) > 0 {
			log.DefaultLogger.Info("Wallet files missing from disk; restoring from stored zip", "uid", settings.UID)
			if info, err := ExtractWallet(datasourceSettings.O_walletZip, dataPath, settings.UID); err != nil {
				log.DefaultLogger.Error("Failed to restore wallet from stored zip", "uid", settings.UID, "error", err)
			} else {
				datasourceSettings.O_walletPath = info.Path
			}
		}
	}

	return &OracleDatasource{
		connection:    OracleDatasourceConnection{},
		dataPath:      dataPath,
		datasourceUID: settings.UID,
		name:          settings.Name,
		settings:      datasourceSettings,
	}, nil
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (d *OracleDatasource) CheckHealth(_ context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	d.connMu.Lock()
	defer d.connMu.Unlock()

	message := "Oracle datasource succesfully connected!"
	status := backend.HealthStatusOk

	d.name = req.PluginContext.DataSourceInstanceSettings.Name
	d.datasourceUID = req.PluginContext.DataSourceInstanceSettings.UID
	d.settings = ParseDatasourceSettings(req.PluginContext.DataSourceInstanceSettings.JSONData, req.PluginContext.DataSourceInstanceSettings.DecryptedSecureJSONData)
	log.DefaultLogger.Debug("Health check datasource settings", "name", d.name, "object", d.settings)

	// Resolve wallet path for wallet mode
	if d.settings.O_walletMode {
		// If a staging wallet exists (uploaded but not yet persisted), move it to
		// persistent storage now. This handles both new datasources and edits where
		// a new wallet replaces an existing persisted one.
		stagingPath := GetWalletStagingPath(d.datasourceUID)
		if _, err := os.Stat(stagingPath); err == nil {
			if persistedPath, err := MoveWalletToPersistent(d.dataPath, d.datasourceUID); err != nil {
				log.DefaultLogger.Warn("Failed to persist wallet, continuing with staging path", "error", err)
				d.settings.O_walletPath = stagingPath
			} else {
				d.settings.O_walletPath = persistedPath
			}
		} else {
			d.settings.O_walletPath = resolveWalletPath(d.dataPath, d.datasourceUID)
			if d.settings.O_walletPath == "" && len(d.settings.O_walletZip) > 0 {
				log.DefaultLogger.Info("Wallet files missing from disk; restoring from stored zip (CheckHealth)", "datasource", d.name)
				if info, err := ExtractWallet(d.settings.O_walletZip, d.dataPath, d.datasourceUID); err != nil {
					log.DefaultLogger.Error("Failed to restore wallet from stored zip", "datasource", d.name, "error", err)
				} else {
					d.settings.O_walletPath = info.Path
				}
			}
		}
		if d.settings.O_walletPath == "" {
			return &backend.CheckHealthResult{
				Message: "Wallet mode is enabled but no wallet has been uploaded. Please upload a wallet zip file.",
				Status:  backend.HealthStatusError,
			}, nil
		}
	}

	err := d.connection.Reconnect(&d.settings)
	if err != nil {
		message = "Health check error: " + err.Error()
		status = backend.HealthStatusError
	}

	return &backend.CheckHealthResult{
		Message: message,
		Status:  status,
	}, nil
}

// Dispose here tells plugin SDK that plugin wants to clean up resources when a new instance
// created. As soon as datasource settings change detected by SDK old datasource instance will
// be disposed and a new one will be created using NewSampleDatasource factory function.
func (d *OracleDatasource) Dispose() {
	d.connMu.Lock()
	err := d.connection.Disconnect()
	d.connMu.Unlock()
	if err != nil {
		log.DefaultLogger.Error("Error closing Oracle connection: ", err)
	}
}

// CallResource handles HTTP-style resource calls from the frontend.
// Routes:
//
//	POST /wallet-upload  - Upload a wallet zip file
//	GET  /wallet-aliases - List TNS aliases from the uploaded wallet
//	DELETE /wallet       - Remove the uploaded wallet
func (d *OracleDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	log.DefaultLogger.Debug("CallResource", "path", req.Path, "method", req.Method)

	switch req.Path {
	case "wallet-upload":
		return d.handleWalletUpload(ctx, req, sender)
	case "wallet-aliases":
		return d.handleWalletAliases(ctx, req, sender)
	case "wallet":
		if req.Method == http.MethodDelete {
			return d.handleWalletDelete(ctx, req, sender)
		}
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusMethodNotAllowed,
			Body:   []byte(`{"error": "method not allowed"}`),
		})
	default:
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusNotFound,
			Body:   []byte(`{"error": "resource not found"}`),
		})
	}
}

func (d *OracleDatasource) handleWalletUpload(_ context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req.Method != http.MethodPost {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusMethodNotAllowed,
			Body:   []byte(`{"error": "method not allowed"}`),
		})
	}

	if len(req.Body) == 0 {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusBadRequest,
			Body:   []byte(`{"error": "empty request body"}`),
		})
	}

	// Validate the zip
	if err := ValidateWalletZip(req.Body); err != nil {
		errMsg, _ := json.Marshal(map[string]string{"error": err.Error()})
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusBadRequest,
			Body:   errMsg,
		})
	}

	// Determine datasource UID
	dsUID := d.datasourceUID
	if req.PluginContext.DataSourceInstanceSettings != nil {
		dsUID = req.PluginContext.DataSourceInstanceSettings.UID
	}

	// Extract wallet to staging (temp) directory; persisted on Save & Test (CheckHealth)
	log.DefaultLogger.Debug("Wallet upload: extracting", "dsUID", dsUID, "stagingPath", GetWalletStagingPath(dsUID))
	info, err := ExtractWallet(req.Body, os.TempDir(), dsUID)
	if err != nil {
		errMsg, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("failed to extract wallet: %v", err)})
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   errMsg,
		})
	}

	// Update settings with wallet path
	d.settings.O_walletPath = info.Path

	respBody, _ := json.Marshal(map[string]interface{}{
		"message":    "Wallet uploaded successfully",
		"path":       filepath.Base(info.Path),
		"tnsAliases": info.TnsAliases,
	})

	return sender.Send(&backend.CallResourceResponse{
		Status:  http.StatusOK,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    respBody,
	})
}

func (d *OracleDatasource) handleWalletAliases(_ context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if req.Method != http.MethodGet {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusMethodNotAllowed,
			Body:   []byte(`{"error": "method not allowed"}`),
		})
	}

	dsUID := d.datasourceUID
	if req.PluginContext.DataSourceInstanceSettings != nil {
		dsUID = req.PluginContext.DataSourceInstanceSettings.UID
	}

	// Check staging first (freshly uploaded), then persistent
	walletDir := GetWalletStagingPath(dsUID)
	if _, err := os.Stat(filepath.Join(walletDir, "tnsnames.ora")); os.IsNotExist(err) {
		walletDir = GetWalletPath(d.dataPath, dsUID)
	}
	tnsnamesPath := filepath.Join(walletDir, "tnsnames.ora")

	if _, err := os.Stat(tnsnamesPath); os.IsNotExist(err) {
		return sender.Send(&backend.CallResourceResponse{
			Status:  http.StatusOK,
			Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body:    []byte(`{"tnsAliases": [], "uploaded": false}`),
		})
	}

	aliases, err := ParseTnsnames(tnsnamesPath)
	if err != nil {
		errMsg, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("failed to parse tnsnames.ora: %v", err)})
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   errMsg,
		})
	}

	respBody, _ := json.Marshal(map[string]interface{}{
		"tnsAliases": aliases,
		"uploaded":   true,
	})

	return sender.Send(&backend.CallResourceResponse{
		Status:  http.StatusOK,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    respBody,
	})
}

func (d *OracleDatasource) handleWalletDelete(_ context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	dsUID := d.datasourceUID
	if req.PluginContext.DataSourceInstanceSettings != nil {
		dsUID = req.PluginContext.DataSourceInstanceSettings.UID
	}

	if err := CleanupWallet(d.dataPath, dsUID); err != nil {
		errMsg, _ := json.Marshal(map[string]string{"error": fmt.Sprintf("failed to delete wallet: %v", err)})
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   errMsg,
		})
	}
	// Also remove staging copy
	os.RemoveAll(GetWalletStagingPath(dsUID))

	d.settings.O_walletPath = ""

	respBody, _ := json.Marshal(map[string]string{"message": "Wallet deleted successfully"})
	return sender.Send(&backend.CallResourceResponse{
		Status:  http.StatusOK,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    respBody,
	})
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifier).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (d *OracleDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	d.connMu.Lock()
	defer d.connMu.Unlock()

	// create response struct
	response := backend.NewQueryDataResponse()

	// Check connection health and reconnect if needed
	if !d.connection.IsConnected() {
		log.DefaultLogger.Debug("Connection not available, attempting to connect", "datasource", d.name)
		err := d.connection.Connect(&d.settings)
		if err != nil {
			log.DefaultLogger.Error("Failed to establish database connection", "datasource", d.name, "error", err)
			// Return error for all queries
			for _, q := range req.Queries {
				response.Responses[q.RefID] = backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("Error connecting to datasource: %v", err.Error()))
			}
			return response, nil
		}
		log.DefaultLogger.Debug("Successfully connected to database", "datasource", d.name)
	}

	// loop over queries and execute them individually.
	for _, q := range req.Queries {
		res := d.query(ctx, q)
		// save the response in a hashmap
		// based on with RefID as identifier
		response.Responses[q.RefID] = res
	}

	return response, nil
}

// resolveWalletPath returns the effective wallet directory path.
// Checks the persistent upload path first, then the staging path (uploaded but not yet persisted).
func resolveWalletPath(dataPath string, datasourceUID string) string {
	// 1. Persistent upload path (moved here after Save & Test)
	persistentPath := GetWalletPath(dataPath, datasourceUID)
	log.DefaultLogger.Debug("resolveWalletPath: checking persistent", "path", persistentPath, "exists", WalletExists(dataPath, datasourceUID))
	if WalletExists(dataPath, datasourceUID) {
		return persistentPath
	}
	// 2. Staging path (uploaded but Save & Test not yet clicked)
	stagingPath := GetWalletStagingPath(datasourceUID)
	_, statErr := os.Stat(stagingPath)
	log.DefaultLogger.Debug("resolveWalletPath: checking staging", "path", stagingPath, "statErr", statErr)
	if statErr == nil {
		log.DefaultLogger.Debug("Using staging wallet path", "path", stagingPath)
		return stagingPath
	}
	return ""
}

func (d *OracleDatasource) query(_ context.Context, query backend.DataQuery) backend.DataResponse {
	var response backend.DataResponse

	queryObj := OracleDatasourceQuery{}
	err := queryObj.ParseDatasourceQuery(query)
	log.DefaultLogger.Debug(fmt.Sprintf("Executing new query: %+v", queryObj))
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("Error parsing query: %v", err.Error()))
	}

	result := queryObj.MakeQuery(&d.connection, query.TimeRange.From, query.TimeRange.To)

	// Check if query execution resulted in an error
	if result.err != nil {
		log.DefaultLogger.Error("Query execution failed", "error", result.err, "query", queryObj.O_parsed)
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("Error executing query: %v", result.err.Error()))
	}

	// create data frame response.
	// For an overview on data frames and how grafana handles them:
	// https://grafana.com/docs/grafana/latest/developers/plugins/data-frames/
	frame := data.NewFrame("response")

	// add fields.
	for _, column := range result.columns {
		values := ConvertValueArray(column.dataType, column.values)
		frame.Fields = append(frame.Fields, data.NewField(column.name, nil, values))
	}

	// add the frames to the response.
	response.Frames = append(response.Frames, frame)

	return response
}
