package git

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/herdworks/hyperproviders-go/secrets"
	"github.com/herdworks/hyperproviders-go/session"
	stdssh "golang.org/x/crypto/ssh"
)

const (
	secretToken         = "token"
	secretSSHPrivateKey = "ssh_private_key"
	secretSSHPassphrase = "ssh_passphrase"
)

// resolveAuth selects the right go-git transport AuthMethod based on the URL.
// HTTPS URLs use BasicAuth with a "token" secret; SSH URLs use NewPublicKeys
// with an "ssh_private_key" secret containing PEM bytes. Local file:// URLs
// (used in tests) need no auth.
func resolveAuth(ctx context.Context, runtime *session.Session, cfg *settings, secretSet secrets.Secrets) (transport.AuthMethod, error) {
	if strings.HasPrefix(cfg.RemoteURL, "file://") {
		return nil, nil
	}
	if isHTTP(cfg.RemoteURL) {
		return httpAuth(ctx, runtime, secretSet)
	}
	return sshAuth(ctx, runtime, cfg, secretSet)
}

func isHTTP(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func httpAuth(ctx context.Context, runtime *session.Session, secretSet secrets.Secrets) (transport.AuthMethod, error) {
	token, err := secretSet.Get(secretToken)
	if err != nil {
		return nil, err
	}
	value, err := runtime.ResolveSecret(ctx, token.LookupKey())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", secretToken, err)
	}
	return &githttp.BasicAuth{Username: "hyperproviders", Password: value.String()}, nil
}

func sshAuth(ctx context.Context, runtime *session.Session, cfg *settings, secretSet secrets.Secrets) (transport.AuthMethod, error) {
	pk, err := secretSet.Get(secretSSHPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("ssh auth requires %q", secretSSHPrivateKey)
	}
	value, err := runtime.ResolveSecret(ctx, pk.LookupKey())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", secretSSHPrivateKey, err)
	}
	pemBytes := value.Bytes()
	passphrase := ""

	if pp, err := secretSet.Get(secretSSHPassphrase); err == nil {
		v, err := runtime.ResolveSecret(ctx, pp.LookupKey())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", secretSSHPassphrase, err)
		}
		passphrase = v.String()
	}

	auth, err := gitssh.NewPublicKeys("git", pemBytes, passphrase)
	if err != nil {
		return nil, err
	}
	callback, err := hostKeyCallback(cfg.KnownHosts)
	if err != nil {
		return nil, err
	}
	auth.HostKeyCallback = callback
	return auth, nil
}

func hostKeyCallback(knownHosts string) (stdssh.HostKeyCallback, error) {
	if strings.TrimSpace(knownHosts) == "" {
		return stdssh.InsecureIgnoreHostKey(), nil
	}

	// go-git's known_hosts parser accepts files, so use a temporary file as a
	// parsing boundary while keeping provider config as literal known_hosts text.
	f, err := os.CreateTemp("", "hyperproviders-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("create known_hosts temp file: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)

	if _, err := f.WriteString(knownHosts); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write known_hosts temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close known_hosts temp file: %w", err)
	}
	callback, err := gitssh.NewKnownHostsCallback(name)
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}
	return callback, nil
}
