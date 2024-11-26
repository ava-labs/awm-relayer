// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"net/netip"

	"github.com/ava-labs/avalanchego/ids"
)

type PeerConfig struct {
	ID string `mapstructure:"id" json:"id"`
	IP string `mapstructure:"ip" json:"ip"`

	id ids.NodeID
	ip netip.AddrPort
}

func (c *PeerConfig) Validate() error {
	var (
		id  ids.NodeID
		ip  netip.AddrPort
		err error
	)
	if id, err = ids.NodeIDFromString(c.ID); err != nil {
		return err
	}
	if ip, err = netip.ParseAddrPort(c.IP); err != nil {
		return err
	}
	c.id = id
	c.ip = ip

	return nil
}

func (c *PeerConfig) GetID() ids.NodeID {
	return c.id
}

func (c *PeerConfig) GetIP() netip.AddrPort {
	return c.ip
}
