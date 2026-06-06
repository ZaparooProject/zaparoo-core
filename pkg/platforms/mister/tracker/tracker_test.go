//go:build linux

package tracker

import (
	"context"
	"testing"
	"time"
)

func TestMediaLookupContextUsesServiceContext(t *testing.T) {
	t.Parallel()

	serviceCtx, cancelService := context.WithCancel(context.Background())
	tr := &Tracker{serviceCtx: serviceCtx}

	ctx, cancelLookup := tr.mediaLookupContext()
	defer cancelLookup()

	select {
	case <-ctx.Done():
		t.Fatal("MediaDB lookup context should not be canceled before service context")
	default:
	}

	cancelService()
	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("MediaDB lookup context should follow service context")
	}
}
