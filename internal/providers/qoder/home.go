package qoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

const CredentialFormat = "qoder-native-v1"

type CredentialStore interface {
	LoadCredential(ctx context.Context, accountID string) (accounts.NativeCredential, error)
	SaveCredential(ctx context.Context, accountID, authType string, credential accounts.NativeCredential) error
}

func ConfigDirName(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "cn") {
		return ".qoder-cn"
	}
	return ".qoder"
}

func AuthDir(home, region string) string {
	return filepath.Join(home, ConfigDirName(region), ".auth")
}

func MaterializeHome(ctx context.Context, store CredentialStore, account accounts.Account, home string) error {
	authDir := AuthDir(home, account.ProviderRegion)
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		return fmt.Errorf("create account home: %w", err)
	}
	credential, err := store.LoadCredential(ctx, account.ID)
	if errors.Is(err, accounts.ErrAccountNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(authDir, "user"), credential.UserBlob, 0o600); err != nil {
		return fmt.Errorf("write user credential: %w", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "machine_id"), []byte(credential.MachineID), 0o600); err != nil {
		return fmt.Errorf("write machine id: %w", err)
	}
	return nil
}

func SyncCredential(ctx context.Context, store CredentialStore, account accounts.Account, home, authType string) error {
	authDir := AuthDir(home, account.ProviderRegion)
	userBlob, err := os.ReadFile(filepath.Join(authDir, "user"))
	if err != nil {
		return fmt.Errorf("read qoder user credential: %w", err)
	}
	machineID, err := os.ReadFile(filepath.Join(authDir, "machine_id"))
	if err != nil {
		return fmt.Errorf("read qoder machine id: %w", err)
	}
	return store.SaveCredential(ctx, account.ID, authType, accounts.NativeCredential{
		UserBlob:  userBlob,
		MachineID: string(machineID),
	})
}

type RuntimePaths struct {
	CLIPath   string
	Site      string
	ConfigDir string
	ConfigEnv string
}

func RuntimeSpec(cliPath, cnCLIPath, region, home string) (RuntimePaths, error) {
	region = strings.ToLower(strings.TrimSpace(region))
	configDir := filepath.Join(home, ConfigDirName(region))
	switch region {
	case "", "global":
		cliPath = strings.TrimSpace(cliPath)
		if cliPath == "" {
			return RuntimePaths{}, fmt.Errorf("qoder global CLI path required")
		}
		return RuntimePaths{CLIPath: cliPath, Site: "global", ConfigDir: configDir, ConfigEnv: "QODER_CONFIG_DIR"}, nil
	case "cn":
		cnCLIPath = strings.TrimSpace(cnCLIPath)
		if cnCLIPath == "" {
			return RuntimePaths{}, fmt.Errorf("qoder CN CLI path required: set QODERCNCLI_JS to @qodercn-ai/qoderclicn bundle/qoderclicn.js")
		}
		return RuntimePaths{CLIPath: cnCLIPath, Site: "cn", ConfigDir: configDir, ConfigEnv: "QODERCN_CONFIG_DIR"}, nil
	default:
		return RuntimePaths{}, fmt.Errorf("unknown qoder region %q", region)
	}
}
