// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "testing"

func TestConvertProtocol(t *testing.T) {
	testCases := []struct {
		urlString     string
		protocol      string
		expectedUrl   string
		expectedError bool
	}{
		{
			urlString:     "http://www.hello.com",
			protocol:      "https",
			expectedUrl:   "https://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "https://www.hello.com",
			protocol:      "http",
			expectedUrl:   "http://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "http://www.hello.com",
			protocol:      "http",
			expectedUrl:   "http://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "https://www.hello.com",
			protocol:      "https",
			expectedUrl:   "https://www.hello.com",
			expectedError: false,
		},
		{
			urlString:     "http://www.hello.com",
			protocol:      "\n",
			expectedUrl:   "",
			expectedError: true,
		},
	}

	for i, testCase := range testCases {
		actualUrl, err := ConvertProtocol(testCase.urlString, testCase.protocol)
		if err != nil && !testCase.expectedError {
			t.Errorf("test case %d failed with unexpected error", i)
		}
		if err == nil && testCase.expectedError {
			t.Errorf("test case %d did not produce expected error", i)
		}
		if actualUrl != testCase.expectedUrl {
			t.Errorf("test case %d had unexpected URL. Actual: %s, Expected: %s", i, actualUrl, testCase.expectedUrl)
		}
	}
}

func TestSanitizeHashString(t *testing.T) {
	testCases := []struct {
		hash           string
		expectedResult string
	}{
		// Remove leading 0x from hex string
		{
			hash:           "0x1234",
			expectedResult: "1234",
		},
		// Return original hex string
		{
			hash:           "1234",
			expectedResult: "1234",
		},
		// Return original string, leading 0x is not hex
		{
			hash:           "0x1234g",
			expectedResult: "0x1234g",
		},
	}
	for i, testCase := range testCases {
		actualResult := SanitizeHashString(testCase.hash)
		if actualResult != testCase.expectedResult {
			t.Errorf("test case %d had unexpected result. Actual: %s, Expected: %s", i, actualResult, testCase.expectedResult)
		}
	}
}
