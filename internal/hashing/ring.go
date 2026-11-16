package hashing

import (
	"errors"
	"fmt"
	"hash/crc32"
	"sort"
	"sync"

	"baryon/internal/proxy"
)

var (
	// ErrNodeAlreadyExists is returned when trying to add a backend that is already in the ring.
	ErrNodeAlreadyExists = errors.New("backend node already exists in hash ring")

	// ErrNodeNotFound is returned when trying to remove a backend that does not exist in the ring.
	ErrNodeNotFound = errors.New("backend node not found in hash ring")

	// ErrNilBackend is returned when attempting to add a nil backend.
	ErrNilBackend = errors.New("backend cannot be nil")

	// ErrEmptyBackendName is returned when backend name is empty.
	ErrEmptyBackendName = errors.New("backend name cannot be empty")
)

// HashRing manages a consistent hashing ring using virtual nodes and CRC32 hashing.
type HashRing struct {
	mu       sync.RWMutex
	replicas int
	hashKeys []uint32                  // Sorted ring positions
	hashMap  map[uint32]*proxy.Backend // Hash position -> Backend pointer
	nodes    map[string]*proxy.Backend // Registered unique backend name -> Backend pointer
}

// NewHashRing creates a new HashRing with the specified number of virtual node replicas per backend.
func NewHashRing(replicas int) *HashRing {
	if replicas <= 0 {
		replicas = 100
	}
	return &HashRing{
		replicas: replicas,
		hashMap:  make(map[uint32]*proxy.Backend),
		nodes:    make(map[string]*proxy.Backend),
	}
}

// AddNode registers a backend and its virtual nodes onto the consistent hash ring.
// Thread-safe and atomic with respect to ring state.
func (r *HashRing) AddNode(backend *proxy.Backend) error {
	if backend == nil {
		return ErrNilBackend
	}
	if backend.Name == "" {
		return ErrEmptyBackendName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[backend.Name]; exists {
		return ErrNodeAlreadyExists
	}

	for i := 0; i < r.replicas; i++ {
		vnodeKey := fmt.Sprintf("%s#%d", backend.Name, i)
		hash := crc32.ChecksumIEEE([]byte(vnodeKey))

		// Collision resolution policy: if another backend occupies this exact hash position,
		// deterministically probe until an empty position is found.
		probe := 0
		for {
			existing, occupied := r.hashMap[hash]
			if !occupied || existing.Name == backend.Name {
				break
			}
			probe++
			probeKey := fmt.Sprintf("%s#%d#c%d", backend.Name, i, probe)
			hash = crc32.ChecksumIEEE([]byte(probeKey))
		}

		r.hashMap[hash] = backend
		r.hashKeys = append(r.hashKeys, hash)
	}

	sort.Slice(r.hashKeys, func(i, j int) bool {
		return r.hashKeys[i] < r.hashKeys[j]
	})

	r.nodes[backend.Name] = backend
	return nil
}

// RemoveNode removes a backend and all of its virtual nodes from the consistent hash ring.
// Thread-safe.
func (r *HashRing) RemoveNode(name string) error {
	if name == "" {
		return ErrEmptyBackendName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[name]; !exists {
		return ErrNodeNotFound
	}

	// Filter out hashKeys belonging to this backend
	newKeys := make([]uint32, 0, len(r.hashKeys))
	for _, k := range r.hashKeys {
		b, exists := r.hashMap[k]
		if exists && b.Name == name {
			delete(r.hashMap, k)
		} else {
			newKeys = append(newKeys, k)
		}
	}
	r.hashKeys = newKeys
	delete(r.nodes, name)

	return nil
}

// LoadEvaluator is a function that checks whether a backend is currently considered overloaded.
type LoadEvaluator = func(backend *proxy.Backend) bool

// GetNode looks up the assigned backend for the given routing key (e.g. client IP).
// Returns nil if the hash ring is empty.
// Thread-safe; acquires a read lock only for the duration of the lookup without network I/O.
func (r *HashRing) GetNode(key string) *proxy.Backend {
	return r.GetNodeWithBoundedLoad(key, nil)
}

// GetNodeWithBoundedLoad looks up the assigned backend for the given routing key,
// skipping overloaded backends in clockwise ring order (Consistent Hashing with Bounded Loads).
// If all backends are overloaded or isOverloaded is nil, it falls back to the least-loaded backend.
func (r *HashRing) GetNodeWithBoundedLoad(key string, isOverloaded LoadEvaluator) *proxy.Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.hashKeys) == 0 {
		return nil
	}

	hash := crc32.ChecksumIEEE([]byte(key))

	// Binary search for the first position >= hash
	idx := sort.Search(len(r.hashKeys), func(i int) bool {
		return r.hashKeys[i] >= hash
	})

	// Wrap around to index 0 if hash exceeds the largest ring position
	if idx == len(r.hashKeys) {
		idx = 0
	}

	primary := r.hashMap[r.hashKeys[idx]]
	if isOverloaded == nil || len(r.nodes) <= 1 {
		return primary
	}

	// Fast path: if primary candidate is not overloaded, return it directly
	if !isOverloaded(primary) {
		return primary
	}

	// Bounded-load fallthrough: traverse clockwise until an underloaded backend is found
	visited := make(map[string]bool, len(r.nodes))
	visited[primary.Name] = true

	var leastLoaded *proxy.Backend = primary
	_, minActive := primary.GetStats()

	totalKeys := len(r.hashKeys)
	for step := 1; step < totalKeys; step++ {
		pos := (idx + step) % totalKeys
		candidate := r.hashMap[r.hashKeys[pos]]
		if visited[candidate.Name] {
			continue
		}
		visited[candidate.Name] = true

		if !isOverloaded(candidate) {
			return candidate
		}

		_, active := candidate.GetStats()
		if active < minActive {
			minActive = active
			leastLoaded = candidate
		}

		// Stop once all distinct backends have been evaluated
		if len(visited) == len(r.nodes) {
			break
		}
	}

	// If all backends are currently overloaded, fall back to the least-loaded node
	return leastLoaded
}

// HasNode returns true if a backend with the given name is currently registered.
func (r *HashRing) HasNode(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.nodes[name]
	return exists
}

// NodeCount returns the number of active distinct backends currently registered on the ring.
func (r *HashRing) NodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

// GetNodes returns a slice of all currently registered backends.
func (r *HashRing) GetNodes() []*proxy.Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	backends := make([]*proxy.Backend, 0, len(r.nodes))
	for _, b := range r.nodes {
		backends = append(backends, b)
	}
	return backends
}

