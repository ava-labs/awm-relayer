// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

// Supported VMs
type VM int

const (
	UNKNOWN_VM VM = iota
	EVM
	EVM_BLOCKHASH
)

func (vm VM) String() string {
	switch vm {
	case EVM:
		return "evm"
	case EVM_BLOCKHASH:
		return "evm_blockhash"
	default:
		return "unknown"
	}
}

// ParseVM returns the VM corresponding to [vm]
func ParseVM(vm string) VM {
	switch vm {
	case "evm":
		return EVM
	case "evm_blockhash":
		return EVM_BLOCKHASH
	default:
		return UNKNOWN_VM
	}
}

// Supported Message Protocols
type MessageProtocol int

const (
	UNKNOWN_MESSAGE_PROTOCOL MessageProtocol = iota
	TELEPORTER
	BLOCK_HASH_PUBLISHER
)

func (msg MessageProtocol) String() string {
	switch msg {
	case TELEPORTER:
		return "teleporter"
	case BLOCK_HASH_PUBLISHER:
		return "block_hash_publisher"
	default:
		return "unknown"
	}
}

// ParseMessageProtocol returns the MessageProtocol corresponding to [msg]
func ParseMessageProtocol(msg string) MessageProtocol {
	switch msg {
	case "teleporter":
		return TELEPORTER
	case "block_hash_publisher":
		return BLOCK_HASH_PUBLISHER
	default:
		return UNKNOWN_MESSAGE_PROTOCOL
	}
}
