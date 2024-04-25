// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import "time"

type Ticker struct {
	interval      time.Duration
	subscriptions []chan struct{}
}

func NewTicker(writeIntervalSeconds uint64) *Ticker {
	return &Ticker{
		interval: time.Duration(writeIntervalSeconds) * time.Second,
	}
}

func (t *Ticker) Subscribe() chan struct{} {
	sub := make(chan struct{})
	t.subscriptions = append(t.subscriptions, sub)
	return sub
}

func (t *Ticker) Run() {
	for range time.Tick(t.interval) {
		for _, sub := range t.subscriptions {
			sub <- struct{}{}
		}
	}
}
