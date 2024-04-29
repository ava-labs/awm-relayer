// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sync"
	"time"
)

// Ticker is a timer that can be subscribed to. When the timer ticks,
// all subscribers will receive a signal on the channel they were given
// when subscribing.
type Ticker struct {
	interval      time.Duration
	subscriptions []chan struct{}
	lock          *sync.Mutex
}

func NewTicker(writeIntervalSeconds uint64) *Ticker {
	return &Ticker{
		interval: time.Duration(writeIntervalSeconds) * time.Second,
		lock:     &sync.Mutex{},
	}
}

func (t *Ticker) Subscribe() chan struct{} {
	t.lock.Lock()
	defer t.lock.Unlock()

	sub := make(chan struct{})
	t.subscriptions = append(t.subscriptions, sub)
	return sub
}

func (t *Ticker) Run() {
	for range time.Tick(t.interval) {
		t.lock.Lock()
		for _, sub := range t.subscriptions {
			sub <- struct{}{}
		}
		t.lock.Unlock()
	}
}
