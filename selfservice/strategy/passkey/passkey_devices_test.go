package passkey_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/ory/kratos/identity"
	"github.com/ory/kratos/pkg/testhelpers"
	"github.com/ory/kratos/ui/node"
)

type deviceProfile struct {
	name              string
	transport         protocol.AuthenticatorTransport
	attachment        protocol.AuthenticatorAttachment
	aaguid            [16]byte
	userVerified      bool
	backupEligible    bool
	backupState       bool
	registrationCount uint32
}

type softwarePasskey struct {
	profile    deviceProfile
	privateKey *ecdsa.PrivateKey
	id         []byte
	userHandle []byte
	counter    uint32
}

func TestPasskeyRegistrationAndLoginOnThreeDevices(t *testing.T) {
	fix := newRegistrationFixture(t)
	client := testhelpers.NewDebugClient(t)

	devices := []deviceProfile{
		{
			name:              "platform-syncable-passkey",
			transport:         protocol.Internal,
			attachment:        protocol.Platform,
			aaguid:            [16]byte{0x01},
			userVerified:      true,
			backupEligible:    true,
			backupState:       true,
			registrationCount: 1,
		},
		{
			name:              "roaming-usb-security-key",
			transport:         protocol.USB,
			attachment:        protocol.CrossPlatform,
			aaguid:            [16]byte{0x02},
			userVerified:      true,
			backupEligible:    false,
			backupState:       false,
			registrationCount: 7,
		},
		{
			name:              "hybrid-phone-passkey",
			transport:         protocol.Hybrid,
			attachment:        protocol.CrossPlatform,
			aaguid:            [16]byte{0x03},
			userVerified:      true,
			backupEligible:    true,
			backupState:       true,
			registrationCount: 12,
		},
	}

	for _, profile := range devices {
		t.Run("device="+profile.name, func(t *testing.T) {
			userHandle := []byte("device-user-" + profile.name)
			authenticator := newSoftwarePasskey(t, profile, userHandle)
			email := profile.name + "@devices.example"

			registrationFlow := testhelpers.InitializeRegistrationFlowViaAPI(t, client, fix.publicTS)
			registrationChallenge := randomBase64URLString(t, 32)
			injectPasskeySession(t, fix, registrationFlow.Id, "registration", webauthn.SessionData{
				Challenge: registrationChallenge,
				UserID:    userHandle,
			})

			registrationResponse := authenticator.registrationResponse(t, registrationChallenge)
			registrationValues := url.Values{}
			registrationValues.Set("method", identity.CredentialsTypePasskey.String())
			registrationValues.Set("traits.username", email)
			registrationValues.Set("traits.foobar", "device-test")
			registrationValues.Set(node.PasskeyRegister, string(registrationResponse))

			registrationBody, registrationRes := testhelpers.RegistrationMakeRequest(
				t,
				true,
				false,
				registrationFlow,
				client,
				testhelpers.EncodeFormAsJSON(t, true, registrationValues),
			)
			require.Equalf(t, http.StatusOK, registrationRes.StatusCode, "registration failed: %s", registrationBody)
			assert.NotEmpty(t, gjson.Get(registrationBody, "identity.id").String(), registrationBody)

			registeredIdentity, _, err := fix.reg.PrivilegedIdentityPool().FindByCredentialsIdentifier(
				t.Context(), identity.CredentialsTypePasskey, string(userHandle))
			require.NoError(t, err)
			require.Equal(t, email, gjson.GetBytes(registeredIdentity.Traits, "username").String())
			require.NoError(t, fix.reg.PrivilegedIdentityPool().HydrateIdentityAssociations(
				t.Context(), registeredIdentity, identity.ExpandCredentials))

			storedCredential, ok := registeredIdentity.GetCredentials(identity.CredentialsTypePasskey)
			require.True(t, ok)
			var storedConfig identity.CredentialsWebAuthnConfig
			require.NoError(t, json.Unmarshal(storedCredential.Config, &storedConfig))
			require.Len(t, storedConfig.Credentials, 1)
			assert.Equal(t, []protocol.AuthenticatorTransport{profile.transport}, storedConfig.Credentials[0].Transport)
			require.NotNil(t, storedConfig.Credentials[0].Authenticator)
			assert.Equal(t, profile.aaguid[:], storedConfig.Credentials[0].Authenticator.AAGUID)
			assert.Equal(t, profile.registrationCount, storedConfig.Credentials[0].Authenticator.SignCount)

			loginFlow := testhelpers.InitializeLoginFlowViaAPI(t, client, fix.publicTS, false)
			loginChallenge := randomBase64URLString(t, 32)
			injectPasskeySession(t, fix, loginFlow.Id, "login", webauthn.SessionData{
				Challenge:            loginChallenge,
				AllowedCredentialIDs: [][]byte{authenticator.id},
			})

			loginResponse := authenticator.assertionResponse(t, loginChallenge)
			loginValues := url.Values{}
			loginValues.Set("method", identity.CredentialsTypePasskey.String())
			loginValues.Set(node.PasskeyLogin, string(loginResponse))

			loginBody, loginRes := testhelpers.LoginMakeRequest(
				t,
				true,
				false,
				loginFlow,
				client,
				testhelpers.EncodeFormAsJSON(t, true, loginValues),
			)
			require.Equalf(t, http.StatusOK, loginRes.StatusCode, "login failed: %s", loginBody)
			assert.True(t, gjson.Get(loginBody, "session.active").Bool(), loginBody)
			assert.Equal(t, registeredIdentity.ID.String(), gjson.Get(loginBody, "session.identity.id").String(), loginBody)
			assert.Equal(t,
				identity.CredentialsTypePasskey.String(),
				gjson.Get(loginBody, "session.authentication_methods.#(method==passkey).method").String(),
				loginBody,
			)
		})
	}
}

func TestPasskeyLoginCompatibleWithLegacyWebAuthnCredential(t *testing.T) {
	fix := newLoginFixture(t)
	client := testhelpers.NewDebugClient(t)

	profile := deviceProfile{
		name:              "legacy-passwordless-webauthn",
		transport:         protocol.USB,
		attachment:        protocol.CrossPlatform,
		aaguid:            [16]byte{0x04},
		userVerified:      true,
		registrationCount: 3,
	}
	userHandle := []byte("legacy-webauthn-user-handle")
	authenticator := newSoftwarePasskey(t, profile, userHandle)

	credentialConfig, err := json.Marshal(identity.CredentialsWebAuthnConfig{
		Credentials: identity.CredentialsWebAuthn{{
			ID:              authenticator.id,
			PublicKey:       authenticator.cosePublicKey(),
			AttestationType: string(protocol.PreferNoAttestation),
			DisplayName:     "legacy security key",
			IsPasswordless:  true,
			Authenticator: &identity.AuthenticatorWebAuthn{
				AAGUID:    profile.aaguid[:],
				SignCount: profile.registrationCount,
			},
			Flags: &identity.CredentialWebAuthnFlags{
				UserPresent:  true,
				UserVerified: true,
			},
			Transport: []protocol.AuthenticatorTransport{profile.transport},
		}},
		UserHandle: userHandle,
	})
	require.NoError(t, err)

	registeredIdentity := &identity.Identity{
		Traits: identity.Traits(`{"email":"legacy-webauthn@devices.example"}`),
		Credentials: map[identity.CredentialsType]identity.Credentials{
			identity.CredentialsTypeWebAuthn: {
				Type:        identity.CredentialsTypeWebAuthn,
				Version:     1,
				Config:      credentialConfig,
				Identifiers: []string{string(userHandle)},
			},
		},
	}
	require.NoError(t, fix.reg.PrivilegedIdentityPool().CreateIdentity(t.Context(), registeredIdentity))

	loginFlow := testhelpers.InitializeLoginFlowViaAPI(t, client, fix.publicTS, false)
	loginChallenge := randomBase64URLString(t, 32)
	injectPasskeySession(t, fix, loginFlow.Id, "login", webauthn.SessionData{
		Challenge:            loginChallenge,
		AllowedCredentialIDs: [][]byte{authenticator.id},
	})

	loginValues := url.Values{}
	loginValues.Set("method", identity.CredentialsTypePasskey.String())
	loginValues.Set(node.PasskeyLogin, string(authenticator.assertionResponse(t, loginChallenge)))

	body, res := testhelpers.LoginMakeRequest(
		t,
		true,
		false,
		loginFlow,
		client,
		testhelpers.EncodeFormAsJSON(t, true, loginValues),
	)
	require.Equalf(t, http.StatusOK, res.StatusCode, "legacy WebAuthn login through passkey strategy failed: %s", body)
	assert.True(t, gjson.Get(body, "session.active").Bool(), body)
	assert.Equal(t, registeredIdentity.ID.String(), gjson.Get(body, "session.identity.id").String(), body)
}

func newSoftwarePasskey(t *testing.T, profile deviceProfile, userHandle []byte) *softwarePasskey {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	credentialID := make([]byte, 32)
	_, err = rand.Read(credentialID)
	require.NoError(t, err)

	return &softwarePasskey{
		profile:    profile,
		privateKey: privateKey,
		id:         credentialID,
		userHandle: userHandle,
		counter:    profile.registrationCount,
	}
}

func (a *softwarePasskey) registrationResponse(t *testing.T, challenge string) json.RawMessage {
	t.Helper()

	authData := a.authenticatorData(true)
	attestationObject, err := webauthncbor.Marshal(map[string]any{
		"fmt":      string(protocol.AttestationFormatNone),
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	require.NoError(t, err)

	clientDataJSON := a.clientDataJSON(t, protocol.CreateCeremony, challenge)
	payload := map[string]any{
		"id":                      base64.RawURLEncoding.EncodeToString(a.id),
		"rawId":                   base64.RawURLEncoding.EncodeToString(a.id),
		"type":                    string(protocol.PublicKeyCredentialType),
		"authenticatorAttachment": string(a.profile.attachment),
		"response": map[string]any{
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestationObject),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
			"transports":        []string{string(a.profile.transport)},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func (a *softwarePasskey) assertionResponse(t *testing.T, challenge string) json.RawMessage {
	t.Helper()

	a.counter++
	authData := a.authenticatorData(false)
	clientDataJSON := a.clientDataJSON(t, protocol.AssertCeremony, challenge)
	clientDataHash := sha256.Sum256(clientDataJSON)

	signedData := append(append([]byte{}, authData...), clientDataHash[:]...)
	digest := sha256.Sum256(signedData)
	signature, err := ecdsa.SignASN1(rand.Reader, a.privateKey, digest[:])
	require.NoError(t, err)

	payload := map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(a.id),
		"rawId": base64.RawURLEncoding.EncodeToString(a.id),
		"type":  string(protocol.PublicKeyCredentialType),
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientDataJSON),
			"signature":         base64.RawURLEncoding.EncodeToString(signature),
			"userHandle":        base64.RawURLEncoding.EncodeToString(a.userHandle),
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func (a *softwarePasskey) cosePublicKey() []byte {
	x := a.privateKey.PublicKey.X.Bytes()
	y := a.privateKey.PublicKey.Y.Bytes()

	key := map[any]any{
		int64(1):  int64(2),
		int64(3):  int64(-7),
		int64(-1): int64(1),
		int64(-2): leftPad32(x),
		int64(-3): leftPad32(y),
	}
	raw, err := webauthncbor.Marshal(key)
	if err != nil {
		panic(err)
	}
	return raw
}

func (a *softwarePasskey) authenticatorData(withCredential bool) []byte {
	rpIDHash := sha256.Sum256([]byte("localhost"))

	flags := byte(0x01) // user present
	if a.profile.userVerified {
		flags |= 0x04
	}
	if a.profile.backupEligible {
		flags |= 0x08
	}
	if a.profile.backupState {
		flags |= 0x10
	}
	if withCredential {
		flags |= 0x40
	}

	counter := make([]byte, 4)
	binary.BigEndian.PutUint32(counter, a.counter)

	data := append(append(append([]byte{}, rpIDHash[:]...), flags), counter...)
	if !withCredential {
		return data
	}

	credentialIDLength := make([]byte, 2)
	binary.BigEndian.PutUint16(credentialIDLength, uint16(len(a.id)))

	data = append(data, a.profile.aaguid[:]...)
	data = append(data, credentialIDLength...)
	data = append(data, a.id...)
	data = append(data, a.cosePublicKey()...)
	return data
}

func (a *softwarePasskey) clientDataJSON(t *testing.T, ceremony protocol.CeremonyType, challenge string) []byte {
	t.Helper()

	var ceremonyName string
	switch ceremony {
	case protocol.CreateCeremony:
		ceremonyName = "webauthn.create"
	case protocol.AssertCeremony:
		ceremonyName = "webauthn.get"
	default:
		t.Fatalf("unsupported ceremony: %s", ceremony)
	}

	payload := map[string]any{
		"type":        ceremonyName,
		"challenge":   challenge,
		"origin":      "http://localhost:4455",
		"crossOrigin": false,
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func injectPasskeySession(t *testing.T, fix *fixture, flowID string, flowType string, session webauthn.SessionData) {
	t.Helper()

	sessionJSON, err := json.Marshal(session)
	require.NoError(t, err)

	key := "passkey_session_data"
	switch flowType {
	case "registration":
		flow, err := fix.reg.RegistrationFlowPersister().GetRegistrationFlow(t.Context(), uuid.FromStringOrNil(flowID))
		require.NoError(t, err)
		flow.InternalContext, err = sjson.SetRawBytes(flow.InternalContext, key, sessionJSON)
		require.NoError(t, err)
		require.NoError(t, fix.reg.RegistrationFlowPersister().UpdateRegistrationFlow(t.Context(), flow))
	case "login":
		flow, err := fix.reg.LoginFlowPersister().GetLoginFlow(t.Context(), uuid.FromStringOrNil(flowID))
		require.NoError(t, err)
		flow.InternalContext, err = sjson.SetRawBytes(flow.InternalContext, key, sessionJSON)
		require.NoError(t, err)
		require.NoError(t, fix.reg.LoginFlowPersister().UpdateLoginFlow(t.Context(), flow))
	default:
		t.Fatalf("unsupported flow type: %s", flowType)
	}
}

func randomBase64URLString(t *testing.T, size int) string {
	t.Helper()
	buf := make([]byte, size)
	_, err := rand.Read(buf)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func leftPad32(in []byte) []byte {
	if len(in) == 32 {
		return in
	}
	out := make([]byte, 32)
	copy(out[32-len(in):], in)
	return out
}
