package plugin

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

const (
	MaxWalletUploadSize = 10 * 1024 * 1024 // 10 MB
	WalletBasePath      = "oracle-wallets"
)

// WalletInfo holds parsed wallet metadata after extraction.
type WalletInfo struct {
	Path       string   // directory where wallet files were extracted
	TnsAliases []string // service aliases parsed from tnsnames.ora
}

// ValidateWalletZip checks that the uploaded data is a valid zip archive
// within the size limit and contains the required wallet files.
func ValidateWalletZip(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("wallet file is empty")
	}
	if len(data) > MaxWalletUploadSize {
		return fmt.Errorf("wallet file exceeds maximum size of %d MB", MaxWalletUploadSize/(1024*1024))
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("invalid zip archive: %w", err)
	}

	hasTnsnames := false
	hasWalletFile := false
	for _, f := range reader.File {
		name := strings.ToLower(filepath.Base(f.Name))
		if name == "tnsnames.ora" {
			hasTnsnames = true
		}
		if name == "ewallet.sso" || name == "ewallet.p12" || name == "cwallet.sso" {
			hasWalletFile = true
		}
	}

	if !hasTnsnames {
		return fmt.Errorf("wallet zip must contain a tnsnames.ora file")
	}
	if !hasWalletFile {
		return fmt.Errorf("wallet zip must contain a wallet file (ewallet.sso, ewallet.p12, or cwallet.sso)")
	}

	return nil
}

// ExtractWallet extracts the wallet zip to a temp directory under the given base path.
// The datasourceUID is used to isolate wallets per datasource instance.
func ExtractWallet(data []byte, basePath string, datasourceUID string) (*WalletInfo, error) {
	walletDir := filepath.Join(basePath, WalletBasePath, datasourceUID)

	// Clean previous wallet if exists
	if err := os.RemoveAll(walletDir); err != nil {
		log.DefaultLogger.Warn("Could not remove old wallet directory", "path", walletDir, "error", err)
	}

	if err := os.MkdirAll(walletDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create wallet directory: %w", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to read wallet zip: %w", err)
	}

	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Only extract known wallet files to prevent zip slip and unwanted files
		name := strings.ToLower(filepath.Base(f.Name))
		if !isAllowedWalletFile(name) {
			log.DefaultLogger.Debug("Skipping non-wallet file in zip", "name", f.Name)
			continue
		}

		destPath := filepath.Join(walletDir, filepath.Base(f.Name))

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open file in zip: %w", err)
		}

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			rc.Close()
			return nil, fmt.Errorf("failed to create file %s: %w", destPath, err)
		}

		// Limit copy to prevent decompression bombs
		_, err = io.Copy(outFile, io.LimitReader(rc, MaxWalletUploadSize))
		rc.Close()
		outFile.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to extract file %s: %w", destPath, err)
		}
	}

	// Parse tnsnames.ora for aliases
	aliases, err := ParseTnsnames(filepath.Join(walletDir, "tnsnames.ora"))
	if err != nil {
		log.DefaultLogger.Warn("Could not parse tnsnames.ora", "error", err)
		aliases = []string{}
	}

	info := &WalletInfo{
		Path:       walletDir,
		TnsAliases: aliases,
	}

	log.DefaultLogger.Debug("Wallet extracted", "path", walletDir, "aliases", aliases)
	return info, nil
}

// CleanupWallet removes the wallet directory for a datasource.
func CleanupWallet(basePath string, datasourceUID string) error {
	safeBase := filepath.Clean(filepath.Join(basePath, WalletBasePath)) + string(filepath.Separator)
	walletDir := filepath.Clean(filepath.Join(basePath, WalletBasePath, datasourceUID))
	if !strings.HasPrefix(walletDir, safeBase) {
		return fmt.Errorf("invalid datasourceUID %q: path traversal detected", datasourceUID)
	}
	return os.RemoveAll(walletDir)
}

// ParseTnsnames reads a tnsnames.ora file and extracts service alias names.
func ParseTnsnames(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open tnsnames.ora: %w", err)
	}
	defer file.Close()

	return parseTnsnamesContent(file)
}

// parseTnsnamesContent parses tnsnames.ora content from a reader.
// It looks for lines matching: ALIAS_NAME = (DESCRIPTION=...)
var tnsnamesAliasRegex = regexp.MustCompile(`^([A-Za-z0-9_.\-]+)\s*=\s*\(`)

func parseTnsnamesContent(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	var aliases []string
	seen := make(map[string]bool)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		matches := tnsnamesAliasRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			alias := strings.ToUpper(matches[1])
			if !seen[alias] {
				seen[alias] = true
				aliases = append(aliases, alias)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return aliases, fmt.Errorf("error reading tnsnames.ora: %w", err)
	}

	return aliases, nil
}

// ResolveTnsAlias reads the tnsnames.ora file from the wallet directory and
// returns the full connection descriptor for the given alias.
// go-ora's BuildJDBC expects a full descriptor like (DESCRIPTION=...),
// not just a TNS alias name.
func ResolveTnsAlias(walletPath string, alias string) (string, error) {
	tnsnamesPath := filepath.Join(walletPath, "tnsnames.ora")
	data, err := os.ReadFile(tnsnamesPath)
	if err != nil {
		return "", fmt.Errorf("failed to read tnsnames.ora: %w", err)
	}

	content := string(data)
	upperAlias := strings.ToUpper(strings.TrimSpace(alias))

	// Find the alias definition in tnsnames.ora
	// Format: ALIAS_NAME = (DESCRIPTION=...)
	// Entries can span multiple lines; we match balanced parentheses.
	lines := strings.Split(content, "\n")
	found := false
	var descBuilder strings.Builder

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if !found {
			// Check if this line starts with the alias
			eqIdx := strings.Index(trimmed, "=")
			if eqIdx < 0 {
				continue
			}
			lineAlias := strings.ToUpper(strings.TrimSpace(trimmed[:eqIdx]))
			if lineAlias != upperAlias {
				continue
			}
			// Found the alias; start collecting the descriptor
			found = true
			descPart := strings.TrimSpace(trimmed[eqIdx+1:])
			descBuilder.WriteString(descPart)
		} else {
			// Check if this line starts a new alias (contains = before any paren)
			if tnsnamesAliasRegex.MatchString(trimmed) {
				break
			}
			descBuilder.WriteString(trimmed)
		}
	}

	if !found {
		return "", fmt.Errorf("TNS alias %q not found in tnsnames.ora", alias)
	}

	descriptor := strings.TrimSpace(descBuilder.String())
	if descriptor == "" {
		return "", fmt.Errorf("empty descriptor for TNS alias %q", alias)
	}

	// Validate balanced parentheses
	depth := 0
	for _, ch := range descriptor {
		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
		}
	}
	if depth != 0 {
		return "", fmt.Errorf("unbalanced parentheses in descriptor for TNS alias %q", alias)
	}

	return descriptor, nil
}

// isAllowedWalletFile checks if a filename is a recognized Oracle wallet file.
func isAllowedWalletFile(name string) bool {
	allowed := map[string]bool{
		"tnsnames.ora":     true,
		"sqlnet.ora":       true,
		"ewallet.sso":      true,
		"ewallet.p12":      true,
		"cwallet.sso":      true,
		"keystore.jks":     true,
		"ojdbc.properties": true,
		"readme":           true,
	}
	return allowed[strings.ToLower(name)]
}

// GetWalletPath returns the expected wallet directory path for a datasource.
func GetWalletPath(basePath string, datasourceUID string) string {
	return filepath.Join(basePath, WalletBasePath, datasourceUID)
}

// GetWalletStagingPath returns the temporary staging wallet directory for a datasource.
// Uploaded wallets are extracted here first and moved to persistent storage on Save & Test.
func GetWalletStagingPath(datasourceUID string) string {
	return filepath.Join(os.TempDir(), WalletBasePath, datasourceUID)
}

// MoveWalletToPersistent moves a wallet from the staging (temp) directory to a persistent base path.
// Falls back to a file-by-file copy if os.Rename fails across filesystem boundaries.
func MoveWalletToPersistent(persistentBasePath string, datasourceUID string) (string, error) {
	stagingDir := GetWalletStagingPath(datasourceUID)
	persistentDir := GetWalletPath(persistentBasePath, datasourceUID)

	// When GF_PATHS_DATA is not set, dataPath defaults to os.TempDir() which makes
	// staging and persistent the same path — nothing to move.
	if stagingDir == persistentDir {
		log.DefaultLogger.Debug("Staging and persistent wallet paths are identical, skipping move", "path", stagingDir)
		return persistentDir, nil
	}

	if err := os.RemoveAll(persistentDir); err != nil {
		log.DefaultLogger.Warn("Could not remove old persistent wallet directory", "path", persistentDir, "error", err)
	}

	if err := os.MkdirAll(filepath.Dir(persistentDir), 0700); err != nil {
		return "", fmt.Errorf("failed to create persistent wallet parent directory: %w", err)
	}

	// Try atomic rename first (works when staging and persistent are on the same filesystem)
	if err := os.Rename(stagingDir, persistentDir); err != nil {
		// Cross-filesystem move: copy files then remove staging
		if err2 := copyWalletDir(stagingDir, persistentDir); err2 != nil {
			return "", fmt.Errorf("failed to move wallet to persistent storage: %w", err2)
		}
		if err := os.RemoveAll(stagingDir); err != nil {
			log.DefaultLogger.Warn("Could not remove staging wallet after copy", "path", stagingDir, "error", err)
		}
	}

	log.DefaultLogger.Info("Wallet persisted", "from", stagingDir, "to", persistentDir)
	return persistentDir, nil
}

func cleanPath(path string, filename string) string {
	return filepath.Join(filepath.Clean(path), filepath.Base(filename))
}

// copyWalletDir copies wallet files from src to dst directory.
func copyWalletDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(cleanPath(src, entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", entry.Name(), err)
		}
		if err := os.WriteFile(cleanPath(dst, entry.Name()), data, 0600); err != nil {
			return fmt.Errorf("failed to write file %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// WalletExists checks if a wallet has been uploaded for the given datasource.
func WalletExists(basePath string, datasourceUID string) bool {
	walletDir := GetWalletPath(basePath, datasourceUID)
	info, err := os.Stat(walletDir)
	if err != nil {
		return false
	}
	return info.IsDir()
}
