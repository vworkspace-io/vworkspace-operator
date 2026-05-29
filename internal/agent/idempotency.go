package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// DefaultIdempotencyConfigMap is the ConfigMap storing applied idempotency keys.
	DefaultIdempotencyConfigMap = "vworkspace-applied-jobs"

	idempotencyDataKey = "applied-keys.json"
)

// IdempotencyStore tracks applied Pull-mode job idempotency keys in a ConfigMap.
type IdempotencyStore struct {
	Client    client.Client
	Namespace string
	Name      string

	mu    sync.RWMutex
	cache map[string]struct{}
}

// Contains reports whether key was already applied.
func (s *IdempotencyStore) Contains(ctx context.Context, key string) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return false, nil
	}
	if err := s.ensureLoaded(ctx); err != nil {
		return false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.cache[key]
	return ok, nil
}

// Record stores key after a successful apply.
func (s *IdempotencyStore) Record(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if err := s.ensureLoaded(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cache[key]; ok {
		return nil
	}
	s.cache[key] = struct{}{}
	return s.persistLocked(ctx)
}

func (s *IdempotencyStore) ensureLoaded(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache != nil {
		return nil
	}
	if s.Name == "" {
		s.Name = DefaultIdempotencyConfigMap
	}
	if s.Namespace == "" {
		return fmt.Errorf("idempotency store namespace is required")
	}

	cm := &corev1.ConfigMap{}
	err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: s.Name}, cm)
	if apierrors.IsNotFound(err) {
		s.cache = map[string]struct{}{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get idempotency configmap %s/%s: %w", s.Namespace, s.Name, err)
	}

	keys := []string{}
	if raw, ok := cm.Data[idempotencyDataKey]; ok && strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &keys); err != nil {
			return fmt.Errorf("decode idempotency keys: %w", err)
		}
	}
	s.cache = make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			s.cache[trimmed] = struct{}{}
		}
	}
	return nil
}

func (s *IdempotencyStore) persistLocked(ctx context.Context) error {
	keys := make([]string, 0, len(s.cache))
	for key := range s.cache {
		keys = append(keys, key)
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return fmt.Errorf("marshal idempotency keys: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
		},
	}
	_, err = controllerutil.CreateOrUpdate(ctx, s.Client, cm, func() error {
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[idempotencyDataKey] = string(raw)
		return nil
	})
	if err != nil {
		return fmt.Errorf("persist idempotency configmap %s/%s: %w", s.Namespace, s.Name, err)
	}
	return nil
}
