package signer

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

func TestRecoverEIP155Signature(t *testing.T) {
	testCases := []struct {
		name              string
		txHash            string
		rValue            string
		sValue            string
		pubKey            string
		expectedSignature string
		expectedError     bool
	}{
		{
			name:              "valid signature",
			txHash:            "69963cf4839e149fea0e7b0969dd6834ea4b22fa7f9209a46683982320e5edfd",
			rValue:            "00a94bfca53b42454acc43e7328ce7a8f244a629026095c36c0ee82607377cbee4",
			sValue:            "5964b0dd9bf23723570b6a073cb945fd1ba853ebf5579c8d11bb4fdb305cd079",
			pubKey:            "04a3b664aa1f37bf6c46a3f2cdb209091e16070208b30244838e2cb9fe1465c4911ba2ab0c54bfc39972740ef7b24904f8740b0a69aca2bbfce0f2829429b9a5c5",
			expectedSignature: "a94bfca53b42454acc43e7328ce7a8f244a629026095c36c0ee82607377cbee45964b0dd9bf23723570b6a073cb945fd1ba853ebf5579c8d11bb4fdb305cd07901",
			expectedError:     false,
		},
		{
			name:              "invalid r value",
			txHash:            "69963cf4839e149fea0e7b0969dd6834ea4b22fa7f9209a46683982320e5edfd",
			rValue:            "10a94bfca53b42454acc43e7328ce7a8f244a629026095c36c0ee82607377cbee4",
			sValue:            "5964b0dd9bf23723570b6a073cb945fd1ba853ebf5579c8d11bb4fdb305cd079",
			pubKey:            "04a3b664aa1f37bf6c46a3f2cdb209091e16070208b30244838e2cb9fe1465c4911ba2ab0c54bfc39972740ef7b24904f8740b0a69aca2bbfce0f2829429b9a5c5",
			expectedSignature: "a94bfca53b42454acc43e7328ce7a8f244a629026095c36c0ee82607377cbee45964b0dd9bf23723570b6a073cb945fd1ba853ebf5579c8d11bb4fdb305cd07901",
			expectedError:     true,
		},
		{
			name:              "invalid s value",
			txHash:            "69963cf4839e149fea0e7b0969dd6834ea4b22fa7f9209a46683982320e5edfd",
			rValue:            "00a94bfca53b42454acc43e7328ce7a8f244a629026095c36c0ee82607377cbee4",
			sValue:            "1964b0dd9bf23723570b6a073cb945fd1ba853ebf5579c8d11bb4fdb305cd079",
			pubKey:            "04a3b664aa1f37bf6c46a3f2cdb209091e16070208b30244838e2cb9fe1465c4911ba2ab0c54bfc39972740ef7b24904f8740b0a69aca2bbfce0f2829429b9a5c5",
			expectedSignature: "a94bfca53b42454acc43e7328ce7a8f244a629026095c36c0ee82607377cbee45964b0dd9bf23723570b6a073cb945fd1ba853ebf5579c8d11bb4fdb305cd07901",
			expectedError:     true,
		},
		{
			name:              "s-value too high",
			txHash:            "e29dc6b15950a5433e239155a2208156ba8fd81eeb81ade45329d4b2fb2e8421",
			rValue:            "00d993636ed097bf04b7c982e0394d572ead3c8f23ecc8baba0bbdfaa91aba4376",
			sValue:            "00d1ac23b517ae2569522626096fa1922681c87612cceac971e855d42103fea7c6", // This is > secp256k1n/2
			pubKey:            "04a3b664aa1f37bf6c46a3f2cdb209091e16070208b30244838e2cb9fe1465c4911ba2ab0c54bfc39972740ef7b24904f8740b0a69aca2bbfce0f2829429b9a5c5",
			expectedSignature: "d993636ed097bf04b7c982e0394d572ead3c8f23ecc8baba0bbdfaa91aba43762e53dc4ae851da96add9d9f6905e6dd838e666d3e25dd6c9d77c8a6bcc37997b00",
			expectedError:     false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			signer := &KMSSigner{
				pubKey: common.Hex2Bytes(testCase.pubKey),
			}
			txHash := common.Hex2Bytes(testCase.txHash)
			rValue := common.Hex2Bytes(testCase.rValue)
			sValue := common.Hex2Bytes(testCase.sValue)
			signature, err := signer.recoverEIP155Signature(txHash, rValue, sValue)
			if testCase.expectedError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, common.Hex2Bytes(testCase.expectedSignature), signature)
			}
		})
	}
}
