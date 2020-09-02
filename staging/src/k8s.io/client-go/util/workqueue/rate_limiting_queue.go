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

// RateLimitingInterface is an interface that rate limits items being added to the queue.
// RateLimitingInterface是一个接口，用于限制要添加到队列中的元素的速率。
type RateLimitingInterface interface {
	// 继承延时队列
	DelayingInterface

	// AddRateLimited adds an item to the workqueue after the rate limiter says it's ok
	// 当通过限速后，添加元素到队列
	AddRateLimited(item interface{})

	// Forget indicates that an item is finished being retried.  Doesn't matter whether it's for perm failing
	// or for success, we'll stop the rate limiter from tracking it.  This only clears the `rateLimiter`, you
	// still have to call `Done` on the queue.
	// 移除正在限速器中的元素
	// 在队列中获取到该元素依然需要使用 Done 方法
	Forget(item interface{})

	// NumRequeues returns back how many times the item was requeued
	// 查询该元素的排队数
	NumRequeues(item interface{}) int
}

// NewRateLimitingQueue constructs a new workqueue with rateLimited queuing ability
// Remember to call Forget!  If you don't, you may end up tracking failures forever.
func NewRateLimitingQueue(rateLimiter RateLimiter) RateLimitingInterface {
	return &rateLimitingType{
		DelayingInterface: NewDelayingQueue(),
		rateLimiter:       rateLimiter,
	}
}

func NewNamedRateLimitingQueue(rateLimiter RateLimiter, name string) RateLimitingInterface {
	return &rateLimitingType{
		DelayingInterface: NewNamedDelayingQueue(name),
		rateLimiter:       rateLimiter,
	}
}

// rateLimitingType wraps an Interface and provides rateLimited re-enquing
type rateLimitingType struct {
	// 继承延时队列接口
	DelayingInterface

	// 限速器
	rateLimiter RateLimiter
}

// AddRateLimited AddAfter's the item based on the time when the rate limiter says it's ok
// 添加元素到限速队列
func (q *rateLimitingType) AddRateLimited(item interface{}) {
	// 从限速器获取排队时间，然后加入到延时队列中
	q.DelayingInterface.AddAfter(item, q.rateLimiter.When(item))
}

// 获取排队数
func (q *rateLimitingType) NumRequeues(item interface{}) int {
	return q.rateLimiter.NumRequeues(item)
}

// 从限速去中移除元素
func (q *rateLimitingType) Forget(item interface{}) {
	q.rateLimiter.Forget(item)
}
