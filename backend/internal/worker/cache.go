package worker

import (
	"container/list"
	"sync"
	"time"

	"climate-hacktion-curtailment/backend/internal/model/simulate"
)

// cache keeps recent finished runs, so that a request for the detail of a step does not replay
// the whole window again. It holds at most max runs, each for at most ttl.
type cache struct {
	mu    sync.Mutex
	max   int
	ttl   time.Duration
	now   func() time.Time
	items map[string]*list.Element
	order *list.List // front is the most recently used
}

type entry struct {
	id  string
	res *simulate.Result
	at  time.Time
}

func newCache(max int, ttl time.Duration) *cache {
	return &cache{max: max, ttl: ttl, now: time.Now, items: map[string]*list.Element{}, order: list.New()}
}

func (c *cache) get(id string) (*simulate.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[id]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if c.now().Sub(e.at) > c.ttl {
		c.order.Remove(el)
		delete(c.items, id)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e.res, true
}

func (c *cache) put(id string, res *simulate.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[id]; ok {
		el.Value = &entry{id: id, res: res, at: c.now()}
		c.order.MoveToFront(el)
		return
	}
	c.items[id] = c.order.PushFront(&entry{id: id, res: res, at: c.now()})
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*entry).id)
	}
}

func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
