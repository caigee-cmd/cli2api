package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

type callLog struct {
	names []string
}

func (l *callLog) add(name string) {
	l.names = append(l.names, name)
}

type fakeStore struct {
	log             *callLog
	accounts        map[string]accounts.Account
	payloads        map[string][]byte
	payloadFormats  map[string]string
	native          map[string]accounts.NativeCredential
	checkins        map[string][]accounts.CheckinRecord
	secrets         map[string]string
	keys            map[string]accounts.APIKey
	contexts        map[string]int
	providerSetting accounts.ProviderModelSetting
	backup          accounts.Backup
	createErr       error
	payloadErr      error
	getErr          error
	getFailOnce     bool
	updateErr       error
	deleteErr       error
	backupErr       error
	listKeysErr     error
}

func newFakeStore(log *callLog) *fakeStore {
	return &fakeStore{
		log:            log,
		accounts:       map[string]accounts.Account{},
		payloads:       map[string][]byte{},
		payloadFormats: map[string]string{},
		native:         map[string]accounts.NativeCredential{},
		checkins:       map[string][]accounts.CheckinRecord{},
		secrets:        map[string]string{},
		keys:           map[string]accounts.APIKey{},
		contexts:       map[string]int{},
	}
}

func (s *fakeStore) RecordPoolState(context.Context, accounts.PoolState) error { return nil }
func (s *fakeStore) SaveCooldowns(context.Context, string, []accounts.CooldownRow) error {
	return nil
}
func (s *fakeStore) LoadCooldowns(context.Context) ([]accounts.CooldownRow, error) {
	return nil, nil
}
func (s *fakeStore) Close() error { return nil }
func (s *fakeStore) Backup(_ context.Context, directory string, keep int) (accounts.Backup, error) {
	s.log.add("store.Backup")
	if s.backupErr != nil {
		return accounts.Backup{}, s.backupErr
	}
	if s.backup.Name == "" {
		s.backup = accounts.Backup{Name: "snap.db", Path: filepath.Join(directory, "snap.db"), CreatedAt: time.Unix(0, 0).UTC()}
	}
	s.backup.Path = filepath.Join(directory, s.backup.Name)
	_ = keep
	return s.backup, nil
}
func (s *fakeStore) Create(_ context.Context, input accounts.CreateAccount) (accounts.Account, error) {
	s.log.add("store.Create")
	if s.createErr != nil {
		return accounts.Account{}, s.createErr
	}
	account := accounts.Account{ID: "acc-1", Name: input.Name, Provider: input.Provider, ProviderRegion: input.Region, Enabled: input.Enabled}
	s.accounts[account.ID] = account
	return account, nil
}
func (s *fakeStore) Get(_ context.Context, id string) (accounts.Account, error) {
	s.log.add("store.Get")
	if s.getFailOnce {
		s.getFailOnce = false
		return accounts.Account{}, errors.New("get after write failed")
	}
	if s.getErr != nil {
		return accounts.Account{}, s.getErr
	}
	account, ok := s.accounts[id]
	if !ok {
		return accounts.Account{}, accounts.ErrAccountNotFound
	}
	return account, nil
}
func (s *fakeStore) List(context.Context) ([]accounts.Account, error) { return nil, nil }
func (s *fakeStore) Update(_ context.Context, id string, input accounts.UpdateAccount) error {
	s.log.add("store.Update")
	if s.updateErr != nil {
		return s.updateErr
	}
	account, ok := s.accounts[id]
	if !ok {
		return accounts.ErrAccountNotFound
	}
	if input.Enabled != nil {
		account.Enabled = *input.Enabled
	}
	s.accounts[id] = account
	return nil
}
func (s *fakeStore) Delete(_ context.Context, id string) error {
	s.log.add("store.Delete")
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.accounts, id)
	delete(s.payloads, id)
	return nil
}
func (s *fakeStore) SaveCredential(_ context.Context, accountID, _ string, credential accounts.NativeCredential) error {
	s.log.add("store.SaveCredential")
	s.native[accountID] = credential
	return nil
}
func (s *fakeStore) LoadCredential(_ context.Context, accountID string) (accounts.NativeCredential, error) {
	s.log.add("store.LoadCredential")
	return s.native[accountID], nil
}
func (s *fakeStore) SaveCredentialPayload(_ context.Context, accountID, format string, payload []byte) error {
	s.log.add("store.SaveCredentialPayload")
	if s.payloadErr != nil {
		return s.payloadErr
	}
	s.payloads[accountID] = payload
	s.payloadFormats[accountID] = format
	return nil
}
func (s *fakeStore) LoadCredentialPayload(_ context.Context, accountID string) (string, []byte, error) {
	s.log.add("store.LoadCredentialPayload")
	return s.payloadFormats[accountID], s.payloads[accountID], nil
}
func (s *fakeStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}
func (s *fakeStore) SaveQuota(context.Context, string, *accounts.QuotaSnapshot) error { return nil }
func (s *fakeStore) RecordCheckin(context.Context, string, string, string, time.Time) error {
	return nil
}
func (s *fakeStore) ListCheckinRecords(_ context.Context, accountID string, _ int) ([]accounts.CheckinRecord, error) {
	s.log.add("store.ListCheckinRecords")
	return s.checkins[accountID], nil
}
func (s *fakeStore) GetSecret(_ context.Context, name string) (string, bool, error) {
	s.log.add("store.GetSecret")
	value, ok := s.secrets[name]
	return value, ok, nil
}
func (s *fakeStore) SetSecret(_ context.Context, name, value string) error {
	s.log.add("store.SetSecret")
	s.secrets[name] = value
	return nil
}
func (s *fakeStore) SetSecretOrEmpty(_ context.Context, name, value string) error {
	s.log.add("store.SetSecretOrEmpty")
	s.secrets[name] = value
	return nil
}
func (s *fakeStore) WorkBuddyCheckinTimeDefault(context.Context) string {
	s.log.add("store.WorkBuddyCheckinTimeDefault")
	return "09:00"
}
func (s *fakeStore) GetModelContext(_ context.Context, modelID string) (int, bool, error) {
	value, ok := s.contexts[modelID]
	return value, ok, nil
}
func (s *fakeStore) SetModelContext(_ context.Context, modelID string, contextLength int) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if contextLength == 0 {
		delete(s.contexts, modelID)
		return nil
	}
	s.contexts[modelID] = contextLength
	return nil
}
func (s *fakeStore) ListModelContexts(context.Context) (map[string]int, error) {
	return s.contexts, nil
}
func (s *fakeStore) GetProviderModelSetting(context.Context, string, string) (accounts.ProviderModelSetting, error) {
	if s.getErr != nil {
		return accounts.ProviderModelSetting{}, s.getErr
	}
	return s.providerSetting, nil
}
func (s *fakeStore) SetProviderModelSetting(_ context.Context, _, _ string, setting accounts.ProviderModelSetting) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.providerSetting = setting
	return nil
}
func (s *fakeStore) InsertAPIKey(_ context.Context, key accounts.StoredAPIKey) (accounts.APIKey, error) {
	s.log.add("store.InsertAPIKey")
	if s.createErr != nil {
		return accounts.APIKey{}, s.createErr
	}
	out := accounts.APIKey{ID: "key-1", Name: key.Name, Prefix: key.Prefix, Providers: key.Providers, Enabled: key.Enabled}
	if out.Providers == nil {
		out.Providers = []string{}
	}
	s.keys[out.ID] = out
	return out, nil
}
func (s *fakeStore) ListAPIKeys(context.Context) ([]accounts.APIKey, error) {
	s.log.add("store.ListAPIKeys")
	if s.listKeysErr != nil {
		return nil, s.listKeysErr
	}
	if len(s.keys) == 0 {
		return nil, nil
	}
	out := make([]accounts.APIKey, 0, len(s.keys))
	for _, key := range s.keys {
		out = append(out, key)
	}
	return out, nil
}
func (s *fakeStore) GetAPIKey(_ context.Context, id string) (accounts.APIKey, error) {
	s.log.add("store.GetAPIKey")
	key, ok := s.keys[id]
	if !ok {
		return accounts.APIKey{}, accounts.ErrAPIKeyNotFound
	}
	return key, nil
}
func (s *fakeStore) LookupAPIKey(context.Context, string) (accounts.APIKey, bool, error) {
	return accounts.APIKey{}, false, nil
}
func (s *fakeStore) SaveAPIKey(_ context.Context, key accounts.StoredAPIKey) (accounts.APIKey, error) {
	s.log.add("store.SaveAPIKey")
	if s.updateErr != nil {
		return accounts.APIKey{}, s.updateErr
	}
	current, ok := s.keys[key.ID]
	if !ok {
		return accounts.APIKey{}, accounts.ErrAPIKeyNotFound
	}
	current.Name = key.Name
	current.Providers = key.Providers
	current.Enabled = key.Enabled
	s.keys[key.ID] = current
	return current, nil
}
func (s *fakeStore) DeleteAPIKey(_ context.Context, id string) error {
	s.log.add("store.DeleteAPIKey")
	if _, ok := s.keys[id]; !ok {
		return accounts.ErrAPIKeyNotFound
	}
	delete(s.keys, id)
	return nil
}
func (s *fakeStore) TouchAPIKey(_ context.Context, id string) error {
	s.log.add("store.TouchAPIKey")
	_, ok := s.keys[id]
	if !ok {
		return accounts.ErrAPIKeyNotFound
	}
	return nil
}

type fakeRuntime struct {
	log         *callLog
	store       *fakeStore
	startErr    error
	stopErr     error
	syncErr     error
	refreshErr  error
	checkinErr  error
	started     []string
	stopped     []string
	removed     []string
	views       []accounts.AccountView
	view        accounts.AccountView
	checkedIn   accounts.Account
	refreshAll  bool
	forceQuota  bool
	proxyURL    string
	proxyAPIKey string
	adminReq    providers.AdminRequest
}

func (r *fakeRuntime) StartAccount(_ context.Context, account accounts.Account) error {
	r.log.add("runtime.StartAccount")
	r.started = append(r.started, account.ID)
	if r.startErr != nil {
		return r.startErr
	}
	return nil
}
func (r *fakeRuntime) StopAccount(id string) error {
	r.log.add("runtime.StopAccount")
	r.stopped = append(r.stopped, id)
	return r.stopErr
}
func (r *fakeRuntime) RemoveAccount(id string) error {
	r.log.add("runtime.RemoveAccount")
	r.removed = append(r.removed, id)
	return nil
}
func (r *fakeRuntime) SyncAccount(_ context.Context, _, after accounts.Account) error {
	r.log.add("runtime.SyncAccount")
	if after.Enabled {
		r.started = append(r.started, after.ID)
	} else {
		r.stopped = append(r.stopped, after.ID)
	}
	return r.syncErr
}
func (r *fakeRuntime) AccountView(context.Context, string) (accounts.AccountView, error) {
	r.log.add("runtime.AccountView")
	return r.view, nil
}
func (r *fakeRuntime) Accounts(context.Context) ([]accounts.AccountView, error) {
	r.log.add("runtime.Accounts")
	return r.views, nil
}
func (r *fakeRuntime) RefreshAccount(_ context.Context, _ string, forceQuota bool) error {
	r.log.add("runtime.RefreshAccount")
	r.forceQuota = forceQuota
	return r.refreshErr
}
func (r *fakeRuntime) RefreshAll(_ context.Context, forceQuota bool) error {
	r.log.add("runtime.RefreshAll")
	r.refreshAll = true
	r.forceQuota = forceQuota
	return r.refreshErr
}
func (r *fakeRuntime) CheckinAccount(_ context.Context, id string) (accounts.Account, error) {
	r.log.add("runtime.CheckinAccount")
	if r.checkinErr != nil {
		return accounts.Account{}, r.checkinErr
	}
	if r.checkedIn.ID == "" {
		r.checkedIn = r.store.accounts[id]
	}
	return r.checkedIn, nil
}
func (r *fakeRuntime) ReloadProxyURL(_ context.Context, value string) error {
	r.log.add("runtime.ReloadProxyURL")
	r.proxyURL = value
	return nil
}
func (r *fakeRuntime) ReplaceProxyAPIKey(_ context.Context, key string) error {
	r.log.add("runtime.ReplaceProxyAPIKey")
	r.proxyAPIKey = key
	return nil
}
func (r *fakeRuntime) WorkerAdmin(_ context.Context, input providers.AdminRequest) (providers.AdminResponse, error) {
	r.log.add("runtime.WorkerAdmin")
	r.adminReq = input
	return providers.AdminResponse{Status: 200}, nil
}
func (r *fakeRuntime) Store() accounts.AccountStore {
	return r.store
}

func newTestServices() (*Services, *fakeRuntime, *fakeStore, *callLog) {
	log := &callLog{}
	store := newFakeStore(log)
	runtime := &fakeRuntime{log: log, store: store}
	return New(runtime), runtime, store, log
}

func TestImportCredentialPayloadDeletesOnPayloadFailure(t *testing.T) {
	svc, _, store, log := newTestServices()
	store.payloadErr = errors.New("blob write failed")
	_, err := svc.Accounts.ImportCredentialPayload(context.Background(), accounts.CreateAccount{
		Name: "Trae", Provider: "trae",
	}, "trae-oauth-v1", []byte(`{"uid":"u1"}`), true)
	if err == nil || err.Error() != "blob write failed" {
		t.Fatalf("err=%v", err)
	}
	if _, ok := store.accounts["acc-1"]; ok {
		t.Fatal("failed payload import left the account")
	}
	if got := log.names; !equalCalls(got, []string{
		"store.Create", "store.SaveCredentialPayload", "store.Delete", "runtime.RemoveAccount",
	}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestImportCredentialPayloadEnablesAfterCredentialWrite(t *testing.T) {
	svc, _, _, log := newTestServices()
	account, err := svc.Accounts.ImportCredentialPayload(context.Background(), accounts.CreateAccount{
		Name: "WB", Provider: "workbuddy", Enabled: true,
	}, "workbuddy-oauth-v1", []byte(`{"uid":"u1"}`), true)
	if err != nil {
		t.Fatal(err)
	}
	if !account.Enabled {
		t.Fatalf("imported %+v", account)
	}
	if got := log.names; !equalCalls(got, []string{
		"store.Create", "store.SaveCredentialPayload", "store.Update", "store.Get", "runtime.StartAccount", "store.Get",
	}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestImportCredentialPayloadIgnoresGetError(t *testing.T) {
	svc, _, store, _ := newTestServices()
	store.getFailOnce = true
	account, err := svc.Accounts.ImportCredentialPayload(context.Background(), accounts.CreateAccount{
		Name: "Devin", Provider: "devin",
	}, "devin-session-v1", []byte(`{"user_id":"u1"}`), false)
	if err != nil {
		t.Fatalf("Get failure after import must stay 201-equivalent: %v", err)
	}
	if account.ID != "" {
		t.Fatalf("ignored Get should return zero account, got %+v", account)
	}
}

func TestCreatePassesThroughStartFailure(t *testing.T) {
	svc, runtime, store, _ := newTestServices()
	runtime.startErr = errors.New("start failed")
	account, err := svc.Accounts.Create(context.Background(), accounts.CreateAccount{Name: "Qoder", Enabled: true})
	if err == nil || err.Error() != "start failed" {
		t.Fatalf("err=%v", err)
	}
	if account.ID != "acc-1" {
		t.Fatalf("row must already exist: %+v", account)
	}
	if _, ok := store.accounts["acc-1"]; !ok {
		t.Fatal("start failure must not roll back the row")
	}
}

func TestUpdateReadsStoreAfterRuntime(t *testing.T) {
	svc, _, store, log := newTestServices()
	store.accounts["acc-1"] = accounts.Account{ID: "acc-1", Name: "before"}
	enabled := true
	account, err := svc.Accounts.Update(context.Background(), "acc-1", accounts.UpdateAccount{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if !account.Enabled {
		t.Fatalf("updated %+v", account)
	}
	if got := log.names; !equalCalls(got, []string{"store.Get", "store.Update", "store.Get", "runtime.SyncAccount"}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestListRefreshUsesForceQuotaThenAccounts(t *testing.T) {
	svc, runtime, _, log := newTestServices()
	runtime.views = []accounts.AccountView{{Account: accounts.Account{ID: "acc-1"}}}
	items, err := svc.Accounts.List(context.Background(), true)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if !runtime.refreshAll || !runtime.forceQuota {
		t.Fatalf("refreshAll=%v forceQuota=%v", runtime.refreshAll, runtime.forceQuota)
	}
	if got := log.names; !equalCalls(got, []string{"runtime.RefreshAll", "runtime.Accounts"}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestListWithoutRefreshSkipsRefreshAll(t *testing.T) {
	svc, runtime, _, log := newTestServices()
	if _, err := svc.Accounts.List(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if runtime.refreshAll {
		t.Fatal("refresh=0 must not call RefreshAll")
	}
	if got := log.names; !equalCalls(got, []string{"runtime.Accounts"}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestKeysListEmptySliceNotNil(t *testing.T) {
	svc, _, _, _ := newTestServices()
	keys, err := svc.Keys.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if keys == nil {
		t.Fatal("empty key list must be a non-nil slice")
	}
}

func TestBackupSnapshotCallsStore(t *testing.T) {
	svc, _, _, log := newTestServices()
	backup, err := svc.Backup.Snapshot(context.Background(), "/tmp/backups", 5)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Name != "snap.db" {
		t.Fatalf("backup=%+v", backup)
	}
	if got := log.names; !equalCalls(got, []string{"store.Backup"}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestNativeImportFailureDoesNotCreateViaControl(t *testing.T) {
	svc, _, store, _ := newTestServices()
	store.createErr = errors.New("native credential requires user blob and machine id")
	_, err := svc.Accounts.ImportNative(context.Background(), accounts.ImportAccount{
		Name: "Broken", Credential: accounts.NativeCredential{UserBlob: []byte("cipher")},
	})
	if err == nil {
		t.Fatal("expected import error")
	}
	if len(store.accounts) != 0 {
		t.Fatalf("leftover accounts=%+v", store.accounts)
	}
}

func TestKeysCreateReturnsSecretOnceAndPersistsHashOnly(t *testing.T) {
	svc, _, store, log := newTestServices()
	key, err := svc.Keys.Create(context.Background(), accounts.CreateAPIKey{Name: "CI", Providers: []string{"qoder"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if key.Secret == "" || !key.SecretOnce || key.Name != "CI" {
		t.Fatalf("created=%+v", key)
	}
	stored := store.keys[key.ID]
	if stored.Secret != "" || stored.SecretOnce {
		t.Fatalf("store leaked secret: %+v", stored)
	}
	if got := log.names; !equalCalls(got, []string{"store.InsertAPIKey"}) {
		t.Fatalf("calls=%v", got)
	}
}

func TestKeysCreateRejectsEmptyNameAndSaveFailure(t *testing.T) {
	svc, _, store, _ := newTestServices()
	if _, err := svc.Keys.Create(context.Background(), accounts.CreateAPIKey{}); err == nil {
		t.Fatal("empty name must fail")
	}
	store.createErr = errors.New("db write failed")
	if _, err := svc.Keys.Create(context.Background(), accounts.CreateAPIKey{Name: "CI"}); err == nil {
		t.Fatal("save failure must fail")
	}
	if len(store.keys) != 0 {
		t.Fatalf("leftover=%+v", store.keys)
	}
}

func TestKeysUpdateKeepsOriginalSecret(t *testing.T) {
	svc, _, store, _ := newTestServices()
	created, err := svc.Keys.Create(context.Background(), accounts.CreateAPIKey{Name: "CI", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	updated, err := svc.Keys.Update(context.Background(), created.ID, accounts.UpdateAPIKey{Name: "CI prod", Providers: []string{"qoder", "trae"}, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "CI prod" || updated.Enabled || len(updated.Providers) != 2 || updated.Secret != "" {
		t.Fatalf("updated=%+v", updated)
	}
	if store.keys[created.ID].Secret != "" {
		t.Fatalf("update leaked secret: %+v", store.keys[created.ID])
	}
}

func equalCalls(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
