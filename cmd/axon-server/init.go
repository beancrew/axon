package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/beancrew/axon/pkg/auth"
	_ "modernc.org/sqlite"
)

func initCmd() *cobra.Command {
	var (
		flagListen  string
		flagDataDir string
		flagTLS     bool
		flagForce   bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize server configuration",
		Long:  "Creates a server config file, signs an admin token, and generates an initial join token. Does not start the server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagDataDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home dir: %w", err)
				}
				flagDataDir = filepath.Join(home, ".axon-server")
			}

			configPath := filepath.Join(flagDataDir, "config.yaml")

			if _, err := os.Stat(configPath); err == nil && !flagForce {
				return fmt.Errorf("config already exists at %s\nTo regenerate, use --force. To list tokens, run: axon token list", configPath)
			}

			// Generate 32-byte random JWT secret (64 hex chars).
			secretBuf := make([]byte, 32)
			if _, err := rand.Read(secretBuf); err != nil {
				return fmt.Errorf("generate JWT secret: %w", err)
			}
			jwtSecret := hex.EncodeToString(secretBuf)

			// Generate join token: "axon-join-" + 64 random hex chars.
			tokenBuf := make([]byte, 32)
			if _, err := rand.Read(tokenBuf); err != nil {
				return fmt.Errorf("generate join token: %w", err)
			}
			joinToken := "axon-join-" + hex.EncodeToString(tokenBuf)

			// Create data directory.
			if err := os.MkdirAll(flagDataDir, 0700); err != nil {
				return fmt.Errorf("create data dir %q: %w", flagDataDir, err)
			}

			// Open SQLite database and initialise the join token and CLI token stores.
			dbPath := filepath.Join(flagDataDir, "axon.db")
			if flagForce {
				// Remove the existing DB so we start with a clean state.
				if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove existing db %q: %w", dbPath, err)
				}
			}
			db, err := sql.Open("sqlite", dbPath)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer func() { _ = db.Close() }()
			if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
				return fmt.Errorf("set WAL: %w", err)
			}

			joinStore, err := auth.NewJoinTokenStoreFromDB(db)
			if err != nil {
				return fmt.Errorf("init join token store: %w", err)
			}

			tokenStore, err := auth.NewTokenStoreFromDB(db)
			if err != nil {
				return fmt.Errorf("init token store: %w", err)
			}

			// Hash and persist the join token.
			sum := sha256.Sum256([]byte(joinToken))
			joinTokenHash := hex.EncodeToString(sum[:])

			idBytes := make([]byte, 4)
			if _, err := rand.Read(idBytes); err != nil {
				return fmt.Errorf("generate join token ID: %w", err)
			}
			joinTokenID := hex.EncodeToString(idBytes)

			if err := joinStore.Insert(&auth.JoinTokenEntry{
				ID:        joinTokenID,
				TokenHash: joinTokenHash,
				CreatedAt: time.Now().Unix(),
			}); err != nil {
				return fmt.Errorf("insert join token: %w", err)
			}

			// Sign a no-expiry admin CLI token.
			adminToken, adminJTI, err := auth.SignCLIToken(jwtSecret, "admin", []string{"*"}, 0)
			if err != nil {
				return fmt.Errorf("sign admin token: %w", err)
			}

			// Persist the admin token so it can be listed and revoked.
			if err := tokenStore.Insert(&auth.TokenEntry{
				ID:       adminJTI,
				Kind:     string(auth.KindCLI),
				UserID:   "admin",
				NodeIDs:  []string{"*"},
				IssuedAt: time.Now().Unix(),
				// ExpiresAt = 0 means no expiry
			}); err != nil {
				return fmt.Errorf("persist admin token: %w", err)
			}

			// Construct and write config.yaml.
			auditDBPath := filepath.Join(flagDataDir, "audit.db")
			fc := fileConfig{
				Listen: flagListen,
				Auth: authConfig{
					JWTSigningKey: jwtSecret,
				},
				Data: dataConfig{
					DBPath: dbPath,
				},
				Audit: auditConfig{
					DBPath: auditDBPath,
				},
			}

			if flagTLS {
				t := true
				fc.TLS = tlsConfig{Auto: &t}
			}

			yamlData, err := yaml.Marshal(fc)
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}
			if err := os.WriteFile(configPath, yamlData, 0600); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "Server initialized")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintf(out, "   Config:     %s\n", configPath)
			_, _ = fmt.Fprintf(out, "   Database:   %s\n", dbPath)
			_, _ = fmt.Fprintf(out, "   Listen:     %s\n", flagListen)
			if flagTLS {
				_, _ = fmt.Fprintf(out, "   TLS:        enabled (auto-TLS)\n")
			} else {
				_, _ = fmt.Fprintf(out, "   TLS:        disabled\n")
			}
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintf(out, "   Admin token:  %s\n", adminToken)
			_, _ = fmt.Fprintf(out, "   Join token:   %s\n", joinToken)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Start the server:")
			_, _ = fmt.Fprintf(out, "   axon-server start\n")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Join a node:")
			if flagTLS {
				_, _ = fmt.Fprintf(out, "   axon-agent config set tls_insecure true\n")
			}
			_, _ = fmt.Fprintf(out, "   axon-agent join <SERVER_IP>%s %s\n", flagListen, joinToken)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Use CLI:")
			_, _ = fmt.Fprintf(out, "   axon config set server <SERVER_IP>%s\n", flagListen)
			if flagTLS {
				_, _ = fmt.Fprintf(out, "   axon config set tls_insecure true\n")
			}
			_, _ = fmt.Fprintf(out, "   axon config set token <admin-token>\n")
			_, _ = fmt.Fprintf(out, "   axon node list\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagListen, "listen", ":9090", "listen address")
	cmd.Flags().StringVar(&flagDataDir, "data-dir", "", "data directory (default: ~/.axon-server)")
	cmd.Flags().BoolVar(&flagTLS, "tls", false, "enable auto-TLS")
	cmd.Flags().BoolVar(&flagForce, "force", false, "overwrite existing config")

	return cmd
}
