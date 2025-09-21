package methods

import (
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleMedia_TotalMediaCount(t *testing.T) {
	tests := []struct {
		name                    string
		optimizationStatus      string
		indexing                bool
		totalMediaCount         int
		getTotalMediaCountError error
		expectedTotalMedia      *int
	}{
		{
			name:                "database exists and not indexing - includes total count",
			optimizationStatus:  "",
			indexing:            false,
			totalMediaCount:     1337,
			getTotalMediaCountError: nil,
			expectedTotalMedia:  intPtr(1337),
		},
		{
			name:                "database indexing - no total count",
			optimizationStatus:  "",
			indexing:            true,
			totalMediaCount:     1337,
			getTotalMediaCountError: nil,
			expectedTotalMedia:  nil,
		},
		{
			name:                "database optimizing - includes total count",
			optimizationStatus:  "running",
			indexing:            false,
			totalMediaCount:     500,
			getTotalMediaCountError: nil,
			expectedTotalMedia:  intPtr(500),
		},
		{
			name:                "GetTotalMediaCount error - no total count",
			optimizationStatus:  "",
			indexing:            false,
			totalMediaCount:     0,
			getTotalMediaCountError: assert.AnError,
			expectedTotalMedia:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockMediaDB := &helpers.MockMediaDBI{}
			mockUserDB := &helpers.MockUserDBI{}
			mockPlatform := mocks.NewMockPlatform()
			testState, _ := state.NewState(mockPlatform)

			// Mock optimization status
			mockMediaDB.On("GetOptimizationStatus").Return(tt.optimizationStatus, nil)

			// Set indexing status
			statusInstance.indexing = tt.indexing

			if tt.optimizationStatus == "running" && !tt.indexing {
				// Database exists but is optimizing
				mockMediaDB.On("GetOptimizationStep").Return("", nil)
				mockMediaDB.On("GetTotalMediaCount").Return(tt.totalMediaCount, tt.getTotalMediaCountError)
			} else if !tt.indexing {
				// Database exists and not indexing or optimizing
				mockMediaDB.On("GetLastGenerated").Return(time.Now(), nil)
				mockMediaDB.On("GetTotalMediaCount").Return(tt.totalMediaCount, tt.getTotalMediaCountError)
			}

			db := &database.Database{
				MediaDB: mockMediaDB,
				UserDB:  mockUserDB,
			}
			env := requests.RequestEnv{
				Database: db,
				State:    testState,
			}

			result, err := HandleMedia(env)
			require.NoError(t, err)

			response, ok := result.(models.MediaResponse)
			require.True(t, ok, "result should be MediaResponse")

			if tt.expectedTotalMedia != nil {
				require.NotNil(t, response.Database.TotalMedia, "TotalMedia should not be nil")
				assert.Equal(t, *tt.expectedTotalMedia, *response.Database.TotalMedia, "TotalMedia value should match expected")
			} else {
				assert.Nil(t, response.Database.TotalMedia, "TotalMedia should be nil")
			}

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func intPtr(i int) *int {
	return &i
}