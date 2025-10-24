package utils

import "time"

type cachedEntry[T any] struct {
	value     T
	createdAt time.Time
}

type CachedMap[T comparable, U any] struct {
	data     SafeMap[T, cachedEntry[U]]
	interval time.Duration
}

func NewCachedMap[T comparable, U any](interval time.Duration) CachedMap[T, U] {
	return CachedMap[T, U]{
		data:     NewSafeMap[T, cachedEntry[U]](),
		interval: interval,
	}
}

func (m *CachedMap[T, U]) Get(key T) Optional[U] {
	val := m.data.Get(key)

	if !val.IsDefined() {
		return Optional[U]{}
	}

	entry := val.Unwrap()

	if (time.Now().After(entry.createdAt.Add(m.interval))) {
		m.data.Del(key)
		return Optional[U]{}
	}

	return NewOptional(entry.value)
}

func (m *CachedMap[T, U]) Set(key T, val U) {
	m.data.Set(key, cachedEntry[U]{
		value:     val,
		createdAt: time.Now(),
	})
}

func (m *CachedMap[T, U]) Del(key T) {
	m.data.Del(key)
}
