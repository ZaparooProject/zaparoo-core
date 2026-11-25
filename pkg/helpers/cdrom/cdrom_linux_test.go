//go:build linux

package cdrom

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func overrideUnixIO(
	t *testing.T,
	pread func(int, []byte, int64) (int, error),
	closeFn func(int) error,
) {
	t.Helper()
	oldPread := unixPread
	oldClose := unixClose
	oldDelay := readRetryDelay
	unixPread = pread
	unixClose = closeFn
	readRetryDelay = time.Millisecond
	t.Cleanup(func() {
		unixPread = oldPread
		unixClose = oldClose
		readRetryDelay = oldDelay
	})
}

func TestUnixDiscDeviceReaderReadAtContext_PartialReads(t *testing.T) {
	calls := 0
	offsets := make([]int64, 0, 2)
	overrideUnixIO(t, func(_ int, p []byte, off int64) (int, error) {
		calls++
		offsets = append(offsets, off)
		if calls == 1 {
			return copy(p, "ab"), nil
		}
		return copy(p, "cd"), nil
	}, unix.Close)

	buf := make([]byte, 4)
	n, err := (&linuxDrive{fd: -1}).ReadAtContext(context.Background(), buf, 10)

	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, "abcd", string(buf))
	assert.Equal(t, []int64{10, 12}, offsets)
}

func TestUnixDiscDeviceReaderReadAtContext_RetriesTemporaryErrors(t *testing.T) {
	calls := 0
	overrideUnixIO(t, func(_ int, p []byte, _ int64) (int, error) {
		calls++
		switch calls {
		case 1:
			return 0, unix.EAGAIN
		case 2:
			return 0, unix.EINTR
		default:
			return copy(p, "ok"), nil
		}
	}, unix.Close)

	buf := make([]byte, 2)
	n, err := (&linuxDrive{fd: -1}).ReadAtContext(context.Background(), buf, 0)

	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, "ok", string(buf))
	assert.Equal(t, 3, calls)
}

func TestUnixDiscDeviceReaderReadAtContext_ZeroReadIsEOF(t *testing.T) {
	overrideUnixIO(t, func(_ int, _ []byte, _ int64) (int, error) {
		return 0, nil
	}, unix.Close)

	n, err := (&linuxDrive{fd: -1}).ReadAtContext(context.Background(), make([]byte, 1), 0)

	assert.Equal(t, 0, n)
	require.ErrorIs(t, err, io.EOF)
}

func TestUnixDiscDeviceReaderReadAtContext_CancelDuringRetry(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	overrideUnixIO(t, func(_ int, _ []byte, _ int64) (int, error) {
		calls++
		cancel()
		return 0, unix.EAGAIN
	}, unix.Close)

	n, err := (&linuxDrive{fd: -1}).ReadAtContext(ctx, make([]byte, 1), 0)

	assert.Equal(t, 0, n)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}

func TestUnixDiscDeviceReaderClose_ReturnsCloseError(t *testing.T) {
	overrideUnixIO(t, unix.Pread, func(int) error {
		return unix.EBADF
	})

	err := (&linuxDrive{fd: -1}).Close()

	require.ErrorIs(t, err, unix.EBADF)
}

func TestIsUnsupportedRequest(t *testing.T) {
	t.Parallel()

	for _, err := range []error{unix.ENOTTY, unix.EINVAL, unix.ENOSYS, ErrUnsupported} {
		assert.True(t, IsUnsupportedRequest(fmt.Errorf("status: %w", err)), err.Error())
	}
	assert.False(t, IsUnsupportedRequest(unix.EIO))
	assert.False(t, IsUnsupportedRequest(nil))
}

func TestReadCDTextPacksRequestsExactLength(t *testing.T) {
	t.Parallel()

	packs := make([]byte, 36)
	for i := range packs {
		packs[i] = byte(i + 1)
	}
	response := append([]byte{0, 38, 0, 0}, packs...) // data length: packs plus 2

	var allocations []int
	read := func(cdb, dst []byte) (int, error) {
		allocations = append(allocations, int(cdb[7])<<8|int(cdb[8]))
		return copy(dst, response), nil
	}
	got, err := readCDTextPacks(read)
	require.NoError(t, err)
	assert.Equal(t, packs, got)
	assert.Equal(t, []int{4, len(response)}, allocations,
		"a larger request gets a short transfer, which resets drives on MiSTer")
}

func TestReadCDTextPacksEmpty(t *testing.T) {
	t.Parallel()

	calls := 0
	got, err := readCDTextPacks(func(_, dst []byte) (int, error) {
		calls++
		return copy(dst, []byte{0, 2, 0, 0}), nil
	})
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 1, calls)

	_, err = readCDTextPacks(func(_, _ []byte) (int, error) { return 0, errSCSI })
	require.ErrorIs(t, err, errSCSI)
}
