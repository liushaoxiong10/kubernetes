/*
Copyright 2016 The Kubernetes Authors.

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
	"container/heap"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

// DelayingInterface is an Interface that can Add an item at a later time. This makes it easier to
// requeue items after failures without ending up in a hot-loop.
// 延时队列
type DelayingInterface interface {
	// 继承 FIFO 的接口
	Interface
	// AddAfter adds an item to the workqueue after the indicated duration has passed
	// 延时多久后插入元素
	AddAfter(item interface{}, duration time.Duration)
}

// NewDelayingQueue constructs a new workqueue with delayed queuing ability
func NewDelayingQueue() DelayingInterface {
	return newDelayingQueue(clock.RealClock{}, "")
}

func NewNamedDelayingQueue(name string) DelayingInterface {
	return newDelayingQueue(clock.RealClock{}, name)
}

func newDelayingQueue(clock clock.Clock, name string) DelayingInterface {
	ret := &delayingType{
		Interface:         NewNamed(name),
		clock:             clock,
		heartbeat:         clock.NewTicker(maxWait),
		stopCh:            make(chan struct{}),
		waitingForAddCh:   make(chan *waitFor, 1000),
		metrics:           newRetryMetrics(name),
		deprecatedMetrics: newDeprecatedRetryMetrics(name),
	}

	go ret.waitingLoop()

	return ret
}

// delayingType wraps an Interface and provides delayed re-enquing
// delayingType 包装了 Interface， 并且支持延时后再放入队列
type delayingType struct {
	// 支持FIFO 的匿名接口
	Interface

	// clock tracks time for delayed firing
	clock clock.Clock

	// stopCh lets us signal a shutdown to the waiting loop
	stopCh chan struct{}

	// heartbeat ensures we wait no more than maxWait before firing
	heartbeat clock.Ticker

	// waitingForAddCh is a buffered channel that feeds waitingForAdd
	// 缓冲通道，用于提供 waitingForAdd
	// 初始化长度为1000
	waitingForAddCh chan *waitFor

	// metrics counts the number of retries
	metrics           retryMetrics
	deprecatedMetrics retryMetrics
}

// waitFor holds the data to add and the time it should be added
type waitFor struct {
	data    t
	readyAt time.Time
	// index in the priority queue (heap)
	index int
}

// waitForPriorityQueue implements a priority queue for waitFor items.
// 为 waitFor 实现的一个优先队列
//
// waitForPriorityQueue implements heap.Interface. The item occurring next in
// time (i.e., the item with the smallest readyAt) is at the root (index 0).
// Peek returns this minimum item at index 0. Pop returns the minimum item after
// it has been removed from the queue and placed at index Len()-1 by
// container/heap. Push adds an item at index Len(), and container/heap
// percolates it into the correct location.
// waitForPriorityQueue 实现了 heap.Interface 的接口。

type waitForPriorityQueue []*waitFor

func (pq waitForPriorityQueue) Len() int {
	return len(pq)
}
func (pq waitForPriorityQueue) Less(i, j int) bool {
	return pq[i].readyAt.Before(pq[j].readyAt)
}
func (pq waitForPriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push adds an item to the queue. Push should not be called directly; instead,
// use `heap.Push`.
// 向队列中推入一个元素，要是用 heap.Push
func (pq *waitForPriorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*waitFor)
	item.index = n
	*pq = append(*pq, item)
}

// Pop removes an item from the queue. Pop should not be called directly;
// instead, use `heap.Pop`.
// 从队列中拿出并删除一个元素，不能直接使用该方法，要使用 heap.Pop
func (pq *waitForPriorityQueue) Pop() interface{} {
	n := len(*pq)
	item := (*pq)[n-1]
	item.index = -1
	*pq = (*pq)[0:(n - 1)]
	return item
}

// Peek returns the item at the beginning of the queue, without removing the
// item or otherwise mutating the queue. It is safe to call directly.
// 返回队列中第一个元素，该元素不会从队列中删除
func (pq waitForPriorityQueue) Peek() interface{} {
	return pq[0]
}

// ShutDown gives a way to shut off this queue
func (q *delayingType) ShutDown() {
	q.Interface.ShutDown()
	close(q.stopCh)
	q.heartbeat.Stop()
}

// AddAfter adds the given item to the work queue after the given delay
// 在给定延迟后将给定项目添加到工作队列
func (q *delayingType) AddAfter(item interface{}, duration time.Duration) {
	// don't add if we're already shutting down
	if q.ShuttingDown() {
		return
	}

	q.metrics.retry()
	q.deprecatedMetrics.retry()

	// immediately add things with no delay
	// 延时小于0，立即添加
	if duration <= 0 {
		q.Add(item)
		return
	}

	select {
	case <-q.stopCh:
		// unblock if ShutDown() is called
	// 将元素放入 channel 中
	case q.waitingForAddCh <- &waitFor{data: item, readyAt: q.clock.Now().Add(duration)}:
	}
}

// maxWait keeps a max bound on the wait time. It's just insurance against weird things happening.
// Checking the queue every 10 seconds isn't expensive and we know that we'll never end up with an
// expired item sitting for more than 10 seconds.
const maxWait = 10 * time.Second

// waitingLoop runs until the workqueue is shutdown and keeps a check on the list of items to be added.
// 延时队列消费
// 一直运行到关闭工作队列并检查要添加的项目列表。
func (q *delayingType) waitingLoop() {
	defer utilruntime.HandleCrash()

	// Make a placeholder channel to use when there are no items in our list
	never := make(<-chan time.Time)

	// 优先队列
	waitingForQueue := &waitForPriorityQueue{}
	heap.Init(waitingForQueue)

	waitingEntryByData := map[t]*waitFor{}

	for {
		if q.Interface.ShuttingDown() {
			return
		}

		now := q.clock.Now()

		// Add ready entries
		// 遍历优先队列，添加准备好的元素
		for waitingForQueue.Len() > 0 {
			// 从队列中拿出一个元素，但是不移除
			entry := waitingForQueue.Peek().(*waitFor)
			if entry.readyAt.After(now) {
				// 没有到延时时间
				break
			}

			// 已到延时时间，从队列中移出，推入 FIFO 中
			entry = heap.Pop(waitingForQueue).(*waitFor)
			q.Add(entry.data)
			delete(waitingEntryByData, entry.data)
		}

		// Set up a wait for the first item's readyAt (if one exists)
		// 创建一个 waitingForQueue 中第一个元素延时时间的定时器 (if one exists)
		nextReadyAt := never
		if waitingForQueue.Len() > 0 {
			entry := waitingForQueue.Peek().(*waitFor)
			nextReadyAt = q.clock.After(entry.readyAt.Sub(now))
		}

		select {
		case <-q.stopCh:
			return

		case <-q.heartbeat.C():
			// continue the loop, which will add ready items

		case <-nextReadyAt:
			// continue the loop, which will add ready items

		case waitEntry := <-q.waitingForAddCh:
			// 获取延时队列的元素
			if waitEntry.readyAt.After(q.clock.Now()) {
				// 添加或更新元素 到优先队列
				insert(waitingForQueue, waitingEntryByData, waitEntry)
			} else {
				// 推入 FIFO 队列
				q.Add(waitEntry.data)
			}

			drained := false
			for !drained {
				// 如果 waitingForAddCh 有元素，则持续消费
				select {
				case waitEntry := <-q.waitingForAddCh:
					// 判断是否到了延时时间
					if waitEntry.readyAt.After(q.clock.Now()) {
						// 添加到优先队列中
						insert(waitingForQueue, waitingEntryByData, waitEntry)
					} else {
						// 推入 FIFO 队列
						q.Add(waitEntry.data)
					}
				default:
					drained = true
				}
			}
		}
	}
}

// insert adds the entry to the priority queue, or updates the readyAt if it already exists in the queue
// 将条目添加到优先级队列，如果队列中已经存在则更新readyAt
func insert(q *waitForPriorityQueue, knownEntries map[t]*waitFor, entry *waitFor) {
	// if the entry already exists, update the time only if it would cause the item to be queued sooner
	// 如果该条目已经存在，则仅在将导致该项目尽快排队的情况下更新时间
	existing, exists := knownEntries[entry.data]
	if exists {
		// 如果新的时间比老的时间早，则更新 readyAt
		if existing.readyAt.After(entry.readyAt) {
			existing.readyAt = entry.readyAt
			heap.Fix(q, existing.index)
		}

		return
	}

	// 放入优先队列
	heap.Push(q, entry)
	knownEntries[entry.data] = entry
}
