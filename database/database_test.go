package database

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestIsKeyNotFoundError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "key not found error",
			err:      errKeyNotFound,
			expected: true,
		},
		{
			name:     "relayer key not found error",
			err:      errRelayerIDNotFound,
			expected: true,
		},
		{
			name:     "unknown error",
			err:      errors.New("unknown error"),
			expected: false,
		},
	}
	for _, testCase := range testCases {
		result := isKeyNotFoundError(testCase.err)
		require.Equal(t, testCase.expected, result, testCase.name)
	}
}
