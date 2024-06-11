// (c) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: Ecosystem

pragma solidity 0.8.18;

import {TeleporterMessageInput, TeleporterFeeInfo} from "@teleporter/ITeleporterMessenger.sol";
import {SafeERC20TransferFrom, SafeERC20} from "@teleporter/SafeERC20TransferFrom.sol";
import {TeleporterOwnerUpgradeable} from "@teleporter/upgrades/TeleporterOwnerUpgradeable.sol";
import {IERC20} from "@openzeppelin/contracts@4.8.1/token/ERC20/IERC20.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts@4.8.1/security/ReentrancyGuard.sol";

/**
 * THIS IS AN EXAMPLE CONTRACT THAT USES UN-AUDITED CODE.
 * DO NOT USE THIS CODE IN PRODUCTION.
 */

/**
 * @dev BatchCrossChainMessenger batches multiple Teleporter messages into a single transaction
 */
contract BatchCrossChainMessenger is ReentrancyGuard, TeleporterOwnerUpgradeable {
    using SafeERC20 for IERC20;

    // Messages sent to this contract.
    struct Messages {
        address sender;
        string[] messages;
    }

    mapping(bytes32 sourceBlockchainID => string[] messages) private _messages;

    /**
     * @dev Emitted when a message is submited to be sent.
     */
    event SendMessages(
        bytes32 indexed destinationBlockchainID,
        address indexed destinationAddress,
        address feeTokenAddress,
        uint256 feeAmount,
        uint256 requiredGasLimit,
        string[] messages
    );

    /**
     * @dev Emitted when a new message is received from a given chain ID.
     */
    event ReceiveMessage(
        bytes32 indexed sourceBlockchainID, address indexed originSenderAddress, string message
    );

    constructor(
        address teleporterRegistryAddress,
        address teleporterManager
    ) TeleporterOwnerUpgradeable(teleporterRegistryAddress, teleporterManager) {}

    /**
     * @dev Sends a message to another chain.
     * @return The message ID of the newly sent message.
     */
    function sendMessages(
        bytes32 destinationBlockchainID,
        address destinationAddress,
        address feeTokenAddress,
        uint256 feeAmount,
        uint256 requiredGasLimit,
        string[] memory messages
    ) external nonReentrant returns (bytes32[] memory) {
        // For non-zero fee amounts, first transfer the fee to this contract.
        uint256 adjustedFeeAmount;
        if (feeAmount > 0) {
            adjustedFeeAmount =
                SafeERC20TransferFrom.safeTransferFrom(IERC20(feeTokenAddress), feeAmount);
        }

        emit SendMessages({
            destinationBlockchainID: destinationBlockchainID,
            destinationAddress: destinationAddress,
            feeTokenAddress: feeTokenAddress,
            feeAmount: adjustedFeeAmount,
            requiredGasLimit: requiredGasLimit,
            messages: messages
        });
        bytes32[] memory messageIDs = new bytes32[](messages.length);
        for (uint256 i = 0; i < messages.length; i++) {
            bytes32 messageID = _sendTeleporterMessage(
                TeleporterMessageInput({
                    destinationBlockchainID: destinationBlockchainID,
                    destinationAddress: destinationAddress,
                    feeInfo: TeleporterFeeInfo({feeTokenAddress: feeTokenAddress, amount: adjustedFeeAmount}),
                    requiredGasLimit: requiredGasLimit,
                    allowedRelayerAddresses: new address[](0),
                    message: abi.encode(messages[i])
                })
            );
            messageIDs[i] = messageID;
        }
        return messageIDs;
    }

    /**
     * @dev Returns the current message from another chain.
     * @return The sender of the message, and the message itself.
     */
    function getCurrentMessages(bytes32 sourceBlockchainID)
        external
        view
        returns (string[] memory)
    {
        string[] memory messages = _messages[sourceBlockchainID];
        return messages;
    }

    /**
     * @dev See {TeleporterUpgradeable-receiveTeleporterMessage}.
     *
     * Receives a message from another chain.
     */
    function _receiveTeleporterMessage(
        bytes32 sourceBlockchainID,
        address originSenderAddress,
        bytes memory message
    ) internal override {
        // Store the message.
        string memory messageString = abi.decode(message, (string));
        _messages[sourceBlockchainID].push(messageString);
        emit ReceiveMessage(sourceBlockchainID, originSenderAddress, messageString);
    }
}
