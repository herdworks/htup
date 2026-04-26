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
)

const (
	secretToken         = "token"
	secretSSHPrivateKey = "ssh_private_key"
	secretSSHKeyPath    = "ssh_key_path"
	secretSSHPassphrase = "ssh_passphrase"
)

// resolveAuth selects the right go-git transport AuthMethod based on the URL.
// HTTPS URLs use BasicAuth with a "token" secret; SSH URLs use NewPublicKeys
// with either "ssh_private_key" (PEM bytes) or "ssh_key_path". Local file://
// URLs (used in tests) need no auth.
func resolveAuth(ctx context.Context, runtime *session.Session, remoteURL string, secretSet secrets.Secrets) (transport.AuthMethod, error) {
	if strings.HasPrefix(remoteURL, "file://") {
		return nil, nil
	}
	if isHTTP(remoteURL) {
		return httpAuth(ctx, runtime, secretSet)
	}
	return sshAuth(ctx, runtime, secretSet)
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

func sshAuth(ctx context.Context, runtime *session.Session, secretSet secrets.Secrets) (transport.AuthMethod, error) {
	var (
		pemBytes   []byte
		passphrase string
	)

	if pk, err := secretSet.Get(secretSSHPrivateKey); err == nil {
		value, err := runtime.ResolveSecret(ctx, pk.LookupKey())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", secretSSHPrivateKey, err)
		}
		pemBytes = value.Bytes()
	}

	if len(pemBytes) == 0 {
		pathSecret, err := secretSet.Get(secretSSHKeyPath)
		if err != nil {
			return nil, fmt.Errorf("ssh auth requires %q or %q", secretSSHPrivateKey, secretSSHKeyPath)
		}
		keyPath, err := runtime.ResolveSecret(ctx, pathSecret.LookupKey())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", secretSSHKeyPath, err)
		}
		keyPathValue := keyPath.String()
		b, err := os.ReadFile(keyPathValue)
		if err != nil {
			return nil, fmt.Errorf("read ssh key %s: %w", keyPathValue, err)
		}
		pemBytes = b
	}

	if pp, err := secretSet.Get(secretSSHPassphrase); err == nil {
		v, err := runtime.ResolveSecret(ctx, pp.LookupKey())
		if err == nil {
			passphrase = v.String()
		}
	}

	return gitssh.NewPublicKeys("git", pemBytes, passphrase)
}
