// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

// Supported VMs
type VM int

const (
	UNKNOWN_VM VM = iota
	EVM
)

func (vm VM) String() string {
	switch vm {
	case EVM:
		return "evm"
	default:
		return "unknown"
	}
}

// ParseVM returns the VM corresponding to [vm]
func ParseVM(vm string) VM {
	switch vm {
	case "evm":
		return EVM
	default:
		return UNKNOWN_VM
	}
}

// Supported Message Protocols
type MessageProtocol int

const (
	UNKNOWN_MESSAGE_PROTOCOL MessageProtocol = iota
	TELEPORTER
	OFF_CHAIN
)

func (msg MessageProtocol) String() string {
	switch msg {
	case TELEPORTER:
		return "teleporter"
	case OFF_CHAIN:
		return "off-chain"
	default:
		return "unknown"
	}
}

// ParseMessageProtocol returns the MessageProtocol corresponding to [msg]
func ParseMessageProtocol(msg string) MessageProtocol {
	switch msg {
	case "teleporter":
		return TELEPORTER
	case "off-chain":
		return OFF_CHAIN
	default:
		return UNKNOWN_MESSAGE_PROTOCOL
	}
}
