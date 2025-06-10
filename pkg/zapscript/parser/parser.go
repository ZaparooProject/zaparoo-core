package parser

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
)

var (
	ErrUnexpectedEOF  = errors.New("unexpected end of file")
	ErrInvalidCmdName = errors.New("invalid characters in command name")
	ErrEmptyCmdName   = errors.New("command name is empty")
	ErrEmptyZapScript = errors.New("zap script is empty")
)

const (
	SymCmdStart    = '*'
	SymCmdSep      = '|'
	SymEscapeSeq   = '\\'
	SymArgStart    = ':'
	SymArgSep      = ','
	SymArgQuote    = '"'
	SymAdvArgStart = '?'
	SymAdvArgSep   = '&'
	SymAdvArgEq    = '='
)

type Command struct {
	Name    string
	Args    []string
	AdvArgs map[string]string
}

type Script struct {
	Cmds []Command
}

func isCmdName(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.'
}

func isWhitespace(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

var eof = rune(0)

type ScriptReader struct {
	r   *bufio.Reader
	pos int64
}

func NewScriptReader(script string) *ScriptReader {
	return &ScriptReader{
		r: bufio.NewReader(bytes.NewReader([]byte(script))),
	}
}

func (sr *ScriptReader) read() (rune, error) {
	ch, _, err := sr.r.ReadRune()
	if errors.Is(err, io.EOF) {
		return eof, nil
	} else if err != nil {
		return eof, err
	}
	sr.pos++
	return ch, nil
}

func (sr *ScriptReader) unread() error {
	err := sr.r.UnreadRune()
	if err != nil {
		return err
	}
	sr.pos--
	return nil
}

func (sr *ScriptReader) peek() (rune, error) {
	for peekBytes := 4; peekBytes > 0; peekBytes-- {
		b, err := sr.r.Peek(peekBytes)
		if err == nil {
			r, _ := utf8.DecodeRune(b)
			if r == utf8.RuneError {
				return r, fmt.Errorf("rune error")
			}
			return r, nil
		}
	}
	return eof, nil
}

func (sr *ScriptReader) skip() error {
	_, err := sr.read()
	if err != nil {
		return err
	} else {
		return nil
	}
}

func (sr *ScriptReader) checkEndOfCmd(ch rune) (bool, error) {
	if ch != SymCmdSep {
		return false, nil
	}

	next, err := sr.peek()
	if err != nil {
		return false, err
	}

	if next == eof {
		return true, nil
	} else if next == SymCmdSep {
		err := sr.skip()
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

func (sr *ScriptReader) parseGenericLaunchArg() (string, error) {
	arg := ""

	for {
		ch, err := sr.read()
		if err != nil {
			return arg, err
		} else if ch == eof {
			// an eof is effectively the same as || here
			break
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return arg, err
		} else if eoc {
			break
		}

		arg = arg + string(ch)
	}

	return arg, nil
}

func (sr *ScriptReader) parseArgs() ([]string, error) {
	args := make([]string, 0)
	currentArg := ""

	for {
		ch, err := sr.read()
		if err != nil {
			return args, err
		} else if ch == eof {
			// an eof is effectively the same as || here
			break
		}

		if ch == SymEscapeSeq {
			// escaping next character
			next, err := sr.read()
			if err != nil {
				return args, err
			} else if next == eof {
				break
			}

			if slices.Contains(
				[]rune{
					SymEscapeSeq, SymArgSep, SymCmdSep,
					SymAdvArgStart, SymArgQuote,
				},
				next,
			) {
				// insert escaped char and continue
				currentArg = currentArg + string(next)
				continue
			} else {
				// insert literal \<char> and continue
				currentArg = currentArg + string(SymEscapeSeq) + string(next)
				continue
			}
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return args, err
		} else if eoc {
			break
		}

		if ch == SymArgSep {
			// new argument
			args = append(args, currentArg)
			currentArg = ""
			continue
		} else {
			currentArg = currentArg + string(ch)
			continue
		}
	}

	if currentArg != "" {
		args = append(args, currentArg)
	}

	return args, nil
}

func (sr *ScriptReader) parseCommand() (Command, error) {
	cmd := Command{}

	for {
		ch, err := sr.read()
		if err != nil {
			return cmd, err
		} else if ch == eof {
			// an eof is effectively the same as || here
			break
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return cmd, err
		} else if eoc {
			break
		}

		if isCmdName(ch) {
			cmd.Name = cmd.Name + string(ch)
		} else if ch == SymArgStart {
			// parse arguments
			if cmd.Name == "" {
				break
			}

			args, err := sr.parseArgs()
			if err != nil {
				return cmd, err
			} else if len(args) > 0 {
				cmd.Args = args
			}

			break
		} else {
			// might be a launch cmd
			for i := 0; i < len(cmd.Name)+1; i++ {
				err := sr.unread()
				if err != nil {
					return cmd, err
				}
			}
			return cmd, ErrInvalidCmdName
		}
	}

	if cmd.Name == "" {
		return cmd, ErrEmptyCmdName
	}

	return cmd, nil
}

func (sr *ScriptReader) Parse() (Script, error) {
	script := Script{}

	parseErr := func(err error) error {
		return fmt.Errorf("parse error at %d: %w", sr.pos, err)
	}

	parseGenericLaunchCmd := func() error {
		arg, err := sr.parseGenericLaunchArg()
		if err != nil {
			return fmt.Errorf("parse error at %d: %w", sr.pos, err)
		}
		if arg == "" {
			return nil
		}
		script.Cmds = append(script.Cmds, Command{
			Name: models.ZapScriptCmdLaunch,
			Args: []string{arg},
		})
		return nil
	}

	for {
		ch, err := sr.read()
		if err != nil {
			return script, err
		} else if ch == eof {
			break
		}

		if isWhitespace(ch) {
			continue
		} else if ch == SymCmdStart {
			next, err := sr.peek()
			if err != nil {
				return script, parseErr(err)
			}

			if next == eof {
				return script, ErrUnexpectedEOF
			} else if next == SymCmdStart {
				err := sr.skip()
				if err != nil {
					return script, parseErr(err)
				}
			}

			cmd, err := sr.parseCommand()
			if errors.Is(err, ErrInvalidCmdName) {
				// assume it's actually a generic launch cmd
				err := parseGenericLaunchCmd()
				if err != nil {
					return script, parseErr(err)
				}
			} else if err != nil {
				return script, parseErr(err)
			} else {
				script.Cmds = append(script.Cmds, cmd)
			}

			continue
		} else {
			err := sr.unread()
			if err != nil {
				return script, parseErr(err)
			}

			err = parseGenericLaunchCmd()
			if err != nil {
				return script, parseErr(err)
			}

			continue
		}
	}

	if len(script.Cmds) == 0 {
		return script, ErrEmptyZapScript
	}

	return script, nil
}
