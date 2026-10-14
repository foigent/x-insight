package access

import (
	"crypto/sha256"
	"sync"
	"time"
)

const (
	defaultGlobalFailureLimit     = 50
	defaultGlobalFailureWindow    = time.Minute
	defaultPrincipalFailureLimit  = 5
	defaultPrincipalFailureWindow = 15 * time.Minute
	defaultPrincipalLimitEntries  = 1024
)

type failureWindow struct {
	start    time.Time
	failures int
	pending  int
}

type failureLimiter struct {
	mu                  sync.Mutex
	global              failureWindow
	principals          map[[32]byte]failureWindow
	globalLimit         int
	globalWindow        time.Duration
	principalLimit      int
	principalWindow     time.Duration
	maxPrincipalEntries int
}

func newFailureLimiter(options ServiceOptions) *failureLimiter {
	globalLimit := options.GlobalFailureLimit
	if globalLimit <= 0 {
		globalLimit = defaultGlobalFailureLimit
	}
	globalWindow := options.GlobalFailureWindow
	if globalWindow <= 0 {
		globalWindow = defaultGlobalFailureWindow
	}
	principalLimit := options.PrincipalFailureLimit
	if principalLimit <= 0 {
		principalLimit = defaultPrincipalFailureLimit
	}
	principalWindow := options.PrincipalFailureWindow
	if principalWindow <= 0 {
		principalWindow = defaultPrincipalFailureWindow
	}
	return &failureLimiter{
		principals:          make(map[[32]byte]failureWindow),
		globalLimit:         globalLimit,
		globalWindow:        globalWindow,
		principalLimit:      principalLimit,
		principalWindow:     principalWindow,
		maxPrincipalEntries: defaultPrincipalLimitEntries,
	}
}

func (limiter *failureLimiter) begin(key [32]byte, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.resetExpired(now)
	if limiter.global.failures+limiter.global.pending >= limiter.globalLimit {
		return false
	}
	window := limiter.principals[key]
	if window.failures+window.pending >= limiter.principalLimit {
		return false
	}
	if limiter.global.start.IsZero() {
		limiter.global.start = now
	}
	if window.start.IsZero() {
		window.start = now
	}
	if _, exists := limiter.principals[key]; !exists && len(limiter.principals) >= limiter.maxPrincipalEntries {
		limiter.evictOldestPrincipal()
	}
	limiter.global.pending++
	window.pending++
	limiter.principals[key] = window
	return true
}

func (limiter *failureLimiter) finish(key [32]byte, failed bool) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.global.pending > 0 {
		limiter.global.pending--
	}
	window, exists := limiter.principals[key]
	if !exists {
		return
	}
	if window.pending > 0 {
		window.pending--
	}
	if failed {
		limiter.global.failures++
		window.failures++
	} else {
		window.failures = 0
	}
	if window.pending == 0 && window.failures == 0 {
		delete(limiter.principals, key)
		return
	}
	limiter.principals[key] = window
}

func (limiter *failureLimiter) cancel(key [32]byte) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.global.pending > 0 {
		limiter.global.pending--
	}
	window, exists := limiter.principals[key]
	if !exists {
		return
	}
	if window.pending > 0 {
		window.pending--
	}
	if window.pending == 0 && window.failures == 0 {
		delete(limiter.principals, key)
		return
	}
	limiter.principals[key] = window
}

func (limiter *failureLimiter) resetExpired(now time.Time) {
	if limiter.global.pending == 0 &&
		!limiter.global.start.IsZero() &&
		!now.Before(limiter.global.start.Add(limiter.globalWindow)) {
		limiter.global = failureWindow{}
	}
	for key, window := range limiter.principals {
		if window.pending == 0 && !now.Before(window.start.Add(limiter.principalWindow)) {
			delete(limiter.principals, key)
		}
	}
}

func (limiter *failureLimiter) evictOldestPrincipal() {
	var oldestKey [32]byte
	var oldest time.Time
	found := false
	for key, window := range limiter.principals {
		if !found || window.start.Before(oldest) {
			oldestKey = key
			oldest = window.start
			found = true
		}
	}
	if found {
		delete(limiter.principals, oldestKey)
	}
}

func principalFailureKey(username string) [32]byte {
	normalized := NormalizeUsername(username)
	if len(normalized) > 128 {
		normalized = "__oversized__"
	}
	return sha256.Sum256([]byte(normalized))
}
