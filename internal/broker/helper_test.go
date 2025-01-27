package broker_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/ubuntu/oidc-broker/internal/broker"
	"github.com/ubuntu/oidc-broker/internal/providers/group"
	"github.com/ubuntu/oidc-broker/internal/testutils"
	"golang.org/x/oauth2"
)

// newBrokerForTests is a helper function to create a new broker for tests and already starts a session for user
// t.Name() normalized.
func newBrokerForTests(t *testing.T, cachePath, issuerURL, mode string) (b *broker.Broker, id, key string) {
	t.Helper()

	cfg := broker.Config{
		IssuerURL: issuerURL,
		ClientID:  "test-client-id",
		CachePath: cachePath,
	}

	b, err := broker.New(
		cfg,
		broker.WithSkipSignatureCheck(),
		broker.WithCustomProviderInfo(&testutils.MockProviderInfoer{
			Groups: []group.Info{
				{Name: "remote-group", UGID: "12345"},
				{Name: "linux-local-group", UGID: ""},
			},
		}),
	)
	require.NoError(t, err, "Setup: New should not have returned an error")

	id, key, err = b.NewSession("test-user@email.com", "some lang", mode)
	require.NoError(t, err, "Setup: NewSession should not have returned an error")

	return b, id, key
}

func encryptChallenge(t *testing.T, challenge, strKey string) string {
	t.Helper()

	if strKey == "" {
		return challenge
	}

	pubASN1, err := base64.StdEncoding.DecodeString(strKey)
	require.NoError(t, err, "Setup: base64 decoding should not have failed")

	pubKey, err := x509.ParsePKIXPublicKey(pubASN1)
	require.NoError(t, err, "Setup: parsing public key should not have failed")

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	require.True(t, ok, "Setup: public key should be an RSA key")

	ciphertext, err := rsa.EncryptOAEP(sha512.New(), rand.Reader, rsaPubKey, []byte(challenge), nil)
	require.NoError(t, err, "Setup: encryption should not have failed")

	// encrypt it to base64 and replace the challenge with it
	return base64.StdEncoding.EncodeToString(ciphertext)
}

func updateAuthModes(t *testing.T, b *broker.Broker, sessionID, selectedMode string) {
	t.Helper()

	_, err := b.GetAuthenticationModes(sessionID, supportedLayouts)
	require.NoError(t, err, "Setup: GetAuthenticationModes should not have returned an error")
	_, err = b.SelectAuthenticationMode(sessionID, selectedMode)
	require.NoError(t, err, "Setup: SelectAuthenticationMode should not have returned an error")
}

var testTokens = map[string]broker.AuthCachedInfo{
	"valid": {
		Token: &oauth2.Token{
			AccessToken:  "accesstoken",
			RefreshToken: "refreshtoken",
			Expiry:       time.Now().Add(1000 * time.Hour),
		},
	},
	"expired": {
		Token: &oauth2.Token{
			AccessToken:  "accesstoken",
			RefreshToken: "refreshtoken",
			Expiry:       time.Now().Add(-1000 * time.Hour),
		},
	},
	"no-refresh": {
		Token: &oauth2.Token{
			AccessToken: "accesstoken",
			Expiry:      time.Now().Add(-1000 * time.Hour),
		},
	},
}

func generateCachedInfo(t *testing.T, preexistentToken, issuer string) *broker.AuthCachedInfo {
	t.Helper()

	if preexistentToken == "invalid" {
		return nil
	}

	var email string
	switch preexistentToken {
	case "no-email":
		email = ""
	case "other-email":
		email = "other-user@email.com"
	default:
		email = "test-user@email.com"
	}

	idToken := fmt.Sprintf(`{
		"iss": "%s",
		"sub": "saved-user-id",
		"aud": "test-client-id",
		"name": "saved-user",
		"exp": 9999999999,
		"preferred_username": "User Saved",
		"email": "%s",
		"email_verified": true
	}`, issuer, email)
	encodedToken := fmt.Sprintf(".%s.", base64.RawURLEncoding.EncodeToString([]byte(idToken)))

	tok, ok := testTokens[preexistentToken]
	if !ok {
		tok = testTokens["valid"]
	}

	tok.UserInfo = fmt.Sprintf(`{
		"name": "%[1]s",
		"uuid": "saved-user-id",
		"gecos": "saved-user",
		"dir": "/home/%[1]s",
		"shell": "/usr/bin/bash",
		"groups": [{"name": "saved-remote-group", "gid": "12345"}, {"name": "saved-local-group", "gid": ""}]
}`, email)

	if preexistentToken == "invalid-id" {
		encodedToken = ".invalid."
		tok.UserInfo = ""
	}

	tok.Token = tok.Token.WithExtra(map[string]string{"id_token": encodedToken})
	tok.RawIDToken = encodedToken

	return &tok
}
