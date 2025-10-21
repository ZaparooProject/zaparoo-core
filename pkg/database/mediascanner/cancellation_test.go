// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package mediascanner

import (
	"context"
	"errors"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setIndexingStatusError error
		name                   string
		message                string
		expectedReturnValue    int
		expectedContextError   bool
	}{
		{
			name:                   "successful cancellation",
			message:                "Test cancellation message",
			setIndexingStatusError: nil,
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
		{
			name:                   "cancellation with status error",
			message:                "Test cancellation with error",
			setIndexingStatusError: errors.New("status error"),
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
		{
			name:                   "empty message",
			message:                "",
			setIndexingStatusError: nil,
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create cancelled context
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			// Create mock database
			mockDB := &helpers.MockMediaDBI{}
			mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCancelled).Return(tt.setIndexingStatusError)

			// Call the function
			result, err := handleCancellation(ctx, mockDB, tt.message)

			// Verify expectations
			assert.Equal(t, tt.expectedReturnValue, result)

			if tt.expectedContextError {
				require.Error(t, err)
				assert.Equal(t, context.Canceled, err)
			} else {
				require.NoError(t, err)
			}

			// Verify mock expectations
			mockDB.AssertExpectations(t)
		})
	}
}

func TestHandleCancellationWithRollback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rollbackError          error
		setIndexingStatusError error
		name                   string
		message                string
		expectedReturnValue    int
		expectedContextError   bool
	}{
		{
			name:                   "successful cancellation with rollback",
			message:                "Test cancellation with rollback",
			rollbackError:          nil,
			setIndexingStatusError: nil,
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
		{
			name:                   "rollback error but status success",
			message:                "Test with rollback error",
			rollbackError:          errors.New("rollback failed"),
			setIndexingStatusError: nil,
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
		{
			name:                   "rollback success but status error",
			message:                "Test with status error",
			rollbackError:          nil,
			setIndexingStatusError: errors.New("status error"),
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
		{
			name:                   "both rollback and status errors",
			message:                "Test with both errors",
			rollbackError:          errors.New("rollback failed"),
			setIndexingStatusError: errors.New("status error"),
			expectedReturnValue:    0,
			expectedContextError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create cancelled context
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			// Create mock database
			mockDB := &helpers.MockMediaDBI{}
			mockDB.On("RollbackTransaction").Return(tt.rollbackError)
			mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCancelled).Return(tt.setIndexingStatusError)

			// Call the function
			result, err := handleCancellationWithRollback(ctx, mockDB, tt.message)

			// Verify expectations
			assert.Equal(t, tt.expectedReturnValue, result)

			if tt.expectedContextError {
				require.Error(t, err)
				assert.Equal(t, context.Canceled, err)
			} else {
				require.NoError(t, err)
			}

			// Verify mock expectations
			mockDB.AssertExpectations(t)
		})
	}
}

func TestCancellationHelpers_ErrorLogging(t *testing.T) {
	t.Parallel()

	// This test verifies that errors are logged but don't prevent cancellation completion

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("handleCancellation logs status errors", func(t *testing.T) {
		mockDB := &helpers.MockMediaDBI{}
		statusError := errors.New("failed to set status")
		mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCancelled).Return(statusError)

		// Should complete successfully despite status error
		result, err := handleCancellation(ctx, mockDB, "test message")

		assert.Equal(t, 0, result)
		assert.Equal(t, context.Canceled, err)
		mockDB.AssertExpectations(t)
	})

	t.Run("handleCancellationWithRollback logs both errors", func(t *testing.T) {
		mockDB := &helpers.MockMediaDBI{}
		rollbackError := errors.New("failed to rollback")
		statusError := errors.New("failed to set status")

		mockDB.On("RollbackTransaction").Return(rollbackError)
		mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCancelled).Return(statusError)

		// Should complete successfully despite both errors
		result, err := handleCancellationWithRollback(ctx, mockDB, "test message")

		assert.Equal(t, 0, result)
		assert.Equal(t, context.Canceled, err)
		mockDB.AssertExpectations(t)
	})
}

func TestCancellationHelpers_ContextTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expectedErr error
		setupCtx    func() context.Context
		name        string
	}{
		{
			name: "context.Canceled",
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			expectedErr: context.Canceled,
		},
		{
			name: "context.DeadlineExceeded",
			setupCtx: func() context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 0)
				defer cancel()
				<-ctx.Done() // Wait for timeout
				return ctx
			},
			expectedErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupCtx()

			// Test handleCancellation
			mockDB1 := &helpers.MockMediaDBI{}
			mockDB1.On("SetIndexingStatus", mock.Anything).Return(nil)

			result, err := handleCancellation(ctx, mockDB1, "test")
			assert.Equal(t, 0, result)
			assert.Equal(t, tt.expectedErr, err)

			// Test handleCancellationWithRollback
			mockDB2 := &helpers.MockMediaDBI{}
			mockDB2.On("RollbackTransaction").Return(nil)
			mockDB2.On("SetIndexingStatus", mock.Anything).Return(nil)

			result, err = handleCancellationWithRollback(ctx, mockDB2, "test")
			assert.Equal(t, 0, result)
			assert.Equal(t, tt.expectedErr, err)
		})
	}
}

func TestCancellationHelpers_DatabaseInterface(t *testing.T) {
	t.Parallel()

	// Test that both functions work with the MediaDBI interface correctly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("handleCancellation calls correct interface methods", func(t *testing.T) {
		mockDB := &helpers.MockMediaDBI{}

		// Expect only SetIndexingStatus to be called
		mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCancelled).Return(nil).Once()

		// Ensure RollbackTransaction is NOT called
		mockDB.AssertNotCalled(t, "RollbackTransaction")

		_, err := handleCancellation(ctx, mockDB, "test")
		require.Error(t, err)

		mockDB.AssertExpectations(t)
	})

	t.Run("handleCancellationWithRollback calls correct interface methods", func(t *testing.T) {
		mockDB := &helpers.MockMediaDBI{}

		// Expect both methods to be called in correct order
		mockDB.On("RollbackTransaction").Return(nil).Once()
		mockDB.On("SetIndexingStatus", mediadb.IndexingStatusCancelled).Return(nil).Once()

		_, err := handleCancellationWithRollback(ctx, mockDB, "test")
		require.Error(t, err)

		mockDB.AssertExpectations(t)
	})
}
