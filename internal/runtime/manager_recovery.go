package runtime

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Restart/backoff ownership: recovering, restarts, restartBackoff, recoverDone, runCtx.
// watchAccount is the Qoder child-exit path; in-process accounts have no watcher.

func (m *Manager) startAccountWithRecovery(ctx context.Context, account Account) error {
	err := m.startAccount(ctx, account)
	if err != nil && !errors.Is(err, errManagerClosed) && m.runCtx.Err() == nil {
		m.beginAccountRecovery(account.ID, err)
	}
	return err
}

func (m *Manager) beginAccountRecovery(id string, startErr error) {
	if m == nil || m.store == nil || strings.TrimSpace(id) == "" {
		return
	}
	message := "account daemon failed to start"
	if startErr != nil {
		message = startErr.Error()
	}
	m.mu.Lock()
	if m.runCtx.Err() != nil {
		m.mu.Unlock()
		return
	}
	if m.recovering[id] {
		m.mu.Unlock()
		return
	}
	m.recovering[id] = true
	m.restarts[id]++
	restarts := m.restarts[id]
	if m.restartBackoff[id] < backoffMaxLevel {
		m.restartBackoff[id]++
	}
	backoffLevel := m.restartBackoff[id]
	m.recoverDone.Add(1)
	go m.recoverAccount(id, restarts, backoffLevel, message)
	m.mu.Unlock()
	m.pool.SetRuntimeState(id, "dead", time.Now().Add(m.restartDelay(backoffLevel)), backoffLevel, message)
}

func (m *Manager) recoverAccount(id string, restarts, backoffLevel int, message string) {
	defer func() {
		m.mu.Lock()
		delete(m.recovering, id)
		m.mu.Unlock()
		m.recoverDone.Done()
	}()

	_ = m.store.Observe(context.Background(), id, "", "dead", message, KindUnavailable)

	for {
		if m.runCtx.Err() != nil {
			return
		}
		account, err := m.store.Get(m.runCtx, id)
		if errors.Is(err, ErrAccountNotFound) || (err == nil && !account.Enabled) {
			m.pool.SetRuntimeState(id, "disabled", time.Time{}, restarts, message)
			return
		}
		if err != nil {
			message = err.Error()
		}

		delay := m.restartDelay(backoffLevel)
		m.pool.SetRuntimeState(id, "dead", time.Now().Add(delay), backoffLevel, message)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-m.runCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}

		if m.runCtx.Err() != nil {
			return
		}
		account, err = m.store.Get(m.runCtx, id)
		if errors.Is(err, ErrAccountNotFound) || (err == nil && !account.Enabled) {
			m.pool.SetRuntimeState(id, "disabled", time.Time{}, restarts, message)
			return
		}
		if err != nil {
			message = err.Error()
			m.mu.Lock()
			m.restarts[id]++
			restarts = m.restarts[id]
			m.mu.Unlock()
			continue
		}

		if m.runCtx.Err() != nil {
			return
		}
		err = m.startAccount(m.runCtx, account)
		if err == nil || errors.Is(err, errManagerClosed) || m.runCtx.Err() != nil {
			return
		}
		message = err.Error()
		m.mu.Lock()
		m.restarts[id]++
		restarts = m.restarts[id]
		if m.restartBackoff[id] < backoffMaxLevel {
			m.restartBackoff[id]++
		}
		backoffLevel = m.restartBackoff[id]
		m.mu.Unlock()
		_ = m.store.Observe(context.Background(), id, "", "dead", message, KindUnavailable)
	}
}
func (m *Manager) resetRestartBackoff(id string) {
	if m == nil || id == "" {
		return
	}
	m.mu.Lock()
	delete(m.restartBackoff, id)
	m.mu.Unlock()
}
func (m *Manager) watchAccount(id string, process ManagedProcess) {
	var exitErr error
	select {
	case exitErr = <-process.Done():
	case <-m.runCtx.Done():
		return
	}
	m.mu.Lock()
	if m.processes[id] != process {
		m.mu.Unlock()
		return
	}
	delete(m.processes, id)
	m.mu.Unlock()
	message := "account daemon exited"
	if exitErr != nil {
		message = exitErr.Error()
	}
	m.beginAccountRecovery(id, errors.New(message))
}

func (m *Manager) restartDelay(level int) time.Duration {
	if level <= 0 {
		level = 1
	}
	delay := m.config.RestartDelay
	for step := 1; step < level && delay < m.config.RestartMaxDelay; step++ {
		if delay > m.config.RestartMaxDelay/2 {
			delay = m.config.RestartMaxDelay
			break
		}
		delay *= 2
	}
	if delay > m.config.RestartMaxDelay {
		return m.config.RestartMaxDelay
	}
	return delay
}
