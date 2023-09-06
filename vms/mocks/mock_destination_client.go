// Code generated by MockGen. DO NOT EDIT.
// Source: destination_client.go

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	ids "github.com/ava-labs/avalanchego/ids"
	warp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	common "github.com/ethereum/go-ethereum/common"
	gomock "go.uber.org/mock/gomock"
)

// MockDestinationClient is a mock of DestinationClient interface.
type MockDestinationClient struct {
	ctrl     *gomock.Controller
	recorder *MockDestinationClientMockRecorder
}

// MockDestinationClientMockRecorder is the mock recorder for MockDestinationClient.
type MockDestinationClientMockRecorder struct {
	mock *MockDestinationClient
}

// NewMockDestinationClient creates a new mock instance.
func NewMockDestinationClient(ctrl *gomock.Controller) *MockDestinationClient {
	mock := &MockDestinationClient{ctrl: ctrl}
	mock.recorder = &MockDestinationClientMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDestinationClient) EXPECT() *MockDestinationClientMockRecorder {
	return m.recorder
}

// Allowed mocks base method.
func (m *MockDestinationClient) Allowed(chainID ids.ID, allowedRelayers []common.Address) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Allowed", chainID, allowedRelayers)
	ret0, _ := ret[0].(bool)
	return ret0
}

// Allowed indicates an expected call of Allowed.
func (mr *MockDestinationClientMockRecorder) Allowed(chainID, allowedRelayers interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Allowed", reflect.TypeOf((*MockDestinationClient)(nil).Allowed), chainID, allowedRelayers)
}

// SendTx mocks base method.
func (m *MockDestinationClient) SendTx(signedMessage *warp.Message, toAddress string, gasLimit uint64, callData []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SendTx", signedMessage, toAddress, gasLimit, callData)
	ret0, _ := ret[0].(error)
	return ret0
}

// SendTx indicates an expected call of SendTx.
func (mr *MockDestinationClientMockRecorder) SendTx(signedMessage, toAddress, gasLimit, callData interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SendTx", reflect.TypeOf((*MockDestinationClient)(nil).SendTx), signedMessage, toAddress, gasLimit, callData)
}
