/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workqueue

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
)

type Interface interface {
	// 给队列添加元素（item）
	Add(item interface{})
	// 返回当前队列长度
	Len() int
	// 获取队列头部的一个元素
	Get() (item interface{}, shutdown bool)
	// 标记队列中改元素已被处理
	Done(item interface{})
	// 关闭队列
	ShutDown()
	// 查询队列是否正在关闭
	ShuttingDown() bool
}

// New constructs a new work queue (see the package comment).
func New() *Type {
	return NewNamed("")
}

func NewNamed(name string) *Type {
	rc := clock.RealClock{}
	return newQueue(
		rc,
		globalMetricsFactory.newQueueMetrics(name, rc),
		defaultUnfinishedWorkUpdatePeriod,
	)
}

func newQueue(c clock.Clock, metrics queueMetrics, updatePeriod time.Duration) *Type {
	t := &Type{
		clock:                      c,
		dirty:                      set{},
		processing:                 set{},
		cond:                       sync.NewCond(&sync.Mutex{}),
		metrics:                    metrics,
		unfinishedWorkUpdatePeriod: updatePeriod,
	}
	go t.updateUnfinishedWorkLoop()
	return t
}

const defaultUnfinishedWorkUpdatePeriod = 500 * time.Millisecond

// Type is a work queue (see the package comment).
// FIFO 队列
type Type struct {
	// queue defines the order in which we will work on items. Every
	// element of queue should be in the dirty set and not in the
	// processing set.
	// 队列定义了我们处理项目的顺序。 队列中的每个元素都应该在 dirty set 中而不是在 processing set 中。
	// 实际存储元素
	// slice 保证有序
	queue []t

	// dirty defines all of the items that need to be processed.
	// dirty 定义所有需要处理的项目。
	// 去重，保证元素只被添加了一次
	dirty set

	// Things that are currently being processed are in the processing set.
	// These things may be simultaneously in the dirty set. When we finish
	// processing something and remove it from this set, we'll check if
	// it's in the dirty set, and if so, add it to the queue.
	// 当前正在处理的事物在处理集中。
	// 这些东西可能同时在 dirty set 中。
	// 当我们完成处理并将其从这个 set 中删除时，我们将检查它是否在 dirty set 中
	// 如果是，则将其添加到队列中。
	// 标记元素是否正在被处理
	processing set

	cond *sync.Cond

	shuttingDown bool

	metrics queueMetrics

	unfinishedWorkUpdatePeriod time.Duration
	clock                      clock.Clock
}

type empty struct{}
type t interface{}
type set map[t]empty

func (s set) has(item t) bool {
	_, exists := s[item]
	return exists
}

func (s set) insert(item t) {
	s[item] = empty{}
}

func (s set) delete(item t) {
	delete(s, item)
}

// Add marks item as needing processing.
func (q *Type) Add(item interface{}) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	if q.shuttingDown {
		return
	}
	// 如果 dirty 中已经存在改元素，则不添加
	if q.dirty.has(item) {
		return
	}

	// 添加 metric
	q.metrics.add(item)

	// 添加元素到 dirty
	q.dirty.insert(item)
	// 该元素正在被执行
	if q.processing.has(item) {
		return
	}

	// 添加元素到队列
	q.queue = append(q.queue, item)
	// 通知有新元素
	q.cond.Signal()
}

// Len returns the current queue length, for informational purposes only. You
// shouldn't e.g. gate a call to Add() or Get() on Len() being a particular
// value, that can't be synchronized properly.
func (q *Type) Len() int {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	return len(q.queue)
}

// Get blocks until it can return an item to be processed. If shutdown = true,
// the caller should end their goroutine. You must call Done with item when you
// have finished processing it.
// 阻塞型的获取元素。
// 如果shutdown = true，则调用方应结束其goroutine。
// 完成处理后，必须调用 Done() 方法 。
func (q *Type) Get() (item interface{}, shutdown bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	// 等待队列中有元素
	for len(q.queue) == 0 && !q.shuttingDown {
		q.cond.Wait()
	}
	if len(q.queue) == 0 {
		// We must be shutting down.
		return nil, true
	}

	// 取出队列的第一个元素
	item, q.queue = q.queue[0], q.queue[1:]

	// 设置 metric
	q.metrics.get(item)

	// 将元素放到正在执行的队列中
	q.processing.insert(item)
	// 从dirty 中将他删除
	q.dirty.delete(item)

	return item, false
}

// Done marks item as done processing, and if it has been marked as dirty again
// while it was being processed, it will be re-added to the queue for
// re-processing.
//完成将元素标记为已完成处理，如果该元素在 dirty set 中还存在，则会将其重新添加到队列中以进行重新处理。
func (q *Type) Done(item interface{}) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	q.metrics.done(item)

	// 从执行队列删除
	q.processing.delete(item)
	if q.dirty.has(item) {
		// 如果dirty set 中还存在该元素，则添加到队列中
		q.queue = append(q.queue, item)
		q.cond.Signal()
	}
}

// ShutDown will cause q to ignore all new items added to it. As soon as the
// worker goroutines have drained the existing items in the queue, they will be
// instructed to exit.
func (q *Type) ShutDown() {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()
	q.shuttingDown = true
	q.cond.Broadcast()
}

func (q *Type) ShuttingDown() bool {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	return q.shuttingDown
}

func (q *Type) updateUnfinishedWorkLoop() {
	t := q.clock.NewTicker(q.unfinishedWorkUpdatePeriod)
	defer t.Stop()
	for range t.C() {
		if !func() bool {
			q.cond.L.Lock()
			defer q.cond.L.Unlock()
			if !q.shuttingDown {
				q.metrics.updateUnfinishedWork()
				return true
			}
			return false

		}() {
			return
		}
	}
}
