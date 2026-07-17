/*
 *
 * Module:    bibtex_check_dev
 * Package:   Main
 * Component: TableStore
 *
 * Generic write-through cache layer. TSQLiteTable backs a table with SQLite;
 * TCachedTable wraps any backing store with an in-memory read cache so that
 * every Set/Delete propagates to the DB immediately without a batch save step.
 *
 * Creator: Henderik A. Proper (e.proper@acm.org), Luxembourg, in collaboration with Claude.ai
 *
 * Version of: 17.06.2026
 *
 */

package main

import "database/sql"

// TTableStore is the read/write interface that both TSQLiteTable and TCachedTable satisfy.
type TTableStore[K comparable, V any] interface {
	Get(K) (V, bool)
	Set(K, V)
	Delete(K)
	ForEach(func(K, V))
}

// --- TSQLiteTable ---

// TSQLiteTable is a TTableStore backed by a SQLite table via the package-level db.
// Direct Get always returns false — wrap with TCachedTable for reads.
// Failed writes set dbWriteFailed and emit a warning, matching dbExecSave behaviour.
type TSQLiteTable[K comparable, V any] struct {
	upsertSQL  string
	deleteSQL  string
	selectSQL  string
	upsertArgs func(K, V) []any
	deleteArgs func(K) []any
	scanRow    func(*sql.Rows) (K, V, error)
}

func (s *TSQLiteTable[K, V]) Get(_ K) (V, bool) {
	var zero V
	return zero, false
}

func (s *TSQLiteTable[K, V]) Set(key K, value V) {
	if err := bibExec(s.upsertSQL, s.upsertArgs(key, value)...); err != nil {
		dbInteraction.Warning("table write failed: %s", err)
		dbWriteFailed = true
	}
}

func (s *TSQLiteTable[K, V]) Delete(key K) {
	if err := bibExec(s.deleteSQL, s.deleteArgs(key)...); err != nil {
		dbInteraction.Warning("table delete failed: %s", err)
		dbWriteFailed = true
	}
}

func (s *TSQLiteTable[K, V]) ForEach(fn func(K, V)) {
	rows, err := db.Query(s.selectSQL)
	if err != nil {
		dbInteraction.Warning("table query failed: %s", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		k, v, err := s.scanRow(rows)
		if err != nil {
			dbInteraction.Warning("table scan failed: %s", err)
			continue
		}
		fn(k, v)
	}
}

// --- TCachedTable ---

// TCachedTable wraps a TTableStore with an in-memory read cache.
// Call Load() once after the DB is open; thereafter all Gets come from the map.
// Set and Delete write through to the backing store and keep the map consistent.
// onModify, when set, is called after each successful persistent write (Set/Delete).
// It is NOT called by SetTransient. Wire it to setTableDate for modification tracking.
type TCachedTable[K comparable, V any] struct {
	cache    map[K]V
	backing  TTableStore[K, V]
	onModify func()
}

// newCachedTable wraps backing in an unloaded TCachedTable. Call Load() before reading.
func newCachedTable[K comparable, V any](backing TTableStore[K, V]) *TCachedTable[K, V] {
	return &TCachedTable[K, V]{backing: backing}
}

// Load populates the in-memory cache from the backing store.
func (c *TCachedTable[K, V]) Load() {
	c.cache = make(map[K]V)
	c.backing.ForEach(func(k K, v V) {
		c.cache[k] = v
	})
}

// Get returns the cached value and true, or the zero value and false when absent.
func (c *TCachedTable[K, V]) Get(key K) (V, bool) {
	if c.cache == nil {
		var zero V
		return zero, false
	}
	v, ok := c.cache[key]
	return v, ok
}

// GetValue returns the cached value, or the zero value when the key is absent.
func (c *TCachedTable[K, V]) GetValue(key K) V {
	v, _ := c.Get(key)
	return v
}

// Len returns the number of entries in the cache (0 if not yet loaded).
func (c *TCachedTable[K, V]) Len() int {
	return len(c.cache)
}

// Contains reports whether key is present in the cache.
func (c *TCachedTable[K, V]) Contains(key K) bool {
	_, ok := c.Get(key)
	return ok
}

// SetTransient updates the cache without writing to the backing store.
// Use for entries that must be available for in-process lookups but must
// not be persisted (e.g. DBLP-derived hints regenerated from bib_entries each run).
func (c *TCachedTable[K, V]) SetTransient(key K, value V) {
	if c.cache != nil {
		c.cache[key] = value
	}
}

// DeleteWhere removes all entries satisfying match, collecting keys first to avoid
// modifying the cache during iteration.
func (c *TCachedTable[K, V]) DeleteWhere(match func(K, V) bool) {
	var toDelete []K
	c.ForEach(func(k K, v V) {
		if match(k, v) {
			toDelete = append(toDelete, k)
		}
	})
	for _, k := range toDelete {
		c.Delete(k)
	}
}

// Set writes through to the backing store and updates the cache.
func (c *TCachedTable[K, V]) Set(key K, value V) {
	c.backing.Set(key, value)
	if c.cache != nil {
		c.cache[key] = value
	}
	if c.onModify != nil {
		c.onModify()
	}
}

// Delete removes key from the backing store and the cache.
func (c *TCachedTable[K, V]) Delete(key K) {
	c.backing.Delete(key)
	if c.cache != nil {
		delete(c.cache, key)
	}
	if c.onModify != nil {
		c.onModify()
	}
}

// ForEach iterates over the cached entries when loaded, or falls through to the backing store.
func (c *TCachedTable[K, V]) ForEach(fn func(K, V)) {
	if c.cache == nil {
		c.backing.ForEach(fn)
		return
	}
	for k, v := range c.cache {
		fn(k, v)
	}
}

// --- TKeyAliasTable ---

// TKeyAliasTable is a write-through cache for alias→canonical key mappings.
// It maintains a forward map (alias→canonical, always flat) and an inverse map
// (canonical→aliases) so that when alias B gains a target C, any existing alias A→B
// is immediately updated to A→C without waiting for a save-time chain-flattening pass.
// Transient aliases (e.g. DBLP-derived) use SetTransient: they populate the forward
// map for in-process lookups but are never written to the DB and never appear in the
// inverse map, so they do not participate in bulk chain updates.
// onModify, when set, is called after each persistent write (Set/Delete/DeleteByTarget).
// It is NOT called by SetTransient. Wire it to setTableDate for modification tracking.
type TKeyAliasTable struct {
	forward    map[string]string   // alias → canonical (always flat)
	inverse    map[string][]string // canonical → persistent aliases pointing to it
	upsertSQL  string
	deleteSQL  string
	selectSQL  string
	onModify   func()
}

func removeStringFromSlice(slice []string, value string) []string {
	var out []string
	for _, s := range slice {
		if s != value {
			out = append(out, s)
		}
	}
	return out
}

// Load populates forward and inverse maps from the DB.
// Chains in the stored data are flattened eagerly; stale entries (target not in
// bib_entries) are deleted from the DB immediately.
func (t *TKeyAliasTable) Load() {
	t.forward = make(map[string]string)
	t.inverse = make(map[string][]string)

	rows, err := db.Query(t.selectSQL)
	if err != nil {
		dbInteraction.Warning("table query failed: %s", err)
		return
	}
	defer rows.Close()

	type rawPair struct{ alias, key string }
	var pairs []rawPair
	for rows.Next() {
		var alias, key string
		if err := rows.Scan(&alias, &key); err != nil {
			dbInteraction.Warning("table scan failed: %s", err)
			continue
		}
		pairs = append(pairs, rawPair{alias, key})
	}

	// Build a raw forward map from the DB rows.
	raw := make(map[string]string, len(pairs))
	for _, p := range pairs {
		raw[p.alias] = p.key
	}

	// Flatten chains: for each alias, follow raw until no further mapping.
	for _, p := range pairs {
		canonical := p.key
		visited := map[string]bool{p.alias: true}
		for {
			next, ok := raw[canonical]
			if !ok || visited[canonical] {
				break
			}
			visited[canonical] = true
			canonical = next
		}

		if !bibEntryExists(canonical) {
			// Target gone — delete from DB immediately.
			if err := bibExec(t.deleteSQL, p.alias); err != nil {
				dbWriteFailed = true
			}
			continue
		}
		if p.alias == canonical {
			continue
		}

		// Write flattened form back to DB if it differs from what was stored.
		if p.key != canonical {
			if err := bibExec(t.upsertSQL, p.alias, canonical); err != nil {
				dbInteraction.Warning("table write failed: %s", err)
			}
		}
		t.forward[p.alias] = canonical
		t.inverse[canonical] = append(t.inverse[canonical], p.alias)
	}
}

// Get returns the canonical key for alias, or "" if not mapped.
func (t *TKeyAliasTable) Get(alias string) string {
	return t.forward[alias]
}

// Has reports whether alias is present in the forward map.
func (t *TKeyAliasTable) Has(alias string) bool {
	_, ok := t.forward[alias]
	return ok
}

// Len returns the number of entries in the forward map.
func (t *TKeyAliasTable) Len() int {
	return len(t.forward)
}

// Set records a persistent alias→canonical mapping with eager chain-flattening.
// If canonical is itself an alias, the true canonical is resolved first.
// Any existing alias→alias entries in the inverse map are bulk-updated to point
// to the resolved canonical and written to the DB.
// Conflicts (alias already maps to a different canonical) are silently ignored;
// the caller is responsible for conflict-checking before calling Set.
func (t *TKeyAliasTable) Set(alias, canonical string) {
	if alias == "" || canonical == "" {
		return
	}

	// Resolve canonical through the forward map to ensure flatness.
	resolved := canonical
	for {
		next, ok := t.forward[resolved]
		if !ok {
			break
		}
		resolved = next
	}
	canonical = resolved

	if alias == canonical {
		return
	}

	// If alias already maps to the same canonical, nothing to do.
	if existing := t.forward[alias]; existing == canonical {
		return
	}

	// Bulk-update any persistent aliases that currently point to alias.
	// These are in inverse[alias] — transient entries are not in inverse.
	if aliases := t.inverse[alias]; len(aliases) > 0 {
		for _, indirect := range aliases {
			t.forward[indirect] = canonical
			t.inverse[canonical] = append(t.inverse[canonical], indirect)
			if err := bibExec(t.upsertSQL, indirect, canonical); err != nil {
				dbInteraction.Warning("table write failed: %s", err)
				dbWriteFailed = true
			}
		}
		delete(t.inverse, alias)
	}

	// If alias was previously mapped to another canonical, remove it from that inverse list.
	if old := t.forward[alias]; old != "" {
		t.inverse[old] = removeStringFromSlice(t.inverse[old], alias)
	}

	t.forward[alias] = canonical
	t.inverse[canonical] = append(t.inverse[canonical], alias)

	if err := bibExec(t.upsertSQL, alias, canonical); err != nil {
		dbInteraction.Warning("table write failed: %s", err)
		dbWriteFailed = true
		return
	}
	if t.onModify != nil {
		t.onModify()
	}
}

// SetTransient adds alias→canonical to the forward map for in-process lookups
// without touching the DB or the inverse map. Used for DBLP-derived aliases
// that are regenerated from bib_entries on every run.
func (t *TKeyAliasTable) SetTransient(alias, canonical string) {
	if t.forward != nil {
		t.forward[alias] = canonical
	}
}

// Delete removes alias from the forward map, the inverse map, and the DB.
func (t *TKeyAliasTable) Delete(alias string) {
	canonical, ok := t.forward[alias]
	if !ok {
		return
	}
	t.inverse[canonical] = removeStringFromSlice(t.inverse[canonical], alias)
	delete(t.forward, alias)
	if err := bibExec(t.deleteSQL, alias); err != nil {
		dbInteraction.Warning("table delete failed: %s", err)
		dbWriteFailed = true
		return
	}
	if t.onModify != nil {
		t.onModify()
	}
}

// DeleteByTarget removes all persistent aliases pointing to canonical.
// Uses the inverse map for O(1) lookup; does not touch transient-only entries.
func (t *TKeyAliasTable) DeleteByTarget(canonical string) {
	aliases := t.inverse[canonical]
	modified := false
	for _, alias := range aliases {
		delete(t.forward, alias)
		if err := bibExec(t.deleteSQL, alias); err != nil {
			dbInteraction.Warning("table delete failed: %s", err)
			dbWriteFailed = true
		} else {
			modified = true
		}
	}
	delete(t.inverse, canonical)
	if modified && t.onModify != nil {
		t.onModify()
	}
}

// EachAlias calls fn for each persistent alias pointing to canonical.
func (t *TKeyAliasTable) EachAlias(canonical string, fn func(string)) {
	for _, alias := range t.inverse[canonical] {
		fn(alias)
	}
}

// ForEach iterates over all forward-map entries (persistent and transient).
func (t *TKeyAliasTable) ForEach(fn func(alias, canonical string)) {
	for alias, canonical := range t.forward {
		fn(alias, canonical)
	}
}

// ForEachPersistent iterates only over aliases that were loaded from the DB or
// written via Set. Transient aliases (added via SetTransient, e.g. DBLP-derived
// lookups) are excluded because they are not tracked in the inverse map.
func (t *TKeyAliasTable) ForEachPersistent(fn func(alias, canonical string)) {
	for canonical, aliases := range t.inverse {
		for _, alias := range aliases {
			fn(alias, canonical)
		}
	}
}
