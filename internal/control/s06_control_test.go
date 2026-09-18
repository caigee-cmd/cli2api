package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
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

func (s *fakeStore) RecordPoolState(context.Context, accounts.Item) error { return nil }
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
func (s *fakeStore) SaveCredential(context.Context, string, string, accounts.NativeCredential) error {
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
	s.contexts[modelID] = contextLength
	return nil
}
func (s *fakeStore) ListModelContexts(context.Context) (map[string]int, error) {
	return s.contexts, nil
}
func (s *fakeStore) GetProviderModelSetting(context.Context, string, string) (accounts.ProviderModelSetting, error) {
	return s.providerSetting, nil
}
func (s *fakeStore) SetProviderModelSetting(_ context.Context, _, _ string, setting accounts.ProviderModelSetting) error {
	s.providerSetting = setting
	return nil
}
func (s *fakeStore) CreateAPIKey(_ context.Context, input accounts.CreateAPIKey) (accounts.APIKey, error) {
	s.log.add("store.CreateAPIKey")
	key := accounts.APIKey{ID: "key-1", Name: input.Name, Enabled: input.Enabled, Secret: "secret-once"}
	s.keys[key.ID] = key
	return key, nil
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
func (s *fakeStore) UpdateAPIKey(_ context.Context, id string, input accounts.UpdateAPIKey) (accounts.APIKey, error) {
	s.log.add("store.UpdateAPIKey")
	key, ok := s.keys[id]
	if !ok {
		return accounts.APIKey{}, accounts.ErrAPIKeyNotFound
	}
	if input.Name != "" {
		key.Name = input.Name
	}
	s.keys[id] = key
	return key, nil
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
	createErr   error
	updateErr   error
	deleteErr   error
	importErr   error
	refreshErr  error
	checkinErr  error
	created     accounts.Account
	imported    accounts.Account
	views       []accounts.AccountView
	view        accounts.AccountView
	checkedIn   accounts.Account
	refreshAll  bool
	forceQuota  bool
	proxyURL    string
	proxyAPIKey string
}

func (r *fakeRuntime) Create(_ context.Context, input accounts.CreateAccount) (accounts.Account, error) {
	r.log.add("runtime.Create")
	if r.createErr != nil {
		account := r.created
		if account.ID == "" {
			account = accounts.Account{ID: "acc-1", Name: input.Name, Provider: input.Provider, Enabled: input.Enabled}
		}
		r.store.accounts[account.ID] = account
		return account, r.createErr
	}
	account, err := r.store.Create(context.Background(), input)
	if err != nil {
		return accounts.Account{}, err
	}
	r.created = account
	return account, nil
}
func (r *fakeRuntime) Update(_ context.Context, id string, input accounts.UpdateAccount) error {
	r.log.add("runtime.Update")
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.store.Update(context.Background(), id, input)
}
func (r *fakeRuntime) Delete(_ context.Context, id string) error {
	r.log.add("runtime.Delete")
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return r.store.Delete(context.Background(), id)
}
func (r *fakeRuntime) Import(_ context.Context, input accounts.ImportAccount) (accounts.Account, error) {
	r.log.add("runtime.Import")
	if r.importErr != nil {
		return accounts.Account{}, r.importErr
	}
	r.imported = accounts.Account{ID: "acc-native", Name: input.Name, Enabled: input.Enabled}
	r.store.accounts[r.imported.ID] = r.imported
	return r.imported, nil
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
		"runtime.Create", "store.Create", "store.SaveCredentialPayload", "runtime.Delete", "store.Delete",
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
		"runtime.Create", "store.Create", "store.SaveCredentialPayload", "runtime.Update", "store.Update", "store.Get",
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
	runtime.createErr = errors.New("start failed")
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
	if got := log.names; !equalCalls(got, []string{"runtime.Update", "store.Update", "store.Get"}) {
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
	svc, runtime, store, _ := newTestServices()
	runtime.importErr = errors.New("native credential requires user blob and machine id")
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
