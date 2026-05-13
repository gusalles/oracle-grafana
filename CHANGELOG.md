# Oracle Grafana Changelog

## 2.1.2
* **Stateless wallet persistence** - Oracle wallet zip files are now stored encrypted in Grafana's database (secureJsonData). This fixes wallet disappearance after Kubernetes pod restarts or reschedules. When wallet files are missing from disk, the plugin automatically restores them from the stored zip.
* **Improved reliability** - Wallets now survive container restarts without requiring PVCs or manual re-upload.

### Backend Changes
- **pkg/plugin/settings.go**: 
  - Added `encoding/base64` import
  - Added `O_walletZip []byte` field to OracleDatasourceSettings
  - Added base64 decoding logic in ParseDatasourceSettings to restore wallet zip from secureJsonData
- **pkg/plugin/datasource.go**:
  - Modified NewDatasource to restore wallet from O_walletZip when disk files are missing
  - Modified CheckHealth to restore wallet from O_walletZip when disk files are missing
  - Added logging for wallet restoration events
- **pkg/plugin/datasource_test.go**:
  - Added TestNewDatasource_RestoresWalletFromZip regression test simulating pod restart scenario

### Frontend Changes
- **src/types.ts**:
  - Added `o_walletZip?: string` to MySecureJsonData interface
- **src/components/ConfigEditor.tsx**:
  - Modified onFileSelected to base64-encode wallet zip using btoa() before upload
  - Store base64 zip in secureJsonData.o_walletZip on successful upload
  - Modified onWalletDelete to clear o_walletZip from secureJsonData and secureJsonFields
  - Updated delete success message to prompt "Save & Test to finalize"

## 2.1.1
* **Persistent wallet storage** - Oracle wallet files are now persisted to `/var/lib/grafana/oracle-wallets/` directory, surviving Grafana restarts when using persistent volumes.
* **TLS/SSL support** - Added support for TLS/SSL encrypted connections (TCPS protocol) when connecting to Oracle listeners configured for secure transport.
* **Wallet management improvements** - Added wallet upload, delete, and TNS alias parsing functionality.

## 2.1.0
* **Oracle Wallet mode support** - Added support for connecting to Oracle Cloud ATP/ADW databases using Oracle Wallet authentication.
* **Wallet upload interface** - Users can upload wallet zip files through the datasource configuration UI.
* **TNS alias selection** - Automatic parsing of tnsnames.ora from uploaded wallets with dropdown selection for service aliases.
* **Wallet password protection** - Secure storage of wallet passwords using Grafana's secureJsonData.

## 2.0.0
* **Multiple variable binding support** - Enhanced query engine to support binding multiple variables in a single SQL query.
* **Variable interpolation improvements** - Better handling of Grafana variables in SQL queries with proper escaping.
* **Query performance optimizations** - Improved query parsing and execution for complex variable scenarios.

## 1.0.1 
* support alerting
* support retrieve number/time/string

## 1.0.0 (Unreleased)

Initial release as a Datasource with internal backend support.

### Added
* Github Actions for making releases
* Scripts to make release files and update dev containers
* Support for variables on queries
* Support for Query variables

### Removed
* Dockerfile
* GitLab CI/CD
* NodeJs external service
* Pod for develop
* SonarQube scan (for now)

### Updated
* CHANGELOG file
* Config editor refactor to group information
* Docker compose file to develop
* Examples images
* Grafana framework
* Libraries
* README file
* Query editor refactor with SQL preview

## 0.9.0 (unreleased)

Import from https://github.com/JamesOsgood/mongodb-grafana

### Added
* Simple SELECT queries now works
* Dockerfile for containering
* Entire server rewrite using ES2020
* Examples with images and queries
* GitLab CI/CD
* Lint scan
* Oracle driver
* Winston/Morgan logger
* Pod for develop
* SonarQube scan

### Removed
* MongoDb driver

### Updated
* Lib updates

---
# TODO
* SonarQube scan
* Real tests
* Multiple value variables
