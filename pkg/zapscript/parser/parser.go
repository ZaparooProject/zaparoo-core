package parser

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
)

var (
	ErrUnexpectedEOF  = errors.New("unexpected end of file")
	ErrInvalidCmdName = errors.New("invalid characters in command name")
	ErrEmptyCmdName   = errors.New("command name is empty")
	ErrEmptyZapScript = errors.New("script is empty")
	ErrUnmatchedQuote = errors.New("unmatched quote")
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

var (
	QuoteEscape         = []rune{SymEscapeSeq, SymArgQuote}
	GenericLaunchEscape = []rune{SymEscapeSeq, SymCmdSep, SymAdvArgStart, SymArgQuote}
	ArgsEscape          = []rune{SymEscapeSeq, SymCmdSep, SymArgSep, SymAdvArgStart, SymArgQuote}
	AdvArgsEscape       = []rune{SymEscapeSeq, SymCmdSep, SymAdvArgSep, SymAdvArgEq, SymArgQuote}
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

func (sr *ScriptReader) parseQuotedArg() (string, error) {
	arg := ""

	for {
		ch, err := sr.read()
		if err != nil {
			return arg, err
		} else if ch == eof {
			return arg, ErrUnmatchedQuote
		}

		if ch == SymEscapeSeq {
			// escaping next character
			next, err := sr.read()
			if err != nil {
				return arg, err
			} else if next == eof {
				return arg, ErrUnmatchedQuote
			}

			if slices.Contains(QuoteEscape, next) {
				// insert escaped char and continue
				arg = arg + string(next)
				continue
			} else {
				// insert literal \<char> and continue
				arg = arg + string(SymEscapeSeq) + string(next)
				continue
			}
		}

		if ch == SymArgQuote {
			break
		}

		arg = arg + string(ch)
	}

	return arg, nil
}

func (sr *ScriptReader) parseAdvArgs() (map[string]string, error) {
	advArgs := make(map[string]string)
	inValue := false
	currentArg := ""
	currentValue := ""
	valueStart := int64(-1)

	storeArg := func() {
		if currentArg != "" {
			currentValue = strings.TrimSpace(currentValue)
			advArgs[currentArg] = currentValue
		}
		currentArg = ""
		currentValue = ""
	}

	for {
		ch, err := sr.read()
		if err != nil {
			return advArgs, err
		} else if ch == eof {
			break
		}

		if inValue && ch == SymArgQuote && valueStart == sr.pos-1 {
			quotedValue, err := sr.parseQuotedArg()
			if err != nil {
				return advArgs, err
			}
			currentValue = quotedValue
			continue
		} else if ch == SymEscapeSeq {
			// escaping next character
			next, err := sr.read()
			if err != nil {
				return advArgs, err
			} else if next == eof {
				if inValue {
					currentValue = currentValue + string(SymEscapeSeq)
				} else {
					currentArg = currentArg + string(SymEscapeSeq)
				}
				break
			}

			if slices.Contains(AdvArgsEscape, next) {
				// insert escaped char and continue
				if inValue {
					currentValue = currentValue + string(next)
				} else {
					currentArg = currentArg + string(next)
				}
				continue
			} else {
				// insert literal \<char> and continue
				if inValue {
					currentValue = currentValue + string(SymEscapeSeq) + string(next)
				} else {
					currentArg = currentArg + string(SymEscapeSeq) + string(next)
				}
				continue
			}
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return advArgs, err
		} else if eoc {
			break
		}

		if ch == SymAdvArgSep {
			storeArg()
			inValue = false
			continue
		} else if ch == SymAdvArgEq && !inValue {
			valueStart = sr.pos
			inValue = true
			continue
		}

		if inValue {
			currentValue = currentValue + string(ch)
			continue
		} else {
			currentArg = currentArg + string(ch)
			continue
		}
	}

	storeArg()

	return advArgs, nil
}

func (sr *ScriptReader) parseGenericLaunchArg(prefix string) (string, map[string]string, error) {
	arg := prefix + ""
	advArgs := make(map[string]string)
	argStart := sr.pos

	for {
		ch, err := sr.read()
		if err != nil {
			return arg, advArgs, err
		} else if ch == eof {
			break
		}

		if argStart == sr.pos-1 && ch == SymArgQuote {
			quotedArg, err := sr.parseQuotedArg()
			if err != nil {
				return arg, advArgs, err
			}
			arg = quotedArg
			continue
		} else if ch == SymEscapeSeq {
			// escaping next character
			next, err := sr.read()
			if err != nil {
				return arg, advArgs, err
			} else if next == eof {
				arg = arg + string(SymEscapeSeq)
				break
			}

			if slices.Contains(GenericLaunchEscape, next) {
				// insert escaped char and continue
				arg = arg + string(next)
				continue
			} else {
				// insert literal \<char> and continue
				arg = arg + string(SymEscapeSeq) + string(next)
				continue
			}
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return arg, advArgs, err
		} else if eoc {
			break
		}

		if ch == SymAdvArgStart {
			newAdvArgs, err := sr.parseAdvArgs()
			if err != nil {
				return arg, advArgs, err
			}

			advArgs = newAdvArgs

			// advanced args are always the last part of a command
			break
		}

		arg = arg + string(ch)
	}

	return arg, advArgs, nil
}

func (sr *ScriptReader) parseArgs(onlyAdvArgs bool) ([]string, map[string]string, error) {
	args := make([]string, 0)
	advArgs := make(map[string]string)
	currentArg := ""
	argStart := sr.pos

	for {
		ch, err := sr.read()
		if err != nil {
			return args, advArgs, err
		} else if ch == eof {
			break
		}

		if argStart == sr.pos-1 && ch == SymArgQuote {
			quotedArg, err := sr.parseQuotedArg()
			if err != nil {
				return args, advArgs, err
			}
			currentArg = quotedArg
			continue
		} else if ch == SymEscapeSeq {
			// escaping next character
			next, err := sr.read()
			if err != nil {
				return args, advArgs, err
			} else if next == eof {
				currentArg = currentArg + string(SymEscapeSeq)
				break
			}

			if slices.Contains(ArgsEscape, next) {
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
			return args, advArgs, err
		} else if eoc {
			break
		}

		if ch == SymArgSep {
			// new argument
			currentArg = strings.TrimSpace(currentArg)
			args = append(args, currentArg)
			currentArg = ""
			argStart = sr.pos
			continue
		} else if ch == SymAdvArgStart {
			newAdvArgs, err := sr.parseAdvArgs()
			if err != nil {
				return args, advArgs, err
			}

			advArgs = newAdvArgs

			// advanced args are always the last part of a command
			break
		} else {
			currentArg = currentArg + string(ch)
			continue
		}
	}

	if !onlyAdvArgs {
		// if a cmd was called with ":" it will always have at least 1 blank arg
		currentArg = strings.TrimSpace(currentArg)
		args = append(args, currentArg)
	}

	return args, advArgs, nil
}

func (sr *ScriptReader) parseCommand() (Command, string, error) {
	cmd := Command{}
	var buf []rune

	for {
		ch, err := sr.read()
		if err != nil {
			return cmd, string(buf), err
		} else if ch == eof {
			break
		}

		buf = append(buf, ch)

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return cmd, string(buf), err
		} else if eoc {
			break
		}

		if isCmdName(ch) {
			cmd.Name = cmd.Name + string(ch)
		} else if ch == SymArgStart || ch == SymAdvArgStart {
			// parse arguments
			if cmd.Name == "" {
				break
			}

			onlyAdvArgs := false
			if ch == SymAdvArgStart {
				// roll it back to trigger adv arg parsing in parseArgs
				err := sr.unread()
				if err != nil {
					return cmd, string(buf), err
				}
				onlyAdvArgs = true
			}

			args, advArgs, err := sr.parseArgs(onlyAdvArgs)
			if err != nil {
				return cmd, string(buf), err
			}

			if len(args) > 0 {
				cmd.Args = args
			}

			if len(advArgs) > 0 {
				cmd.AdvArgs = advArgs
			}

			break
		} else {
			// might be a launch cmd
			return cmd, string(buf), ErrInvalidCmdName
		}
	}

	if cmd.Name == "" {
		return cmd, string(buf), ErrEmptyCmdName
	}

	cmd.Name = strings.ToLower(cmd.Name)

	return cmd, string(buf), nil
}

func (sr *ScriptReader) Parse() (Script, error) {
	script := Script{}

	parseErr := func(err error) error {
		return fmt.Errorf("parse error at %d: %w", sr.pos, err)
	}

	parseGenericLaunchCmd := func(prefix string) error {
		arg, advArgs, err := sr.parseGenericLaunchArg(prefix)
		if err != nil {
			return parseErr(err)
		}
		if arg == "" {
			return nil
		}
		cmd := Command{
			Name: models.ZapScriptCmdLaunch,
			Args: []string{arg},
		}
		if len(advArgs) > 0 {
			cmd.AdvArgs = advArgs
		}
		script.Cmds = append(script.Cmds, cmd)
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
			pre := "*"
			next, err := sr.peek()
			if err != nil {
				return script, parseErr(err)
			}

			if next == eof {
				return script, ErrUnexpectedEOF
			} else if next == SymCmdStart {
				pre = "**"
				err := sr.skip()
				if err != nil {
					return script, parseErr(err)
				}
			}

			cmd, buf, err := sr.parseCommand()
			if errors.Is(err, ErrInvalidCmdName) {
				// assume it's actually a generic launch cmd
				err := parseGenericLaunchCmd(pre + buf)
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

			err = parseGenericLaunchCmd("")
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
