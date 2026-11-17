package handlers

import (
	"net/netip"
	"sync"
	"time"
)

const authRateLimitWindow = time.Minute

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

type requestLimiter struct {
	mu         sync.Mutex
	limit      int
	maxClients int
	clients    map[netip.Addr]rateLimitEntry
	cleanupAt  time.Time
}

func newRequestLimiter(limit, maxClients int) *requestLimiter {
	return &requestLimiter{limit: limit, maxClients: maxClients, clients: make(map[netip.Addr]rateLimitEntry)}
}

func (limiter *requestLimiter) allow(ip netip.Addr, now time.Time) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if !now.Before(limiter.cleanupAt) {
		for client, window := range limiter.clients {
			if !now.Before(window.resetAt) {
				delete(limiter.clients, client)
			}
		}
		limiter.cleanupAt = now.Add(authRateLimitWindow)
	}

	window, exists := limiter.clients[ip]
	if !exists {
		if len(limiter.clients) >= limiter.maxClients {
			return false, limiter.cleanupAt.Sub(now)
		}
		window.resetAt = now.Add(authRateLimitWindow)
	} else if !now.Before(window.resetAt) {
		window = rateLimitEntry{resetAt: now.Add(authRateLimitWindow)}
	}

	if window.count >= limiter.limit {
		return false, window.resetAt.Sub(now)
	}

	window.count++
	limiter.clients[ip] = window

	return true, 0
}
