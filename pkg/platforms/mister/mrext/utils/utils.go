package utils

import (
	"bufio"
	"fmt"
	"io/fs"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"golang.org/x/exp/constraints"
	"golang.org/x/term"
)

var r = rand.New(rand.NewSource(time.Now().UnixNano()))



// MoveFile moves a file. Supports moving between filesystems.
func MoveFile(sourcePath, destPath string) error {
	err := helpers.CopyFile(sourcePath, destPath)
	if err != nil {
		return err
	}

	err = os.Remove(sourcePath)
	if err != nil {
		return err
	}

	return nil
}

// Max returns the highest value in a slice.
func Max[T constraints.Ordered](xs []T) T {
	if len(xs) == 0 {
		var zv T
		return zv
	}
	max := xs[0]
	for _, x := range xs {
		if x > max {
			max = x
		}
	}
	return max
}

// Min returns the lowest value in a slice.
func Min[T constraints.Ordered](xs []T) T {
	if len(xs) == 0 {
		var zv T
		return zv
	}
	min := xs[0]
	for _, x := range xs {
		if x < min {
			min = x
		}
	}
	return min
}


func StripChars(s string, chars string) string {
	for _, c := range chars {
		s = strings.ReplaceAll(s, string(c), "")
	}
	return s
}

// StripBadFileChars removes all characters from a string that are not allowed in filenames.
func StripBadFileChars(s string) string {
	return StripChars(s, "/\\:*?\"<>|")
}


// YesOrNoPrompt displays a simple yes/no prompt for use with a controller.
func YesOrNoPrompt(prompt string) bool {
	fmt.Printf("%s [DOWN=Yes/UP=No] ", prompt)

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	reader := bufio.NewReader(os.Stdin)
	buf := make([]byte, 3)
	reader.Read(buf)

	term.Restore(int(os.Stdin.Fd()), oldState)

	delay := func() { time.Sleep(400 * time.Millisecond) }

	if buf[0] == 27 && buf[1] == 91 && buf[2] == 66 {
		fmt.Println("Yes")
		delay()
		return true
	} else {
		// 27 91 65 is up arrow
		fmt.Println("No")
		delay()
		return false
	}
}

// InfoPrompt displays an information prompt for use with a controller.
func InfoPrompt(prompt string) {
	fmt.Println(prompt)
	fmt.Println("Press any key to continue...")

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		panic(err)
	}

	reader := bufio.NewReader(os.Stdin)
	buf := make([]byte, 1)
	reader.Read(buf)

	term.Restore(int(os.Stdin.Fd()), oldState)

	time.Sleep(400 * time.Millisecond)
}

func IsEmptyDir(path string) (bool, error) {
	dir, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}

	return len(dir) == 0, nil
}

// RemoveEmptyDirs removes all empty folders in a path, including folders containing only empty
// folders and the path itself.
func RemoveEmptyDirs(path string) error {
	var dirs []string

	err := filepath.WalkDir(path, func(path string, info fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			dirs = append(dirs, path)
		}

		return nil
	})

	if err != nil {
		return err
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]

		empty, err := IsEmptyDir(dir)
		if err != nil {
			return err
		}

		if empty {
			err = os.Remove(dir)
			if err != nil {
				return err
			}
		}
	}

	rootEmpty, err := IsEmptyDir(path)
	if err != nil {
		return err
	} else if rootEmpty {
		err = os.Remove(path)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetLocalIp() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP, nil
}



func Reverse[S ~[]E, E any](s S) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func RemoveFileExt(s string) string {
	parts := strings.Split(s, ".")
	if len(parts) > 1 {
		return strings.Join(parts[:len(parts)-1], ".")
	}
	return s
}
