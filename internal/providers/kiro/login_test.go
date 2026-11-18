package kiro

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// scriptTransport answers each request with the next scripted response, keyed
// by a substring of the URL, and records the order the endpoints were called.
type scriptTransport struct {
	steps []scriptStep
	got   []string
}

type scriptStep struct {
	match  string
	status int
	body   string
}

func (s *scriptTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	s.got = append(s.got, r.URL.String())
	for i, step := range s.steps {
		if strings.Contains(r.URL.String(), step.match) {
			s.steps = append(s.steps[:i], s.steps[i+1:]...)
			status := step.status
			if status == 0 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(step.body)), Header: make(http.Header), Request: r}, nil
		}
	}
	return nil, fmt.Errorf("unexpected request %s", r.URL)
}

func (s *scriptTransport) called(match string) bool {
	for _, url := range s.got {
		if strings.Contains(url, match) {
			return true
		}
	}
	return false
}

type loginStore struct {
	account accounts.Account
	secrets map[string]string
	saved   []byte
}

func (s *loginStore) Get(context.Context, string) (accounts.Account, error) { return s.account, nil }
func (s *loginStore) LoadCredentialPayload(context.Context, string) (string, []byte, error) {
	return "", nil, accounts.ErrAccountNotFound
}
func (s *loginStore) SaveCredentialPayload(_ context.Context, _, _ string, payload []byte) error {
	s.saved = payload
	return nil
}
func (s *loginStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}
func (s *loginStore) SetCredentialAuthType(context.Context, string, string) error { return nil }
func (s *loginStore) GetSecret(_ context.Context, name string) (string, bool, error) {
	value, ok := s.secrets[name]
	return value, ok, nil
}

func TestBuilderIDLoginCompletesOnPoll(t *testing.T) {
	transport := &scriptTransport{steps: []scriptStep{
		{match: "/client/register", body: `{"clientId":"client","clientSecret":"secret"}`},
		{match: "/device_authorization", body: `{"deviceCode":"device","userCode":"ABCD-1234","verificationUriComplete":"https://view.awsapps.com/start/#/device?user_code=ABCD-1234","interval":0,"expiresIn":600}`},
		{match: "/token", status: http.StatusBadRequest, body: `{"error":"authorization_pending"}`},
		{match: "/token", body: `{"accessToken":"access","refreshToken":"refresh","expiresIn":3600}`},
	}}
	store := &loginStore{account: accounts.Account{ID: "acc", ProviderRegion: "us-east-1"}}
	client := &Client{store: store, http: &http.Client{Transport: transport}}

	session, err := client.StartLogin(context.Background(), "acc")
	if err != nil {
		t.Fatal(err)
	}
	client.logins["acc"].interval = 0
	if !strings.Contains(session.AuthURL, "user_code=ABCD-1234") || session.State != "ABCD-1234" {
		t.Fatalf("session = %+v", session)
	}
	done, message, err := client.PollLogin(context.Background(), "acc")
	if err != nil || done || !strings.Contains(message, "ABCD-1234") {
		t.Fatalf("pending poll = %v %q %v", done, message, err)
	}
	done, _, err = client.PollLogin(context.Background(), "acc")
	if err != nil || !done {
		t.Fatalf("final poll = %v %v", done, err)
	}
	cred, err := DecodeCredential(store.saved)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AuthMethod != "builder_id" || cred.ClientID != "client" || cred.RefreshToken != "refresh" {
		t.Fatalf("saved = %+v", cred)
	}
	if strings.Contains(string(store.saved), "AAAACCCCXXXX") {
		t.Fatal("the fixed builder id profile was stored")
	}
}

func TestSSOImportExchangesBearer(t *testing.T) {
	transport := &scriptTransport{steps: []scriptStep{
		{match: "/token/whoAmI", body: `{}`},
		{match: "/client/register", body: `{"clientId":"client","clientSecret":"secret"}`},
		{match: "/device_authorization", body: `{"deviceCode":"device","userCode":"CODE"}`},
		{match: "/session/device", body: `{"token":"session-token"}`},
		{match: "/accept_user_code", body: `{"deviceContext":{"deviceContextId":"ctx","clientId":"client","clientType":"public"}}`},
		{match: "/associate_token", body: `{}`},
		{match: "/token", body: `{"accessToken":"access","refreshToken":"refresh","expiresIn":3600}`},
	}}
	cred, err := importSSOToken(context.Background(), &http.Client{Transport: transport}, "us-east-1", "portal-bearer")
	if err != nil {
		t.Fatal(err)
	}
	if cred.AuthMethod != "builder_id" || cred.AccessToken != "access" || cred.ClientSecret != "secret" {
		t.Fatalf("cred = %+v", cred)
	}
	for _, want := range []string{"/token/whoAmI", "/session/device", "/accept_user_code", "/associate_token"} {
		if !transport.called(want) {
			t.Fatalf("import never called %s (%v)", want, transport.got)
		}
	}
}

func TestMicrosoftLoginAdvancesToProvider(t *testing.T) {
	tenant := "11111111-1111-1111-1111-111111111111"
	issuer := "https://login.microsoftonline.com/" + tenant + "/v2.0"
	transport := &scriptTransport{steps: []scriptStep{
		{match: "/.well-known/openid-configuration", body: fmt.Sprintf(
			`{"issuer":%q,"authorization_endpoint":"https://login.microsoftonline.com/%s/oauth2/v2.0/authorize","token_endpoint":"https://login.microsoftonline.com/%s/oauth2/v2.0/token"}`,
			issuer, tenant, tenant)},
	}}
	store := &loginStore{
		account: accounts.Account{ID: "acc", ProviderRegion: "us-east-1"},
		secrets: map[string]string{loginMethodName + "acc": loginMicrosoft},
	}
	client := &Client{store: store, http: &http.Client{Transport: transport}}
	if _, err := client.StartLogin(context.Background(), "acc"); err != nil {
		t.Fatal(err)
	}
	callback := fmt.Sprintf(
		"http://localhost:3128/signin/callback?login_option=external_idp&issuer_url=%s&client_id=%s&scopes=%s",
		issuer, tenant, "offline_access%20api://"+tenant+"/codewhisperer:completions")
	if err := client.CompleteLogin(context.Background(), "acc", callback); err != nil {
		t.Fatal(err)
	}
	_, message, err := client.PollLogin(context.Background(), "acc")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "login.microsoftonline.com") || !strings.Contains(message, "code_challenge") {
		t.Fatalf("poll = %q", message)
	}
}
