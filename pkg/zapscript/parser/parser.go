package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
)

var (
	ErrUnexpectedEOF          = errors.New("unexpected end of file")
	ErrInvalidCmdName         = errors.New("invalid characters in command name")
	ErrInvalidAdvArgName      = errors.New("invalid characters in advanced arg name")
	ErrEmptyCmdName           = errors.New("command name is empty")
	ErrEmptyZapScript         = errors.New("script is empty")
	ErrUnmatchedQuote         = errors.New("unmatched quote")
	ErrInvalidJSON            = errors.New("invalid JSON argument")
	ErrUnmatchedInputMacroExt = errors.New("unmatched input macro extension")
)

const (
	SymCmdStart            = '*'
	SymCmdSep              = '|'
	SymEscapeSeq           = '^'
	SymArgStart            = ':'
	SymArgSep              = ','
	SymArgDoubleQuote      = '"'
	SymArgSingleQuote      = '\''
	SymAdvArgStart         = '?'
	SymAdvArgSep           = '&'
	SymAdvArgEq            = '='
	SymJSONStart           = '{'
	SymJSONEnd             = '}'
	SymJSONEscapeSeq       = '\\'
	SymJSONString          = '"'
	SymInputMacroEscapeSeq = '\\'
	SymInputMacroExtStart  = '{'
	SymInputMacroExtEnd    = '}'
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

func isAdvArgName(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isWhitespace(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isInputMacroCmd(name string) bool {
	if name == models.ZapScriptCmdInputKeyboard {
		return true
	} else if name == models.ZapScriptCmdInputGamepad {
		return true
	} else {
		return false
	}
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
		return true, nil
	} else {
		return false, nil
	}
}

func (sr *ScriptReader) parseQuotedArg(start rune) (string, error) {
	arg := ""

	for {
		ch, err := sr.read()
		if err != nil {
			return arg, err
		} else if ch == eof {
			return arg, ErrUnmatchedQuote
		}

		if ch == start {
			break
		}

		arg = arg + string(ch)
	}

	return arg, nil
}

func (sr *ScriptReader) parseJSONArg() (string, error) {
	jsonStr := string(SymJSONStart)
	braceCount := 1
	inString := false
	escaped := false

	for braceCount > 0 {
		ch, err := sr.read()
		if err != nil {
			return "", err
		} else if ch == eof {
			return "", ErrInvalidJSON
		}

		jsonStr += string(ch)

		if escaped {
			escaped = false
			continue
		}

		if ch == SymJSONEscapeSeq {
			escaped = true
			continue
		}

		if ch == SymJSONString {
			inString = !inString
			continue
		}

		if !inString {
			if ch == SymJSONStart {
				braceCount++
			} else if ch == SymJSONEnd {
				braceCount--
			}
		}
	}

	// validate json
	var jsonObj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonObj); err != nil {
		return "", ErrInvalidJSON
	}

	// convert back to string
	normalizedJSON, err := json.Marshal(jsonObj)
	if err != nil {
		return "", ErrInvalidJSON
	}

	return string(normalizedJSON), nil
}

func (sr *ScriptReader) parseInputMacroArg() ([]string, map[string]string, error) {
	args := make([]string, 0)
	advArgs := make(map[string]string)

	for {
		ch, err := sr.read()
		if err != nil {
			return args, advArgs, err
		} else if ch == eof {
			break
		}

		if ch == SymInputMacroEscapeSeq {
			next, err := sr.read()
			if err != nil {
				return args, advArgs, err
			} else if next == eof {
				args = append(args, string(SymEscapeSeq))
			}

			args = append(args, string(next))
			continue
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return args, advArgs, err
		} else if eoc {
			break
		}

		if ch == SymInputMacroExtStart {
			extName := string(ch)
			for {
				next, err := sr.read()
				if err != nil {
					return args, advArgs, err
				} else if next == eof {
					return args, advArgs, ErrUnmatchedInputMacroExt
				}

				extName = extName + string(next)

				if next == SymInputMacroExtEnd {
					break
				}
			}
			args = append(args, extName)
			continue
		} else if ch == SymAdvArgStart {
			newAdvArgs, buf, err := sr.parseAdvArgs()
			if errors.Is(err, ErrInvalidAdvArgName) {
				// if an adv arg name is invalid, fallback on treating it
				// as a list of input args
				for _, ch := range string(SymAdvArgStart) + buf {
					args = append(args, string(ch))
				}
				continue
			} else if err != nil {
				return args, advArgs, err
			}

			advArgs = newAdvArgs

			// advanced args are always the last part of a command
			break
		}

		args = append(args, string(ch))
	}

	return args, advArgs, nil
}

func (sr *ScriptReader) parseAdvArgs() (map[string]string, string, error) {
	advArgs := make(map[string]string)
	inValue := false
	currentArg := ""
	currentValue := ""
	valueStart := int64(-1)
	var buf []rune

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
			return advArgs, string(buf), err
		} else if ch == eof {
			break
		}

		buf = append(buf, ch)

		if inValue {
			if valueStart == sr.pos-1 &&
				(ch == SymArgDoubleQuote || ch == SymArgSingleQuote) {
				quotedValue, err := sr.parseQuotedArg(ch)
				if err != nil {
					return advArgs, string(buf), err
				}
				currentValue = quotedValue
				continue
			} else if ch == SymJSONStart && valueStart == sr.pos-1 {
				jsonValue, err := sr.parseJSONArg()
				if err != nil {
					return advArgs, string(buf), err
				}
				currentValue = jsonValue
				continue
			} else if ch == SymEscapeSeq {
				// escaping next character
				next, err := sr.read()
				if err != nil {
					return advArgs, string(buf), err
				} else if next == eof {
					currentValue = currentValue + string(SymEscapeSeq)
				}

				currentValue = currentValue + string(next)
				continue
			}
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return advArgs, string(buf), err
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
			if !isAdvArgName(ch) {
				return advArgs, string(buf), ErrInvalidAdvArgName
			}
			currentArg = currentArg + string(ch)
			continue
		}
	}

	storeArg()

	return advArgs, string(buf), nil
}

func (sr *ScriptReader) parseArgs(
	prefix string,
	onlyAdvArgs bool,
	onlyOneArg bool,
) ([]string, map[string]string, error) {
	args := make([]string, 0)
	advArgs := make(map[string]string)
	currentArg := prefix
	argStart := sr.pos

	for {
		ch, err := sr.read()
		if err != nil {
			return args, advArgs, err
		} else if ch == eof {
			break
		}

		if argStart == sr.pos-1 &&
			(ch == SymArgDoubleQuote || ch == SymArgSingleQuote) {
			quotedArg, err := sr.parseQuotedArg(ch)
			if err != nil {
				return args, advArgs, err
			}
			currentArg = quotedArg
			continue
		} else if argStart == sr.pos-1 && ch == SymJSONStart {
			jsonArg, err := sr.parseJSONArg()
			if err != nil {
				return args, advArgs, err
			}
			currentArg = jsonArg
			continue
		} else if ch == SymEscapeSeq {
			// escaping next character
			next, err := sr.read()
			if err != nil {
				return args, advArgs, err
			} else if next == eof {
				currentArg = currentArg + string(SymEscapeSeq)
			}

			currentArg = currentArg + string(next)
			continue
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return args, advArgs, err
		} else if eoc {
			break
		}

		if !onlyOneArg && ch == SymArgSep {
			// new argument
			currentArg = strings.TrimSpace(currentArg)
			args = append(args, currentArg)
			currentArg = ""
			argStart = sr.pos
			continue
		} else if ch == SymAdvArgStart {
			newAdvArgs, buf, err := sr.parseAdvArgs()
			if errors.Is(err, ErrInvalidAdvArgName) {
				// if an adv arg name is invalid, fallback on treating it
				// as a positional arg with a ? in it
				currentArg = currentArg + string(SymAdvArgStart) + buf
				continue
			} else if err != nil {
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

func (sr *ScriptReader) parseCommand(onlyOneArg bool) (Command, string, error) {
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

			var args []string
			var advArgs map[string]string
			var err error

			if isInputMacroCmd(cmd.Name) {
				args, advArgs, err = sr.parseInputMacroArg()
				if err != nil {
					return cmd, string(buf), err
				}
			} else {
				args, advArgs, err = sr.parseArgs("", onlyAdvArgs, onlyOneArg)
				if err != nil {
					return cmd, string(buf), err
				}
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

	parseAutoLaunchCmd := func(prefix string) error {
		args, advArgs, err := sr.parseArgs(prefix, false, true)
		if err != nil {
			return parseErr(err)
		}
		cmd := Command{
			Name: models.ZapScriptCmdLaunch,
			Args: args,
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
		} else if sr.pos == 1 && ch == SymJSONStart {
			// reserve starting { as json script for later
			return Script{}, ErrInvalidJSON
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
			} else {
				// assume it's actually an auto launch cmd
				err := parseAutoLaunchCmd("*")
				if err != nil {
					return script, parseErr(err)
				}
				continue
			}

			cmd, buf, err := sr.parseCommand(false)
			if errors.Is(err, ErrInvalidCmdName) {
				// assume it's actually an auto launch cmd
				err := parseAutoLaunchCmd("**" + buf)
				if err != nil {
					return script, parseErr(err)
				}
				continue
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

			err = parseAutoLaunchCmd("")
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
